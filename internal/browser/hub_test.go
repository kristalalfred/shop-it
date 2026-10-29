package browser

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

func TestCommandsAreIsolatedAndCancelledWorkCannotBeCompleted(t *testing.T) {
	hub := New()
	hub.Claim(t.Context(), "browser", 0)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := hub.Do(ctx, "browser", "willys", "search", nil); done <- err }()
	var commands []Command
	deadline := time.Now().Add(time.Second)
	for len(commands) == 0 && time.Now().Before(deadline) {
		if got := hub.Claim(t.Context(), "other", 0); len(got) != 0 {
			t.Fatal("other client claimed a command")
		}
		commands = hub.Claim(t.Context(), "browser", 0)
		if len(commands) == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if len(commands) != 1 {
		t.Fatal("command not available")
	}
	if len(hub.Claim(t.Context(), "browser", 0)) != 0 {
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

func TestWaitingClaimsReturnOnNewWorkOrTimeout(t *testing.T) {
	hub := New()
	started := time.Now()
	if got := hub.Claim(t.Context(), "browser", 50*time.Millisecond); len(got) != 0 || time.Since(started) < 50*time.Millisecond {
		t.Fatalf("idle claim returned %v after %v", got, time.Since(started))
	}
	claimed := make(chan []Command, 1)
	go func() { claimed <- hub.Claim(t.Context(), "browser", time.Minute) }()
	go func() { _, _ = hub.Do(t.Context(), "browser", "willys", "search", nil) }()
	select {
	case got := <-claimed:
		if len(got) != 1 {
			t.Fatalf("claimed %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting claim missed a new command")
	}
	go func() { claimed <- hub.Claim(t.Context(), "browser", time.Minute) }()
	deadline := time.After(5 * time.Second)
	for woke := false; !woke; {
		hub.Notify()
		select {
		case got := <-claimed:
			if len(got) != 0 {
				t.Fatalf("notification claimed %v", got)
			}
			woke = true
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("waiting claim missed a notification")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { claimed <- hub.Claim(ctx, "browser", time.Minute) }()
	cancel()
	select {
	case <-claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("waiting claim ignored cancellation")
	}
}
