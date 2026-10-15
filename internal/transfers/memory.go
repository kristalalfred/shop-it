package transfers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

type entry struct {
	Transfer shopping.Transfer `json:"transfer"`
	Payload  string            `json:"payload"`
}

type snapshot struct {
	Entries map[string]entry  `json:"entries"`
	Keys    map[string]string `json:"keys"`
	Order   []string          `json:"order"`
}

type Store struct {
	state *persistence.Store[snapshot]
	lease time.Duration
	wait  time.Duration
}

func NewMemory() *Store {
	s, _ := Open("")
	return s
}

func Open(path string) (*Store, error) {
	state, err := persistence.Open(path, snapshot{Entries: map[string]entry{}, Keys: map[string]string{}, Order: []string{}})
	if err != nil {
		return nil, err
	}
	data, err := state.Read()
	if err != nil {
		return nil, err
	}
	if data.Entries == nil || data.Keys == nil || data.Order == nil {
		return nil, shopping.ErrInvalid
	}
	return &Store{state: state, lease: 30 * time.Second, wait: 2 * time.Minute}, nil
}

func (s *Store) Create(ctx context.Context, req shopping.TransferRequest, key, basketURL string) (t shopping.Transfer, created bool, err error) {
	err = s.state.Update(func(data *snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		payload, err := json.Marshal(req)
		if err != nil {
			return err
		}
		scopedKey := req.ClientID + "/" + key
		if id, ok := data.Keys[scopedKey]; ok {
			e := data.Entries[id]
			if e.Payload != string(payload) {
				return shopping.ErrConflict
			}
			t = s.view(e.Transfer)
			return nil
		}
		t = shopping.Transfer{ID: "transfer-" + rand.Text(), TransferRequest: req, State: "pending", BasketURL: basketURL, CreatedAt: time.Now().UTC()}
		data.Entries[t.ID] = entry{Transfer: clone(t), Payload: string(payload)}
		data.Keys[scopedKey] = t.ID
		data.Order = append(data.Order, t.ID)
		created = true
		return nil
	})
	return t, created, err
}

func (s *Store) Get(ctx context.Context, id string) (shopping.Transfer, error) {
	if err := ctx.Err(); err != nil {
		return shopping.Transfer{}, err
	}
	data, err := s.state.Read()
	if err != nil {
		return shopping.Transfer{}, err
	}
	e, ok := data.Entries[id]
	if !ok {
		return shopping.Transfer{}, shopping.ErrNotFound
	}
	return s.view(e.Transfer), nil
}

func (s *Store) unclaimed(t shopping.Transfer) bool {
	return t.State == "pending" && time.Since(t.CreatedAt) > s.wait
}

func (s *Store) view(t shopping.Transfer) shopping.Transfer {
	t = clone(t)
	if s.unclaimed(t) {
		t.State = "expired"
	}
	return t
}

func (s *Store) Claim(ctx context.Context, clientID string) (t shopping.Transfer, err error) {
	claimed := false
	err = s.state.Update(func(data *snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed := false
		for _, id := range data.Order {
			e := data.Entries[id]
			if s.unclaimed(e.Transfer) {
				e.Transfer.State = "expired"
				data.Entries[id] = e
				changed = true
				continue
			}
			expired := e.Transfer.State == "claimed" && (e.Transfer.ClaimExpiresAt == nil || !e.Transfer.ClaimExpiresAt.After(time.Now()))
			if e.Transfer.ClientID == clientID && (e.Transfer.State == "pending" || expired) {
				e.Transfer.State = "claimed"
				expires := time.Now().UTC().Add(s.lease)
				e.Transfer.ClaimExpiresAt = &expires
				data.Entries[id] = e
				t = clone(e.Transfer)
				claimed = true
				return nil
			}
		}
		if changed {
			return nil
		}
		return shopping.ErrNotFound
	})
	if err == nil && !claimed {
		err = shopping.ErrNotFound
	}
	return t, err
}

func (s *Store) Renew(ctx context.Context, id, clientID string) (t shopping.Transfer, err error) {
	err = s.state.Update(func(data *snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		e, ok := data.Entries[id]
		if !ok || e.Transfer.ClientID != clientID {
			return shopping.ErrNotFound
		}
		if e.Transfer.State != "claimed" || e.Transfer.ClaimExpiresAt == nil || !e.Transfer.ClaimExpiresAt.After(time.Now()) {
			return shopping.ErrConflict
		}
		expires := time.Now().UTC().Add(s.lease)
		e.Transfer.ClaimExpiresAt = &expires
		data.Entries[id] = e
		t = clone(e.Transfer)
		return nil
	})
	return t, err
}

func (s *Store) Complete(ctx context.Context, id, clientID string, receipt shopping.Receipt) (t shopping.Transfer, err error) {
	receipt.Lines = slices.Clone(receipt.Lines)
	slices.SortFunc(receipt.Lines, func(a, b shopping.LineResult) int { return strings.Compare(a.ProductID, b.ProductID) })
	err = s.state.Update(func(data *snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		e, ok := data.Entries[id]
		if !ok || e.Transfer.ClientID != clientID {
			return shopping.ErrNotFound
		}
		if e.Transfer.Receipt != nil {
			if !reflect.DeepEqual(*e.Transfer.Receipt, receipt) {
				return shopping.ErrConflict
			}
			t = clone(e.Transfer)
			return nil
		}
		if e.Transfer.State != "claimed" {
			return shopping.ErrConflict
		}
		state, err := receiptState(e.Transfer.Items, receipt)
		if err != nil {
			return err
		}
		e.Transfer.Receipt = &receipt
		e.Transfer.State = state
		data.Entries[id] = e
		t = clone(e.Transfer)
		return nil
	})
	return t, err
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
		if len(line.Message) > 500 {
			return "", shopping.ErrInvalid
		}
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
	if t.ClaimExpiresAt != nil {
		expires := *t.ClaimExpiresAt
		t.ClaimExpiresAt = &expires
	}
	return t
}
