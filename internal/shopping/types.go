package shopping

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid request")
	ErrUpstream = errors.New("adapter unavailable")
	ErrTimeout  = errors.New("adapter timeout")
)

type StoreContext struct {
	SessionID      string   `json:"session_id,omitempty"`
	StoreID        string   `json:"store_id"`
	PostalCode     string   `json:"postal_code"`
	Fulfillment    string   `json:"fulfillment"`
	DeliverySlotID string   `json:"delivery_slot_id,omitempty"`
	Memberships    []string `json:"memberships,omitempty"`
}

type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type Pack struct {
	Quantity int64  `json:"quantity"`
	Unit     string `json:"unit"`
}

type Offer struct {
	Price       Money  `json:"price"`
	MinQuantity int    `json:"min_quantity"`
	Membership  string `json:"membership,omitempty"`
}

type Product struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Brand        string            `json:"brand"`
	Pack         Pack              `json:"pack"`
	Price        Money             `json:"price"`
	PriceKind    string            `json:"price_kind"`
	Availability string            `json:"availability"`
	Traits       map[string]string `json:"traits"`
	Offers       []Offer           `json:"offers,omitempty"`
}

type SearchRequest struct {
	RetailerID string       `json:"retailer_id"`
	Context    StoreContext `json:"context"`
	Query      string       `json:"query"`
	Cursor     string       `json:"cursor,omitempty"`
}

type SearchResult struct {
	RetailerID string       `json:"retailer_id"`
	Context    StoreContext `json:"context"`
	ObservedAt time.Time    `json:"observed_at"`
	Products   []Product    `json:"products"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type Catalog interface {
	Search(context.Context, SearchRequest) (SearchResult, error)
}

type BasketItem struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name,omitempty"`
	Quantity  int    `json:"target_quantity"`
}

type TransferRequest struct {
	RetailerID string       `json:"retailer_id"`
	ClientID   string       `json:"client_id"`
	Context    StoreContext `json:"context"`
	Items      []BasketItem `json:"items"`
}

type LineResult struct {
	ProductID string `json:"product_id"`
	Status    string `json:"status"`
	Quantity  int    `json:"confirmed_quantity"`
	Message   string `json:"message,omitempty"`
}

type Receipt struct {
	Lines []LineResult `json:"lines"`
}

type Transfer struct {
	ID string `json:"id"`
	TransferRequest
	State          string     `json:"state"`
	BasketURL      string     `json:"basket_url"`
	CreatedAt      time.Time  `json:"created_at"`
	Receipt        *Receipt   `json:"receipt,omitempty"`
	ClaimExpiresAt *time.Time `json:"claim_expires_at,omitempty"`
}

type TransferStore interface {
	Create(context.Context, TransferRequest, string, string) (Transfer, bool, error)
	Get(context.Context, string) (Transfer, error)
	Claim(context.Context, string) (Transfer, error)
	Renew(context.Context, string, string) (Transfer, error)
	Complete(context.Context, string, string, Receipt) (Transfer, error)
}
