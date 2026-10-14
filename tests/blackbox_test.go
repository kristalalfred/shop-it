package tests_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/mockio"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

const userToken = "local-demo-api-token"
const clientToken = "local-demo-client-token"
const otherToken = "other-client-token"

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ship-it-blackbox-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "ship-it")
	cmd := exec.Command("go", "build", "-race", "-o", binary, "./cmd/ship-it")
	cmd.Dir = ".."
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	code := 1
	if err := cmd.Run(); err == nil {
		code = m.Run()
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type system struct {
	url    string
	calls  chan shopping.SearchRequest
	client *http.Client
}

func demoScenario(t *testing.T) mockio.Scenario {
	t.Helper()
	scenario, err := mockio.LoadScenario("../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	return scenario
}

func startSystem(t *testing.T, scenario mockio.Scenario) system {
	t.Helper()
	calls := make(chan shopping.SearchRequest, 100)
	handler := mockio.RetailerHandler(scenario)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var req shopping.SearchRequest
		if json.Unmarshal(body, &req) == nil {
			calls <- req
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	data, err := os.ReadFile("../config/dev.json")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["listen"], config["adapter_timeout_ms"] = "127.0.0.1:0", 200
	for _, retailer := range config["retailers"].([]any) {
		r := retailer.(map[string]any)
		r["gateway_url"] = gateway.URL + "/retailers/" + r["id"].(string)
	}
	config["clients"] = append(config["clients"].([]any), map[string]any{"id": "other-browser", "token": otherToken})
	data, _ = json.Marshal(config)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-config", path)
	cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
	logs := &lockedBuffer{}
	cmd.Stderr = logs
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = logs.Write([]byte(line + "\n"))
			if strings.HasPrefix(line, "listening on ") {
				ready <- strings.TrimPrefix(line, "listening on ")
			}
		}
	}()
	done := make(chan struct{})
	var processErr error
	go func() { processErr = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
			if processErr != nil {
				t.Errorf("backend exited: %v\n%s", processErr, logs.String())
			}
		case <-time.After(6 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Errorf("backend did not shut down\n%s", logs.String())
		}
	})
	select {
	case url := <-ready:
		return system{url: url, calls: calls, client: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}}
	case <-done:
		t.Fatalf("backend did not start: %v\n%s", processErr, logs.String())
	case <-time.After(10 * time.Second):
		t.Fatal("backend startup timed out")
	}
	return system{}
}

type response struct {
	status int
	data   map[string]any
	header http.Header
}

func (s system) request(t *testing.T, method, path string, input any, token, key string) response {
	t.Helper()
	var payload []byte
	if raw, ok := input.(string); ok {
		payload = []byte(raw)
	} else if input != nil {
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, s.url+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{}
	if len(body) > 0 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("invalid JSON response: %s", body)
		}
	}
	return response{res.StatusCode, data, res.Header}
}

func expectStatus(t *testing.T, res response, status int) {
	t.Helper()
	if res.status != status {
		t.Fatalf("HTTP %d, want %d: %v", res.status, status, res.data)
	}
}

func storeContext(retailer string) shopping.StoreContext {
	return shopping.StoreContext{StoreID: retailer + "-demo", PostalCode: "11122", Fulfillment: "delivery", DeliverySlotID: "demo-slot", Memberships: []string{retailer + "-demo-member"}}
}

func searchRequest(retailer string) shopping.SearchRequest {
	return shopping.SearchRequest{RetailerID: retailer, Context: storeContext(retailer), Query: "mjölk"}
}

func transferRequest() shopping.TransferRequest {
	return shopping.TransferRequest{
		RetailerID: "ica", ClientID: "demo-browser", Context: storeContext("ica"),
		Items: []shopping.BasketItem{{ProductID: "milk-basic", Quantity: 2}, {ProductID: "milk-lactose-free", Quantity: 1}},
	}
}

