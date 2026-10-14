package backend

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kristalalfred/ship-it/internal/interpret"
	"github.com/kristalalfred/ship-it/internal/planner"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

func (s *Server) registerPlans(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/clients", s.user(func(w http.ResponseWriter, r *http.Request) {
		clients := []map[string]string{}
		for _, client := range s.config.Clients {
			clients = append(clients, map[string]string{"id": client.ID})
		}
		respond(w, 200, map[string]any{"clients": clients})
	}))
	mux.HandleFunc("POST /v1/interpretations", s.user(s.interpretText))
	mux.HandleFunc("POST /v1/shopping-plans", s.user(s.createPlan))
	mux.HandleFunc("GET /v1/shopping-plans", s.user(s.listPlans))
	mux.HandleFunc("GET /v1/shopping-plans/{id}", s.user(s.getPlan))
	mux.HandleFunc("PATCH /v1/shopping-plans/{id}/lines/{line}", s.user(s.updateLine))
	mux.HandleFunc("POST /v1/shopping-plans/{id}/refresh", s.user(s.refreshPlan))
	mux.HandleFunc("POST /v1/shopping-plans/{id}/transfer", s.user(s.transferPlan))
}

func validText(text string) bool {
	return strings.TrimSpace(text) != "" && len(text) <= 16000 && utf8.ValidString(text)
}

func (s *Server) interpretText(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validText(req.Text) {
		fail(w, shopping.ErrInvalid)
		return
	}
	result, err := s.interpreter.Interpret(r.Context(), req.Text)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, result)
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	var req shopping.PlanRequest
	if !decode(w, r, &req) {
		return
	}
	if !validText(req.Text) || !validContext(req.Context) || !interpret.ValidTraits(req.RequiredTraits) || req.BudgetMinor < 0 || req.BudgetMinor > 100000000 {
		fail(w, shopping.ErrInvalid)
		return
	}
	if _, ok := s.catalogs[req.RetailerID]; !ok {
		fail(w, shopping.ErrNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	intent, err := s.interpreter.Interpret(ctx, req.Text)
	if err != nil {
		fail(w, err)
		return
	}
	plan := shopping.Plan{PlanRequest: req, InterpreterMode: intent.Mode, Warnings: intent.Warnings, Lines: make([]shopping.PlanLine, len(intent.Items))}
	for i, item := range intent.Items {
		item.RequiredTraits = interpret.Unique(append(item.RequiredTraits, req.RequiredTraits...))
		plan.Lines[i] = shopping.PlanLine{ID: "line-" + rand.Text(), Item: item}
	}
	plan.Lines, err = s.matchLines(ctx, plan, false)
	if err != nil {
		fail(w, err)
		return
	}
	plan, err = s.plans.Create(ctx, plan)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Location", "/v1/shopping-plans/"+plan.ID)
	respond(w, 201, plan)
}

func (s *Server) matchLines(ctx context.Context, plan shopping.Plan, preserve bool) ([]shopping.PlanLine, error) {
	lines := make([]shopping.PlanLine, len(plan.Lines))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var firstError error
	var mu sync.Mutex
	limit := make(chan struct{}, 6)
	for i, old := range plan.Lines {
		wg.Go(func() {
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-limit }()
			if old.Skipped {
				lines[i] = old
				return
			}
			line, err := planner.Match(ctx, s.catalogs[plan.RetailerID], plan.PlanRequest, old.Item, old.ID)
			if err != nil {
				mu.Lock()
				if firstError == nil {
					firstError = err
					cancel()
				}
				mu.Unlock()
				return
			}
			if preserve {
				line.Selected = nil
				if old.Selected != nil {
					for _, c := range line.Candidates {
						if c.Eligible && c.Product.ID == old.Selected.Product.ID {
							chosen := c
							line.Selected = &chosen
							break
						}
					}
				}
				if line.Selected == nil {
					line.Issue = "Review this line after refreshing the catalog."
				}
			}
			lines[i] = line
		})
	}
	wg.Wait()
	if firstError != nil {
		return nil, firstError
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, shopping.ErrTimeout
		}
		return nil, err
	}
	return lines, nil
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := s.plans.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.plans.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, map[string]any{"plans": plans})
}

