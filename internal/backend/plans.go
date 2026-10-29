package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kristalalfred/shop-it/internal/browser"
	"github.com/kristalalfred/shop-it/internal/planner"
	"github.com/kristalalfred/shop-it/internal/shopping"
)

const searchLimit = 20

type problem struct {
	status  int
	message string
}

func (p problem) Error() string { return p.message }

func (s *Server) registerPlans(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/clients", s.user(func(w http.ResponseWriter, r *http.Request) {
		clients := []map[string]string{}
		for _, client := range s.config.Clients {
			clients = append(clients, map[string]string{"id": client.ID})
		}
		respond(w, 200, map[string]any{"clients": clients})
	}))
	mux.HandleFunc("GET /v1/shopping-plans", s.user(s.listPlans))
	mux.HandleFunc("GET /v1/shopping-plans/{id}", s.user(s.getPlan))
	mux.HandleFunc("PATCH /v1/shopping-plans/{id}", s.user(s.chooseBasket))
	mux.HandleFunc("POST /v1/shopping-plans/{id}/refresh", s.user(s.refreshPlan))
	mux.HandleFunc("POST /v1/shopping-plans/{id}/transfer", s.user(s.transferPlan))
}

func validText(text string) bool {
	return strings.TrimSpace(text) != "" && len(text) <= 16000 && utf8.ValidString(text)
}

func (s *Server) retailer(id string) (RetailerConfig, bool) {
	i := slices.IndexFunc(s.config.Retailers, func(r RetailerConfig) bool { return r.ID == id })
	if i < 0 {
		return RetailerConfig{}, false
	}
	return s.config.Retailers[i], true
}

func (s *Server) storeBaskets(ids []string) ([]shopping.Basket, error) {
	baskets := []shopping.Basket{}
	for _, id := range ids {
		r, ok := s.retailer(id)
		if !ok || slices.ContainsFunc(baskets, func(b shopping.Basket) bool { return b.RetailerID == id }) {
			return nil, shopping.ErrInvalid
		}
		storeContext := r.Context()
		if r.Mode == "browser" {
			session, err := s.browser.Session(id)
			if err != nil {
				return nil, err
			}
			storeContext = session.Context
		}
		baskets = append(baskets, shopping.Basket{RetailerID: id, Context: storeContext})
	}
	if len(baskets) == 0 {
		return nil, errors.New("Choose and connect at least one store in Shop It.")
	}
	return baskets, nil
}

func (s *Server) readyStore(ctx context.Context, retailerID string) (browser.Session, error) {
	session, err := s.connectStore(ctx, retailerID)
	if err != nil {
		return session, err
	}
	r, _ := s.retailer(retailerID)
	switch session.Status {
	case "ready":
		return session, nil
	case "login_required":
		return session, problem{http.StatusConflict, fmt.Sprintf("Sign in at %s, then try again.", r.Name)}
	default:
		return session, problem{http.StatusConflict, fmt.Sprintf("Choose delivery or pickup at %s, then try again.", r.Name)}
	}
}