func TestCatalogAcrossRetailers(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	expectStatus(t, s.request(t, "GET", "/healthz", nil, "", ""), 200)
	listed := s.request(t, "GET", "/v1/retailers", nil, userToken, "")
	expectStatus(t, listed, 200)
	retailers := listed.data["retailers"].([]any)
	if len(retailers) != 6 {
		t.Fatalf("retailers: %v", retailers)
	}
	for i, id := range []string{"ica", "mathem", "willys", "coop", "hemkop", "citygross"} {
		t.Run(id, func(t *testing.T) {
			if retailers[i].(map[string]any)["mode"] != "mock" {
				t.Fatal("fixture advertised as a real integration")
			}
			req := searchRequest(id)
			res := s.request(t, "POST", "/v1/catalog/search", req, userToken, "")
			expectStatus(t, res, 200)
			products := res.data["products"].([]any)
			price := products[0].(map[string]any)["price"].(map[string]any)
			if res.data["retailer_id"] != id || price["amount_minor"] != float64(1290+i*50) || price["currency"] != "SEK" {
				t.Fatalf("wrong retailer or price: %v", res.data)
			}
			call := <-s.calls
			if !reflect.DeepEqual(call, req) {
				t.Fatalf("adapter request changed: %v != %v", call, req)
			}
		})
	}
}

func TestCatalogPaginationAndEvidence(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	req := searchRequest("ica")
	first := s.request(t, "POST", "/v1/catalog/search", req, userToken, "")
	expectStatus(t, first, 200)
	if first.data["observed_at"] != "2026-10-14T12:00:00Z" {
		t.Fatal(first.data)
	}
	products := first.data["products"].([]any)
	organic := products[1].(map[string]any)
	offer := organic["offers"].([]any)[0].(map[string]any)
	if offer["min_quantity"] != float64(2) || offer["membership"] != "ica-demo-member" || organic["traits"].(map[string]any)["gluten_free"] != "unknown" {
		t.Fatal("conditional offer or unknown evidence lost", organic)
	}
	req.Cursor = first.data["next_cursor"].(string)
	second := s.request(t, "POST", "/v1/catalog/search", req, userToken, "")
	expectStatus(t, second, 200)
	products = second.data["products"].([]any)
	if len(products) != 2 || products[0].(map[string]any)["availability"] != "out_of_stock" || products[1].(map[string]any)["availability"] != "unknown" || second.data["next_cursor"] != nil {
		t.Fatal("pagination or stock evidence changed", second.data)
	}
	req.Query, req.Cursor = "nonexistent food", ""
	empty := s.request(t, "POST", "/v1/catalog/search", req, userToken, "")
	expectStatus(t, empty, 200)
	if len(empty.data["products"].([]any)) != 0 {
		t.Fatal(empty.data)
	}
	req.Query = "bananer"
	weighted := s.request(t, "POST", "/v1/catalog/search", req, userToken, "")
	expectStatus(t, weighted, 200)
	if weighted.data["products"].([]any)[0].(map[string]any)["price_kind"] != "estimated" {
		t.Fatal(weighted.data)
	}
	req.Context.Memberships = []string{}
	req.Context.Fulfillment = "pickup"
	req.Context.DeliverySlotID = ""
	expectStatus(t, s.request(t, "POST", "/v1/catalog/search", req, userToken, ""), 200)
}

func TestAdapterFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fault  mockio.Fault
		status int
	}{
		{"unavailable", mockio.Fault{Status: 503, Body: "private upstream error"}, 502},
		{"malformed", mockio.Fault{Body: "{broken"}, 502},
		{"timeout", mockio.Fault{DelayMS: 500}, 504},
		{"body_timeout", mockio.Fault{BodyDelayMS: 500, Body: "{}"}, 504},
		{"redirect", mockio.Fault{Status: 302}, 502},
		{"oversized", mockio.Fault{Body: strings.Repeat("x", (1<<20)+1)}, 502},
		{"wrong_context", mockio.Fault{Body: `{"retailer_id":"ica","context":{},"observed_at":"2026-10-14T12:00:00Z","products":[]}`}, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := demoScenario(t)
			fixture := scenario.Retailers["ica"]
			fixture.Fault = tc.fault
			scenario.Retailers["ica"] = fixture
			s := startSystem(t, scenario)
			res := s.request(t, "POST", "/v1/catalog/search", searchRequest("ica"), userToken, "")
			expectStatus(t, res, tc.status)
			if strings.Contains(fmt.Sprint(res.data), "private upstream error") {
				t.Fatal("upstream details leaked")
			}
		})
	}
}

