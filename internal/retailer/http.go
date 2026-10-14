package retailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type HTTP struct {
	baseURL string
	client  *http.Client
}

func NewHTTP(baseURL string, timeout time.Duration) *HTTP {
	return &HTTP{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (h *HTTP) Search(ctx context.Context, input shopping.SearchRequest) (shopping.SearchResult, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return shopping.SearchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/search", bytes.NewReader(payload))
	if err != nil {
		return shopping.SearchResult{}, shopping.ErrUpstream
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return shopping.SearchResult{}, shopping.ErrTimeout
		}
		return shopping.SearchResult{}, shopping.ErrUpstream
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return shopping.SearchResult{}, shopping.ErrUpstream
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if errors.Is(err, context.DeadlineExceeded) {
		return shopping.SearchResult{}, shopping.ErrTimeout
	}
	if err != nil || len(body) > 1<<20 {
		return shopping.SearchResult{}, shopping.ErrUpstream
	}
	var result shopping.SearchResult
	if json.Unmarshal(body, &result) != nil || result.RetailerID != input.RetailerID || !sameContext(result.Context, input.Context) || result.ObservedAt.IsZero() || result.Products == nil {
		return shopping.SearchResult{}, shopping.ErrUpstream
	}
	ids := map[string]bool{}
	for _, p := range result.Products {
		if p.ID == "" || ids[p.ID] || p.Price.AmountMinor < 0 || p.Price.Currency != "SEK" || p.Pack.Quantity < 1 || !oneOf(p.Pack.Unit, "g", "ml", "each") || !oneOf(p.PriceKind, "fixed", "estimated") || !oneOf(p.Availability, "in_stock", "out_of_stock", "unknown") {
			return shopping.SearchResult{}, shopping.ErrUpstream
		}
		ids[p.ID] = true
		for _, evidence := range p.Traits {
			if !oneOf(evidence, "yes", "no", "unknown") {
				return shopping.SearchResult{}, shopping.ErrUpstream
			}
		}
		for _, offer := range p.Offers {
			if offer.MinQuantity < 1 || offer.Price.AmountMinor < 0 || offer.Price.Currency != "SEK" {
				return shopping.SearchResult{}, shopping.ErrUpstream
			}
		}
	}
	return result, nil
}

func sameContext(a, b shopping.StoreContext) bool {
	return a.StoreID == b.StoreID && a.PostalCode == b.PostalCode && a.Fulfillment == b.Fulfillment && a.DeliverySlotID == b.DeliverySlotID && slices.Equal(a.Memberships, b.Memberships)
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
