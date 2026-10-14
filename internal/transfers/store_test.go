package transfers

import (
	"context"
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
