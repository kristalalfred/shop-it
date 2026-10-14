package planner

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func Match(ctx context.Context, catalog shopping.Catalog, req shopping.PlanRequest, item shopping.Item, id string) (shopping.PlanLine, error) {
	line, err := Search(ctx, catalog, req, item, id, 500)
	if err == nil {
		Select(&line)
	}
	return line, err
}

func Search(ctx context.Context, catalog shopping.Catalog, req shopping.PlanRequest, item shopping.Item, id string, limit int) (shopping.PlanLine, error) {
	line := shopping.PlanLine{ID: id, Item: item, Candidates: []shopping.Candidate{}, SearchLimit: limit}
	search := shopping.SearchRequest{RetailerID: req.RetailerID, Context: req.Context, Query: item.Query}
	cursors, ids := map[string]bool{}, map[string]bool{}
pages:
	for page := 0; ; page++ {
		if page >= 25 || cursors[search.Cursor] {
			return line, shopping.ErrUpstream
		}
		cursors[search.Cursor] = true
		result, err := catalog.Search(ctx, search)
		if err != nil {
			return line, err
		}
		if line.ObservedAt.IsZero() || result.ObservedAt.Before(line.ObservedAt) {
			line.ObservedAt = result.ObservedAt
		}
		for _, product := range result.Products {
			if ids[product.ID] {
				continue
			}
			if len(line.Candidates) >= limit {
				line.SearchLimited = true
				break pages
			}
			ids[product.ID] = true
			line.Candidates = append(line.Candidates, candidate(product, item, req.Context.Memberships))
		}
		if result.NextCursor == "" {
			break
		}
		if len(line.Candidates) >= limit {
			line.SearchLimited = true
			break
		}
		search.Cursor = result.NextCursor
	}
	return line, nil
}

func Select(line *shopping.PlanLine) {
	line.Selected = nil
	line.Issue = ""
	slices.SortStableFunc(line.Candidates, func(a, b shopping.Candidate) int {
		if a.Eligible != b.Eligible {
			if a.Eligible {
				return -1
			}
			return 1
		}
		if a.Total.AmountMinor < b.Total.AmountMinor {
			return -1
		}
		if a.Total.AmountMinor > b.Total.AmountMinor {
			return 1
		}
		return strings.Compare(a.Product.ID, b.Product.ID)
	})
	for _, option := range line.Candidates {
		if option.Eligible {
			chosen := option
			line.Selected = &chosen
			break
		}
	}
	if len(line.Candidates) == 0 {
		line.Issue = "No matching product found."
	} else if line.Selected == nil {
		line.Issue = "No product has verified availability, requirements and a compatible pack size."
	}
}

func ApplyMatches(line *shopping.PlanLine, ids []string) {
	for i := range line.Candidates {
		candidate := &line.Candidates[i]
		if !slices.Contains(ids, candidate.Product.ID) {
			if candidate.Eligible {
				candidate.Reasons = []string{}
			}
			candidate.Eligible = false
			candidate.Reasons = append(candidate.Reasons, "This product does not match the requested ingredient.")
		}
	}
	Select(line)
}

func candidate(product shopping.Product, item shopping.Item, memberships []string) shopping.Candidate {
	c := shopping.Candidate{Product: product, UnitPrice: product.Price, Total: shopping.Money{Currency: "SEK"}, Reasons: []string{}}
	if item.Amount.Unit == "pack" {
		c.Quantity = int(item.Amount.Quantity)
	} else if item.Amount.Unit == product.Pack.Unit {
		c.Quantity = int((item.Amount.Quantity + product.Pack.Quantity - 1) / product.Pack.Quantity)
	} else {
		c.Reasons = append(c.Reasons, "Requested quantity cannot be converted to this pack size.")
	}
	if c.Quantity > 1000 {
		c.Reasons = append(c.Reasons, "More than 1000 packs would be needed.")
	}
	if product.Availability != "in_stock" {
		c.Reasons = append(c.Reasons, "Availability: "+product.Availability)
	}
	for _, trait := range item.RequiredTraits {
		if product.Traits[trait] != "yes" {
			c.Reasons = append(c.Reasons, "Requirement unverified or unmet: "+trait)
		}
	}
	if c.Quantity > 0 && c.Quantity <= 1000 {
		c.UnitPrice = price(product, c.Quantity, memberships)
		c.Total.AmountMinor = c.UnitPrice.AmountMinor * int64(c.Quantity)
	}
	c.Eligible = len(c.Reasons) == 0
	if c.Eligible {
		c.Reasons = append(c.Reasons, "Lowest-cost options are ranked by the total needed quantity.")
		if c.UnitPrice.AmountMinor != product.Price.AmountMinor {
			c.Reasons = append(c.Reasons, "Eligible quantity or membership offer applied.")
		}
		if product.PriceKind == "estimated" {
			c.Reasons = append(c.Reasons, "Final price depends on the supplied weight.")
		}
	}
	return c
}

func price(product shopping.Product, quantity int, memberships []string) shopping.Money {
	best := product.Price
	for _, offer := range product.Offers {
		if quantity >= offer.MinQuantity && (offer.Membership == "" || slices.Contains(memberships, offer.Membership)) && offer.Price.AmountMinor < best.AmountMinor {
			best = offer.Price
		}
	}
	return best
}

func Recalculate(plan *shopping.Plan) {
	plan.Total = shopping.Money{Currency: "SEK"}
	plan.Estimated, plan.OverBudget = false, false
	plan.Status = "ready"
	quantities := map[string]int{}
	for _, line := range plan.Lines {
		if line.Selected != nil && line.Selected.Eligible && !line.Skipped {
			quantities[line.Selected.Product.ID] += line.Selected.Quantity
		}
	}
	count := 0
	for i := range plan.Lines {
		line := &plan.Lines[i]
		if line.Skipped {
			continue
		}
		if line.Selected == nil {
			continue
		}
		if !line.Selected.Eligible {
			plan.Status = "needs_review"
			continue
		}
		count++
		line.Selected.UnitPrice = price(line.Selected.Product, quantities[line.Selected.Product.ID], plan.Context.Memberships)
		line.Selected.Total.AmountMinor = line.Selected.UnitPrice.AmountMinor * int64(line.Selected.Quantity)
		plan.Total.AmountMinor += line.Selected.Total.AmountMinor
		plan.Estimated = plan.Estimated || line.Selected.Product.PriceKind == "estimated"
		if quantities[line.Selected.Product.ID] > 1000 {
			plan.Status = "needs_review"
		}
	}
	if count == 0 {
		plan.Status = "needs_review"
	}
	plan.OverBudget = plan.BudgetMinor > 0 && plan.Total.AmountMinor > plan.BudgetMinor
}

func BasketItems(plan shopping.Plan) ([]shopping.BasketItem, error) {
	if plan.Status != "ready" {
		return nil, shopping.ErrConflict
	}
	items := []shopping.BasketItem{}
	indices := map[string]int{}
	for _, line := range plan.Lines {
		if line.Skipped || line.Selected == nil {
			continue
		}
		if !line.Selected.Eligible {
			return nil, shopping.ErrConflict
		}
		id := line.Selected.Product.ID
		index, exists := indices[id]
		if !exists {
			index = len(items)
			indices[id] = index
			items = append(items, shopping.BasketItem{ProductID: id})
		}
		items[index].Quantity += line.Selected.Quantity
		if items[index].Quantity > 1000 {
			return nil, fmt.Errorf("%w: too many packs", shopping.ErrInvalid)
		}
	}
	if len(items) == 0 {
		return nil, shopping.ErrConflict
	}
	return items, nil
}
