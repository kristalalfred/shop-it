package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/kristalalfred/shop-it/internal/browser"
	"github.com/kristalalfred/shop-it/internal/retailer"
	"github.com/kristalalfred/shop-it/internal/shopping"
)

type browserCatalog struct {
	hub        *browser.Hub
	retailerID string
}

func (c browserCatalog) Search(ctx context.Context, input shopping.SearchRequest) (shopping.SearchResult, error) {
	session, err := c.hub.Session(c.retailerID)
	if err != nil {
		return shopping.SearchResult{}, problem{http.StatusConflict, err.Error()}
	}
	if !retailer.SameContext(session.Context, input.Context) {
		return shopping.SearchResult{}, shopping.ErrConflict
	}
	page := 0
	if input.Cursor != "" {
		page, err = strconv.Atoi(input.Cursor)
		if err != nil || page < 1 || page > 24 {
			return shopping.SearchResult{}, shopping.ErrInvalid
		}
	}
	raw, err := c.hub.Do(ctx, session.ClientID, c.retailerID, "search", map[string]any{"query": input.Query, "page": page, "context": input.Context})
	if err != nil {
		return shopping.SearchResult{}, err
	}
	products, more, err := retailer.ParseStorefront(c.retailerID, raw, page)
	if err != nil {
		return shopping.SearchResult{}, err
	}
	result := shopping.SearchResult{RetailerID: c.retailerID, Context: session.Context, ObservedAt: time.Now().UTC(), Products: products}
	if more && page < 24 {
		result.NextCursor = strconv.Itoa(page + 1)
	}
	return result, retailer.ValidateResult(input, result)
}

func (s *Server) registerCommands(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/client/commands", s.client(func(w http.ResponseWriter, r *http.Request, clientID string) {
		wait := 0
		if raw := r.URL.Query().Get("wait"); raw != "" {
			var err error
			if wait, err = strconv.Atoi(raw); err != nil || wait < 0 || wait > 25 {
				fail(w, shopping.ErrInvalid)
				return
			}
		}
		respond(w, 200, map[string]any{"commands": s.browser.Claim(r.Context(), clientID, time.Duration(wait)*time.Second)})
	}))
	mux.HandleFunc("PUT /v1/client/commands/{id}/result", s.client(func(w http.ResponseWriter, r *http.Request, clientID string) {
		var reply browser.Reply
		if !decode(w, r, &reply) {
			return
		}
		if err := s.browser.Complete(clientID, r.PathValue("id"), reply); err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	}))
}

func (s *Server) connectStore(ctx context.Context, retailerID string) (browser.Session, error) {
	var configured *RetailerConfig
	for _, r := range s.config.Retailers {
		if r.ID == retailerID {
			configured = &r
			break
		}
	}
	if configured == nil {
		return browser.Session{}, shopping.ErrInvalid
	}
	if configured.Mode != "browser" {
		session := browser.Session{RetailerID: retailerID, Status: "ready", Label: configured.Name + " demo", Context: configured.Context(), CheckedAt: time.Now().UTC()}
		return session, nil
	}
	clientID := ""
	for _, c := range s.config.Clients {
		if s.browser.Online(c.ID) {
			clientID = c.ID
			break
		}
	}
	if clientID == "" {
		return browser.Session{}, problem{http.StatusConflict, "Connect the Shop It browser extension, then try again."}
	}
	raw, err := s.browser.Do(ctx, clientID, retailerID, "session", nil)
	if err != nil {
		return browser.Session{}, err
	}
	var session browser.Session
	if json.Unmarshal(raw, &session) != nil || session.Status != "ready" && session.Status != "setup_required" && session.Status != "login_required" {
		return browser.Session{}, shopping.ErrUpstream
	}
	if session.Status == "ready" && (!validContext(session.Context) || session.Context.Fulfillment == "") {
		return browser.Session{}, shopping.ErrUpstream
	}
	session.RetailerID, session.ClientID = retailerID, clientID
	s.browser.Remember(session)
	return session, nil
}
