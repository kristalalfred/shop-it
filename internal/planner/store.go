package planner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

const savedPlans = 30

type Store struct {
	state *persistence.Store[map[string]shopping.Plan]
}

func Open(path string) (*Store, error) {
	state, err := persistence.Open(path, map[string]shopping.Plan{}, compareShops, offerAlternatives)
	if err != nil {
		return nil, err
	}
	data, err := state.Read()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, shopping.ErrInvalid
	}
	if len(data) > 0 {
		if err := state.Update(func(data *map[string]shopping.Plan) error {
			for id, plan := range *data {
				Recalculate(&plan)
				(*data)[id] = plan
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return &Store{state}, nil
}

func (s *Store) Create(ctx context.Context, plan shopping.Plan) (shopping.Plan, error) {
	plan.ID, plan.Revision = "plan-"+rand.Text(), 1
	plan.CreatedAt = time.Now().UTC()
	plan.UpdatedAt = plan.CreatedAt
	Recalculate(&plan)
	err := s.state.Update(func(data *map[string]shopping.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, old := range newestFirst(*data)[min(len(*data), savedPlans-1):] {
			delete(*data, old.ID)
		}
		(*data)[plan.ID] = plan
		return nil
	})
	if err != nil {
		return shopping.Plan{}, err
	}
	return s.Get(ctx, plan.ID)
}

func (s *Store) Get(ctx context.Context, id string) (shopping.Plan, error) {
	if err := ctx.Err(); err != nil {
		return shopping.Plan{}, err
	}
	data, err := s.state.Read()
	if err != nil {
		return shopping.Plan{}, err
	}
	plan, ok := data[id]
	if !ok {
		return shopping.Plan{}, shopping.ErrNotFound
	}
	return plan, nil
}

func (s *Store) List(ctx context.Context) ([]shopping.PlanSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := s.state.Read()
	if err != nil {
		return nil, err
	}
	plans := newestFirst(data)
	summaries := make([]shopping.PlanSummary, len(plans))
	for i, plan := range plans {
		basket, _ := Chosen(plan)
		summaries[i] = shopping.PlanSummary{ID: plan.ID, Text: plan.Text, RetailerID: plan.RetailerID, Total: basket.Total, UpdatedAt: plan.UpdatedAt}
	}
	return summaries, nil
}

func newestFirst(data map[string]shopping.Plan) []shopping.Plan {
	plans := slices.Collect(maps.Values(data))
	slices.SortFunc(plans, func(a, b shopping.Plan) int {
		if n := b.UpdatedAt.Compare(a.UpdatedAt); n != 0 {
			return n
		}
		if n := b.CreatedAt.Compare(a.CreatedAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return plans
}

func (s *Store) Update(ctx context.Context, id string, revision int, change func(*shopping.Plan) error) (shopping.Plan, error) {
	var updated shopping.Plan
	err := s.state.Update(func(data *map[string]shopping.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		plan, ok := (*data)[id]
		if !ok {
			return shopping.ErrNotFound
		}
		if plan.Revision != revision {
			return shopping.ErrConflict
		}
		if err := change(&plan); err != nil {
			return err
		}
		plan.Revision++
		plan.UpdatedAt = time.Now().UTC()
		Recalculate(&plan)
		(*data)[id] = plan
		updated = plan
		return nil
	})
	if err != nil {
		return shopping.Plan{}, err
	}
	return updated, nil
}

func (s *Store) Review(ctx context.Context, id string, revision int, review func(shopping.Plan) error) error {
	return s.state.Update(func(data *map[string]shopping.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		plan, ok := (*data)[id]
		if !ok {
			return shopping.ErrNotFound
		}
		if plan.Revision != revision {
			return shopping.ErrConflict
		}
		return review(plan)
	})
}

func compareShops(data json.RawMessage) (json.RawMessage, error) {
	var saved map[string]struct {
		shopping.Plan
		Context shopping.StoreContext `json:"context"`
		Lines   []struct {
			shopping.PlanLine
			shopping.BasketLine
		} `json:"lines"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, err
	}
	plans := make(map[string]shopping.Plan, len(saved))
	for id, old := range saved {
		plan := old.Plan
		plan.Baskets = []shopping.Basket{{RetailerID: plan.RetailerID, Context: old.Context}}
		plan.Lines = make([]shopping.PlanLine, len(old.Lines))
		for i, line := range old.Lines {
			plan.Lines[i] = line.PlanLine
			plan.Lines[i].Baskets = map[string]shopping.BasketLine{plan.RetailerID: line.BasketLine}
		}
		plans[id] = plan
	}
	return json.Marshal(plans)
}

func offerAlternatives(data json.RawMessage) (json.RawMessage, error) {
	var plans map[string]shopping.Plan
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		for _, line := range plan.Lines {
			for retailer, entry := range line.Baskets {
				for i := range entry.Candidates {
					candidate := &entry.Candidates[i]
					reasons := []string{}
					for _, reason := range candidate.Reasons {
						if trait, ok := strings.CutPrefix(reason, "Requirement unverified or unmet: "); ok {
							candidate.UnmetTraits = append(candidate.UnmetTraits, trait)
						} else {
							reasons = append(reasons, reason)
						}
					}
					candidate.Reasons = reasons
					candidate.Eligible = candidate.Eligible || len(reasons) == 0
				}
				if entry.Selected == nil && entry.Issue == "No product has verified availability, requirements and a compatible pack size." {
					Select(&entry)
				} else {
					slices.SortStableFunc(entry.Candidates, rank)
				}
				line.Baskets[retailer] = entry
			}
		}
	}
	return json.Marshal(plans)
}
