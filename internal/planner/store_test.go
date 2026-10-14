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

func TestStoredPlanWithMissingItemsBecomesReady(t *testing.T) {
	updated := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	plan := shopping.Plan{ID: "saved", Revision: 3, Status: "needs_review", UpdatedAt: updated,
		Lines: []shopping.PlanLine{
			{ID: "missing", Item: shopping.Item{Label: "Sallad"}},
			{ID: "milk", Selected: &shopping.Candidate{Eligible: true, Product: catalogProduct("milk", 2000), Quantity: 2}},
		},
	}
	data, err := json.Marshal(map[string]any{"version": 1, "data": map[string]shopping.Plan{plan.ID: plan}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plans.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.Get(context.Background(), plan.ID)
	if err != nil || restored.Status != "ready" || restored.Revision != 3 || !restored.UpdatedAt.Equal(updated) || restored.Total.AmountMinor != 4000 || len(restored.Lines) != 2 {
		t.Fatalf("restored plan: %+v, %v", restored, err)
	}
	items, err := BasketItems(restored)
	if err != nil || len(items) != 1 || items[0].ProductID != "milk" || items[0].Quantity != 2 {
		t.Fatalf("basket: %+v, %v", items, err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Data map[string]shopping.Plan `json:"data"`
	}
	if err := json.Unmarshal(data, &saved); err != nil || saved.Data[plan.ID].Status != "ready" {
		t.Fatalf("updated state was not persisted: %+v, %v", saved, err)
	}
}
