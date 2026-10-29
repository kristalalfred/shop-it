package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/browser"
	"github.com/kristalalfred/shop-it/internal/shopping"
	"github.com/kristalalfred/shop-it/internal/transfers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type testAuth struct{}

func (testAuth) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer user-secret")
	return http.DefaultTransport.RoundTrip(r)
}

func pluginTestServer(t *testing.T, dir string) (*httptest.Server, Config) {
	t.Helper()
	config := Config{Listen: "127.0.0.1:24816", APIToken: "user-secret", AdapterTimeoutMS: 1000, DataDir: dir, Clients: []ClientConfig{{ID: "browser", Token: "client-secret"}, {ID: "other", Token: "other-secret"}}, Retailers: []RetailerConfig{{ID: "willys", Name: "Willys", Mode: "browser", BasketURL: BasketURLs["willys"]}}}
	store, err := transfers.Open(filepath.Join(dir, "transfers.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(config, store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, config
}

func browserRequest(t *testing.T, ctx context.Context, url, path, method, token string, body any) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(body)
	request, _ := http.NewRequestWithContext(ctx, method, url+path, bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil
		}
		t.Error(err)
		return 0, nil
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}

func pluginClient(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "plugin-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: url + "/mcp", HTTPClient: &http.Client{Transport: testAuth{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callTestTool(t *testing.T, c *mcp.ClientSession, name string, args any) pluginSnapshot {
	t.Helper()
	result, err := c.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("%s: %+v", name, result.Content)
	}
	var snapshot pluginSnapshot
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func fillRequest(t *testing.T, url, planID string, revision int, key string) (int, shopping.Transfer, string) {
	t.Helper()
	body, _ := json.Marshal(fillOptions{Revision: revision})
	request, _ := http.NewRequestWithContext(t.Context(), "POST", url+"/v1/shopping-plans/"+planID+"/transfer", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer user-secret")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var transfer shopping.Transfer
	_ = json.Unmarshal(raw, &transfer)
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &failure)
	return response.StatusCode, transfer, failure.Error
}

func TestChatGPTMakesThePlanAndThePageFillsIt(t *testing.T) {
	server, config := pluginTestServer(t, t.TempDir())
	client := pluginClient(t, server.URL)
	tools, err := client.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 8 || slices.ContainsFunc(tools.Tools, func(tool *mcp.Tool) bool { return tool.Name == "fill_basket" }) {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	resource, err := client.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: pluginURI})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Contents[0].MIMEType != pluginMIME || strings.Contains(resource.Contents[0].Text, config.APIToken) || strings.Contains(resource.Contents[0].Text, "client-secret") {
		t.Fatal("UI resource has wrong MIME type or leaked credentials")
	}
	fresh := callTestTool(t, client, "open_shop_it", map[string]any{})
	if fresh.BrowserOnline || fresh.Plan != nil || len(fresh.Preferences.RetailerIDs) != 0 {
		t.Fatalf("fresh setup: %+v", fresh)
	}
	callTestTool(t, client, "configure_shop_it", map[string]any{"retailer_ids": []string{"willys"}, "draft": "Milk for breakfast"})
	status, _ := browserRequest(t, t.Context(), server.URL, "/v1/client/commands", "POST", "client-secret", nil)
	if status != 200 {
		t.Fatal(status)
	}
	data, err := os.ReadFile("../retailer/testdata/storefronts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	json.Unmarshal(data, &fixtures)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var searchCount atomic.Int32
	var storeContext atomic.Value
	storeContext.Store(shopping.StoreContext{StoreID: "store-1", PostalCode: "11122", Fulfillment: "delivery"})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			status, raw := browserRequest(t, ctx, server.URL, "/v1/client/commands", "POST", "client-secret", nil)
			if status == 0 {
				return
			}
			if status != 200 {
				t.Errorf("claim %d", status)
				return
			}
			var batch struct {
				Commands []browser.Command `json:"commands"`
			}
			json.Unmarshal(raw, &batch)
			for _, command := range batch.Commands {
				var reply browser.Reply
				switch command.Operation {
				case "session":
					reply.Result, _ = json.Marshal(browser.Session{Status: "ready", Label: "Willys Stockholm", Context: storeContext.Load().(shopping.StoreContext)})
				case "search":
					searchCount.Add(1)
					reply.Result = fixtures["willys"]
				default:
					t.Errorf("unexpected command %q", command.Operation)
				}
				forbidden, _ := browserRequest(t, ctx, server.URL, "/v1/client/commands/"+command.ID+"/result", "PUT", "other-secret", reply)
				if forbidden != 404 {
					t.Errorf("foreign command completion: %d", forbidden)
				}
				status, _ := browserRequest(t, ctx, server.URL, "/v1/client/commands/"+command.ID+"/result", "PUT", "client-secret", reply)
				if status != 200 {
					t.Errorf("completion: %d", status)
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	connected := callTestTool(t, client, "connect_store", map[string]any{"retailer_id": "willys"})
	if !connected.BrowserOnline || connected.Stores[0].Session.Status != "ready" || connected.Preferences.Draft != "Milk for breakfast" {
		t.Fatalf("connection: %+v", connected)
	}
	created := callTestTool(t, client, "create_shopping_plan", map[string]any{"text": "Milk for breakfast", "items": []shopping.Item{{Label: "Milk", Query: "mjölk", Amount: shopping.Pack{Quantity: 2000, Unit: "ml"}, RequiredTraits: []string{}}}})
	plan := created.Plan
	if plan == nil || searchCount.Load() < 1 || plan.Lines[0].Baskets["willys"].Selected != nil || created.ReviewURL != "http://127.0.0.1:24816/?plan="+plan.ID || len(created.SavedPlans) != 1 || created.SavedPlans[0].ID != plan.ID {
		t.Fatalf("structured plan: %+v", created)
	}
	if status, _, _ := fillRequest(t, server.URL, plan.ID, plan.Revision, "first"); status != http.StatusConflict {
		t.Fatalf("a basket with nothing chosen was filled: %d", status)
	}
	line := plan.Lines[0]
	invalid, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "select_products", Arguments: map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "selections": []productSelection{{LineID: line.ID, RetailerID: "willys", ProductID: "101476218_ST"}, {LineID: line.ID, RetailerID: "invented-store", ProductID: "invented-product"}}}})
	if err != nil || !invalid.IsError {
		t.Fatal("invalid batch was accepted")
	}
	unchanged := callTestTool(t, client, "open_shop_it", map[string]any{})
	if unchanged.Plan.Revision != plan.Revision || unchanged.Plan.Lines[0].Baskets["willys"].Selected != nil {
		t.Fatal("invalid batch partially changed the plan")
	}
	chosen := callTestTool(t, client, "select_products", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "selections": []productSelection{{LineID: line.ID, RetailerID: "willys", ProductID: "101476218_ST"}}})
	if chosen.Plan.Lines[0].Baskets["willys"].Selected.Quantity != 2 {
		t.Fatal("wrong pack arithmetic")
	}
	stale, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "select_store", Arguments: map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "retailer_id": "willys"}})
	if err != nil || !stale.IsError {
		t.Fatal("stale revision accepted")
	}
	edited := callTestTool(t, client, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": chosen.Plan.Revision, "line_id": line.ID, "product_id": "101205823_ST"})
	status, raw := browserRequest(t, t.Context(), server.URL, "/v1/shopping-plans/"+plan.ID, "GET", "user-secret", nil)
	var viewed shopping.Plan
	_ = json.Unmarshal(raw, &viewed)
	if status != 200 || viewed.Revision != edited.Plan.Revision || viewed.Lines[0].Baskets["willys"].Selected.Product.ID != "101205823_ST" {
		t.Fatalf("the page did not see the ChatGPT edit: %d %s", status, raw)
	}
	refreshed := callTestTool(t, client, "refresh_plan", map[string]any{"plan_id": plan.ID, "revision": edited.Plan.Revision})
	if refreshed.Plan.Lines[0].Baskets["willys"].Selected.Product.ID != "101205823_ST" {
		t.Fatal("refresh lost the selection")
	}
	status, fill, _ := fillRequest(t, server.URL, plan.ID, refreshed.Plan.Revision, "verified-fill")
	replayStatus, replay, _ := fillRequest(t, server.URL, plan.ID, refreshed.Plan.Revision, "verified-fill")
	if status != http.StatusAccepted || replayStatus != http.StatusOK || replay.ID != fill.ID || fill.Context.StoreID != "store-1" {
		t.Fatalf("transfer retry or store context failed: %d %d %+v", status, replayStatus, fill)
	}
	status, raw = browserRequest(t, t.Context(), server.URL, "/v1/client/claims", "POST", "client-secret", nil)
	if status != 200 {
		t.Fatalf("transfer claim: %d %s", status, raw)
	}
	var transfer shopping.Transfer
	json.Unmarshal(raw, &transfer)
	receipt := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: transfer.Items[0].ProductID, Status: "applied", Quantity: transfer.Items[0].Quantity}}}
	if status, _ = browserRequest(t, t.Context(), server.URL, "/v1/client/transfers/"+transfer.ID+"/result", "PUT", "client-secret", receipt); status != 200 {
		t.Fatal(status)
	}
	status, raw = browserRequest(t, t.Context(), server.URL, "/v1/basket-transfers/"+fill.ID, "GET", "user-secret", nil)
	if status != 200 || !strings.Contains(string(raw), `"state":"completed"`) {
		t.Fatalf("the page cannot see the finished fill: %d %s", status, raw)
	}
	nextContext := shopping.StoreContext{StoreID: "store-2", PostalCode: "99999", Fulfillment: "delivery", DeliverySlotID: "new-slot"}
	storeContext.Store(nextContext)
	if status, _, message := fillRequest(t, server.URL, plan.ID, refreshed.Plan.Revision, "fill-again"); status != http.StatusConflict || !strings.Contains(message, "Refresh prices") {
		t.Fatalf("changed store was not detected before filling: %d %q", status, message)
	}
	status, raw = browserRequest(t, t.Context(), server.URL, "/v1/shopping-plans/"+plan.ID+"/refresh", "POST", "user-secret", map[string]any{"revision": refreshed.Plan.Revision})
	var repriced shopping.Plan
	_ = json.Unmarshal(raw, &repriced)
	if status != 200 || repriced.ID != plan.ID || !sameStore(repriced.Baskets[0].Context, nextContext) || repriced.Lines[0].Baskets["willys"].Selected.Product.ID != "101205823_ST" {
		t.Fatalf("the page could not refresh the plan for the current store: %d %s", status, raw)
	}
	status, again, _ := fillRequest(t, server.URL, plan.ID, repriced.Revision, "fill-again")
	_, againReplay, _ := fillRequest(t, server.URL, plan.ID, repriced.Revision, "fill-again")
	if status != http.StatusAccepted || again.ID == fill.ID || againReplay.ID != again.ID || !sameStore(again.Context, nextContext) {
		t.Fatal("fill again did not create an idempotent transfer for the refreshed store")
	}
	server.Close()
	cancel()
	<-done
	restarted, _ := pluginTestServer(t, config.DataDir)
	resumed := callTestTool(t, pluginClient(t, restarted.URL), "open_shop_it", map[string]any{})
	if resumed.Preferences.Draft != "Milk for breakfast" || resumed.Plan.ID != plan.ID || resumed.BrowserOnline {
		t.Fatal("setup did not resume safely after restart")
	}
}

