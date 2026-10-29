package browser

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

type Command struct {
	ID         string `json:"id"`
	RetailerID string `json:"retailer_id"`
	Operation  string `json:"operation"`
	Input      any    `json:"input,omitempty"`
	clientID   string
	expires    time.Time
	claimed    bool
	done       chan Reply
}

type Reply struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Status string          `json:"status,omitempty"`
}

type Session struct {
	RetailerID string                `json:"retailer_id"`
	ClientID   string                `json:"client_id"`
	Status     string                `json:"status"`
	Label      string                `json:"label"`
	Context    shopping.StoreContext `json:"context"`
	CheckedAt  time.Time             `json:"checked_at"`
}

type Hub struct {
	mu       sync.Mutex
	commands map[string]*Command
	sessions map[string]Session
	seen     map[string]time.Time
	changed  chan struct{}
}

func New() *Hub {
	return &Hub{commands: map[string]*Command{}, sessions: map[string]Session{}, seen: map[string]time.Time{}, changed: make(chan struct{})}
}

func (h *Hub) Online(clientID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Since(h.seen[clientID]) < 75*time.Second
}

func (h *Hub) Claim(ctx context.Context, clientID string, wait time.Duration) []Command {
	h.mu.Lock()
	result, changed := h.claim(clientID), h.changed
	h.mu.Unlock()
	if len(result) > 0 || wait <= 0 {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	select {
	case <-changed:
	case <-ctx.Done():
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.claim(clientID)
}

func (h *Hub) Notify() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notify()
}

func (h *Hub) notify() {
	close(h.changed)
	h.changed = make(chan struct{})
}

func (h *Hub) claim(clientID string) []Command {
	now := time.Now()
	h.seen[clientID] = now
	result := []Command{}
	for id, c := range h.commands {
		if now.After(c.expires) {
			delete(h.commands, id)
			continue
		}
		if c.clientID == clientID && !c.claimed && len(result) < 6 {
			c.claimed = true
			result = append(result, *c)
		}
	}
	return result
}

func (h *Hub) Complete(clientID, id string, reply Reply) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.commands[id]
	if c == nil || c.clientID != clientID || !c.claimed || time.Now().After(c.expires) {
		return shopping.ErrNotFound
	}
	delete(h.commands, id)
	c.done <- reply
	return nil
}

func (h *Hub) Do(ctx context.Context, clientID, retailerID, operation string, input any) (json.RawMessage, error) {
	if !h.Online(clientID) {
		return nil, errors.New("Open Shop It in your browser to reconnect the extension.")
	}
	ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	c := &Command{ID: "command-" + rand.Text(), RetailerID: retailerID, Operation: operation, Input: input, clientID: clientID, expires: time.Now().Add(65 * time.Second), done: make(chan Reply, 1)}
	h.mu.Lock()
	h.commands[c.ID] = c
	h.notify()
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.commands, c.ID); h.mu.Unlock() }()
	select {
	case <-ctx.Done():
		return nil, shopping.ErrTimeout
	case reply := <-c.done:
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		if len(reply.Result) == 0 {
			return nil, shopping.ErrUpstream
		}
		return reply.Result, nil
	}
}

func (h *Hub) Remember(session Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session.CheckedAt = time.Now().UTC()
	h.sessions[session.RetailerID] = session
}

func (h *Hub) Sessions() []Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := []Session{}
	for _, s := range h.sessions {
		if time.Since(h.seen[s.ClientID]) >= 75*time.Second {
			s.Status = "offline"
		}
		result = append(result, s)
	}
	return result
}

func (h *Hub) Session(retailerID string) (Session, error) {
	for _, s := range h.Sessions() {
		if s.RetailerID == retailerID && s.Status == "ready" {
			return s, nil
		}
	}
	return Session{}, errors.New("Connect this store in Shop It before searching.")
}
