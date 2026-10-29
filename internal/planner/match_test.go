package planner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

type catalogPages []shopping.SearchResult

func (pages catalogPages) Search(_ context.Context, req shopping.SearchRequest) (shopping.SearchResult, error) {
	index := 0
	if req.Cursor != "" {
		index, _ = strconv.Atoi(req.Cursor)
	}
	return pages[index], nil
}

func catalogProduct(id string, price int64) shopping.Product {
	return shopping.Product{ID: id, Name: id, Pack: shopping.Pack{Quantity: 1000, Unit: "ml"}, Price: shopping.Money{AmountMinor: price, Currency: "SEK"}, PriceKind: "fixed", Availability: "in_stock"}
}

func match(catalog shopping.Catalog, item shopping.Item) (shopping.BasketLine, error) {
	line, err := Search(context.Background(), catalog, shopping.Basket{}, item, 500)
	if err == nil {
		Select(&line)
	}
	return line, err
}

func TestRepeatedProductsAcrossPages(t *testing.T) {
	pages := catalogPages{
		{Products: []shopping.Product{catalogProduct("milk", 2000), catalogProduct("sponsored", 2200)}, NextCursor: "1", ObservedAt: time.Now()},
		{Products: []shopping.Product{catalogProduct("sponsored", 2200), catalogProduct("cheapest", 1800)}, ObservedAt: time.Now()},
	}
	line, err := match(pages, shopping.Item{Query: "milk", Amount: shopping.Pack{Quantity: 2, Unit: "pack"}})
	if err != nil || len(line.Candidates) != 3 || line.Selected == nil || line.Selected.Product.ID != "cheapest" || line.SearchLimited {
		t.Fatalf("pagination with repeated products: %+v, %v", line, err)
	}
}

func TestBroadSearchReturnsBoundedCandidates(t *testing.T) {
	pages := catalogPages{{Products: []shopping.Product{}, NextCursor: "1", ObservedAt: time.Now()}, {Products: []shopping.Product{}, ObservedAt: time.Now()}}
	for i := range 600 {
		page := i / 300
		pages[page].Products = append(pages[page].Products, catalogProduct(fmt.Sprintf("product-%03d", i), int64(i+1)))
	}
	line, err := match(pages, shopping.Item{Query: "milk", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}})
	if err != nil || len(line.Candidates) != 500 || !line.SearchLimited || line.Selected == nil {
		t.Fatalf("broad search: candidates=%d, limited=%v, error=%v", len(line.Candidates), line.SearchLimited, err)
	}
}

func TestRepeatedCursorStillFails(t *testing.T) {
	pages := catalogPages{{Products: []shopping.Product{catalogProduct("milk", 2000)}, NextCursor: "1", ObservedAt: time.Now()}, {Products: []shopping.Product{catalogProduct("milk", 2000)}, NextCursor: "1", ObservedAt: time.Now()}}
	_, err := match(pages, shopping.Item{Query: "milk", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}})
	if !errors.Is(err, shopping.ErrUpstream) {
		t.Fatalf("repeated cursor was accepted: %v", err)
	}
}

func TestSearchLeavesTheChoiceOpen(t *testing.T) {
	pages := catalogPages{{Products: []shopping.Product{catalogProduct("rice-flour", 1000), catalogProduct("basmati", 3000)}, ObservedAt: time.Now()}}
	line, err := Search(context.Background(), pages, shopping.Basket{}, shopping.Item{Query: "ris", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}}, 40)
	if err != nil || line.Selected != nil || len(line.Candidates) != 2 {
		t.Fatalf("search chose a product: %+v, %v", line, err)
	}
}

func TestUnmetRequirementsAreOfferedAsAlternatives(t *testing.T) {
	pages := catalogPages{{Products: []shopping.Product{catalogProduct("milk", 1000)}, ObservedAt: time.Now()}}
	item := shopping.Item{Query: "mjölk", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}, RequiredTraits: []string{"lactose_free"}}
	line, err := Search(context.Background(), pages, shopping.Basket{}, item, 40)
	if err != nil || line.Selected != nil || !line.Candidates[0].Eligible || !slices.Equal(line.Candidates[0].UnmetTraits, []string{"lactose_free"}) {
		t.Fatalf("an unlabelled product was hidden or chosen: %+v, %v", line, err)
	}
}

func TestClosestAlternativesRankBeforePrice(t *testing.T) {
	labelled := func(id string, price int64, traits ...string) shopping.Product {
		product := catalogProduct(id, price)
		product.Traits = map[string]string{}
		for _, trait := range traits {
			product.Traits[trait] = "yes"
		}
		return product
	}
	pages := catalogPages{{Products: []shopping.Product{labelled("plain", 1000), labelled("organic", 1500, "organic"), labelled("unavailable", 500, "organic", "gluten_free")}, ObservedAt: time.Now()}}
	pages[0].Products[2].Availability = "out_of_stock"
	item := shopping.Item{Query: "ris", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}, RequiredTraits: []string{"gluten_free", "organic"}}
	line, err := match(pages, item)
	ids := []string{}
	for _, candidate := range line.Candidates {
		ids = append(ids, candidate.Product.ID)
	}
	if err != nil || line.Selected != nil || !slices.Equal(ids, []string{"organic", "plain", "unavailable"}) || !slices.Equal(line.Candidates[0].UnmetTraits, []string{"gluten_free"}) {
		t.Fatalf("closest alternative: %v, %+v, %v", ids, line, err)
	}
	pages[0].Products = append(pages[0].Products, labelled("exact", 4000, "organic", "gluten_free"))
	line, err = match(pages, item)
	if err != nil || line.Selected == nil || line.Selected.Product.ID != "exact" || line.Issue != "" {
		t.Fatalf("product meeting every requirement: %+v, %v", line, err)
	}
}

func TestBasketsAreRankedByMissingItemsThenTotal(t *testing.T) {
	selected := func(price int64) shopping.BasketLine {
		return shopping.BasketLine{Selected: &shopping.Candidate{Eligible: true, Product: catalogProduct("milk", price), Quantity: 1}}
	}
	plan := shopping.Plan{
		Baskets: []shopping.Basket{{RetailerID: "partial"}, {RetailerID: "expensive"}, {RetailerID: "cheap"}},
		Lines: []shopping.PlanLine{
			{ID: "milk", Baskets: map[string]shopping.BasketLine{"partial": selected(500), "expensive": selected(3000), "cheap": selected(2000)}},
			{ID: "salad", Baskets: map[string]shopping.BasketLine{"partial": {}, "expensive": selected(1000), "cheap": selected(1000)}},
			{ID: "skipped", Skipped: true, Baskets: map[string]shopping.BasketLine{"partial": {}, "expensive": {}, "cheap": {}}},
		},
	}
	Recalculate(&plan)
	order := []string{}
	for _, basket := range plan.Baskets {
		order = append(order, basket.RetailerID)
	}
	if !slices.Equal(order, []string{"cheap", "expensive", "partial"}) || plan.RetailerID != "cheap" || plan.Baskets[2].Missing != 1 || plan.Baskets[0].Total.AmountMinor != 3000 {
		t.Fatalf("ranking: %v, chosen %q, %+v", order, plan.RetailerID, plan.Baskets)
	}
	plan.RetailerID = "partial"
	Recalculate(&plan)
	items, err := BasketItems(plan)
	if err != nil || plan.RetailerID != "partial" || len(items) != 1 || items[0].ProductID != "milk" {
		t.Fatalf("chosen basket items: %q, %+v, %v", plan.RetailerID, items, err)
	}
}
