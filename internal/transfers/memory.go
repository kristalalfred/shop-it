package transfers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type entry struct {
	transfer shopping.Transfer
	payload  string
}

type Memory struct {
	mu      sync.Mutex
	entries map[string]*entry
	keys    map[idempotencyKey]string
	order   []string
}

type idempotencyKey struct {
	clientID string
	key      string
}

func NewMemory() *Memory {
	return &Memory{entries: make(map[string]*entry), keys: make(map[idempotencyKey]string)}
}

func (m *Memory) Create(ctx context.Context, req shopping.TransferRequest, key, basketURL string) (shopping.Transfer, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return shopping.Transfer{}, false, err
	}
	payload, _ := json.Marshal(req)
	scopedKey := idempotencyKey{req.ClientID, key}
	if id, ok := m.keys[scopedKey]; ok {
		e := m.entries[id]
		if e.payload != string(payload) {
			return shopping.Transfer{}, false, shopping.ErrConflict
		}
		return clone(e.transfer), false, nil
	}
	t := shopping.Transfer{
		ID: "transfer-" + rand.Text(), TransferRequest: req,
		State: "pending", BasketURL: basketURL, CreatedAt: time.Now().UTC(),
	}
	m.entries[t.ID] = &entry{transfer: clone(t), payload: string(payload)}
	m.keys[scopedKey] = t.ID
	m.order = append(m.order, t.ID)
	return clone(t), true, nil
}

func (m *Memory) Get(ctx context.Context, id string) (shopping.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return shopping.Transfer{}, err
	}
	e, ok := m.entries[id]
	if !ok {
		return shopping.Transfer{}, shopping.ErrNotFound
	}
	return clone(e.transfer), nil
}

func (m *Memory) Claim(ctx context.Context, clientID string) (shopping.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return shopping.Transfer{}, err
	}
	for _, id := range m.order {
		e := m.entries[id]
		if e.transfer.ClientID == clientID && e.transfer.State == "pending" {
			e.transfer.State = "claimed"
			return clone(e.transfer), nil
		}
	}
	return shopping.Transfer{}, shopping.ErrNotFound
}

func (m *Memory) Complete(ctx context.Context, id, clientID string, receipt shopping.Receipt) (shopping.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return shopping.Transfer{}, err
	}
	receipt.Lines = slices.Clone(receipt.Lines)
	slices.SortFunc(receipt.Lines, func(a, b shopping.LineResult) int { return strings.Compare(a.ProductID, b.ProductID) })
	e, ok := m.entries[id]
	if !ok || e.transfer.ClientID != clientID {
		return shopping.Transfer{}, shopping.ErrNotFound
	}
	if e.transfer.Receipt != nil {
		if !reflect.DeepEqual(*e.transfer.Receipt, receipt) {
			return shopping.Transfer{}, shopping.ErrConflict
		}
		return clone(e.transfer), nil
	}
	if e.transfer.State != "claimed" {
		return shopping.Transfer{}, shopping.ErrConflict
	}
	state, err := receiptState(e.transfer.Items, receipt)
	if err != nil {
		return shopping.Transfer{}, err
	}
	e.transfer.Receipt = &receipt
	e.transfer.State = state
	e.transfer = clone(e.transfer)
	return clone(e.transfer), nil
}

func receiptState(items []shopping.BasketItem, receipt shopping.Receipt) (string, error) {
	if len(items) != len(receipt.Lines) {
		return "", shopping.ErrInvalid
	}
	expected := make(map[string]int, len(items))
	for _, item := range items {
		expected[item.ProductID] = item.Quantity
	}
	applied, loginRequired, uncertain := 0, false, false
	for _, line := range receipt.Lines {
		quantity, ok := expected[line.ProductID]
		if !ok {
			return "", shopping.ErrInvalid
		}
		delete(expected, line.ProductID)
		switch line.Status {
		case "applied":
			if line.Quantity != quantity {
				return "", shopping.ErrInvalid
			}
			applied++
		case "unavailable", "failed", "login_required", "uncertain":
			if line.Quantity != 0 {
				return "", shopping.ErrInvalid
			}
			loginRequired = loginRequired || line.Status == "login_required"
			uncertain = uncertain || line.Status == "uncertain"
		default:
			return "", shopping.ErrInvalid
		}
	}
	switch {
	case uncertain:
		return "uncertain", nil
	case applied == len(items):
		return "completed", nil
	case applied > 0:
		return "partial", nil
	case loginRequired:
		return "login_required", nil
	default:
		return "failed", nil
	}
}

func clone(t shopping.Transfer) shopping.Transfer {
	t.Items = append([]shopping.BasketItem(nil), t.Items...)
	t.Context.Memberships = append([]string(nil), t.Context.Memberships...)
	if t.Receipt != nil {
		t.Receipt = &shopping.Receipt{Lines: append([]shopping.LineResult(nil), t.Receipt.Lines...)}
	}
	return t
}
