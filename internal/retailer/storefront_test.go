package retailer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
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
	unknownPack, drug, named := willys.Results[0], willys.Results[0], willys.Results[0]
	unknownPack.DisplayVolume, drug.IsDrugProduct, named.Name = "unknown", true, "Organic milk"
	if _, ok := axfood(unknownPack); ok {
		t.Fatal("product without a pack size was kept")
	}
	if _, ok := axfood(drug); ok {
		t.Fatal("drug product was kept")
	}
	if product, _ := axfood(named); product.Traits["organic"] != "unknown" {
		t.Fatal("product name was treated as dietary evidence")
	}
	for text, want := range map[string]shopping.Pack{"2 x 500 g": {Quantity: 1000, Unit: "g"}, "15 st": {Quantity: 15, Unit: "each"}, "1,5 l": {Quantity: 1500, Unit: "ml"}, "ca: 180g": {Quantity: 180, Unit: "g"}} {
		if got, ok := pack(text); !ok || got != want {
			t.Errorf("pack(%q) = %+v", text, got)
		}
	}
}

func TestStorefrontSearchesShopsWithoutABrowserSession(t *testing.T) {
	data := fixtures(t)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		if r.Header.Get("Cookie") != "" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "unexpected headers", http.StatusBadRequest)
			return
		}
		var payload map[string]any
		switch r.URL.Path {
		case "/axfood/rest/v2/search":
			_ = json.Unmarshal(data["willys"], &payload)
			payload["pagination"] = map[string]int{"currentPage": 0, "numberOfPages": 2}
		case "/api/v1/search/":
			_ = json.Unmarshal(data["mathem"], &payload)
			payload["attributes"] = map[string]int{"total_hits": 100}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()
	store := shopping.StoreContext{StoreID: "willys", PostalCode: "00000"}
	result, err := NewStorefront("willys", server.URL, time.Second).Search(context.Background(), shopping.SearchRequest{RetailerID: "willys", Context: store, Query: "röda linser"})
	if err != nil || len(result.Products) != 3 || result.NextCursor != "1" || !SameContext(result.Context, store) {
		t.Fatalf("willys: %+v, %v", result, err)
	}
	if requests[0] != "/axfood/rest/v2/search?page=0&q=r%C3%B6da+linser&size=40" {
		t.Fatalf("willys request: %s", requests[0])
	}
	store.StoreID = "mathem"
	result, err = NewStorefront("mathem", server.URL, time.Second).Search(context.Background(), shopping.SearchRequest{RetailerID: "mathem", Context: store, Query: "mjölk", Cursor: "1"})
	if err != nil || result.NextCursor != "2" || result.Products[0].ID != "2183" {
		t.Fatalf("mathem: %+v, %v", result, err)
	}
	if requests[1] != "/api/v1/search/?page=2&q=mj%C3%B6lk" {
		t.Fatalf("mathem request: %s", requests[1])
	}
	if _, err := NewStorefront("mathem", server.URL, time.Second).Search(context.Background(), shopping.SearchRequest{RetailerID: "mathem", Context: store, Query: "mjölk", Cursor: "x"}); !errors.Is(err, shopping.ErrInvalid) {
		t.Fatalf("invalid cursor: %v", err)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":null,"pagination":{"pageSize":40,"currentPage":0,"numberOfPages":0,"totalNumberOfResults":0}}`))
	}))
	defer empty.Close()
	store.StoreID = "hemkop"
	result, err = NewStorefront("hemkop", empty.URL, time.Second).Search(context.Background(), shopping.SearchRequest{RetailerID: "hemkop", Context: store, Query: "xqzvwy"})
	if err != nil || len(result.Products) != 0 || result.Products == nil || result.NextCursor != "" {
		t.Fatalf("search without hits: %+v, %v", result, err)
	}
	if _, err := NewStorefront("hemkop", server.URL+"/missing", time.Second).Search(context.Background(), shopping.SearchRequest{RetailerID: "hemkop", Context: store, Query: "mjölk"}); !errors.Is(err, shopping.ErrUpstream) {
		t.Fatalf("failed shop: %v", err)
	}
}
