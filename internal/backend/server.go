package backend

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kristalalfred/ship-it/internal/interpret"
	"github.com/kristalalfred/ship-it/internal/planner"
	"github.com/kristalalfred/ship-it/internal/retailer"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

type Server struct {
	config          Config
	catalogs        map[string]shopping.Catalog
	transfers       shopping.TransferStore
	plans           *planner.Store
	interpreter     shopping.Interpreter
	interpreterMode string
}

func New(config Config, transfers shopping.TransferStore) (http.Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	s := &Server{config: config, transfers: transfers, catalogs: make(map[string]shopping.Catalog)}
	planPath := ""
	if config.DataDir != "" {
		planPath = filepath.Join(config.DataDir, "plans.json")
	}
	var err error
	s.plans, err = planner.Open(planPath)
	if err != nil {
		return nil, err
	}
	s.interpreter, s.interpreterMode = interpret.Builtin{}, "builtin"
	if config.Interpreter.Mode == "openai" {
		key := os.Getenv("OPENAI_API_KEY")
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("OPENAI_API_KEY is required for the openai interpreter")
		}
		s.interpreter = interpret.NewOpenAI(key, config.Interpreter.Model)
		s.interpreterMode = "openai"
	}
	for _, r := range config.Retailers {
		s.catalogs[r.ID] = retailer.NewHTTP(r.GatewayURL, time.Duration(config.AdapterTimeoutMS)*time.Millisecond)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]any{"status": "ok", "mode": "service", "interpreter": s.interpreterMode, "persistent": config.DataDir != ""})
	})
	s.registerPlans(mux)
	s.registerWeb(mux)
	mux.HandleFunc("GET /v1/retailers", s.user(s.listRetailers))
	mux.HandleFunc("POST /v1/catalog/search", s.user(s.search))
	mux.HandleFunc("POST /v1/basket-transfers", s.user(s.createTransfer))
	mux.HandleFunc("GET /v1/basket-transfers/{id}", s.user(s.getTransfer))
	mux.HandleFunc("POST /v1/client/claims", s.client(s.claim))
	mux.HandleFunc("PUT /v1/client/transfers/{id}/result", s.client(s.complete))
	return mux, nil
}

func (s *Server) user(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !matchesToken(r, s.config.APIToken) {
			respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) client(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, client := range s.config.Clients {
			if matchesToken(r, client.Token) {
				next(w, r, client.ID)
				return
			}
		}
		respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
}

func matchesToken(r *http.Request, token string) bool {
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1
}

func (s *Server) listRetailers(w http.ResponseWriter, r *http.Request) {
	type retailerView struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Mode      string `json:"mode"`
		BasketURL string `json:"basket_url"`
	}
	retailers := make([]retailerView, 0, len(s.config.Retailers))
	for _, retailer := range s.config.Retailers {
		retailers = append(retailers, retailerView{retailer.ID, retailer.Name, retailer.Mode, retailer.BasketURL})
	}
	respond(w, http.StatusOK, map[string]any{"retailers": retailers})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var req shopping.SearchRequest
	if !decode(w, r, &req) {
		return
	}
	if !validContext(req.Context) || strings.TrimSpace(req.Query) == "" || len(req.Query) > 200 || len(req.Cursor) > 1024 {
		fail(w, shopping.ErrInvalid)
		return
	}
	catalog, ok := s.catalogs[req.RetailerID]
	if !ok {
		fail(w, shopping.ErrNotFound)
		return
	}
	result, err := catalog.Search(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, http.StatusOK, result)
}

func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	var req shopping.TransferRequest
	if !decode(w, r, &req) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !validID(key) || !validContext(req.Context) || len(req.Items) < 1 || len(req.Items) > 200 || !s.hasClient(req.ClientID) {
		fail(w, shopping.ErrInvalid)
		return
	}
	ids := map[string]bool{}
	for _, item := range req.Items {
		if strings.TrimSpace(item.ProductID) == "" || len(item.ProductID) > 256 || item.Quantity < 1 || item.Quantity > 1000 || ids[item.ProductID] {
			fail(w, shopping.ErrInvalid)
			return
		}
		ids[item.ProductID] = true
	}
	var basketURL string
	for _, retailer := range s.config.Retailers {
		if retailer.ID == req.RetailerID {
			basketURL = retailer.BasketURL
		}
	}
	if basketURL == "" {
		fail(w, shopping.ErrNotFound)
		return
	}
	t, created, err := s.transfers.Create(r.Context(), req, key, basketURL)
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	w.Header().Set("Location", "/v1/basket-transfers/"+t.ID)
	respond(w, status, t)
}

func (s *Server) hasClient(id string) bool {
	for _, client := range s.config.Clients {
		if client.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) getTransfer(w http.ResponseWriter, r *http.Request) {
	t, err := s.transfers.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, http.StatusOK, t)
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request, clientID string) {
	t, err := s.transfers.Claim(r.Context(), clientID)
	if errors.Is(err, shopping.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, http.StatusOK, t)
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request, clientID string) {
	var receipt shopping.Receipt
	if !decode(w, r, &receipt) {
		return
	}
	t, err := s.transfers.Complete(r.Context(), r.PathValue("id"), clientID, receipt)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, http.StatusOK, t)
}

func validContext(c shopping.StoreContext) bool {
	if strings.TrimSpace(c.StoreID) == "" || len(c.StoreID) > 256 || len(c.PostalCode) != 5 || len(c.DeliverySlotID) > 256 || len(c.Memberships) > 20 || (c.Fulfillment != "delivery" && c.Fulfillment != "pickup") {
		return false
	}
	for _, r := range c.PostalCode {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, membership := range c.Memberships {
		if !validID(membership) {
			return false
		}
	}
	return true
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		respond(w, http.StatusUnsupportedMediaType, map[string]string{"error": "application/json required"})
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respond(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request too large"})
		} else {
			fail(w, shopping.ErrInvalid)
		}
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, shopping.ErrInvalid)
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "internal error"
	for _, pair := range []struct {
		err    error
		status int
	}{
		{shopping.ErrInvalid, http.StatusBadRequest},
		{shopping.ErrNotFound, http.StatusNotFound},
		{shopping.ErrConflict, http.StatusConflict},
		{shopping.ErrTimeout, http.StatusGatewayTimeout},
		{shopping.ErrUpstream, http.StatusBadGateway},
	} {
		if errors.Is(err, pair.err) {
			status, message = pair.status, pair.err.Error()
			break
		}
	}
	respond(w, status, map[string]string{"error": message})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
