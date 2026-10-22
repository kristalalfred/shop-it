package browser

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func TestCommandsAreIsolatedAndCancelledWorkCannotBeCompleted(t *testing.T) {
	hub := New()
	hub.Claim("browser")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := hub.Do(ctx, "browser", "willys", "search", nil); done <- err }()
	var commands []Command
	deadline := time.Now().Add(time.Second)
	for len(commands) == 0 && time.Now().Before(deadline) {
		if got := hub.Claim("other"); len(got) != 0 {
			t.Fatal("other client claimed a command")
		}
		commands = hub.Claim("browser")
		if len(commands) == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if len(commands) != 1 {
		t.Fatal("command not available")
	}
	if len(hub.Claim("browser")) != 0 {
		t.Fatal("command claimed twice")
	}
	if !errors.Is(hub.Complete("other", commands[0].ID, Reply{Result: json.RawMessage(`{}`)}), shopping.ErrNotFound) {
		t.Fatal("other client completed a command")
	}
	cancel()
	if err := <-done; !errors.Is(err, shopping.ErrTimeout) {
		t.Fatalf("cancellation: %v", err)
	}
	if !errors.Is(hub.Complete("browser", commands[0].ID, Reply{Result: json.RawMessage(`{}`)}), shopping.ErrNotFound) {
		t.Fatal("completed cancelled work")
	}
	hub.Remember(Session{RetailerID: "willys", ClientID: "browser", Status: "ready"})
	hub.mu.Lock()
	hub.seen["browser"] = time.Now().Add(-2 * time.Minute)
	hub.mu.Unlock()
	if hub.Online("browser") {
		t.Fatal("stale browser remains online")
	}
	if _, err := hub.Session("willys"); err == nil {
		t.Fatal("offline browser session is usable")
	}
}
