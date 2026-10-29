package backend

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kristalalfred/shop-it/internal/interpret"
	"github.com/kristalalfred/shop-it/internal/planner"
	"github.com/kristalalfred/shop-it/internal/shopping"
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
	mux.HandleFunc("PATCH /v1/shopping-plans/{id}", s.user(s.chooseBasket))
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
	if !validText(req.Text) {
		fail(w, shopping.ErrInvalid)
		return
	}
	result, err := s.interpreter.Interpret(r.Context(), req, nil)
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
	if !validText(req.Text) || req.BudgetMinor < 0 || req.BudgetMinor > 100000000 {
		fail(w, shopping.ErrInvalid)
		return
	}
	if !accepts(r, "application/x-ndjson") {
		plan, err := s.newPlan(r.Context(), req, ignoreProgress)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Location", "/v1/shopping-plans/"+plan.ID)
		respond(w, 201, plan)
		return
	}
	stream := startStream(w)
	plan, err := s.newPlan(r.Context(), req, func(progress shopping.Progress) { stream.send(map[string]any{"progress": progress}) })
	if err != nil {
		status, message := failure(err)
		stream.send(map[string]any{"status": status, "error": message})
		return
	}
	stream.send(map[string]any{"plan": plan})
}

func (s *Server) newPlan(ctx context.Context, req shopping.PlanRequest, report func(shopping.Progress)) (shopping.Plan, error) {
	report(shopping.Progress{Stage: "interpret"})
	intent, err := s.interpreter.Interpret(ctx, shopping.InterpretationRequest{Text: req.Text}, func(note string) {
		report(shopping.Progress{Stage: "interpret", Note: note})
	})
	if err != nil {
		return shopping.Plan{}, err
	}
	plan := shopping.Plan{PlanRequest: req, InterpreterMode: intent.Mode, Warnings: intent.Warnings, Baskets: s.baskets(), Lines: make([]shopping.PlanLine, len(intent.Items))}
	for i, item := range intent.Items {
		plan.Lines[i] = shopping.PlanLine{ID: "line-" + rand.Text(), Item: item}
	}
	plan.Lines, err = s.searchLines(ctx, plan.Baskets, plan.Lines, false, report)
	if err != nil {
		return shopping.Plan{}, err
	}
	return s.plans.Create(ctx, plan)
}

func (s *Server) baskets() []shopping.Basket {
	baskets := make([]shopping.Basket, len(s.config.Retailers))
	for i, retailer := range s.config.Retailers {
		baskets[i] = shopping.Basket{RetailerID: retailer.ID, Context: retailer.Context()}
	}
	return baskets
}

func ignoreProgress(shopping.Progress) {}

type eventStream struct {
	mu         sync.Mutex
	encoder    *json.Encoder
	controller *http.ResponseController
}

func startStream(w http.ResponseWriter) *eventStream {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	return &eventStream{encoder: json.NewEncoder(w), controller: http.NewResponseController(w)}
}

func (s *eventStream) send(event any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.encoder.Encode(event) == nil {
		_ = s.controller.Flush()
	}
}

func (s *Server) searchLines(ctx context.Context, baskets []shopping.Basket, lines []shopping.PlanLine, preserve bool, report func(shopping.Progress)) ([]shopping.PlanLine, error) {
	limit := 500
	if s.productMatcher != nil {
		limit = 40
	}
	return s.searchLinesUsing(ctx, baskets, lines, preserve, report, s.catalogs, s.productMatcher, limit, true)
}

