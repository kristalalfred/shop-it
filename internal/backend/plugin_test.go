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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/browser"
	"github.com/kristalalfred/ship-it/internal/shopping"
	"github.com/kristalalfred/ship-it/internal/transfers"
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
	config := Config{Listen: "127.0.0.1:18081", APIToken: "user-secret", AdapterTimeoutMS: 1000, DataDir: dir, Clients: []ClientConfig{{ID: "browser", Token: "client-secret"}, {ID: "other", Token: "other-secret"}}, Retailers: []RetailerConfig{{ID: "willys", Name: "Willys", Mode: "browser", BasketURL: BasketURLs["willys"]}}}
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

func TestPluginOnboardingSharedPlanAndVerifiedTransfer(t *testing.T) {
	server, config := pluginTestServer(t, t.TempDir())
	client := pluginClient(t, server.URL)
	tools, err := client.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 9 {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	resource, err := client.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: pluginURI})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Contents[0].MIMEType != pluginMIME || strings.Contains(resource.Contents[0].Text, config.APIToken) || strings.Contains(resource.Contents[0].Text, "client-secret") {
		t.Fatal("UI resource has wrong MIME type or leaked credentials")
	}
	fresh := callTestTool(t, client, "open_ship_it", map[string]any{})
	if fresh.BrowserOnline || fresh.Plan != nil || len(fresh.Preferences.RetailerIDs) != 0 {
		t.Fatalf("fresh setup: %+v", fresh)
	}
	callTestTool(t, client, "configure_ship_it", map[string]any{"retailer_ids": []string{"willys"}, "draft": "Milk for breakfast"})
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
					reply.Result, _ = json.Marshal(browser.Session{Status: "ready", Label: "Willys Stockholm", Context: shopping.StoreContext{StoreID: "store-1", PostalCode: "11122", Fulfillment: "delivery"}})
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
	if plan == nil || plan.InterpreterMode != "chatgpt" || searchCount.Load() < 1 || plan.Lines[0].Baskets["willys"].Selected != nil {
		t.Fatalf("structured plan: %+v", plan)
	}
	blocked, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "fill_basket", Arguments: map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "request_id": "first"}})
	if err != nil || !blocked.IsError {
		t.Fatalf("unresolved transfer allowed: %+v %v", blocked, err)
	}
	line := plan.Lines[0]
	invalid, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "select_products", Arguments: map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "selections": []productSelection{{LineID: line.ID, RetailerID: "willys", ProductID: "101476218_ST"}, {LineID: line.ID, RetailerID: "invented-store", ProductID: "invented-product"}}}})
	if err != nil || !invalid.IsError {
		t.Fatal("invalid batch was accepted")
	}
	unchanged := callTestTool(t, client, "open_ship_it", map[string]any{})
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
	status, raw := browserRequest(t, t.Context(), server.URL, "/v1/shopping-plans/"+plan.ID+"/lines/"+line.ID, "PATCH", "user-secret", map[string]any{"revision": chosen.Plan.Revision, "product_id": "101205823_ST"})
	if status != 200 {
		t.Fatalf("shared web edit: %d %s", status, raw)
	}
	current := callTestTool(t, client, "open_ship_it", map[string]any{})
	if current.Plan.Lines[0].Baskets["willys"].Selected.Product.ID != "101205823_ST" {
		t.Fatal("MCP did not observe the web edit")
	}
	refreshed := callTestTool(t, client, "refresh_plan", map[string]any{"plan_id": plan.ID, "revision": current.Plan.Revision})
	if refreshed.Plan.Lines[0].Baskets["willys"].Selected.Product.ID != "101205823_ST" {
		t.Fatal("refresh lost the selection")
	}
	args := map[string]any{"plan_id": plan.ID, "revision": refreshed.Plan.Revision, "request_id": "verified-fill"}
	fill := callTestTool(t, client, "fill_basket", args)
	replay := callTestTool(t, client, "fill_basket", args)
	if fill.Transfer == nil || replay.Transfer.ID != fill.Transfer.ID || fill.Transfer.Context.StoreID != "store-1" {
		t.Fatal("transfer retry or store context failed")
	}
	status, raw = browserRequest(t, t.Context(), server.URL, "/v1/client/claims", "POST", "client-secret", nil)
	if status != 200 {
		t.Fatalf("transfer claim: %d %s", status, raw)
	}
	var transfer shopping.Transfer
	json.Unmarshal(raw, &transfer)
	receipt := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: transfer.Items[0].ProductID, Status: "applied", Quantity: transfer.Items[0].Quantity}}}
	status, _ = browserRequest(t, t.Context(), server.URL, "/v1/client/transfers/"+transfer.ID+"/result", "PUT", "client-secret", receipt)
	if status != 200 {
		t.Fatal(status)
	}
	completed := callTestTool(t, client, "open_ship_it", map[string]any{})
	if completed.Transfer.State != "completed" {
		t.Fatal("receipt not reflected in plugin")
	}
	server.Close()
	cancel()
	<-done
	restarted, _ := pluginTestServer(t, config.DataDir)
	resumed := callTestTool(t, pluginClient(t, restarted.URL), "open_ship_it", map[string]any{})
	if resumed.Preferences.Draft != "Milk for breakfast" || resumed.Plan.ID != plan.ID || resumed.BrowserOnline {
		t.Fatal("setup did not resume safely after restart")
	}
}

func TestPluginRejectsMalformedToolsAndUnauthenticatedRequests(t *testing.T) {
	server, _ := pluginTestServer(t, t.TempDir())
	for _, path := range []string{"/mcp", "/v1/plugin/tools/open_ship_it", "/v1/client/commands"} {
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
		{"configure_ship_it", map[string]any{"retailer_ids": []string{"willys", "willys"}}},
		{"configure_ship_it", map[string]any{"retailer_ids": []string{"unknown"}}},
		{"configure_ship_it", map[string]any{"api_token": "leak"}},
		{"create_shopping_plan", map[string]any{"text": "dinner", "items": []any{}}},
		{"connect_store", map[string]any{}},
	} {
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: test.name, Arguments: test.args})
		if err == nil && !result.IsError {
			t.Fatalf("accepted malformed %s: %+v", test.name, test.args)
		}
	}
}
