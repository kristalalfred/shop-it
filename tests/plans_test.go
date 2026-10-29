package tests_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kristalalfred/shop-it/internal/mockio"
	"github.com/kristalalfred/shop-it/internal/shopping"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var everyShop = []string{"ica", "mathem", "willys", "coop", "hemkop", "citygross"}

type bearer string

func (token bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(token))
	return http.DefaultTransport.RoundTrip(r)
}

func chatgpt(t *testing.T, s system) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "chatgpt-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: s.url + "/mcp", HTTPClient: &http.Client{Transport: bearer(userToken)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (shopping.Plan, error) {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		return shopping.Plan{}, fmt.Errorf("%s: %v", name, result.Content[0].(*mcp.TextContent).Text)
	}
	var snapshot struct {
		Plan *shopping.Plan `json:"plan"`
	}
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Plan == nil {
		t.Fatalf("%s returned no plan: %s %v", name, raw, err)
	}
	return *snapshot.Plan, nil
}

func must(t *testing.T) func(shopping.Plan, error) shopping.Plan {
	return func(plan shopping.Plan, err error) shopping.Plan {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
}

func item(label, query string, quantity int64, unit string, traits ...string) shopping.Item {
	return shopping.Item{Label: label, Query: query, Amount: shopping.Pack{Quantity: quantity, Unit: unit}, RequiredTraits: append([]string{}, traits...)}
}

var breakfast = []shopping.Item{item("Organic milk", "mjölk", 2000, "ml", "organic"), item("Eggs", "ägg", 12, "each"), item("Bananas", "bananer", 500, "g")}

func createPlan(t *testing.T, session *mcp.ClientSession, shops []string, budget int64, items ...shopping.Item) (shopping.Plan, error) {
	t.Helper()
	return call(t, session, "create_shopping_plan", map[string]any{"text": "Groceries", "items": items, "retailer_ids": shops, "budget_minor": budget})
}

func choose(t *testing.T, session *mcp.ClientSession, plan shopping.Plan, products ...string) shopping.Plan {
	t.Helper()
	selections := []map[string]string{}
	for i, line := range plan.Lines {
		for retailer, entry := range line.Baskets {
			if slices.ContainsFunc(entry.Candidates, func(c shopping.Candidate) bool { return c.Eligible && c.Product.ID == products[i] }) {
				selections = append(selections, map[string]string{"line_id": line.ID, "retailer_id": retailer, "product_id": products[i]})
			}
		}
	}
	return must(t)(call(t, session, "select_products", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "selections": selections}))
}

func chosen(t *testing.T, plan shopping.Plan) shopping.Basket {
	t.Helper()
	for _, basket := range plan.Baskets {
		if basket.RetailerID == plan.RetailerID {
			return basket
		}
	}
	t.Fatalf("plan has no basket for %q: %+v", plan.RetailerID, plan.Baskets)
	return shopping.Basket{}
}

func selected(plan shopping.Plan, line int) *shopping.Candidate {
	return plan.Lines[line].Baskets[plan.RetailerID].Selected
}

func failEverywhere(scenario mockio.Scenario, queries ...string) {
	for id, fixture := range scenario.Retailers {
		fixture.FailQueries = queries
		scenario.Retailers[id] = fixture
	}
}

func decodePlan(t *testing.T, res response) shopping.Plan {
	t.Helper()
	var plan shopping.Plan
	data, err := json.Marshal(res.data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func fill(t *testing.T, s system, plan shopping.Plan, key string, options map[string]any) response {
	t.Helper()
	body := map[string]any{"revision": plan.Revision}
	for name, value := range options {
		body[name] = value
	}
	res, err := s.request("POST", "/v1/shopping-plans/"+plan.ID+"/transfer", body, userToken, key)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestShoppingPlanToBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	session := chatgpt(t, s)
	plan := must(t)(createPlan(t, session, []string{"ica"}, 0, breakfast...))
	if selected(plan, 0) != nil || chosen(t, plan).Missing != 3 {
		t.Fatalf("Shop It chose products on its own: %+v", plan)
	}
	plan = choose(t, session, plan, "milk-organic", "eggs", "bananas")
	if basket := chosen(t, plan); plan.RetailerID != "ica" || basket.Status != "ready" || basket.Missing != 0 || basket.Total.AmountMinor != 9650 || !basket.Estimated {
		t.Fatalf("wrong plan: %+v", plan)
	}
	if selected(plan, 0).Quantity != 2 || selected(plan, 1).Quantity != 2 {
		t.Fatal("wrong pack conversion")
	}
	created := fill(t, s, plan, "reviewed-plan", nil)
	if created.status != 202 {
		t.Fatalf("transfer: %+v", created)
	}
	if retry := fill(t, s, plan, "reviewed-plan", nil); retry.status != 200 || retry.data["id"] != created.data["id"] {
		t.Fatalf("duplicate handoff: %+v", retry)
	}
	claim, err := s.request("POST", "/v1/client/claims", nil, clientToken, "")
	if err != nil || claim.status != 200 || claim.data["id"] != created.data["id"] {
		t.Fatalf("claim: %+v, %v", claim, err)
	}
	for i, item := range claim.data["items"].([]any) {
		if name := item.(map[string]any)["name"]; name != selected(plan, i).Product.Name {
			t.Fatalf("claimed item %d is named %v, want %q", i, name, selected(plan, i).Product.Name)
		}
	}
	receipt := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: "milk-organic", Status: "applied", Quantity: 2}, {ProductID: "eggs", Status: "applied", Quantity: 2}, {ProductID: "bananas", Status: "applied", Quantity: 1}}}
	completed, err := s.request("PUT", "/v1/client/transfers/"+fmt.Sprint(created.data["id"])+"/result", receipt, clientToken, "")
	if err != nil || completed.status != 200 || completed.data["state"] != "completed" {
		t.Fatalf("completion: %+v, %v", completed, err)
	}
}

