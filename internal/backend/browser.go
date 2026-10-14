package backend

import (
	"net/http"
	"strings"

	"github.com/kristalalfred/ship-it/internal/retailer"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

func (s *Server) browserMode(id string) bool {
	for _, config := range s.config.Retailers {
		if config.ID == id {
			return config.Mode == "browser"
		}
	}
	return false
}

func (s *Server) transferSession(retailerID, clientID string, context shopping.StoreContext) bool {
	return !s.browserMode(retailerID) || s.bridge.HasSession(retailerID, clientID, context, true)
}

func (s *Server) registerBrowser(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/browser-sessions", s.user(func(w http.ResponseWriter, r *http.Request) {
		sessions := []retailer.Session{}
		if s.bridge != nil {
			sessions = s.bridge.Sessions()
		}
		respond(w, 200, map[string]any{"sessions": sessions})
	}))
	mux.HandleFunc("POST /v1/clients/{id}/pair", s.user(func(w http.ResponseWriter, r *http.Request) {
		for _, client := range s.config.Clients {
			if client.ID == r.PathValue("id") {
				respond(w, 200, client)
				return
			}
		}
		fail(w, shopping.ErrNotFound)
	}))
	mux.HandleFunc("POST /v1/client/sessions", s.client(func(w http.ResponseWriter, r *http.Request, clientID string) {
		var input struct {
			Sessions []retailer.Session `json:"sessions"`
		}
		if !decode(w, r, &input) {
			return
		}
		if s.bridge == nil || input.Sessions == nil || len(input.Sessions) > 3 {
			fail(w, shopping.ErrInvalid)
			return
		}
		seen := map[string]bool{}
		for _, session := range input.Sessions {
			if !s.browserMode(session.RetailerID) || seen[session.RetailerID] || !validContext(session.Context) || !validID(session.Context.SessionID) || strings.TrimSpace(session.Name) == "" || len(session.Name) > 200 {
				fail(w, shopping.ErrInvalid)
				return
			}
			seen[session.RetailerID] = true
		}
		s.bridge.Publish(clientID, input.Sessions)
		respond(w, 200, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("POST /v1/client/search-claims", s.client(func(w http.ResponseWriter, r *http.Request, clientID string) {
		jobs := []retailer.SearchJob{}
		if s.bridge != nil {
			jobs = s.bridge.Claim(clientID)
		}
		respond(w, 200, map[string]any{"jobs": jobs})
	}))
	mux.HandleFunc("PUT /v1/client/searches/{id}/result", s.client(func(w http.ResponseWriter, r *http.Request, clientID string) {
		var reply retailer.SearchReply
		if !decode(w, r, &reply) {
			return
		}
		if s.bridge == nil {
			fail(w, shopping.ErrNotFound)
			return
		}
		if err := s.bridge.Complete(clientID, r.PathValue("id"), reply); err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("POST /v1/client/transfers/{id}/renew", s.client(func(w http.ResponseWriter, r *http.Request, clientID string) {
		transfer, err := s.transfers.Renew(r.Context(), r.PathValue("id"), clientID)
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, transfer)
	}))
}
