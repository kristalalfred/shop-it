package shopping

import "time"

type Item struct {
	Label          string   `json:"label"`
	Query          string   `json:"query"`
	Amount         Pack     `json:"amount"`
	RequiredTraits []string `json:"required_traits"`
}

type PlanRequest struct {
	Text        string `json:"text"`
	BudgetMinor int64  `json:"budget_minor,omitempty"`
}

type Candidate struct {
	Product     Product  `json:"product"`
	Quantity    int      `json:"target_quantity"`
	UnitPrice   Money    `json:"unit_price"`
	Total       Money    `json:"total"`
	Eligible    bool     `json:"eligible"`
	UnmetTraits []string `json:"unmet_traits,omitempty"`
	Reasons     []string `json:"reasons"`
}

type BasketLine struct {
	Candidates    []Candidate `json:"candidates"`
	Selected      *Candidate  `json:"selected,omitempty"`
	ObservedAt    time.Time   `json:"observed_at,omitempty"`
	Issue         string      `json:"issue,omitempty"`
	SearchLimited bool        `json:"search_limited,omitempty"`
	SearchLimit   int         `json:"search_limit,omitempty"`
}

type PlanLine struct {
	ID      string                `json:"id"`
	Item    Item                  `json:"item"`
	Skipped bool                  `json:"skipped"`
	Baskets map[string]BasketLine `json:"baskets"`
}

type Basket struct {
	RetailerID string       `json:"retailer_id"`
	Context    StoreContext `json:"context"`
	Status     string       `json:"status"`
	Total      Money        `json:"total"`
	Estimated  bool         `json:"estimated"`
	OverBudget bool         `json:"over_budget"`
	Missing    int          `json:"missing"`
}

type PlanSummary struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	RetailerID string    `json:"retailer_id"`
	Total      Money     `json:"total"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Plan struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	PlanRequest
	RetailerID string     `json:"retailer_id"`
	Lines      []PlanLine `json:"lines"`
	Baskets    []Basket   `json:"baskets"`
	Warnings   []string   `json:"warnings"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
