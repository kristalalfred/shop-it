package tests_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kristalalfred/ship-it/internal/mockio"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

func planRequest(text string) shopping.PlanRequest {
	return shopping.PlanRequest{Text: text}
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

func createPlan(t *testing.T, s system, req shopping.PlanRequest) shopping.Plan {
	t.Helper()
	res, err := s.request("POST", "/v1/shopping-plans", req, userToken, "")
	if err != nil || res.status != 201 {
		t.Fatalf("create plan: %+v, %v", res, err)
	}
	return decodePlan(t, res)
}

func streamPlan(t *testing.T, s system, req shopping.PlanRequest) []map[string]json.RawMessage {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq, err := http.NewRequest("POST", s.url+"/v1/shopping-plans", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+userToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, application/x-ndjson")
	res, err := s.client.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("stream response: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	events := []map[string]json.RawMessage{}
	decoder := json.NewDecoder(res.Body)
	for {
		var event map[string]json.RawMessage
		if err := decoder.Decode(&event); err == io.EOF {
			return events
		} else if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func TestPlanCreationStreamsProgress(t *testing.T) {
	scenario := demoScenario(t)
	s := startSystem(t, scenario)
	events := streamPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
	stages := []string{}
	for _, event := range events[:len(events)-1] {
		var progress shopping.Progress
		if err := json.Unmarshal(event["progress"], &progress); err != nil {
			t.Fatalf("progress event: %s", event)
		}
		if progress.Stage == "search" && (progress.Total != 3 || progress.Done != len(stages)-1) {
			t.Fatalf("search progress out of order: %+v after %v", progress, stages)
		}
		stages = append(stages, progress.Stage)
	}
	if !slices.Equal(stages, []string{"interpret", "search", "search", "search", "search"}) {
		t.Fatalf("stages: %v", stages)
	}
	var plan shopping.Plan
	if err := json.Unmarshal(events[len(events)-1]["plan"], &plan); err != nil || plan.ID == "" || chosen(t, plan).Status != "ready" || len(plan.Lines) != 3 {
		t.Fatalf("streamed plan: %+v, %v", plan, err)
	}
	if res, err := s.request("GET", "/v1/shopping-plans/"+plan.ID, nil, userToken, ""); err != nil || res.status != 200 {
		t.Fatalf("streamed plan was not saved: %+v, %v", res, err)
	}
	failEverywhere(scenario, "mjölk", "ägg", "bananer")
	s = startSystem(t, scenario)
	events = streamPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
	var failure struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	last, _ := json.Marshal(events[len(events)-1])
	if err := json.Unmarshal(last, &failure); err != nil || failure.Status != 502 || failure.Error == "" {
		t.Fatalf("stream failure: %s", last)
	}
}

func TestShoppingPlanToBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
	if basket := chosen(t, plan); plan.RetailerID != "ica" || basket.Status != "ready" || len(plan.Lines) != 3 || basket.Total.AmountMinor != 9650 || !basket.Estimated {
		t.Fatalf("wrong plan: %+v", plan)
	}
	for i, id := range []string{"milk-organic", "eggs", "bananas"} {
		if selected(plan, i) == nil || selected(plan, i).Product.ID != id {
			t.Fatalf("wrong choice: %+v", plan.Lines[i])
		}
	}
	if selected(plan, 0).Quantity != 2 || selected(plan, 1).Quantity != 2 {
		t.Fatal("wrong pack conversion")
	}
	path := "/v1/shopping-plans/" + plan.ID + "/transfer"
	input := map[string]any{"revision": plan.Revision, "client_id": "demo-browser"}
	created, err := s.request("POST", path, input, userToken, "reviewed-plan")
	if err != nil || created.status != 202 {
		t.Fatalf("transfer: %+v, %v", created, err)
	}
	retry, err := s.request("POST", path, input, userToken, "reviewed-plan")
	if err != nil || retry.status != 200 || retry.data["id"] != created.data["id"] {
		t.Fatalf("duplicate handoff: %+v, %v", retry, err)
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
	plan := createPlan(t, s, planRequest("2 l lactose-free milk\n500 g bananas"))
	milk := plan.Lines[0].Baskets[plan.RetailerID]
	if chosen(t, plan).Status != "ready" || selected(plan, 0) != nil || milk.Issue != "No product meets every requirement." {
		t.Fatalf("unsafe dietary choice: %+v", plan)
	}
	if closest := milk.Candidates[0]; closest.Product.ID != "milk-basic" || !closest.Eligible || !slices.Equal(closest.UnmetTraits, []string{"lactose_free"}) {
		t.Fatalf("closest alternative: %+v", closest)
	}
	for _, candidate := range milk.Candidates {
		if candidate.Eligible && len(candidate.UnmetTraits) == 0 {
			t.Fatalf("unsafe candidate: %+v", candidate)
		}
	}
	path := "/v1/shopping-plans/" + plan.ID
	res, err := s.request("POST", path+"/transfer", map[string]any{"revision": plan.Revision, "client_id": "demo-browser"}, userToken, "unresolved")
	if err != nil || res.status != 202 {
		t.Fatalf("available groceries blocked: %+v, %v", res, err)
	}
	items := res.data["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["product_id"] != "bananas" {
		t.Fatalf("unverified dietary product transferred: %+v", items)
	}
	res, err = s.request("PATCH", path+"/lines/"+plan.Lines[0].ID, map[string]any{"revision": plan.Revision, "product_id": "milk-lactose-free"}, userToken, "")
	if err != nil || res.status != 400 {
		t.Fatalf("unavailable product accepted: %+v, %v", res, err)
	}
	res, err = s.request("PATCH", path+"/lines/"+plan.Lines[0].ID, map[string]any{"revision": plan.Revision, "product_id": "milk-basic"}, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatalf("chosen alternative rejected: %+v, %v", res, err)
	}
	plan = decodePlan(t, res)
	if alternative := selected(plan, 0); alternative == nil || alternative.Product.ID != "milk-basic" || !slices.Equal(alternative.UnmetTraits, []string{"lactose_free"}) || chosen(t, plan).Missing != 0 {
		t.Fatalf("chosen alternative: %+v", plan)
	}
	res, err = s.request("PATCH", path+"/lines/"+plan.Lines[0].ID, map[string]any{"revision": plan.Revision, "skipped": true}, userToken, "")
	if err != nil || res.status != 200 || chosen(t, decodePlan(t, res)).Status != "ready" {
		t.Fatalf("skip: %+v, %v", res, err)
	}
	req := planRequest("milk")
	req.BudgetMinor = 100
	plan = createPlan(t, s, req)
	if !chosen(t, plan).OverBudget || len(plan.Warnings) == 0 {
		t.Fatalf("assumptions missing: %+v", plan)
	}
	path = "/v1/shopping-plans/" + plan.ID + "/transfer"
	for _, body := range []map[string]any{
		{"revision": plan.Revision, "client_id": "demo-browser"},
		{"revision": plan.Revision, "client_id": "demo-browser", "acknowledge_warnings": true},
	} {
		res, err = s.request("POST", path, body, userToken, "budget")
		if err != nil || res.status != 409 {
			t.Fatalf("unreviewed handoff: %+v, %v", res, err)
		}
	}
	res, err = s.request("POST", path, map[string]any{"revision": plan.Revision, "client_id": "demo-browser", "acknowledge_warnings": true, "allow_over_budget": true}, userToken, "budget")
	if err != nil || res.status != 202 {
		t.Fatalf("reviewed budget: %+v, %v", res, err)
	}
}

func TestRequirementWordsBroadenAnEmptySearch(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("2 packs organic milk"))
	path := "/v1/shopping-plans/" + plan.ID + "/lines/" + plan.Lines[0].ID
	item := plan.Lines[0].Item
	item.Query = "ekologisk mjölk"
	res, err := s.request("PATCH", path, map[string]any{"revision": plan.Revision, "item": item}, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatalf("search with requirement words: %+v, %v", res, err)
	}
	plan = decodePlan(t, res)
	if milk := selected(plan, 0); milk == nil || milk.Product.ID != "milk-organic" || plan.Lines[0].Item.Query != "ekologisk mjölk" {
		t.Fatalf("broader search: %+v", plan.Lines[0])
	}
	item.RequiredTraits = []string{}
	res, err = s.request("PATCH", path, map[string]any{"revision": plan.Revision, "item": item}, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatalf("search without requirements: %+v, %v", res, err)
	}
	if plan = decodePlan(t, res); len(plan.Lines[0].Baskets[plan.RetailerID].Candidates) != 0 {
		t.Fatalf("search was broadened without a requirement: %+v", plan.Lines[0])
	}
}

func TestConcurrentPlanEditsAndRefresh(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("2 packs organic milk"))
	path := "/v1/shopping-plans/" + plan.ID
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, quantity := range []int64{3, 4} {
		wg.Go(func() {
			item := plan.Lines[0].Item
			item.Amount.Quantity = quantity
			res, err := s.request("PATCH", path+"/lines/"+plan.Lines[0].ID, map[string]any{"revision": 1, "item": item}, userToken, "")
			if err != nil {
				t.Error(err)
				return
			}
			statuses <- res.status
		})
	}
	wg.Wait()
	close(statuses)
	got := []int{}
	for status := range statuses {
		got = append(got, status)
	}
	slices.Sort(got)
	if !slices.Equal(got, []int{200, 409}) {
		t.Fatalf("lost update: %v", got)
	}
	res, err := s.request("GET", path, nil, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatal(err)
	}
	plan = decodePlan(t, res)
	if plan.Revision != 2 {
		t.Fatalf("wrong revision: %+v", plan)
	}
	res, err = s.request("POST", path+"/refresh", map[string]int{"revision": plan.Revision}, userToken, "")
	if err != nil || res.status != 200 || decodePlan(t, res).Revision != 3 {
		t.Fatalf("refresh: %+v, %v", res, err)
	}
}

func TestMissingGroceriesDoNotBlockBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("500 g sallad\n500 g bananer"))
	if chosen(t, plan).Status != "ready" || len(plan.Lines) != 2 || selected(plan, 0) != nil || plan.Lines[0].Skipped || plan.Lines[0].Baskets[plan.RetailerID].Issue == "" {
		t.Fatalf("missing salad blocks or disappears: %+v", plan)
	}
	res, err := s.request("POST", "/v1/shopping-plans/"+plan.ID+"/transfer", map[string]any{"revision": plan.Revision, "client_id": "demo-browser"}, userToken, "missing-salad")
	if err != nil || res.status != 202 {
		t.Fatalf("available groceries blocked: %+v, %v", res, err)
	}
	items := res.data["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["product_id"] != "bananas" {
		t.Fatalf("unexpected basket items: %+v", items)
	}
	plan = createPlan(t, s, planRequest("500 g sallad"))
	res, err = s.request("POST", "/v1/shopping-plans/"+plan.ID+"/transfer", map[string]any{"revision": plan.Revision, "client_id": "demo-browser"}, userToken, "empty-basket")
	if err != nil || res.status != 409 {
		t.Fatalf("empty basket accepted: %+v, %v", res, err)
	}
}

func TestPlanAuthenticationAndInput(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	res, err := s.request("POST", "/v1/shopping-plans", planRequest("milk"), clientToken, "")
	if err != nil || res.status != 401 {
		t.Fatalf("client accessed user API: %+v, %v", res, err)
	}
	for _, text := range []string{"", "0 l milk", "1000001 g bananas"} {
		res, err = s.request("POST", "/v1/shopping-plans", planRequest(text), userToken, "")
		if err != nil || res.status != 400 {
			t.Fatalf("invalid text accepted: %q, %+v, %v", text, res, err)
		}
	}
	res, err = s.request("GET", "/v1/clients", nil, userToken, "")
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
	plan := createPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
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
	plan = createPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
	if plan.RetailerID != "mathem" || plan.Baskets[len(plan.Baskets)-1].RetailerID != "ica" || plan.Baskets[len(plan.Baskets)-1].Missing != 3 {
		t.Fatalf("one unreachable shop blocked the others: %+v", plan.Baskets)
	}
	failEverywhere(scenario, "mjölk", "ägg", "bananer")
	s = startSystem(t, scenario)
	res, err := s.request("POST", "/v1/shopping-plans", planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."), userToken, "")
	if err != nil || res.status != 502 {
		t.Fatalf("plan without any searchable line: %+v, %v", res, err)
	}
}

func TestPlanComparesEveryShopAndFillsTheChosenBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
	order := []string{}
	for i, basket := range plan.Baskets {
		order = append(order, basket.RetailerID)
		if basket.Missing != 0 || basket.Status != "ready" || i > 0 && basket.Total.AmountMinor < plan.Baskets[i-1].Total.AmountMinor {
			t.Fatalf("baskets are not ranked by price: %+v", plan.Baskets)
		}
	}
	if !slices.Equal(order, []string{"ica", "mathem", "willys", "coop", "hemkop", "citygross"}) || plan.RetailerID != "ica" {
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
	res, err = s.request("PATCH", path, map[string]any{"revision": stale, "retailer_id": "willys"}, userToken, "")
	if err != nil || res.status != 409 {
		t.Fatalf("stale choice: %+v, %v", res, err)
	}
	line := plan.Lines[1]
	res, err = s.request("PATCH", path+"/lines/"+line.ID, map[string]any{"revision": plan.Revision, "product_id": "eggs-organic"}, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatalf("choose product: %+v, %v", res, err)
	}
	plan = decodePlan(t, res)
	if selected(plan, 1).Product.ID != "eggs-organic" || plan.Lines[1].Baskets["ica"].Selected.Product.ID != "eggs" {
		t.Fatalf("product choice leaked to another shop: %+v", plan.Lines[1].Baskets)
	}
	res, err = s.request("PATCH", path+"/lines/"+line.ID, map[string]any{"revision": plan.Revision, "skipped": true}, userToken, "")
	if err != nil || res.status != 200 {
		t.Fatalf("skip: %+v, %v", res, err)
	}
	plan = decodePlan(t, res)
	if plan.RetailerID != "hemkop" || len(plan.Baskets) != 6 {
		t.Fatalf("skipping changed the chosen shop: %+v", plan)
	}
	for _, basket := range plan.Baskets {
		if basket.Missing != 0 || basket.Total.AmountMinor == 0 {
			t.Fatalf("skipped line still counts in %s: %+v", basket.RetailerID, basket)
		}
	}
	transfer, err := s.request("POST", path+"/transfer", map[string]any{"revision": plan.Revision, "client_id": "demo-browser"}, userToken, "chosen-shop")
	if err != nil || transfer.status != 202 || transfer.data["retailer_id"] != "hemkop" || transfer.data["basket_url"] != "https://www.hemkop.se/handla" {
		t.Fatalf("transfer: %+v, %v", transfer, err)
	}
	if context := transfer.data["context"].(map[string]any); context["store_id"] != "hemkop-demo" {
		t.Fatalf("transfer context: %+v", context)
	}
	if items := transfer.data["items"].([]any); len(items) != 2 {
		t.Fatalf("skipped line was transferred: %+v", items)
	}
}
