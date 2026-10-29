package planner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

func TestSingleShopPlansAreUpgradedToBaskets(t *testing.T) {
	updated := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	saved := `{"version":1,"data":{"saved":{"id":"saved","revision":3,"text":"milk and salad","retailer_id":"willys","context":{"store_id":"willys","postal_code":"00000"},
		"budget_minor":1000,"interpreter_mode":"codex","status":"needs_review","total":{"amount_minor":0,"currency":"SEK"},"updated_at":"2026-10-14T12:00:00Z","warnings":["Assumed one pack."],
		"lines":[{"id":"missing","item":{"label":"Sallad","query":"sallad","amount":{"quantity":1,"unit":"pack"},"required_traits":[]},"candidates":[],"skipped":false,"issue":"No matching product found."},
		{"id":"milk","item":{"label":"Milk","query":"mjölk","amount":{"quantity":2,"unit":"pack"},"required_traits":[]},"candidates":[],"skipped":false,"selected":{"product":{"id":"milk","name":"milk","pack":{"quantity":1000,"unit":"ml"},"price":{"amount_minor":2000,"currency":"SEK"},"price_kind":"fixed","availability":"in_stock"},"target_quantity":2,"eligible":true}}]}}}`
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.Get(context.Background(), "saved")
	if err != nil || restored.RetailerID != "willys" || restored.Revision != 3 || !restored.UpdatedAt.Equal(updated) || restored.Text != "milk and salad" || restored.BudgetMinor != 1000 || len(restored.Warnings) != 1 || len(restored.Lines) != 2 {
		t.Fatalf("restored plan: %+v, %v", restored, err)
	}
	basket, ok := Chosen(restored)
	if !ok || len(restored.Baskets) != 1 || basket.Context.StoreID != "willys" || basket.Status != "ready" || basket.Total.AmountMinor != 4000 || !basket.OverBudget || basket.Missing != 1 {
		t.Fatalf("restored basket: %+v", restored.Baskets)
	}
	if line := restored.Lines[0]; line.Item.Label != "Sallad" || line.Baskets["willys"].Issue != "No matching product found." {
		t.Fatalf("restored missing line: %+v", line)
	}
	items, err := BasketItems(restored)
	if err != nil || len(items) != 1 || items[0].ProductID != "milk" || items[0].Quantity != 2 {
		t.Fatalf("basket: %+v, %v", items, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Version int                      `json:"version"`
		Data    map[string]shopping.Plan `json:"data"`
	}
	if err := json.Unmarshal(data, &stored); err != nil || stored.Version != 3 || stored.Data["saved"].Baskets[0].Status != "ready" {
		t.Fatalf("upgraded state was not persisted: %s, %v", data, err)
	}
	if _, err := Open(path); err != nil {
		t.Fatalf("reopen upgraded state: %v", err)
	}
}

func TestRequirementMissesBecomeAlternativesOnUpgrade(t *testing.T) {
	candidate := func(id string, price int, eligible bool, reasons ...string) string {
		encoded, _ := json.Marshal(reasons)
		return `{"product":{"id":"` + id + `","name":"` + id + `","pack":{"quantity":1000,"unit":"ml"},"price":{"amount_minor":` + strconv.Itoa(price) + `,"currency":"SEK"},"price_kind":"fixed","availability":"in_stock"},"target_quantity":1,"unit_price":{"amount_minor":` + strconv.Itoa(price) + `,"currency":"SEK"},"total":{"amount_minor":` + strconv.Itoa(price) + `,"currency":"SEK"},"eligible":` + strconv.FormatBool(eligible) + `,"reasons":` + string(encoded) + `}`
	}
	unmet := "Requirement unverified or unmet: "
	item := `{"label":"Milk","query":"mjölk","amount":{"quantity":1,"unit":"pack"},"required_traits":["lactose_free","organic"]}`
	saved := `{"version":2,"data":{"saved":{"id":"saved","revision":1,"text":"milk","required_traits":["organic"],"retailer_id":"willys","interpreter_mode":"codex","warnings":[],
		"baskets":[{"retailer_id":"willys","context":{"store_id":"willys","postal_code":"00000"}}],
		"lines":[{"id":"open","item":` + item + `,"skipped":false,"baskets":{"willys":{"issue":"No product has verified availability, requirements and a compatible pack size.","candidates":[` +
		candidate("unrelated", 500, false, unmet+"organic", "This product does not match the requested ingredient.") + `,` +
		candidate("plain", 1000, false, unmet+"lactose_free", unmet+"organic") + `,` +
		candidate("organic", 1500, false, unmet+"lactose_free") + `]}}},
		{"id":"chosen","item":` + item + `,"skipped":false,"baskets":{"willys":{"selected":` + candidate("exact", 3000, true, "Lowest-cost options are ranked by the total needed quantity.") + `,"candidates":[` +
		candidate("exact", 3000, true, "Lowest-cost options are ranked by the total needed quantity.") + `,` +
		candidate("organic", 1500, false, unmet+"lactose_free") + `]}}}]}}}`
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.Get(context.Background(), "saved")
	if err != nil {
		t.Fatal(err)
	}
	open := plan.Lines[0].Baskets["willys"]
	ids := []string{}
	for _, candidate := range open.Candidates {
		ids = append(ids, candidate.Product.ID)
	}
	first := open.Candidates[0]
	if !slices.Equal(ids, []string{"organic", "plain", "unrelated"}) || !first.Eligible || !slices.Equal(first.UnmetTraits, []string{"lactose_free"}) || len(first.Reasons) != 0 || open.Candidates[2].Eligible || open.Selected != nil || open.Issue != "No product meets every requirement." {
		t.Fatalf("open line: %v, %+v", ids, open)
	}
	chosen := plan.Lines[1].Baskets["willys"]
	if chosen.Selected == nil || chosen.Selected.Product.ID != "exact" || chosen.Candidates[0].Product.ID != "exact" || !chosen.Candidates[1].Eligible {
		t.Fatalf("chosen line: %+v", chosen)
	}
	if basket, _ := Chosen(plan); basket.Missing != 1 || basket.Total.AmountMinor != 3000 {
		t.Fatalf("basket: %+v", basket)
	}
}

func TestOnlyRecentPlansAreKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := store.Create(ctx, shopping.Plan{PlanRequest: shopping.PlanRequest{Text: "first"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range savedPlans - 1 {
		if _, err := store.Create(ctx, shopping.Plan{PlanRequest: shopping.PlanRequest{Text: strconv.Itoa(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Update(ctx, first.ID, first.Revision, func(*shopping.Plan) error { return nil }); err != nil {
		t.Fatal(err)
	}
	latest, err := store.Create(ctx, shopping.Plan{PlanRequest: shopping.PlanRequest{Text: "latest"}})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := reopened.List(ctx)
	if err != nil || len(summaries) != savedPlans || summaries[0].ID != latest.ID || summaries[1].ID != first.ID {
		t.Fatalf("summaries: %+v, %v", summaries, err)
	}
	if slices.ContainsFunc(summaries, func(s shopping.PlanSummary) bool { return s.Text == "0" }) {
		t.Fatal("the least recently updated plan was kept")
	}
}
