package tests_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func planRequest(text string) shopping.PlanRequest {
	return shopping.PlanRequest{Text: text, RetailerID: "ica", Context: storeContext("ica")}
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

func TestShoppingPlanToBasket(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("We need 2 litres of organic milk, 12 eggs and 500 g bananas."))
	if plan.Status != "ready" || len(plan.Lines) != 3 || plan.Total.AmountMinor != 9650 || !plan.Estimated {
		t.Fatalf("wrong plan: %+v", plan)
	}
	for i, id := range []string{"milk-organic", "eggs", "bananas"} {
		if plan.Lines[i].Selected == nil || plan.Lines[i].Selected.Product.ID != id {
			t.Fatalf("wrong choice: %+v", plan.Lines[i])
		}
	}
	if plan.Lines[0].Selected.Quantity != 2 || plan.Lines[1].Selected.Quantity != 2 {
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
	receipt := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: "milk-organic", Status: "applied", Quantity: 2}, {ProductID: "eggs", Status: "applied", Quantity: 2}, {ProductID: "bananas", Status: "applied", Quantity: 1}}}
	completed, err := s.request("PUT", "/v1/client/transfers/"+fmt.Sprint(created.data["id"])+"/result", receipt, clientToken, "")
	if err != nil || completed.status != 200 || completed.data["state"] != "completed" {
		t.Fatalf("completion: %+v, %v", completed, err)
	}
}

func TestConstraintsReviewAndBudget(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	plan := createPlan(t, s, planRequest("2 l lactose-free milk\n500 g bananas"))
	if plan.Status != "ready" || plan.Lines[0].Selected != nil {
		t.Fatalf("unsafe dietary choice: %+v", plan)
	}
	for _, candidate := range plan.Lines[0].Candidates {
		if candidate.Eligible {
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
	res, err = s.request("PATCH", path+"/lines/"+plan.Lines[0].ID, map[string]any{"revision": plan.Revision, "product_id": "milk-basic"}, userToken, "")
	if err != nil || res.status != 400 {
		t.Fatalf("unsafe override accepted: %+v, %v", res, err)
	}
	res, err = s.request("PATCH", path+"/lines/"+plan.Lines[0].ID, map[string]any{"revision": plan.Revision, "skipped": true}, userToken, "")
	if err != nil || res.status != 200 || decodePlan(t, res).Status != "ready" {
		t.Fatalf("skip: %+v, %v", res, err)
	}
	req := planRequest("milk")
	req.BudgetMinor = 100
	plan = createPlan(t, s, req)
	if !plan.OverBudget || len(plan.Warnings) == 0 {
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
	if plan.Status != "ready" || len(plan.Lines) != 2 || plan.Lines[0].Selected != nil || plan.Lines[0].Skipped || plan.Lines[0].Issue == "" {
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