func TestInvalidRequestsDoNotReachAdapters(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	for _, tc := range []struct {
		name   string
		body   any
		status int
	}{
		{"bad_json", "{", 400},
		{"unknown_field", `{"retailer_id":"ica","query":"milk","unexpected":true}`, 400},
		{"trailing_json", `{} {}`, 400},
		{"null", `null`, 400},
		{"empty_query", shopping.SearchRequest{RetailerID: "ica", Context: storeContext("ica")}, 400},
		{"oversized", `{"query":"` + strings.Repeat("x", (1<<20)+1) + `"}`, 413},
		{"unknown_retailer", searchRequest("unknown"), 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectStatus(t, s.request(t, "POST", "/v1/catalog/search", tc.body, userToken, ""), tc.status)
		})
	}
	for _, ctx := range []shopping.StoreContext{
		{StoreID: "store", PostalCode: "111 22", Fulfillment: "delivery"},
		{StoreID: "store", PostalCode: "abcde", Fulfillment: "delivery"},
		{PostalCode: "11122", Fulfillment: "delivery"},
		{StoreID: "store", PostalCode: "11122", Fulfillment: "teleport"},
	} {
		req := searchRequest("ica")
		req.Context = ctx
		expectStatus(t, s.request(t, "POST", "/v1/catalog/search", req, userToken, ""), 400)
	}
	expectStatus(t, s.request(t, "POST", "/v1/catalog/search", searchRequest("ica"), "wrong", ""), 401)
	expectStatus(t, s.request(t, "POST", "/v1/catalog/search", searchRequest("ica"), clientToken, ""), 401)
	expectStatus(t, s.request(t, "GET", "/v1/catalog/search", nil, userToken, ""), 405)
	if len(s.calls) != 0 {
		t.Fatal("invalid request reached an adapter")
	}
}

func TestBasketTransferRoundTrip(t *testing.T) {
	scenario := demoScenario(t)
	s := startSystem(t, scenario)
	req := transferRequest()
	created := s.request(t, "POST", "/v1/basket-transfers", req, userToken, "basket-1")
	expectStatus(t, created, 202)
	id := created.data["id"].(string)
	if created.data["state"] != "pending" || created.header.Get("Location") != "/v1/basket-transfers/"+id {
		t.Fatal(created)
	}
	if created.data["basket_url"] != "https://handla.ica.se/" {
		t.Fatal(created.data)
	}
	duplicate := s.request(t, "POST", "/v1/basket-transfers", req, userToken, "basket-1")
	expectStatus(t, duplicate, 200)
	if duplicate.data["id"] != id {
		t.Fatal("duplicate transfer created")
	}
	browser := mockio.NewBrowser(scenario.Browser)
	if claimed, err := browser.Step(context.Background(), s.url, clientToken); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	finished := s.request(t, "GET", "/v1/basket-transfers/"+id, nil, userToken, "")
	expectStatus(t, finished, 200)
	if finished.data["state"] != "partial" {
		t.Fatal(finished.data)
	}
	if got := browser.Basket("ica", req.Context); !reflect.DeepEqual(got, map[string]int{"milk-basic": 2, "bananas": 3}) {
		t.Fatal("wrong basket or unrelated item removed", got)
	}
	if claimed, err := browser.Step(context.Background(), s.url, clientToken); err != nil || claimed {
		t.Fatal("completed transfer delivered again", claimed, err)
	}
	req.Items[0].Quantity = 3
	expectStatus(t, s.request(t, "POST", "/v1/basket-transfers", req, userToken, "basket-1"), 409)
	if len(s.calls) != 0 {
		t.Fatal("basket operation unexpectedly queried catalog")
	}
}

