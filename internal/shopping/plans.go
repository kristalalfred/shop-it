package shopping

import (
	"context"
	"time"
)

type Item struct {
	Label          string   `json:"label"`
	Query          string   `json:"query"`
	Amount         Pack     `json:"amount"`
	RequiredTraits []string `json:"required_traits"`
}

type Interpretation struct {
	Mode     string   `json:"mode"`
	Items    []Item   `json:"items"`
	Warnings []string `json:"warnings"`
}

type InterpretationRequest struct {
	Text           string   `json:"text"`
	RequiredTraits []string `json:"required_traits,omitempty"`
}

type Notify func(note string)

type Progress struct {
	Stage string `json:"stage"`
	Note  string `json:"note,omitempty"`
	Done  int    `json:"done,omitempty"`
	Total int    `json:"total,omitempty"`
}

type Interpreter interface {
	Interpret(context.Context, InterpretationRequest, Notify) (Interpretation, error)
}

type ProductOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Brand string `json:"brand"`
}

type ProductMatchRequest struct {
	ID       string          `json:"id"`
	Item     Item            `json:"item"`
	Products []ProductOption `json:"products"`
}

type ProductMatch struct {
	ID         string   `json:"id"`
	ProductIDs []string `json:"product_ids"`
}

type ProductMatcher interface {
	MatchProducts(context.Context, []ProductMatchRequest, Notify) ([]ProductMatch, error)
}

type PlanRequest struct {
	Text           string       `json:"text"`
	RetailerID     string       `json:"retailer_id"`
	Context        StoreContext `json:"context"`
	RequiredTraits []string     `json:"required_traits,omitempty"`
	BudgetMinor    int64        `json:"budget_minor,omitempty"`
}

type Candidate struct {
	Product   Product  `json:"product"`
	Quantity  int      `json:"target_quantity"`
	UnitPrice Money    `json:"unit_price"`
	Total     Money    `json:"total"`
	Eligible  bool     `json:"eligible"`
	Reasons   []string `json:"reasons"`
}

type PlanLine struct {
	ID            string      `json:"id"`
	Item          Item        `json:"item"`
	Candidates    []Candidate `json:"candidates"`
	Selected      *Candidate  `json:"selected,omitempty"`
	Skipped       bool        `json:"skipped"`
	ObservedAt    time.Time   `json:"observed_at,omitempty"`
	Issue         string      `json:"issue,omitempty"`
	SearchLimited bool        `json:"search_limited,omitempty"`
	SearchLimit   int         `json:"search_limit,omitempty"`
}

type Plan struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	PlanRequest
	InterpreterMode string     `json:"interpreter_mode"`
	Lines           []PlanLine `json:"lines"`
	Warnings        []string   `json:"warnings"`
	Status          string     `json:"status"`
	Total           Money      `json:"total"`
	Estimated       bool       `json:"estimated"`
	OverBudget      bool       `json:"over_budget"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