func (s *Server) updateLine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Revision  int            `json:"revision"`
		Item      *shopping.Item `json:"item,omitempty"`
		ProductID *string        `json:"product_id,omitempty"`
		Skipped   *bool          `json:"skipped,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Revision < 1 || req.Item == nil && req.ProductID == nil && req.Skipped == nil {
		fail(w, shopping.ErrInvalid)
		return
	}
	plan, err := s.plans.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if plan.Revision != req.Revision {
		fail(w, shopping.ErrConflict)
		return
	}
	index := slices.IndexFunc(plan.Lines, func(line shopping.PlanLine) bool { return line.ID == r.PathValue("line") })
	if index < 0 {
		fail(w, shopping.ErrNotFound)
		return
	}
	line := plan.Lines[index]
	if req.Item != nil {
		if !interpret.ValidItem(*req.Item) {
			fail(w, shopping.ErrInvalid)
			return
		}
		item := *req.Item
		item.RequiredTraits = interpret.Unique(append(item.RequiredTraits, plan.RequiredTraits...))
		line, err = planner.Match(r.Context(), s.catalogs[plan.RetailerID], plan.PlanRequest, item, line.ID)
		if err != nil {
			fail(w, err)
			return
		}
	}
	if req.ProductID != nil {
		line.Selected = nil
		if *req.ProductID != "" {
			for _, c := range line.Candidates {
				if c.Product.ID == *req.ProductID && c.Eligible {
					chosen := c
					line.Selected = &chosen
					break
				}
			}
			if line.Selected == nil {
				fail(w, shopping.ErrInvalid)
				return
			}
			line.Issue = ""
		}
	}
	if req.Skipped != nil {
		line.Skipped = *req.Skipped
	}
	plan, err = s.plans.Update(r.Context(), plan.ID, req.Revision, func(p *shopping.Plan) error { p.Lines[index] = line; return nil })
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) refreshPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Revision int `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	plan, err := s.plans.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if plan.Revision != req.Revision {
		fail(w, shopping.ErrConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	lines, err := s.matchLines(ctx, plan, true)
	if err != nil {
		fail(w, err)
		return
	}
	plan, err = s.plans.Update(ctx, plan.ID, req.Revision, func(p *shopping.Plan) error { p.Lines = lines; return nil })
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) transferPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Revision            int    `json:"revision"`
		ClientID            string `json:"client_id"`
		AcknowledgeWarnings bool   `json:"acknowledge_warnings"`
		AllowOverBudget     bool   `json:"allow_over_budget"`
	}
	if !decode(w, r, &req) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if req.Revision < 1 || !validID(key) || !s.hasClient(req.ClientID) {
		fail(w, shopping.ErrInvalid)
		return
	}
	var transfer shopping.Transfer
	var created bool
	err := s.plans.Review(r.Context(), r.PathValue("id"), req.Revision, func(plan shopping.Plan) error {
		if len(plan.Warnings) > 0 && !req.AcknowledgeWarnings || plan.OverBudget && !req.AllowOverBudget {
			return shopping.ErrConflict
		}
		items, err := planner.BasketItems(plan)
		if err != nil {
			return err
		}
		basketURL := ""
		for _, retailer := range s.config.Retailers {
			if retailer.ID == plan.RetailerID {
				basketURL = retailer.BasketURL
			}
		}
		input := shopping.TransferRequest{RetailerID: plan.RetailerID, ClientID: req.ClientID, Context: plan.Context, Items: items}
		transfer, created, err = s.transfers.Create(r.Context(), input, "plan:"+plan.ID+":"+key, basketURL)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if created {
		status = 202
	}
	w.Header().Set("Location", "/v1/basket-transfers/"+transfer.ID)
	respond(w, status, transfer)
}
