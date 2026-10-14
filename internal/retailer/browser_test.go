package retailer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func TestBrowserSearchOwnershipAndContext(t *testing.T) {
	bridge := NewBridge(time.Second)
	store := shopping.StoreContext{StoreID: "store", PostalCode: "11122", Fulfillment: "delivery", SessionID: "session"}
	bridge.Publish("browser", []Session{{RetailerID: "willys", Context: store, LoggedIn: true}})
	request := shopping.SearchRequest{RetailerID: "willys", Context: store, Query: "milk"}
	finished := make(chan error, 1)
	go func() {
		_, err := bridge.Search(context.Background(), request)
		finished <- err
	}()
	var jobs []SearchJob
	deadline := time.After(time.Second)
	for len(jobs) == 0 {
		select {
		case <-deadline:
			t.Fatal("search was not queued")
		default:
			jobs = bridge.Claim("browser")
			time.Sleep(time.Millisecond)
		}
	}
	if len(bridge.Claim("other")) != 0 || len(bridge.Claim("browser")) != 0 {
		t.Fatal("search was claimed twice or by another client")
	}
	result := shopping.SearchResult{RetailerID: "willys", Context: store, ObservedAt: time.Now(), Products: []shopping.Product{}}
	if err := bridge.Complete("other", jobs[0].ID, SearchReply{Result: &result}); !errors.Is(err, shopping.ErrNotFound) {
		t.Fatalf("wrong client result: %v", err)
	}
	result.Context.SessionID = "other-session"
	if err := bridge.Complete("browser", jobs[0].ID, SearchReply{Result: &result}); !errors.Is(err, shopping.ErrInvalid) {
		t.Fatalf("wrong context result: %v", err)
	}
	result.Context = store
	if err := bridge.Complete("browser", jobs[0].ID, SearchReply{Result: &result}); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if !bridge.HasSession("willys", "browser", store, true) {
		t.Fatal("signed-in session missing")
	}
	store.SessionID = "other-account"
	if bridge.HasSession("willys", "browser", store, true) {
		t.Fatal("another account matched")
	}
}

func TestBrowserSearchTimeoutAndSessionExpiry(t *testing.T) {
	bridge := NewBridge(10 * time.Millisecond)
	store := shopping.StoreContext{StoreID: "store", PostalCode: "11122", Fulfillment: "delivery", SessionID: "session"}
	request := shopping.SearchRequest{RetailerID: "mathem", Context: store, Query: "milk"}
	if _, err := bridge.Search(context.Background(), request); !errors.Is(err, shopping.ErrUpstream) {
		t.Fatalf("offline search: %v", err)
	}
	bridge.Publish("browser", []Session{{RetailerID: "mathem", Context: store}})
	if bridge.HasSession("mathem", "browser", store, true) {
		t.Fatal("anonymous session allowed a transfer")
	}
	if _, err := bridge.Search(context.Background(), request); !errors.Is(err, shopping.ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	if len(bridge.Claim("browser")) != 0 {
		t.Fatal("timed-out job remained queued")
	}
	bridge.sessions["browser"][0].SeenAt = time.Now().Add(-time.Minute)
	if len(bridge.Sessions()) != 0 {
		t.Fatal("expired browser remained connected")
	}
}