func (s *Server) searchLines(ctx context.Context, baskets []shopping.Basket, lines []shopping.PlanLine, preserve bool) ([]shopping.PlanLine, error) {
	found := make([][]shopping.BasketLine, len(lines))
	failures := make([][]error, len(lines))
	for i := range lines {
		found[i], failures[i] = make([]shopping.BasketLine, len(baskets)), make([]error, len(baskets))
	}
	searchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for j, basket := range baskets {
		limit := make(chan struct{}, 6)
		catalog := s.catalogs[basket.RetailerID]
		for i, line := range lines {
			wg.Go(func() {
				var err error
				select {
				case limit <- struct{}{}:
					defer func() { <-limit }()
					found[i][j], err = searchItem(searchCtx, catalog, basket, line.Item)
				case <-searchCtx.Done():
					err = searchCtx.Err()
				}
				if err != nil {
					found[i][j] = shopping.BasketLine{Candidates: []shopping.Candidate{}, Issue: "The shop search failed for this item. Search again or refresh prices to retry."}
				}
				failures[i][j] = err
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

func searchItem(ctx context.Context, catalog shopping.Catalog, basket shopping.Basket, item shopping.Item) (shopping.BasketLine, error) {
	if catalog == nil {
		return shopping.BasketLine{}, shopping.ErrNotFound
	}
	line, err := planner.Search(ctx, catalog, basket, item, searchLimit)
	if broader := planner.WithoutTraitWords(item.Query, item.RequiredTraits); err == nil && len(line.Candidates) == 0 && broader != "" && broader != item.Query {
		item.Query = broader
		line, err = planner.Search(ctx, catalog, basket, item, searchLimit)
	}
	return line, err
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

type fillOptions struct {
	Revision            int  `json:"revision"`
	AcknowledgeWarnings bool `json:"acknowledge_warnings"`
	AllowOverBudget     bool `json:"allow_over_budget"`
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
		if !planner.ValidItem(*req.Item) {
			return shopping.Plan{}, shopping.ErrInvalid
		}
		baskets, err := s.storeBaskets(retailerIDs(plan))
		if err != nil {
			return shopping.Plan{}, err
		}
		for i, b := range baskets {
			if !sameStore(b.Context, plan.Baskets[i].Context) {
				return shopping.Plan{}, errors.New("The store or delivery settings changed. Refresh prices before editing groceries.")
			}
		}
		lines, err := s.searchLines(ctx, baskets, []shopping.PlanLine{{ID: line.ID, Item: *req.Item}}, false)
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

func retailerIDs(plan shopping.Plan) []string {
	ids := make([]string, len(plan.Baskets))
	for i, b := range plan.Baskets {
		ids[i] = b.RetailerID
	}
	return ids
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
	ids := retailerIDs(plan)
	for _, id := range ids {
		if r, ok := s.retailer(id); ok && r.Mode == "browser" {
			if _, err := s.readyStore(ctx, id); err != nil {
				return shopping.Plan{}, err
			}
		}
	}
	baskets, err := s.storeBaskets(ids)
	if err != nil {
		return shopping.Plan{}, err
	}
	lines, err := s.searchLines(ctx, baskets, plan.Lines, true)
	if err != nil {
		return shopping.Plan{}, err
	}
	return s.plans.Update(ctx, id, revision, func(p *shopping.Plan) error { p.Lines, p.Baskets = lines, baskets; return nil })
}

func (s *Server) transferPlan(w http.ResponseWriter, r *http.Request) {
	var req fillOptions
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

func (s *Server) fillPlan(ctx context.Context, id string, req fillOptions, key string) (shopping.Transfer, bool, error) {
	if req.Revision < 1 || !validID(key) {
		return shopping.Transfer{}, false, shopping.ErrInvalid
	}
	plan, err := s.plans.Get(ctx, id)
	if err != nil {
		return shopping.Transfer{}, false, err
	}
	if plan.Revision != req.Revision {
		return shopping.Transfer{}, false, shopping.ErrConflict
	}
	basket, _ := planner.Chosen(plan)
	r, ok := s.retailer(basket.RetailerID)
	if !ok {
		return shopping.Transfer{}, false, shopping.ErrNotFound
	}
	clientID := s.config.Clients[0].ID
	if r.Mode == "browser" {
		session, err := s.readyStore(ctx, r.ID)
		if err != nil {
			return shopping.Transfer{}, false, err
		}
		if !sameStore(session.Context, basket.Context) {
			return shopping.Transfer{}, false, problem{http.StatusConflict, fmt.Sprintf("Your %s store or delivery changed since these prices were checked. Refresh prices, then fill the basket.", r.Name)}
		}
		clientID = session.ClientID
	}
	var transfer shopping.Transfer
	var created bool
	err = s.plans.Review(ctx, id, req.Revision, func(plan shopping.Plan) error {
		basket, _ := planner.Chosen(plan)
		if needsAcknowledgement(plan) && !req.AcknowledgeWarnings || basket.OverBudget && !req.AllowOverBudget {
			return shopping.ErrConflict
		}
		items, err := planner.BasketItems(plan)
		if err != nil {
			return err
		}
		input := shopping.TransferRequest{RetailerID: basket.RetailerID, ClientID: clientID, Context: basket.Context, Items: items}
		transfer, created, err = s.transfers.Create(ctx, input, "plan:"+plan.ID+":"+key, r.BasketURL)
		return err
	})
	if created {
		s.browser.Notify()
	}
	return transfer, created, err
}

func needsAcknowledgement(plan shopping.Plan) bool {
	if len(plan.Warnings) > 0 {
		return true
	}
	for _, line := range plan.Lines {
		if selected := line.Baskets[plan.RetailerID].Selected; !line.Skipped && selected != nil && len(selected.UnmetTraits) > 0 {
			return true
		}
	}
	return false
}
