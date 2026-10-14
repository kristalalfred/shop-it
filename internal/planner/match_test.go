package planner

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
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

func TestRepeatedProductsAcrossPages(t *testing.T) {
	pages := catalogPages{
		{Products: []shopping.Product{catalogProduct("milk", 2000), catalogProduct("sponsored", 2200)}, NextCursor: "1", ObservedAt: time.Now()},
		{Products: []shopping.Product{catalogProduct("sponsored", 2200), catalogProduct("cheapest", 1800)}, ObservedAt: time.Now()},
	}
	line, err := Match(context.Background(), pages, shopping.PlanRequest{}, shopping.Item{Query: "milk", Amount: shopping.Pack{Quantity: 2, Unit: "pack"}}, "line")
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
	line, err := Match(context.Background(), pages, shopping.PlanRequest{}, shopping.Item{Query: "milk", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}}, "line")
	if err != nil || len(line.Candidates) != 500 || !line.SearchLimited || line.Selected == nil {
		t.Fatalf("broad search: candidates=%d, limited=%v, error=%v", len(line.Candidates), line.SearchLimited, err)
	}
}

func TestRepeatedCursorStillFails(t *testing.T) {
	pages := catalogPages{{Products: []shopping.Product{catalogProduct("milk", 2000)}, NextCursor: "1", ObservedAt: time.Now()}, {Products: []shopping.Product{catalogProduct("milk", 2000)}, NextCursor: "1", ObservedAt: time.Now()}}
	_, err := Match(context.Background(), pages, shopping.PlanRequest{}, shopping.Item{Query: "milk", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}}, "line")
	if !errors.Is(err, shopping.ErrUpstream) {
		t.Fatalf("repeated cursor was accepted: %v", err)
	}
}

func TestProductIdentityBeforePrice(t *testing.T) {
	pages := catalogPages{{Products: []shopping.Product{catalogProduct("rice-flour", 1000), catalogProduct("basmati", 3000)}, ObservedAt: time.Now()}}
	line, err := Search(context.Background(), pages, shopping.PlanRequest{}, shopping.Item{Query: "ris", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}}, "rice", 40)
	if err != nil || line.Selected != nil {
		t.Fatalf("search selected before matching: %+v, %v", line, err)
	}
	ApplyMatches(&line, []string{"basmati"})
	if line.Selected == nil || line.Selected.Product.ID != "basmati" || line.Candidates[1].Eligible {
		t.Fatalf("cheaper unrelated product selected: %+v", line)
	}
	ApplyMatches(&line, []string{})
	if line.Selected != nil || line.Issue == "" {
		t.Fatalf("unmatched ingredient selected: %+v", line)
	}
}

func TestProductMatchCannotOverrideRequirements(t *testing.T) {
	product := catalogProduct("milk", 1000)
	pages := catalogPages{{Products: []shopping.Product{product}, ObservedAt: time.Now()}}
	item := shopping.Item{Query: "mjölk", Amount: shopping.Pack{Quantity: 1, Unit: "pack"}, RequiredTraits: []string{"lactose_free"}}
	line, err := Search(context.Background(), pages, shopping.PlanRequest{}, item, "milk", 40)
	if err != nil {
		t.Fatal(err)
	}
	ApplyMatches(&line, []string{"milk"})
	if line.Selected != nil || line.Candidates[0].Eligible {
		t.Fatalf("AI identity match bypassed dietary evidence: %+v", line)
	}
}
