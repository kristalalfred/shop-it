package interpret

import (
	"context"
	"reflect"
	"testing"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func TestGroceryText(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		queries    []string
		amounts    []shopping.Pack
		traits     [][]string
	}{
		{"sentence", "We need 2 litres of organic milk, 12 eggs and 500 g bananas.", []string{"mjölk", "ägg", "bananer"}, []shopping.Pack{{Quantity: 2000, Unit: "ml"}, {Quantity: 12, Unit: "each"}, {Quantity: 500, Unit: "g"}}, [][]string{{"organic"}, {}, {}}},
		{"swedish", "Jag behöver två liter ekologisk mjölk och 1,5kg bananer", []string{"mjölk", "bananer"}, []shopping.Pack{{Quantity: 2000, Unit: "ml"}, {Quantity: 1500, Unit: "g"}}, [][]string{{"organic"}, {}}},
		{"list", "1. 1.5 l milk\n- 2 packs gluten-free bread\n• 12 st ägg", []string{"mjölk", "bröd", "ägg"}, []shopping.Pack{{Quantity: 1500, Unit: "ml"}, {Quantity: 2, Unit: "pack"}, {Quantity: 12, Unit: "each"}}, [][]string{{}, {"gluten_free"}, {}}},
		{"global", "milk and bread. Everything should be vegan.", []string{"mjölk", "bröd"}, []shopping.Pack{{Quantity: 1, Unit: "pack"}, {Quantity: 1, Unit: "pack"}}, [][]string{{"vegan"}, {"vegan"}}},
		{"json", `["2 packs milk", "500 g bananas"]`, []string{"mjölk", "bananer"}, []shopping.Pack{{Quantity: 2, Unit: "pack"}, {Quantity: 500, Unit: "g"}}, [][]string{{}, {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := (Builtin{}).Interpret(context.Background(), tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != len(tc.queries) {
				t.Fatalf("items: %+v", result)
			}
			for i, item := range result.Items {
				if item.Query != tc.queries[i] || item.Amount != tc.amounts[i] || !reflect.DeepEqual(item.RequiredTraits, tc.traits[i]) {
					t.Errorf("item %d: %+v", i, item)
				}
			}
		})
	}
}

func TestUnclearAndInvalidQuantities(t *testing.T) {
	result, err := (Builtin{}).Interpret(context.Background(), "2 x 500 g pasta")
	if err != nil || len(result.Items) != 1 || len(result.Warnings) == 0 {
		t.Fatalf("ambiguous request lost: %+v, %v", result, err)
	}
	for _, text := range []string{"0 l milk", "1000001 g bananas", "1.5 eggs", `["milk", 12]`} {
		if _, err := (Builtin{}).Interpret(context.Background(), text); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}
