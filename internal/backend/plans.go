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
	var req shopping.InterpretationRequest
	if !decode(w, r, &req) {
		return
	}
	if !validText(req.Text) || !interpret.ValidTraits(req.RequiredTraits) {
		fail(w, shopping.ErrInvalid)
		return
	}
	result, err := s.interpreter.Interpret(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	for i := range result.Items {
		result.Items[i].RequiredTraits = interpret.Unique(append(result.Items[i].RequiredTraits, req.RequiredTraits...))
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
	intent, err := s.interpreter.Interpret(r.Context(), shopping.InterpretationRequest{Text: req.Text, RequiredTraits: req.RequiredTraits})
	if err != nil {
		fail(w, err)
		return
	}
	plan := shopping.Plan{PlanRequest: req, InterpreterMode: intent.Mode, Warnings: intent.Warnings, Lines: make([]shopping.PlanLine, len(intent.Items))}
	for i, item := range intent.Items {
		item.RequiredTraits = interpret.Unique(append(item.RequiredTraits, req.RequiredTraits...))
		plan.Lines[i] = shopping.PlanLine{ID: "line-" + rand.Text(), Item: item}
	}
	plan.Lines, err = s.matchLines(r.Context(), plan, false)
	if err != nil {
		fail(w, err)
		return
	}
	plan, err = s.plans.Create(r.Context(), plan)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Location", "/v1/shopping-plans/"+plan.ID)
	respond(w, 201, plan)
}

func (s *Server) matchLines(ctx context.Context, plan shopping.Plan, preserve bool) ([]shopping.PlanLine, error) {
	lines := make([]shopping.PlanLine, len(plan.Lines))
	failures := make([]error, len(plan.Lines))
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	limit := make(chan struct{}, 6)
	for i, old := range plan.Lines {
		if old.Skipped {
			lines[i] = old
			continue
		}
		wg.Go(func() {
			var line shopping.PlanLine
			var err error
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
				if s.productMatcher == nil {
					line, err = planner.Match(searchCtx, s.catalogs[plan.RetailerID], plan.PlanRequest, old.Item, old.ID)
				} else {
					line, err = planner.Search(searchCtx, s.catalogs[plan.RetailerID], plan.PlanRequest, old.Item, old.ID, 40)
				}
			case <-searchCtx.Done():
				err = searchCtx.Err()
			}
			if err != nil {
				line = shopping.PlanLine{ID: old.ID, Item: old.Item, Candidates: []shopping.Candidate{}, Issue: "The shop search failed for this item. Search again or refresh prices to retry."}
			}
			lines[i], failures[i] = line, err
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var failure error
	searched := false
	for i, err := range failures {
		if !plan.Lines[i].Skipped && err == nil {
			searched = true
		} else if err != nil && failure == nil {
			failure = err
		}
	}
	if failure != nil && !searched {
		if errors.Is(failure, context.DeadlineExceeded) {
			return nil, shopping.ErrTimeout
		}
		return nil, failure
	}
	if s.productMatcher != nil {
		requests := []shopping.ProductMatchRequest{}
		for _, line := range lines {
			if line.Skipped || len(line.Candidates) == 0 {
				continue
			}
			products := make([]shopping.ProductOption, len(line.Candidates))
			for i, candidate := range line.Candidates {
				products[i] = shopping.ProductOption{ID: candidate.Product.ID, Name: candidate.Product.Name, Brand: candidate.Product.Brand}
			}
			requests = append(requests, shopping.ProductMatchRequest{ID: line.ID, Item: line.Item, Products: products})
		}
		matches, err := s.productMatcher.MatchProducts(ctx, requests)
		if err != nil {
			return nil, err
		}
		byID := make(map[string][]string, len(matches))
		for _, match := range matches {
			byID[match.ID] = match.ProductIDs
		}
		for i := range lines {
			if !lines[i].Skipped && failures[i] == nil {
				planner.ApplyMatches(&lines[i], byID[lines[i].ID])
			}
		}
	}
	if preserve {
		for i := range lines {
			line, old := &lines[i], plan.Lines[i]
			if old.Skipped || old.Selected == nil || failures[i] != nil {
				continue
			}
			line.Selected = nil
			for _, candidate := range line.Candidates {
				if candidate.Eligible && candidate.Product.ID == old.Selected.Product.ID {
					chosen := candidate
					line.Selected = &chosen
					break
				}
			}
			if line.Selected == nil {
				line.Issue = "No previously selected product available after refreshing."
			}
		}
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
		updated := plan
		updated.Lines = []shopping.PlanLine{{ID: line.ID, Item: item}}
		var lines []shopping.PlanLine
		lines, err = s.matchLines(r.Context(), updated, false)
		if err != nil {
			fail(w, err)
			return
		}
		line = lines[0]
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
	lines, err := s.matchLines(r.Context(), plan, true)
	if err != nil {
		fail(w, err)
		return
	}
	plan, err = s.plans.Update(r.Context(), plan.ID, req.Revision, func(p *shopping.Plan) error { p.Lines = lines; return nil })
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