func TestConstraintsReviewAndBudget(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	session := chatgpt(t, s)
	plan := must(t)(createPlan(t, session, []string{"ica"}, 0, item("Lactose-free milk", "mjölk", 2000, "ml", "lactose_free"), item("Bananas", "bananer", 500, "g")))
	milk := plan.Lines[0].Baskets[plan.RetailerID]
	if closest := milk.Candidates[0]; closest.Product.ID != "milk-basic" || !closest.Eligible || !slices.Equal(closest.UnmetTraits, []string{"lactose_free"}) {
		t.Fatalf("closest alternative: %+v", closest)
	}
	for _, candidate := range milk.Candidates {
		if candidate.Eligible && len(candidate.UnmetTraits) == 0 {
			t.Fatalf("unsafe candidate: %+v", candidate)
		}
	}
	plan = choose(t, session, plan, "", "bananas")
	res := fill(t, s, plan, "unresolved", nil)
	if res.status != 202 {
		t.Fatalf("available groceries blocked: %+v", res)
	}
	if items := res.data["items"].([]any); len(items) != 1 || items[0].(map[string]any)["product_id"] != "bananas" {
		t.Fatalf("an unchosen product was transferred: %+v", items)
	}
	if _, err := call(t, session, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "line_id": plan.Lines[0].ID, "product_id": "milk-lactose-free"}); err == nil {
		t.Fatal("unavailable product accepted")
	}
	plan = must(t)(call(t, session, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "line_id": plan.Lines[0].ID, "product_id": "milk-basic"}))
	if alternative := selected(plan, 0); alternative == nil || !slices.Equal(alternative.UnmetTraits, []string{"lactose_free"}) || chosen(t, plan).Missing != 0 {
		t.Fatalf("chosen alternative: %+v", plan)
	}
	if res := fill(t, s, plan, "unverified", nil); res.status != 409 {
		t.Fatalf("an unverified requirement was filled without review: %+v", res)
	}
	if res := fill(t, s, plan, "unverified", map[string]any{"acknowledge_warnings": true}); res.status != 202 {
		t.Fatalf("reviewed requirement: %+v", res)
	}
	plan = choose(t, session, must(t)(createPlan(t, session, []string{"ica"}, 100, item("Milk", "mjölk", 1, "pack"))), "milk-basic")
	if !chosen(t, plan).OverBudget {
		t.Fatalf("budget ignored: %+v", plan)
	}
	if res := fill(t, s, plan, "budget", nil); res.status != 409 {
		t.Fatalf("over budget handoff: %+v", res)
	}
	if res := fill(t, s, plan, "budget", map[string]any{"allow_over_budget": true}); res.status != 202 {
		t.Fatalf("reviewed budget: %+v", res)
	}
}

func TestRequirementWordsBroadenAnEmptySearch(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	session := chatgpt(t, s)
	plan := must(t)(createPlan(t, session, []string{"ica"}, 0, item("Organic milk", "ekologisk mjölk", 2, "pack", "organic")))
	if !slices.ContainsFunc(plan.Lines[0].Baskets["ica"].Candidates, func(c shopping.Candidate) bool { return c.Product.ID == "milk-organic" }) || plan.Lines[0].Item.Query != "ekologisk mjölk" {
		t.Fatalf("broader search: %+v", plan.Lines[0])
	}
	plain := plan.Lines[0].Item
	plain.RequiredTraits = []string{}
	plan = must(t)(call(t, session, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "line_id": plan.Lines[0].ID, "item": plain}))
	if len(plan.Lines[0].Baskets["ica"].Candidates) != 0 {
		t.Fatalf("search was broadened without a requirement: %+v", plan.Lines[0])
	}
}

