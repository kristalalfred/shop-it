package backend

import (
	"net/http"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func (s *Server) registerBrowser(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/clients/{id}/pair", s.user(func(w http.ResponseWriter, r *http.Request) {
		for _, client := range s.config.Clients {
			if client.ID == r.PathValue("id") {
				respond(w, 200, client)
				return
			}
		}
		fail(w, shopping.ErrNotFound)
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