func TestPluginRejectsMalformedToolsAndUnauthenticatedRequests(t *testing.T) {
	server, _ := pluginTestServer(t, t.TempDir())
	for _, path := range []string{"/mcp", "/v1/shopping-plans/plan-x/transfer", "/v1/client/commands"} {
		response, err := http.Post(server.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("%s: %d", path, response.StatusCode)
		}
	}
	client := pluginClient(t, server.URL)
	for _, test := range []struct {
		name string
		args any
	}{
		{"configure_shop_it", map[string]any{"retailer_ids": []string{"willys", "willys"}}},
		{"configure_shop_it", map[string]any{"retailer_ids": []string{"unknown"}}},
		{"configure_shop_it", map[string]any{"api_token": "leak"}},
		{"create_shopping_plan", map[string]any{"text": "dinner", "items": []any{}}},
		{"connect_store", map[string]any{}},
	} {
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: test.name, Arguments: test.args})
		if err == nil && !result.IsError {
			t.Fatalf("accepted malformed %s: %+v", test.name, test.args)
		}
	}
}

func TestNewTransferWakesTheWaitingBrowser(t *testing.T) {
	server, _ := pluginTestServer(t, t.TempDir())
	if status, _ := browserRequest(t, t.Context(), server.URL, "/v1/client/commands?wait=26", "POST", "client-secret", nil); status != http.StatusBadRequest {
		t.Fatalf("out of range wait: %d", status)
	}
	type poll struct {
		status  int
		raw     []byte
		elapsed time.Duration
	}
	polled := make(chan poll, 1)
	go func() {
		started := time.Now()
		status, raw := browserRequest(t, t.Context(), server.URL, "/v1/client/commands?wait=20", "POST", "client-secret", nil)
		polled <- poll{status, raw, time.Since(started)}
	}()
	client := pluginClient(t, server.URL)
	for !callTestTool(t, client, "open_shop_it", map[string]any{}).BrowserOnline {
	}
	body, _ := json.Marshal(map[string]any{"retailer_id": "willys", "client_id": "browser", "context": map[string]any{"store_id": "willys", "postal_code": "11122"}, "items": []map[string]any{{"product_id": "101205823_ST", "target_quantity": 1}}})
	request, _ := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/v1/basket-transfers", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer user-secret")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "wake-test")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("create transfer: %d", response.StatusCode)
	}
	select {
	case result := <-polled:
		if result.status != http.StatusOK || string(result.raw) != `{"commands":[]}`+"\n" || result.elapsed > 5*time.Second {
			t.Fatalf("poll returned %d %s after %v", result.status, result.raw, result.elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waiting browser was not woken by a new transfer")
	}
	if status, _ := browserRequest(t, t.Context(), server.URL, "/v1/client/claims", "POST", "client-secret", nil); status != http.StatusOK {
		t.Fatalf("claim after wake: %d", status)
	}
}
