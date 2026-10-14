package mockio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type Browser struct {
	mu       sync.Mutex
	scenario BrowserScenario
	baskets  map[string]map[string]int
	receipts map[string]shopping.Receipt
	client   *http.Client
}

func NewBrowser(scenario BrowserScenario) *Browser {
	return &Browser{
		scenario: scenario, baskets: make(map[string]map[string]int), receipts: make(map[string]shopping.Receipt),
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

func (b *Browser) Apply(t shopping.Transfer) shopping.Receipt {
	b.mu.Lock()
	defer b.mu.Unlock()
	if receipt, ok := b.receipts[t.ID]; ok {
		return shopping.Receipt{Lines: slices.Clone(receipt.Lines)}
	}
	key := basketKey(t.RetailerID, t.Context)
	if b.baskets[key] == nil {
		b.baskets[key] = make(map[string]int)
		for id, quantity := range b.scenario.Existing {
			b.baskets[key][id] = quantity
		}
	}
	receipt := shopping.Receipt{Lines: make([]shopping.LineResult, 0, len(t.Items))}
	for _, item := range t.Items {
		line := shopping.LineResult{ProductID: item.ProductID, Status: "applied", Quantity: item.Quantity}
		switch {
		case b.scenario.LoginRequired:
			line.Status, line.Quantity = "login_required", 0
		case slices.Contains(b.scenario.Unavailable, item.ProductID):
			line.Status, line.Quantity = "unavailable", 0
		case slices.Contains(b.scenario.Uncertain, item.ProductID):
			b.baskets[key][item.ProductID] = item.Quantity
			line.Status, line.Quantity = "uncertain", 0
		default:
			b.baskets[key][item.ProductID] = item.Quantity
		}
		receipt.Lines = append(receipt.Lines, line)
	}
	b.receipts[t.ID] = receipt
	return shopping.Receipt{Lines: slices.Clone(receipt.Lines)}
}

func basketKey(retailerID string, ctx shopping.StoreContext) string {
	payload, _ := json.Marshal(struct {
		RetailerID string
		Context    shopping.StoreContext
	}{retailerID, ctx})
	return string(payload)
}

func (b *Browser) Basket(retailerID string, ctx shopping.StoreContext) map[string]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := map[string]int{}
	basket := b.baskets[basketKey(retailerID, ctx)]
	if basket == nil {
		basket = b.scenario.Existing
	}
	for id, quantity := range basket {
		result[id] = quantity
	}
	return result
}

func (b *Browser) Step(ctx context.Context, baseURL, token string) (bool, error) {
	var transfer shopping.Transfer
	status, err := b.request(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/client/claims", token, nil, &transfer)
	if err != nil || status == http.StatusNoContent {
		return false, err
	}
	receipt := b.Apply(transfer)
	_, err = b.request(ctx, http.MethodPut, strings.TrimRight(baseURL, "/")+"/v1/client/transfers/"+transfer.ID+"/result", token, receipt, nil)
	return true, err
}

func (b *Browser) Run(ctx context.Context, baseURL, token string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := b.Step(ctx, baseURL, token); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (b *Browser) request(ctx context.Context, method, url, token string, input, output any) (int, error) {
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := b.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return res.StatusCode, fmt.Errorf("fake browser request returned HTTP %d", res.StatusCode)
	}
	if output != nil && res.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(output); err != nil {
			return res.StatusCode, err
		}
	}
	return res.StatusCode, nil
}
