package planner

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

func Search(ctx context.Context, catalog shopping.Catalog, basket shopping.Basket, item shopping.Item, limit int) (shopping.BasketLine, error) {
	line := shopping.BasketLine{Candidates: []shopping.Candidate{}, SearchLimit: limit}
	search := shopping.SearchRequest{RetailerID: basket.RetailerID, Context: basket.Context, Query: item.Query}
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
			line.Candidates = append(line.Candidates, candidate(product, item, basket.Context.Memberships))
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

func Select(line *shopping.BasketLine) {
	slices.SortStableFunc(line.Candidates, rank)
	line.Selected, line.Issue = nil, ""
	switch {
	case len(line.Candidates) == 0:
		line.Issue = "No matching product found."
	case !line.Candidates[0].Eligible:
		line.Issue = "No product has verified availability and a compatible pack size."
	case len(line.Candidates[0].UnmetTraits) > 0:
		line.Issue = "No product meets every requirement."
	default:
		chosen := line.Candidates[0]
		line.Selected = &chosen
	}
}

func rank(a, b shopping.Candidate) int {
	if a.Eligible != b.Eligible {
		if a.Eligible {
			return -1
		}
		return 1
	}
	if n := len(a.UnmetTraits) - len(b.UnmetTraits); n != 0 {
		return n
	}
	if a.Total.AmountMinor < b.Total.AmountMinor {
		return -1
	}
	if a.Total.AmountMinor > b.Total.AmountMinor {
		return 1
	}
	return strings.Compare(a.Product.ID, b.Product.ID)
}

func ApplyMatches(line *shopping.BasketLine, ids []string) {
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
			c.UnmetTraits = append(c.UnmetTraits, trait)
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
	for i := range plan.Baskets {
		total(plan, &plan.Baskets[i])
	}
	slices.SortStableFunc(plan.Baskets, func(a, b shopping.Basket) int {
		if a.Missing != b.Missing {
			return a.Missing - b.Missing
		}
		if a.Total.AmountMinor < b.Total.AmountMinor {
			return -1
		}
		if a.Total.AmountMinor > b.Total.AmountMinor {
			return 1
		}
		return 0
	})
	if _, ok := Chosen(*plan); !ok && len(plan.Baskets) > 0 {
		plan.RetailerID = plan.Baskets[0].RetailerID
	}
}

func total(plan *shopping.Plan, basket *shopping.Basket) {
	basket.Total = shopping.Money{Currency: "SEK"}
	basket.Estimated, basket.OverBudget = false, false
	basket.Status = "ready"
	basket.Missing = 0
	quantities := map[string]int{}
	for _, line := range plan.Lines {
		if selected := line.Baskets[basket.RetailerID].Selected; selected != nil && selected.Eligible && !line.Skipped {
			quantities[selected.Product.ID] += selected.Quantity
		}
	}
	count := 0
	for _, line := range plan.Lines {
		if line.Skipped {
			continue
		}
		selected := line.Baskets[basket.RetailerID].Selected
		if selected == nil || !selected.Eligible {
			basket.Missing++
		}
		if selected == nil {
			continue
		}
		if !selected.Eligible {
			basket.Status = "needs_review"
			continue
		}
		count++
		selected.UnitPrice = price(selected.Product, quantities[selected.Product.ID], basket.Context.Memberships)
		selected.Total.AmountMinor = selected.UnitPrice.AmountMinor * int64(selected.Quantity)
		basket.Total.AmountMinor += selected.Total.AmountMinor
		basket.Estimated = basket.Estimated || selected.Product.PriceKind == "estimated"
		if quantities[selected.Product.ID] > 1000 {
			basket.Status = "needs_review"
		}
	}
	if count == 0 {
		basket.Status = "needs_review"
	}
	basket.OverBudget = plan.BudgetMinor > 0 && basket.Total.AmountMinor > plan.BudgetMinor
}

func Chosen(plan shopping.Plan) (shopping.Basket, bool) {
	index := slices.IndexFunc(plan.Baskets, func(basket shopping.Basket) bool { return basket.RetailerID == plan.RetailerID })
	if index < 0 {
		return shopping.Basket{}, false
	}
	return plan.Baskets[index], true
}

func BasketItems(plan shopping.Plan) ([]shopping.BasketItem, error) {
	basket, ok := Chosen(plan)
	if !ok || basket.Status != "ready" {
		return nil, shopping.ErrConflict
	}
	items := []shopping.BasketItem{}
	indices := map[string]int{}
	for _, line := range plan.Lines {
		selected := line.Baskets[basket.RetailerID].Selected
		if line.Skipped || selected == nil {
			continue
		}
		if !selected.Eligible {
			return nil, shopping.ErrConflict
		}
		id := selected.Product.ID
		index, exists := indices[id]
		if !exists {
			index = len(items)
			indices[id] = index
			items = append(items, shopping.BasketItem{ProductID: id, Name: selected.Product.Name})
		}
		items[index].Quantity += selected.Quantity
		if items[index].Quantity > 1000 {
			return nil, fmt.Errorf("%w: too many packs", shopping.ErrInvalid)
		}
	}
	if len(items) == 0 {
		return nil, shopping.ErrConflict
	}
	return items, nil
}
