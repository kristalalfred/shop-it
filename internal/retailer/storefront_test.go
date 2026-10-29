package retailer

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

func fixtures(t *testing.T) map[string]json.RawMessage {
	data, err := os.ReadFile("testdata/storefronts.json")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestStorefrontProductsPreserveIdentifiersPricesPacksAndDietaryEvidence(t *testing.T) {
	data := fixtures(t)
	var willys, hemkop struct{ Results []axfoodProduct }
	var weighted []axfoodProduct
	var mathemFixture struct{ Products []mathemProduct }
	for key, target := range map[string]any{"willys": &willys, "hemkop": &hemkop, "weighted": &weighted, "mathem": &mathemFixture} {
		if err := json.Unmarshal(data[key], target); err != nil {
			t.Fatal(err)
		}
	}
	milk, ok := axfood(willys.Results[0])
	if !ok || milk.ID != "101233933_ST" || milk.Pack != (shopping.Pack{Quantity: 1500, Unit: "ml"}) || milk.Price.AmountMinor != 1690 || milk.PriceKind != "fixed" || milk.Availability != "in_stock" {
		t.Fatalf("milk: %+v", milk)
	}
	if organic, _ := axfood(hemkop.Results[2]); organic.Traits["organic"] != "yes" {
		t.Fatalf("organic evidence lost: %+v", organic)
	}
	if lactoseFree, ok := mathem(mathemFixture.Products[2]); !ok || lactoseFree.Traits["lactose_free"] != "yes" {
		t.Fatalf("lactose-free evidence lost: %+v", lactoseFree)
	}
	banana, ok := axfood(weighted[0])
	if !ok || banana.Pack != (shopping.Pack{Quantity: 180, Unit: "g"}) || banana.Price.AmountMinor != 358 || banana.PriceKind != "estimated" {
		t.Fatalf("banana: %+v", banana)
	}
	loose := weighted[0]
	loose.ProductBasketType.Code = "KG"
	if product, _ := axfood(loose); product.Pack != (shopping.Pack{Quantity: 1000, Unit: "g"}) {
		t.Fatalf("loose produce: %+v", product)
	}
	unknownPack, drug, named, unrelated, vegan := willys.Results[0], willys.Results[0], willys.Results[0], willys.Results[0], willys.Results[0]
	unknownPack.DisplayVolume, drug.IsDrugProduct, named.Name, unrelated.Name, vegan.Name = "unknown", true, "Mjölkdryck Laktosfri 1,5% EKO/KRAV", "Ekonomiförpackning Glutenfrihet", "Peas of Heaven Vegansk Middagskorv"
	if _, ok := axfood(unknownPack); ok {
		t.Fatal("product without a pack size was kept")
	}
	if _, ok := axfood(drug); ok {
		t.Fatal("drug product was kept")
	}
	if product, _ := axfood(named); product.Traits["lactose_free"] != "yes" || product.Traits["organic"] != "yes" || product.Traits["gluten_free"] != "unknown" {
		t.Fatalf("product name evidence: %+v", product.Traits)
	}
	if product, _ := axfood(unrelated); product.Traits["organic"] != "unknown" || product.Traits["gluten_free"] != "unknown" {
		t.Fatalf("partial words in a product name were treated as evidence: %+v", product.Traits)
	}
	if product, _ := axfood(vegan); product.Traits["vegan"] != "yes" || product.Traits["lactose_free"] != "yes" {
		t.Fatalf("vegan product is not lactose free: %+v", product.Traits)
	}
	for text, want := range map[string]shopping.Pack{"2 x 500 g": {Quantity: 1000, Unit: "g"}, "15 st": {Quantity: 15, Unit: "each"}, "1,5 l": {Quantity: 1500, Unit: "ml"}, "ca: 180g": {Quantity: 180, Unit: "g"}} {
		if got, ok := pack(text); !ok || got != want {
			t.Errorf("pack(%q) = %+v", text, got)
		}
	}
}