func TestClientIsolation(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	firstReq, secondReq := transferRequest(), transferRequest()
	secondReq.ClientID = "other-browser"
	first := s.request(t, "POST", "/v1/basket-transfers", firstReq, userToken, "same-key")
	second := s.request(t, "POST", "/v1/basket-transfers", secondReq, userToken, "same-key")
	expectStatus(t, first, 202)
	expectStatus(t, second, 202)
	firstID, secondID := first.data["id"].(string), second.data["id"].(string)
	if firstID == secondID {
		t.Fatal("idempotency keys crossed client boundaries")
	}
	expectStatus(t, s.request(t, "POST", "/v1/client/claims", nil, userToken, ""), 401)
	claim := s.request(t, "POST", "/v1/client/claims", nil, otherToken, "")
	expectStatus(t, claim, 200)
	if claim.data["id"] != secondID {
		t.Fatal("client received another client's transfer", claim.data)
	}
	receipt := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: "milk-basic", Status: "applied", Quantity: 2}, {ProductID: "milk-lactose-free", Status: "applied", Quantity: 1}}}
	expectStatus(t, s.request(t, "PUT", "/v1/client/transfers/"+firstID+"/result", receipt, otherToken, ""), 404)
	expectStatus(t, s.request(t, "PUT", "/v1/client/transfers/"+secondID+"/result", receipt, clientToken, ""), 404)
	expectStatus(t, s.request(t, "PUT", "/v1/client/transfers/"+secondID+"/result", receipt, otherToken, ""), 200)
	expectStatus(t, s.request(t, "POST", "/v1/client/claims", nil, otherToken, ""), 204)
	claim = s.request(t, "POST", "/v1/client/claims", nil, clientToken, "")
	expectStatus(t, claim, 200)
	if claim.data["id"] != firstID {
		t.Fatal(claim.data)
	}
}

func TestBrowserOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		browser mockio.BrowserScenario
		state   string
	}{
		{"completed", mockio.BrowserScenario{}, "completed"},
		{"login", mockio.BrowserScenario{LoginRequired: true}, "login_required"},
		{"unavailable", mockio.BrowserScenario{Unavailable: []string{"milk-basic", "milk-lactose-free"}}, "failed"},
		{"uncertain", mockio.BrowserScenario{Uncertain: []string{"milk-basic"}}, "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startSystem(t, demoScenario(t))
			req := transferRequest()
			created := s.request(t, "POST", "/v1/basket-transfers", req, userToken, tc.name)
			expectStatus(t, created, 202)
			browser := mockio.NewBrowser(tc.browser)
			if _, err := browser.Step(context.Background(), s.url, clientToken); err != nil {
				t.Fatal(err)
			}
			finished := s.request(t, "GET", "/v1/basket-transfers/"+created.data["id"].(string), nil, userToken, "")
			expectStatus(t, finished, 200)
			if finished.data["state"] != tc.state {
				t.Fatal(finished.data)
			}
			if tc.browser.LoginRequired && len(browser.Basket("ica", req.Context)) != 0 {
				t.Fatal("logged-out browser mutated basket")
			}
			if claimed, err := browser.Step(context.Background(), s.url, clientToken); claimed || err != nil {
				t.Fatal("terminal outcome was retried", claimed, err)
			}
		})
	}
}