func (s *Server) searchLinesUsing(ctx context.Context, baskets []shopping.Basket, lines []shopping.PlanLine, preserve bool, report func(shopping.Progress), catalogs map[string]shopping.Catalog, matcher shopping.ProductMatcher, searchLimit int, autoSelect bool) ([]shopping.PlanLine, error) {
	found := make([][]shopping.BasketLine, len(lines))
	failures := make([][]error, len(lines))
	remaining := make([]int, len(lines))
	for i := range lines {
		found[i], failures[i], remaining[i] = make([]shopping.BasketLine, len(baskets)), make([]error, len(baskets)), len(baskets)
	}
	searchTimeout := 30 * time.Second
	if !autoSelect {
		searchTimeout = 90 * time.Second
	}
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	var wg sync.WaitGroup
	var searched sync.Mutex
	finished := 0
	report(shopping.Progress{Stage: "search", Total: len(lines)})
	for j, basket := range baskets {
		limit := make(chan struct{}, 6)
		catalog := catalogs[basket.RetailerID]
		for i, line := range lines {
			wg.Go(func() {
				var result shopping.BasketLine
				var err error
				select {
				case limit <- struct{}{}:
					defer func() { <-limit }()
					if catalog == nil {
						err = shopping.ErrNotFound
					} else {
						result, err = s.searchItem(searchCtx, catalog, basket, line.Item, searchLimit, autoSelect && matcher == nil)
					}
				case <-searchCtx.Done():
					err = searchCtx.Err()
				}
				if err != nil {
					result = shopping.BasketLine{Candidates: []shopping.Candidate{}, Issue: "The shop search failed for this item. Search again or refresh prices to retry."}
				}
				searched.Lock()
				defer searched.Unlock()
				found[i][j], failures[i][j] = result, err
				if remaining[i]--; remaining[i] == 0 {
					finished++
					report(shopping.Progress{Stage: "search", Done: finished, Total: len(lines)})
				}
			})
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var searchFailure error
	succeeded := false
	for _, errs := range failures {
		for _, err := range errs {
			if err == nil {
				succeeded = true
			} else if searchFailure == nil {
				searchFailure = err
			}
		}
	}
	if searchFailure != nil && !succeeded {
		if errors.Is(searchFailure, context.DeadlineExceeded) {
			return nil, shopping.ErrTimeout
		}
		return nil, searchFailure
	}
	if matcher != nil {
		if err := s.matchProducts(ctx, baskets, lines, found, failures, report, matcher); err != nil {
			return nil, err
		}
	}
	result := make([]shopping.PlanLine, len(lines))
	for i, line := range lines {
		result[i] = shopping.PlanLine{ID: line.ID, Item: line.Item, Skipped: line.Skipped, Baskets: make(map[string]shopping.BasketLine, len(baskets))}
		for j, basket := range baskets {
			entry, old := found[i][j], line.Baskets[basket.RetailerID]
			if preserve && old.Selected != nil && failures[i][j] == nil {
				entry.Selected = nil
				for _, candidate := range entry.Candidates {
					if candidate.Eligible && candidate.Product.ID == old.Selected.Product.ID {
						chosen := candidate
						entry.Selected = &chosen
						break
					}
				}
				if entry.Selected == nil {
					entry.Issue = "No previously selected product available after refreshing."
				}
			}
			result[i].Baskets[basket.RetailerID] = entry
		}
	}
	return result, nil
}

func (s *Server) searchItem(ctx context.Context, catalog shopping.Catalog, basket shopping.Basket, item shopping.Item, limit int, autoSelect bool) (shopping.BasketLine, error) {
	line, err := planner.Search(ctx, catalog, basket, item, limit)
	if broader := interpret.WithoutTraitWords(item.Query, item.RequiredTraits); err == nil && len(line.Candidates) == 0 && broader != "" && broader != item.Query {
		item.Query = broader
		line, err = planner.Search(ctx, catalog, basket, item, limit)
	}
	if err == nil && autoSelect {
		planner.Select(&line)
	}
	return line, err
}

func (s *Server) matchProducts(ctx context.Context, baskets []shopping.Basket, lines []shopping.PlanLine, found [][]shopping.BasketLine, failures [][]error, report func(shopping.Progress), matcher shopping.ProductMatcher) error {
	requests := make([][]shopping.ProductMatchRequest, len(baskets))
	for j := range baskets {
		for i, line := range lines {
			candidates := found[i][j].Candidates
			if failures[i][j] != nil || len(candidates) == 0 {
				continue
			}
			products := make([]shopping.ProductOption, len(candidates))
			for k, candidate := range candidates {
				products[k] = shopping.ProductOption{ID: candidate.Product.ID, Name: candidate.Product.Name, Brand: candidate.Product.Brand}
			}
			requests[j] = append(requests[j], shopping.ProductMatchRequest{ID: line.ID, Item: line.Item, Products: products})
		}
	}
	if slices.ContainsFunc(requests, func(batch []shopping.ProductMatchRequest) bool { return len(batch) > 0 }) {
		report(shopping.Progress{Stage: "match"})
	}
	matches := make([]map[string]shopping.ProductMatch, len(baskets))
	errs := make([]error, len(baskets))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for j, batch := range requests {
		if len(batch) == 0 {
			continue
		}
		wg.Go(func() {
			result, err := matcher.MatchProducts(ctx, batch, func(note string) {
				report(shopping.Progress{Stage: "match", Note: note})
			})
			if err != nil {
				errs[j] = err
				cancel()
				return
			}
			matches[j] = make(map[string]shopping.ProductMatch, len(result))
			for _, match := range result {
				matches[j][match.ID] = match
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	for j := range baskets {
		for i, line := range lines {
			if failures[i][j] == nil {
				planner.ApplyMatches(&found[i][j], matches[j][line.ID])
			}
		}
	}
	return nil
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

type lineUpdate struct {
	Revision  int            `json:"revision"`
	Item      *shopping.Item `json:"item,omitempty"`
	ProductID *string        `json:"product_id,omitempty"`
	Skipped   *bool          `json:"skipped,omitempty"`
}

type transferOptions struct {
	Revision            int    `json:"revision"`
	ClientID            string `json:"client_id"`
	AcknowledgeWarnings bool   `json:"acknowledge_warnings"`
	AllowOverBudget     bool   `json:"allow_over_budget"`
}

func (s *Server) chooseBasket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Revision   int    `json:"revision"`
		RetailerID string `json:"retailer_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	plan, err := s.choose(r.Context(), r.PathValue("id"), req.Revision, req.RetailerID)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) choose(ctx context.Context, id string, revision int, retailerID string) (shopping.Plan, error) {
	return s.plans.Update(ctx, id, revision, func(p *shopping.Plan) error {
		if !slices.ContainsFunc(p.Baskets, func(b shopping.Basket) bool { return b.RetailerID == retailerID }) {
			return shopping.ErrInvalid
		}
		p.RetailerID = retailerID
		return nil
	})
}

func (s *Server) updateLine(w http.ResponseWriter, r *http.Request) {
	var req lineUpdate
	if !decode(w, r, &req) {
		return
	}
	plan, err := s.editLine(r.Context(), r.PathValue("id"), r.PathValue("line"), req)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) editLine(ctx context.Context, id, lineID string, req lineUpdate) (shopping.Plan, error) {
	if req.Revision < 1 || req.Item == nil && req.ProductID == nil && req.Skipped == nil {
		return shopping.Plan{}, shopping.ErrInvalid
	}
	plan, err := s.plans.Get(ctx, id)
	if err != nil {
		return shopping.Plan{}, err
	}
	if plan.Revision != req.Revision {
		return shopping.Plan{}, shopping.ErrConflict
	}
	index := slices.IndexFunc(plan.Lines, func(line shopping.PlanLine) bool { return line.ID == lineID })
	if index < 0 {
		return shopping.Plan{}, shopping.ErrNotFound
	}
	line := plan.Lines[index]
	if req.Item != nil {
		if !interpret.ValidItem(*req.Item) {
			return shopping.Plan{}, shopping.ErrInvalid
		}
		var lines []shopping.PlanLine
		var err error
		if plan.InterpreterMode == "chatgpt" {
			lines, err = s.searchPluginLines(ctx, plan, []shopping.PlanLine{{ID: line.ID, Item: *req.Item}}, false)
		} else {
			lines, err = s.searchLines(ctx, plan.Baskets, []shopping.PlanLine{{ID: line.ID, Item: *req.Item}}, false, ignoreProgress)
		}
		if err != nil {
			return shopping.Plan{}, err
		}
		line = lines[0]
	}

	if req.ProductID != nil {
		if err := selectProduct(&line, plan.RetailerID, *req.ProductID); err != nil {
			return shopping.Plan{}, err
		}
	}

	if req.Skipped != nil {
		line.Skipped = *req.Skipped
	}
	return s.plans.Update(ctx, plan.ID, req.Revision, func(p *shopping.Plan) error { p.Lines[index] = line; return nil })
}

func selectProduct(line *shopping.PlanLine, retailerID, productID string) error {
	entry, ok := line.Baskets[retailerID]
	if !ok {
		return shopping.ErrInvalid
	}
	entry.Selected = nil
	if productID != "" {
		for _, candidate := range entry.Candidates {
			if candidate.Product.ID == productID && candidate.Eligible {
				chosen := candidate
				entry.Selected = &chosen
				break
			}
		}
		if entry.Selected == nil {
			return shopping.ErrInvalid
		}
		entry.Issue = ""
	}
	line.Baskets[retailerID] = entry
	return nil
}

func (s *Server) refreshPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Revision int `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	plan, err := s.refresh(r.Context(), r.PathValue("id"), req.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) refresh(ctx context.Context, id string, revision int) (shopping.Plan, error) {
	plan, err := s.plans.Get(ctx, id)
	if err != nil {
		return shopping.Plan{}, err
	}
	if plan.Revision != revision {
		return shopping.Plan{}, shopping.ErrConflict
	}
	baskets := s.baskets()
	var lines []shopping.PlanLine
	if plan.InterpreterMode == "chatgpt" {
		ids := make([]string, len(plan.Baskets))
		for i, basket := range plan.Baskets {
			ids[i] = basket.RetailerID
		}
		var catalogs map[string]shopping.Catalog
		baskets, catalogs, err = s.pluginBaskets(ids)
		if err == nil {
			lines, err = s.searchLinesUsing(ctx, baskets, plan.Lines, true, ignoreProgress, catalogs, nil, 20, false)
		}
	} else {
		lines, err = s.searchLines(ctx, baskets, plan.Lines, true, ignoreProgress)
	}
	if err != nil {
		return shopping.Plan{}, err
	}
	return s.plans.Update(ctx, id, revision, func(p *shopping.Plan) error { p.Lines, p.Baskets = lines, baskets; return nil })
}

func (s *Server) transferPlan(w http.ResponseWriter, r *http.Request) {
	var req transferOptions
	if !decode(w, r, &req) {
		return
	}
	transfer, created, err := s.fillPlan(r.Context(), r.PathValue("id"), req, r.Header.Get("Idempotency-Key"))
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

func (s *Server) fillPlan(ctx context.Context, id string, req transferOptions, key string) (shopping.Transfer, bool, error) {
	if req.Revision < 1 || !validID(key) || !s.hasClient(req.ClientID) {
		return shopping.Transfer{}, false, shopping.ErrInvalid
	}
	var transfer shopping.Transfer
	var created bool
	err := s.plans.Review(ctx, id, req.Revision, func(plan shopping.Plan) error {
		basket, _ := planner.Chosen(plan)

		if plan.InterpreterMode == "chatgpt" {
			if basket.Missing > 0 {
				return shopping.ErrConflict
			}
			for _, line := range plan.Lines {
				if !line.Skipped {
					selected := line.Baskets[basket.RetailerID].Selected
					if selected != nil && len(selected.UnmetTraits) > 0 && !req.AcknowledgeWarnings {
						return shopping.ErrConflict
					}
				}
			}
			for _, r := range s.config.Retailers {
				if r.ID == basket.RetailerID && r.Mode == "browser" {
					session, err := s.browser.Session(r.ID)
					if err != nil || session.ClientID != req.ClientID || !sameStore(session.Context, basket.Context) {
						return shopping.ErrConflict
					}
				}
			}
		}
		if len(plan.Warnings) > 0 && !req.AcknowledgeWarnings || basket.OverBudget && !req.AllowOverBudget {
			return shopping.ErrConflict
		}
		items, err := planner.BasketItems(plan)
		if err != nil {
			return err
		}
		basketURL := ""
		for _, r := range s.config.Retailers {
			if r.ID == basket.RetailerID {
				basketURL = r.BasketURL
			}
		}
		if basketURL == "" {
			return shopping.ErrNotFound
		}
		input := shopping.TransferRequest{RetailerID: basket.RetailerID, ClientID: req.ClientID, Context: basket.Context, Items: items}
		transfer, created, err = s.transfers.Create(ctx, input, "plan:"+plan.ID+":"+key, basketURL)
		return err
	})
	if created {
		s.browser.Notify()
	}
	return transfer, created, err
}
