package transfers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func TestRestartAndExpiredClaims(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "transfers.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	req := shopping.TransferRequest{ClientID: "browser", RetailerID: "ica", Items: []shopping.BasketItem{{ProductID: "milk", Quantity: 2}}}
	created, _, err := s.Create(ctx, req, "retry", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	s.lease = -time.Second
	claimed, err := s.Claim(ctx, "browser")
	if err != nil || claimed.ID != created.ID {
		t.Fatalf("claim: %+v, %v", claimed, err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	retry, fresh, err := s.Create(ctx, req, "retry", "https://example.com")
	if err != nil || fresh || retry.ID != created.ID {
		t.Fatalf("retry duplicated after restart: %+v, %v", retry, err)
	}
	reclaimed, err := s.Claim(ctx, "browser")
	if err != nil || reclaimed.ID != created.ID {
		t.Fatalf("claim not recovered after restart: %+v, %v", reclaimed, err)
	}
	if _, err := s.Claim(ctx, "browser"); err != shopping.ErrNotFound {
		t.Fatalf("active claim redelivered: %v", err)
	}
	receipt := shopping.Receipt{Lines: []shopping.LineResult{{ProductID: "milk", Status: "applied", Quantity: 2}}}
	if _, err := s.Complete(ctx, created.ID, "browser", receipt); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := s.Get(ctx, created.ID)
	if err != nil || completed.State != "completed" {
		t.Fatalf("receipt lost after restart: %+v, %v", completed, err)
	}
}

func TestRenewalRetainsOwnershipAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "transfers.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	request := shopping.TransferRequest{ClientID: "browser", RetailerID: "willys", Items: []shopping.BasketItem{{ProductID: "milk", Quantity: 1}}}
	transfer, _, err := store.Create(ctx, request, "renew", "https://www.willys.se/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Renew(ctx, transfer.ID, "browser"); !errors.Is(err, shopping.ErrConflict) {
		t.Fatalf("pending transfer renewed: %v", err)
	}
	store.lease = time.Minute
	claimed, err := store.Claim(ctx, "browser")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Renew(ctx, transfer.ID, "other"); !errors.Is(err, shopping.ErrNotFound) {
		t.Fatalf("another client renewed the lease: %v", err)
	}
	renewed, err := store.Renew(ctx, transfer.ID, "browser")
	if err != nil || !renewed.ClaimExpiresAt.After(*claimed.ClaimExpiresAt) {
		t.Fatalf("lease was not extended: %v", err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(ctx, "browser"); !errors.Is(err, shopping.ErrNotFound) {
		t.Fatalf("renewed transfer redelivered after restart: %v", err)
	}
	store.lease = -time.Second
	if _, err := store.Renew(ctx, transfer.ID, "browser"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Renew(ctx, transfer.ID, "browser"); !errors.Is(err, shopping.ErrConflict) {
		t.Fatalf("expired lease was renewed: %v", err)
	}
}