func TestReceiptValidationAndReplay(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	created := s.request(t, "POST", "/v1/basket-transfers", transferRequest(), userToken, "receipt")
	expectStatus(t, created, 202)
	path := "/v1/client/transfers/" + created.data["id"].(string) + "/result"
	good := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: "milk-basic", Status: "applied", Quantity: 2}, {ProductID: "milk-lactose-free", Status: "unavailable"}}}
	expectStatus(t, s.request(t, "PUT", path, good, clientToken, ""), 409)
	expectStatus(t, s.request(t, "POST", "/v1/client/claims", nil, clientToken, ""), 200)
	for _, lines := range [][]shopping.LineResult{
		{},
		{{ProductID: "milk-basic", Status: "applied", Quantity: 2}},
		{{ProductID: "milk-basic", Status: "applied", Quantity: 2}, {ProductID: "milk-basic", Status: "applied", Quantity: 2}},
		{{ProductID: "milk-basic", Status: "applied", Quantity: 1}, {ProductID: "milk-lactose-free", Status: "unavailable"}},
		{{ProductID: "milk-basic", Status: "invented"}, {ProductID: "milk-lactose-free", Status: "unavailable"}},
		{{ProductID: "milk-basic", Status: "failed", Quantity: 2}, {ProductID: "milk-lactose-free", Status: "unavailable"}},
	} {
		expectStatus(t, s.request(t, "PUT", path, shopping.Receipt{Lines: lines}, clientToken, ""), 400)
	}
	expectStatus(t, s.request(t, "PUT", path, good, clientToken, ""), 200)
	expectStatus(t, s.request(t, "PUT", path, good, clientToken, ""), 200)
	reordered := shopping.Receipt{Lines: []shopping.LineResult{good.Lines[1], good.Lines[0]}}
	expectStatus(t, s.request(t, "PUT", path, reordered, clientToken, ""), 200)
	changed := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: "milk-basic", Status: "failed"}, {ProductID: "milk-lactose-free", Status: "unavailable"}}}
	expectStatus(t, s.request(t, "PUT", path, changed, clientToken, ""), 409)
}

func TestInvalidTransfers(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	expectStatus(t, s.request(t, "POST", "/v1/basket-transfers", transferRequest(), userToken, ""), 400)
	for _, mutate := range []func(*shopping.TransferRequest){
		func(req *shopping.TransferRequest) { req.ClientID = "missing" },
		func(req *shopping.TransferRequest) { req.Items = nil },
		func(req *shopping.TransferRequest) { req.Items[0].Quantity = 0 },
		func(req *shopping.TransferRequest) { req.Items[0].Quantity = -1 },
		func(req *shopping.TransferRequest) { req.Items[0].ProductID = "" },
		func(req *shopping.TransferRequest) { req.Items[1] = req.Items[0] },
	} {
		req := transferRequest()
		mutate(&req)
		expectStatus(t, s.request(t, "POST", "/v1/basket-transfers", req, userToken, "invalid"), 400)
	}
	expectStatus(t, s.request(t, "GET", "/v1/basket-transfers/missing", nil, userToken, ""), 404)
	expectStatus(t, s.request(t, "POST", "/v1/client/claims", nil, clientToken, ""), 204)
}

func TestConcurrentIdempotencyAndClaims(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			created := s.request(t, "POST", "/v1/basket-transfers", transferRequest(), userToken, "concurrent")
			if created.status != 202 && created.status != 200 {
				t.Errorf("unexpected create status: %d", created.status)
				return
			}
			ids <- created.data["id"].(string)
		})
	}
	wg.Wait()
	close(ids)
	var id string
	count := 0
	for got := range ids {
		count++
		if id != "" && got != id {
			t.Fatal("concurrent retry created duplicate tasks")
		}
		id = got
	}
	if count != 12 {
		t.Fatalf("only %d create requests succeeded", count)
	}
	claims := make(chan int, 12)
	for range 12 {
		wg.Go(func() {
			claims <- s.request(t, "POST", "/v1/client/claims", nil, clientToken, "").status
		})
	}
	wg.Wait()
	close(claims)
	claimed := 0
	for status := range claims {
		if status == 200 {
			claimed++
		} else if status != 204 {
			t.Fatalf("unexpected claim status: %d", status)
		}
	}
	if claimed != 1 {
		t.Fatalf("transfer delivered %d times", claimed)
	}
	stored := s.request(t, "GET", "/v1/basket-transfers/"+id, nil, userToken, "")
	if stored.data["state"] != "claimed" {
		t.Fatal(stored.data)
	}
}
