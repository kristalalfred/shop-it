package testenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

type SeedTransfer struct {
	Name    string                   `json:"name"`
	Key     string                   `json:"key"`
	Request shopping.TransferRequest `json:"request"`
	Claimed bool                     `json:"claimed,omitempty"`
	Receipt *shopping.Receipt        `json:"receipt,omitempty"`
}

func (e *Environment) Seed(ctx context.Context, seeds []SeedTransfer) (map[string]string, error) {
	seeds = slices.Clone(seeds)
	slices.SortStableFunc(seeds, func(a, b SeedTransfer) int {
		rank := func(seed SeedTransfer) int {
			if seed.Claimed || seed.Receipt != nil {
				return 0
			}
			return 1
		}
		return rank(a) - rank(b)
	})
	variables := map[string]string{}
	for _, seed := range seeds {
		name := "seed_" + seed.Name + "_id"
		if seed.Name == "" || variables[name] != "" {
			return nil, fmt.Errorf("seed names must be nonempty and unique")
		}
		var clientToken string
		for _, client := range e.config.Clients {
			if client.ID == seed.Request.ClientID {
				clientToken = client.Token
			}
		}
		if clientToken == "" {
			return nil, fmt.Errorf("seed %s refers to an unknown client", seed.Name)
		}
		var transfer shopping.Transfer
		if err := e.request(ctx, "POST", "/v1/basket-transfers", UserToken, seed.Key, seed.Request, &transfer, 202); err != nil {
			return nil, fmt.Errorf("seed %s: %w", seed.Name, err)
		}
		variables[name] = transfer.ID
		if seed.Claimed || seed.Receipt != nil {
			var claimed shopping.Transfer
			if err := e.request(ctx, "POST", "/v1/client/claims", clientToken, "", nil, &claimed, 200); err != nil {
				return nil, err
			}
			if claimed.ID != transfer.ID {
				return nil, fmt.Errorf("seed %s claimed a different transfer", seed.Name)
			}
		}
		if seed.Receipt != nil {
			if err := e.request(ctx, "PUT", "/v1/client/transfers/"+transfer.ID+"/result", clientToken, "", seed.Receipt, nil, 200); err != nil {
				return nil, err
			}
		}
	}
	return variables, nil
}

func (e *Environment) request(ctx context.Context, method, path, token, key string, input, output any, expected int) error {
	var payload []byte
	if input != nil {
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, e.URL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, err := e.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != expected {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("%s %s: HTTP %d, want %d: %s", method, path, res.StatusCode, expected, body)
	}
	if output != nil {
		return json.NewDecoder(res.Body).Decode(output)
	}
	_, err = io.Copy(io.Discard, res.Body)
	return err
}

func (e *Environment) registerControls(mux *http.ServeMux) {
	mux.HandleFunc("GET /catalog/requests", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"requests": e.CatalogRequests()})
	})
	mux.HandleFunc("POST /browser/step", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != ClientToken && token != OtherToken {
			http.Error(w, "unknown test client", 401)
			return
		}
		claimed, err := e.Browser.Step(r.Context(), e.URL, token)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]bool{"claimed": claimed})
	})
	mux.HandleFunc("POST /basket", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RetailerID string                `json:"retailer_id"`
			Context    shopping.StoreContext `json:"context"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid basket lookup", 400)
			return
		}
		items := e.Browser.Basket(req.RetailerID, req.Context)
		writeJSON(w, map[string]any{"items": items, "item_count": len(items)})
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
