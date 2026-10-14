package planner

import (
	"context"
	"crypto/rand"
	"slices"
	"time"

	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

type Store struct {
	state *persistence.Store[map[string]shopping.Plan]
}

func Open(path string) (*Store, error) {
	state, err := persistence.Open(path, map[string]shopping.Plan{})
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
	return &Store{state}, nil
}

func (s *Store) Create(ctx context.Context, plan shopping.Plan) (shopping.Plan, error) {
	plan.ID, plan.Revision = "plan-"+rand.Text(), 1
	plan.CreatedAt = time.Now().UTC()
	Recalculate(&plan)
	err := s.state.Update(func(data *map[string]shopping.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
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

func (s *Store) List(ctx context.Context) ([]shopping.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := s.state.Read()
	if err != nil {
		return nil, err
	}
	plans := make([]shopping.Plan, 0, len(data))
	for _, plan := range data {
		plans = append(plans, plan)
	}
	slices.SortFunc(plans, func(a, b shopping.Plan) int {
		if n := b.UpdatedAt.Compare(a.UpdatedAt); n != 0 {
			return n
		}
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	if len(plans) > 100 {
		plans = plans[:100]
	}
	return plans, nil
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
