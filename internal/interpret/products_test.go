package interpret

import (
	"context"
	"errors"
	"testing"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type generatedText string

func (text generatedText) generate(context.Context, string, string, map[string]any, shopping.Notify) ([]byte, error) {
	return []byte(text), nil
}

func TestProductMatchReferences(t *testing.T) {
	requests := []shopping.ProductMatchRequest{
		{ID: "rice", Products: []shopping.ProductOption{{ID: "basmati"}, {ID: "flour"}}},
		{ID: "onions", Products: []shopping.ProductOption{{ID: "onion"}, {ID: "sauce"}}},
	}
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"valid", `{"matches":[{"id":"rice","product_ids":["basmati"]},{"id":"onions","product_ids":["onion"]}]}`, true},
		{"no matches", `{"matches":[{"id":"rice","product_ids":[]},{"id":"onions","product_ids":[]}]}`, true},
		{"invented product", `{"matches":[{"id":"rice","product_ids":["invented"]},{"id":"onions","product_ids":[]}]}`, false},
		{"wrong ingredient", `{"matches":[{"id":"rice","product_ids":["onion"]},{"id":"onions","product_ids":[]}]}`, false},
		{"missing ingredient", `{"matches":[{"id":"rice","product_ids":["basmati"]}]}`, false},
		{"duplicate ingredient", `{"matches":[{"id":"rice","product_ids":["basmati"]},{"id":"rice","product_ids":[]}]}`, false},
		{"unknown ingredient", `{"matches":[{"id":"rice","product_ids":["basmati"]},{"id":"unknown","product_ids":[]}]}`, false},
		{"duplicate product", `{"matches":[{"id":"rice","product_ids":["basmati","basmati"]},{"id":"onions","product_ids":[]}]}`, false},
		{"null products", `{"matches":[{"id":"rice","product_ids":null},{"id":"onions","product_ids":[]}]}`, false},
		{"extra object", `{"matches":[{"id":"rice","product_ids":[]},{"id":"onions","product_ids":[]}]} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := matchProducts(context.Background(), generatedText(tc.text), requests, nil)
			if tc.valid && (err != nil || len(matches) != 2) || !tc.valid && !errors.Is(err, shopping.ErrUpstream) {
				t.Fatalf("matches: %+v, error: %v", matches, err)
			}
		})
	}
}
