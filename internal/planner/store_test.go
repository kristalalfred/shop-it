package planner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
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
	if err := json.Unmarshal(data, &stored); err != nil || stored.Version != 2 || stored.Data["saved"].Baskets[0].Status != "ready" {
		t.Fatalf("upgraded state was not persisted: %s, %v", data, err)
	}
	if _, err := Open(path); err != nil {
		t.Fatalf("reopen upgraded state: %v", err)
	}
}
