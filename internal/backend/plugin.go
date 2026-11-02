package backend

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kristalalfred/shop-it/internal/persistence"
	"github.com/kristalalfred/shop-it/internal/planner"
	"github.com/kristalalfred/shop-it/internal/release"
	"github.com/kristalalfred/shop-it/internal/retailer"
	"github.com/kristalalfred/shop-it/internal/shopping"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed plugin/*
var pluginFiles embed.FS

const pluginURI = "ui://shop-it/planner-v1.html"
const pluginMIME = "text/html;profile=mcp-app"

type pluginPreferences struct {
	RetailerIDs      []string `json:"retailer_ids"`
	Draft            string   `json:"draft"`
	PlanID           string   `json:"plan_id,omitempty"`
	ChatGPTConnected bool     `json:"chatgpt_connected,omitempty"`
}

type pluginService struct {
	state     *persistence.Store[pluginPreferences]
	schemas   map[string]*jsonschema.Resolved
	connected atomic.Bool
}

type pluginSnapshot struct {
	setupState
	Preferences pluginPreferences      `json:"preferences"`
	Plan        *shopping.Plan         `json:"plan,omitempty"`
	ReviewURL   string                 `json:"review_url,omitempty"`
	SavedPlans  []shopping.PlanSummary `json:"saved_plans"`
}

type pluginPlanRequest struct {
	Text        string          `json:"text" jsonschema:"The user's shopping request or a concise meal plan description"`
	Items       []shopping.Item `json:"items" jsonschema:"Concrete ingredients with Swedish search queries and amounts in g, ml, each or pack. Include only requirements actually requested by the user."`
	BudgetMinor int64           `json:"budget_minor,omitempty" jsonschema:"Optional budget in öre. 800 SEK is 80000."`
	RetailerIDs []string        `json:"retailer_ids,omitempty"`
}

func (s *Server) snapshot(ctx context.Context, planID string) (pluginSnapshot, error) {
	prefs, err := s.plugin.state.Read()
	if err != nil {
		return pluginSnapshot{}, err
	}
	out := pluginSnapshot{setupState: s.setup(prefs), Preferences: prefs}
	if planID == "" {
		planID = prefs.PlanID
	}
	if planID != "" {
		plan, err := s.plans.Get(ctx, planID)
		if err == nil {
			out.Plan = &plan
		} else if planID != prefs.PlanID || !errors.Is(err, shopping.ErrNotFound) {
			return out, err
		}
	}
	if out.Plan != nil {
		out.ReviewURL = out.SetupURL + "?plan=" + url.QueryEscape(out.Plan.ID)
	}
	saved, err := s.plans.List(ctx)
	if err != nil {
		return out, err
	}
	out.SavedPlans = saved[:min(len(saved), 10)]
	return out, nil
}

func queryEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func (s *Server) configurePlugin(retailerIDs []string, draft *string) error {
	if retailerIDs != nil {
		seen := map[string]bool{}
		for _, id := range retailerIDs {
			if seen[id] || !slices.ContainsFunc(s.config.Retailers, func(r RetailerConfig) bool { return r.ID == id }) {
				return shopping.ErrInvalid
			}
			seen[id] = true
		}
	}
	if draft != nil && (len(*draft) > 16000) {
		return shopping.ErrInvalid
	}
	return s.plugin.state.Update(func(p *pluginPreferences) error {
		if retailerIDs != nil {
			p.RetailerIDs = retailerIDs
		}
		if draft != nil {
			p.Draft = *draft
		}
		return nil
	})
}

func (s *Server) createPluginPlan(ctx context.Context, req pluginPlanRequest) (shopping.Plan, error) {
	if !validText(req.Text) || len(req.Items) < 1 || len(req.Items) > 100 || req.BudgetMinor < 0 || req.BudgetMinor > 100000000 {
		return shopping.Plan{}, shopping.ErrInvalid
	}
	for _, item := range req.Items {
		if !planner.ValidItem(item) {
			return shopping.Plan{}, shopping.ErrInvalid
		}
	}
	if len(req.RetailerIDs) == 0 {
		prefs, err := s.plugin.state.Read()
		if err != nil {
			return shopping.Plan{}, err
		}
		req.RetailerIDs = prefs.RetailerIDs
	}
	baskets, err := s.storeBaskets(req.RetailerIDs)
	if err != nil {
		return shopping.Plan{}, err
	}
	plan := shopping.Plan{PlanRequest: shopping.PlanRequest{Text: req.Text, BudgetMinor: req.BudgetMinor}, Baskets: baskets, Warnings: []string{}, Lines: make([]shopping.PlanLine, len(req.Items))}
	for i, item := range req.Items {
		plan.Lines[i] = shopping.PlanLine{ID: fmt.Sprintf("line-%d", i+1), Item: item}
	}
	plan.Lines, err = s.searchLines(ctx, baskets, plan.Lines, false)
	if err != nil {
		return shopping.Plan{}, err
	}
	for i := range plan.Lines {
		for id, entry := range plan.Lines[i].Baskets {
			if entry.Selected == nil && entry.Issue == "" {
				entry.Issue = "Choose a matching product."
				plan.Lines[i].Baskets[id] = entry
			}
		}
	}
	plan, err = s.plans.Create(ctx, plan)
	if err != nil {
		return shopping.Plan{}, err
	}
	err = s.plugin.state.Update(func(p *pluginPreferences) error {
		p.PlanID = plan.ID
		p.RetailerIDs = slices.Clone(req.RetailerIDs)
		p.Draft = req.Text
		return nil
	})
	return plan, err
}

type productSelection struct {
	LineID     string `json:"line_id"`
	RetailerID string `json:"retailer_id"`
	ProductID  string `json:"product_id"`
}

func (s *Server) selectProducts(ctx context.Context, id string, revision int, selections []productSelection) (shopping.Plan, error) {
	if len(selections) < 1 || len(selections) > 600 {
		return shopping.Plan{}, shopping.ErrInvalid
	}
	return s.plans.Update(ctx, id, revision, func(plan *shopping.Plan) error {
		if basket, ok := planner.Chosen(*plan); ok && basket.Missing == len(plan.Lines)-skipped(*plan) {
			plan.RetailerID = ""
		}
		seen := map[string]bool{}
		for _, selection := range selections {
			key := selection.LineID + ":" + selection.RetailerID
			if seen[key] {
				return shopping.ErrInvalid
			}
			seen[key] = true
			i := slices.IndexFunc(plan.Lines, func(line shopping.PlanLine) bool { return line.ID == selection.LineID })
			if i < 0 {
				return shopping.ErrNotFound
			}
			if err := selectProduct(&plan.Lines[i], selection.RetailerID, selection.ProductID); err != nil {
				return err
			}
		}
		return nil
	})
}

func skipped(plan shopping.Plan) int {
	count := 0
	for _, line := range plan.Lines {
		if line.Skipped {
			count++
		}
	}
	return count
}

func sameStore(a, b shopping.StoreContext) bool {
	return retailer.SameContext(a, b)
}

func (s *Server) pluginHTML() string {
	page, _ := pluginFiles.ReadFile("plugin/index.html")
	css, _ := pluginFiles.ReadFile("plugin/app.css")
	js, _ := pluginFiles.ReadFile("plugin/app.js")
	script := strings.ReplaceAll(string(js), "{{VERSION}}", release.Version)
	return strings.NewReplacer("{{STYLE}}", string(css), "{{SCRIPT}}", script).Replace(string(page))
}

func (s *Server) registerPlugin(mux *http.ServeMux) error {
	path := ""
	if s.config.DataDir != "" {
		path = filepath.Join(s.config.DataDir, "plugin.json")
	}
	state, err := persistence.Open(path, pluginPreferences{RetailerIDs: []string{}})
	if err != nil {
		return err
	}
	s.plugin = &pluginService{state: state, schemas: map[string]*jsonschema.Resolved{}}
	for _, tool := range pluginTools() {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return err
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(raw, &schema); err != nil {
			return err
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return err
		}
		s.plugin.schemas[tool.Name] = resolved
	}
	server := s.mcpServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux.Handle("/mcp", s.user(func(w http.ResponseWriter, r *http.Request) {
		if !s.plugin.connected.Swap(true) {
			_ = s.plugin.state.Update(func(p *pluginPreferences) error { p.ChatGPTConnected = true; return nil })
		}
		handler.ServeHTTP(w, r)
	}))
	return nil
}

func (s *Server) mcpServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "shop-it", Version: release.Version}, &mcp.ServerOptions{Instructions: "Use open_shop_it first to resume setup, the current shopping plan or a saved one. The user and ChatGPT edit the same plan. Create plans from structured ingredients. Candidate prices and quantities are calculated by Shop It. Select only matching candidates from returned IDs, respect dietary requirements, and explain unresolved items. The user reviews a finished plan and fills the store basket on the Shop It page at review_url; you cannot change a store basket. Checkout stays at the retailer."})
	server.AddResource(&mcp.Resource{URI: pluginURI, Name: "Shop It", MIMEType: pluginMIME}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: pluginURI, MIMEType: pluginMIME, Text: s.pluginHTML(), Meta: map[string]any{"ui": map[string]any{"prefersBorder": false}}}}}, nil
	})
	for _, definition := range pluginTools() {
		tool := definition
		server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := s.callPluginTool(ctx, tool.Name, request.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: pluginSummary(result)}}}, nil
		})
	}
	return server
}

func pluginSummary(s pluginSnapshot) string {
	if s.Plan != nil {
		return fmt.Sprintf("Plan %s, revision %d. %d ingredients. Choose matching products from the returned candidates; read the latest revision before editing. The user reviews it and fills the basket at %s.", s.Plan.ID, s.Plan.Revision, len(s.Plan.Lines), s.ReviewURL)
	}
	if !s.BrowserOnline && !s.Demo {
		return fmt.Sprintf("The browser extension is not connected. Open %s so the user can finish setup there. The shopping request is saved.", s.SetupURL)
	}
	if !slices.ContainsFunc(s.Stores, func(store pluginStore) bool { return store.Selected }) {
		return fmt.Sprintf("No store is chosen. Ask which stores to compare, or open %s so the user can choose and sign in.", s.SetupURL)
	}
	return "Shop It is open. Check each selected store with connect_store before planning."
}

func schema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func field(kind string) map[string]any { return map[string]any{"type": kind} }

func pluginTools() []*mcp.Tool {
	str := field("string")
	integer := field("integer")
	boolean := field("boolean")
	id := map[string]any{"plan_id": str, "revision": integer}
	tools := []*mcp.Tool{
		{Name: "open_shop_it", Title: "Open Shop It", Description: "Open the shared grocery planner, resume onboarding, or read the current plan and revision. Pass the plan_id of a saved plan to reopen it. Returns real browser and store connection status and the user's saved plans.", InputSchema: schema(map[string]any{"plan_id": str})},
		{Name: "configure_shop_it", Title: "Choose stores", Description: "Save the user's chosen stores and shopping request during setup. Does not connect stores or change retailer baskets.", InputSchema: schema(map[string]any{"retailer_ids": map[string]any{"type": "array", "items": str}, "draft": str})},
		{Name: "connect_store", Title: "Check store connection", Description: "Open a selected store in the paired browser and check its current store or delivery context. User may need to sign in or choose a store there, then retry. Never invent a successful connection.", InputSchema: schema(map[string]any{"retailer_id": str}, "retailer_id")},
		{Name: "create_shopping_plan", Title: "Compare groceries", Description: "Create a comparison from concrete ingredients using connected store sessions. ChatGPT supplies ingredients and chooses matching products from returned candidates. No backend AI calls. Required traits belong only on ingredients the user constrained. Prices exclude delivery.", InputSchema: schema(map[string]any{"text": str, "items": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": schema(map[string]any{"label": str, "query": str, "amount": schema(map[string]any{"quantity": integer, "unit": map[string]any{"type": "string", "enum": []string{"g", "ml", "each", "pack"}}}, "quantity", "unit"), "required_traits": map[string]any{"type": "array", "items": str}}, "label", "query", "amount", "required_traits")}, "budget_minor": integer, "retailer_ids": map[string]any{"type": "array", "items": str}}, "text", "items")},
		{Name: "select_store", Title: "Choose comparison", Description: "Choose a store in an existing shopping comparison. Requires its current revision.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "retailer_id": str}, "plan_id", "revision", "retailer_id")},
		{Name: "select_products", Title: "Match products across stores", Description: "Select matching eligible product IDs for multiple ingredients and stores in one atomic plan revision. Use this after creating a plan to build comparable basket totals at every store. Leave uncertain matches unselected for user review. Never invent product IDs.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "selections": map[string]any{"type": "array", "minItems": 1, "maxItems": 600, "items": schema(map[string]any{"line_id": str, "retailer_id": str, "product_id": str}, "line_id", "retailer_id", "product_id")}}, "plan_id", "revision", "selections")},
		{Name: "update_plan_line", Title: "Update grocery", Description: "Choose a matching candidate by product_id, skip or restore an ingredient, or change and search an item. Requires the current plan revision. Product IDs must come from this plan's returned eligible candidates.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "line_id": str, "product_id": str, "skipped": boolean, "item": schema(map[string]any{"label": str, "query": str, "amount": schema(map[string]any{"quantity": integer, "unit": str}, "quantity", "unit"), "required_traits": map[string]any{"type": "array", "items": str}}, "label", "query", "amount", "required_traits")}, "plan_id", "revision", "line_id")},
		{Name: "refresh_plan", Title: "Refresh prices", Description: "Read fresh product prices and availability in the same store context, preserving available selections. Returns a new revision.", InputSchema: schema(id, "plan_id", "revision")},
	}
	for _, t := range tools {
		read := t.Name == "open_shop_it"
		world := t.Name == "connect_store" || t.Name == "create_shopping_plan" || t.Name == "refresh_plan" || t.Name == "update_plan_line"
		destructive := false
		t.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: read, DestructiveHint: &destructive, OpenWorldHint: &world, IdempotentHint: read}
		t.Meta = map[string]any{"ui": map[string]any{"resourceUri": pluginURI}, "openai/ui": map[string]any{}}
		if read {
			t.Meta["openai/ui"] = map[string]any{"entrypoints": []map[string]string{{"type": "global"}, {"type": "thread"}}}
		}
	}
	return tools
}

func (s *Server) callPluginTool(ctx context.Context, name string, raw json.RawMessage) (pluginSnapshot, error) {
	var args struct {
		PlanID      string             `json:"plan_id"`
		Revision    int                `json:"revision"`
		LineID      string             `json:"line_id"`
		RetailerID  string             `json:"retailer_id"`
		RetailerIDs []string           `json:"retailer_ids"`
		Draft       *string            `json:"draft"`
		ProductID   *string            `json:"product_id"`
		Skipped     *bool              `json:"skipped"`
		Item        *shopping.Item     `json:"item"`
		Text        string             `json:"text"`
		Items       []shopping.Item    `json:"items"`
		Selections  []productSelection `json:"selections"`
		BudgetMinor int64              `json:"budget_minor"`
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	resolved, ok := s.plugin.schemas[name]
	if !ok {
		return pluginSnapshot{}, shopping.ErrNotFound
	}
	var input any
	if len(raw) > 1<<20 || json.Unmarshal(raw, &input) != nil || resolved.Validate(input) != nil {
		return pluginSnapshot{}, shopping.ErrInvalid
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &args) != nil {
		return pluginSnapshot{}, shopping.ErrInvalid
	}
	var err error
	switch name {
	case "open_shop_it":
	case "configure_shop_it":
		err = s.configurePlugin(args.RetailerIDs, args.Draft)
	case "connect_store":
		_, err = s.connectStore(ctx, args.RetailerID)
	case "create_shopping_plan":
		_, err = s.createPluginPlan(ctx, pluginPlanRequest{Text: args.Text, Items: args.Items, BudgetMinor: args.BudgetMinor, RetailerIDs: args.RetailerIDs})
		args.PlanID = ""
	case "select_store":
		_, err = s.choose(ctx, args.PlanID, args.Revision, args.RetailerID)
	case "select_products":
		_, err = s.selectProducts(ctx, args.PlanID, args.Revision, args.Selections)
	case "update_plan_line":
		_, err = s.editLine(ctx, args.PlanID, args.LineID, lineUpdate{Revision: args.Revision, Item: args.Item, ProductID: args.ProductID, Skipped: args.Skipped})
	case "refresh_plan":
		_, err = s.refresh(ctx, args.PlanID, args.Revision)
	default:
		err = shopping.ErrNotFound
	}
	if err != nil {
		return pluginSnapshot{}, err
	}
	if name == "select_store" || name == "select_products" || name == "update_plan_line" || name == "refresh_plan" {
		if err := s.plugin.state.Update(func(p *pluginPreferences) error { p.PlanID = args.PlanID; return nil }); err != nil {
			return pluginSnapshot{}, err
		}
	}
	return s.snapshot(ctx, args.PlanID)
}