func TestConcurrentPlanEditsAndRefresh(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	session := chatgpt(t, s)
	plan := must(t)(createPlan(t, session, []string{"ica"}, 0, item("Organic milk", "mjölk", 2, "pack", "organic")))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, quantity := range []int64{3, 4} {
		wg.Go(func() {
			changed := plan.Lines[0].Item
			changed.Amount.Quantity = quantity
			_, err := call(t, session, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": 1, "line_id": plan.Lines[0].ID, "item": changed})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	failed := 0
	for err := range results {
		if err != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("lost update: %d edits failed", failed)
	}
	res, err := s.request("GET", "/v1/shopping-plans/"+plan.ID, nil, userToken, "")
	if err != nil || res.status != 200 || decodePlan(t, res).Revision != 2 {
		t.Fatalf("wrong revision: %+v, %v", res, err)
	}
	res, err = s.request("POST", "/v1/shopping-plans/"+plan.ID+"/refresh", map[string]int{"revision": 2}, userToken, "")
	if err != nil || res.status != 200 || decodePlan(t, res).Revision != 3 {
		t.Fatalf("refresh: %+v, %v", res, err)
	}
}

func TestMissingGroceriesDoNotBlockBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	session := chatgpt(t, s)
	plan := choose(t, session, must(t)(createPlan(t, session, []string{"ica"}, 0, item("Salad", "sallad", 500, "g"), item("Bananas", "bananer", 500, "g"))), "", "bananas")
	if chosen(t, plan).Status != "ready" || selected(plan, 0) != nil || plan.Lines[0].Skipped || plan.Lines[0].Baskets["ica"].Issue == "" {
		t.Fatalf("missing salad blocks or disappears: %+v", plan)
	}
	res := fill(t, s, plan, "missing-salad", nil)
	if res.status != 202 {
		t.Fatalf("available groceries blocked: %+v", res)
	}
	if items := res.data["items"].([]any); len(items) != 1 || items[0].(map[string]any)["product_id"] != "bananas" {
		t.Fatalf("unexpected basket items: %+v", items)
	}
	plan = must(t)(createPlan(t, session, []string{"ica"}, 0, item("Salad", "sallad", 500, "g")))
	if res := fill(t, s, plan, "empty-basket", nil); res.status != 409 {
		t.Fatalf("empty basket accepted: %+v", res)
	}
}

func TestPlanAuthenticationAndInput(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	for _, path := range []string{"/v1/shopping-plans", "/v1/clients"} {
		if res, err := s.request("GET", path, nil, clientToken, ""); err != nil || res.status != 401 {
			t.Fatalf("client accessed %s: %+v, %v", path, res, err)
		}
	}
	session := chatgpt(t, s)
	for _, invalid := range []shopping.Item{item("Milk", "mjölk", 0, "ml"), item("Bananas", "bananer", 1000001, "g"), item("", "mjölk", 1, "pack"), item("Milk", "mjölk", 1, "pack", "kosher")} {
		if _, err := createPlan(t, session, []string{"ica"}, 0, invalid); err == nil {
			t.Fatalf("invalid item accepted: %+v", invalid)
		}
	}
	res, err := s.request("GET", "/v1/clients", nil, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatal(err)
	}
	for _, client := range res.data["clients"].([]any) {
		if _, ok := client.(map[string]any)["token"]; ok {
			t.Fatal("client token exposed")
		}
	}
}

func TestFailedSearchesOnlyAffectTheirOwnLines(t *testing.T) {
	scenario := demoScenario(t)
	ica := scenario.Retailers["ica"]
	ica.FailQueries = []string{"ägg"}
	scenario.Retailers["ica"] = ica
	s := startSystem(t, scenario)
	session := chatgpt(t, s)
	plan := choose(t, session, must(t)(createPlan(t, session, everyShop, 0, breakfast...)), "milk-organic", "eggs", "bananas")
	if plan.RetailerID != "mathem" || chosen(t, plan).Missing != 0 || len(plan.Lines) != 3 {
		t.Fatalf("a basket with a failed search was preferred: %+v", plan)
	}
	plan.RetailerID = "ica"
	if basket := chosen(t, plan); basket.Status != "ready" || basket.Missing != 1 || selected(plan, 0) == nil || selected(plan, 2) == nil {
		t.Fatalf("searchable lines were lost: %+v", plan)
	}
	if eggs := plan.Lines[1].Baskets["ica"]; eggs.Selected != nil || len(eggs.Candidates) != 0 || !strings.Contains(eggs.Issue, "search failed") {
		t.Fatalf("failed search line: %+v", eggs)
	}
	refreshed, err := s.request("POST", "/v1/shopping-plans/"+plan.ID+"/refresh", map[string]any{"revision": plan.Revision}, userToken, "")
	if err != nil || refreshed.status != 200 || !strings.Contains(decodePlan(t, refreshed).Lines[1].Baskets["ica"].Issue, "search failed") {
		t.Fatalf("refresh: %+v, %v", refreshed, err)
	}
	ica.FailQueries = []string{"mjölk", "ägg", "bananer"}
	scenario.Retailers["ica"] = ica
	s = startSystem(t, scenario)
	session = chatgpt(t, s)
	plan = choose(t, session, must(t)(createPlan(t, session, everyShop, 0, breakfast...)), "milk-organic", "eggs", "bananas")
	if plan.RetailerID != "mathem" || plan.Baskets[len(plan.Baskets)-1].RetailerID != "ica" || plan.Baskets[len(plan.Baskets)-1].Missing != 3 {
		t.Fatalf("one unreachable shop blocked the others: %+v", plan.Baskets)
	}
	failEverywhere(scenario, "mjölk", "ägg", "bananer")
	s = startSystem(t, scenario)
	if _, err := createPlan(t, chatgpt(t, s), everyShop, 0, breakfast...); err == nil {
		t.Fatal("a plan without any searchable line was created")
	}
}

func TestPlanComparesEveryShopAndFillsTheChosenBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	session := chatgpt(t, s)
	plan := choose(t, session, must(t)(createPlan(t, session, everyShop, 0, breakfast...)), "milk-organic", "eggs", "bananas")
	order := []string{}
	for i, basket := range plan.Baskets {
		order = append(order, basket.RetailerID)
		if basket.Missing != 0 || basket.Status != "ready" || i > 0 && basket.Total.AmountMinor < plan.Baskets[i-1].Total.AmountMinor {
			t.Fatalf("baskets are not ranked by price: %+v", plan.Baskets)
		}
	}
	if !slices.Equal(order, everyShop) || plan.RetailerID != "ica" {
		t.Fatalf("baskets: %v, chosen %q", order, plan.RetailerID)
	}
	path := "/v1/shopping-plans/" + plan.ID
	for _, body := range []map[string]any{{"revision": plan.Revision, "retailer_id": "lidl"}, {"revision": plan.Revision}} {
		res, err := s.request("PATCH", path, body, userToken, "")
		if err != nil || res.status != 400 {
			t.Fatalf("unknown shop chosen: %+v, %v", res, err)
		}
	}
	res, err := s.request("PATCH", path, map[string]any{"revision": plan.Revision, "retailer_id": "hemkop"}, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatalf("choose basket: %+v, %v", res, err)
	}
	stale := plan.Revision
	plan = decodePlan(t, res)
	if plan.RetailerID != "hemkop" || plan.Revision != stale+1 {
		t.Fatalf("chosen basket: %q at revision %d", plan.RetailerID, plan.Revision)
	}
	if res, err := s.request("PATCH", path, map[string]any{"revision": stale, "retailer_id": "willys"}, userToken, ""); err != nil || res.status != 409 {
		t.Fatalf("stale choice: %+v, %v", res, err)
	}
	line := plan.Lines[1]
	plan = must(t)(call(t, session, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "line_id": line.ID, "product_id": "eggs-organic"}))
	if selected(plan, 1).Product.ID != "eggs-organic" || plan.Lines[1].Baskets["ica"].Selected.Product.ID != "eggs" {
		t.Fatalf("product choice leaked to another shop: %+v", plan.Lines[1].Baskets)
	}
	plan = must(t)(call(t, session, "update_plan_line", map[string]any{"plan_id": plan.ID, "revision": plan.Revision, "line_id": line.ID, "skipped": true}))
	if plan.RetailerID != "hemkop" || len(plan.Baskets) != 6 {
		t.Fatalf("skipping changed the chosen shop: %+v", plan)
	}
	for _, basket := range plan.Baskets {
		if basket.Missing != 0 || basket.Total.AmountMinor == 0 {
			t.Fatalf("skipped line still counts in %s: %+v", basket.RetailerID, basket)
		}
	}
	transfer := fill(t, s, plan, "chosen-shop", nil)
	if transfer.status != 202 || transfer.data["retailer_id"] != "hemkop" || transfer.data["basket_url"] != "https://www.hemkop.se/handla" {
		t.Fatalf("transfer: %+v", transfer)
	}
	if context := transfer.data["context"].(map[string]any); context["store_id"] != "hemkop-demo" {
		t.Fatalf("transfer context: %+v", context)
	}
	if items := transfer.data["items"].([]any); len(items) != 2 {
		t.Fatalf("skipped line was transferred: %+v", items)
	}
}
