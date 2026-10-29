package backend

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kristalalfred/ship-it/internal/browser"
	"github.com/kristalalfred/ship-it/internal/interpret"
	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/retailer"
	"github.com/kristalalfred/ship-it/internal/shopping"
	"github.com/kristalalfred/ship-it/plugin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed plugin/*
var pluginFiles embed.FS

const pluginURI = "ui://ship-it/planner-v1.html"
const pluginMIME = "text/html;profile=mcp-app"

type pluginPreferences struct {
	RetailerIDs []string `json:"retailer_ids"`
	Draft       string   `json:"draft"`
	PlanID      string   `json:"plan_id,omitempty"`
	TransferID  string   `json:"transfer_id,omitempty"`
}

type pluginService struct {
	state   *persistence.Store[pluginPreferences]
	schemas map[string]*jsonschema.Resolved
}

type pluginStore struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	URL      string           `json:"url"`
	Mode     string           `json:"mode"`
	Selected bool             `json:"selected"`
	Session  *browser.Session `json:"session,omitempty"`
}

type pluginSnapshot struct {
	Preferences   pluginPreferences  `json:"preferences"`
	Stores        []pluginStore      `json:"stores"`
	BrowserOnline bool               `json:"browser_online"`
	SetupURL      string             `json:"setup_url"`
	PluginPath    string             `json:"plugin_path,omitempty"`
	InstallURL    string             `json:"install_url,omitempty"`
	ChatURL       string             `json:"chat_url,omitempty"`
	Demo          bool               `json:"demo"`
	Plan          *shopping.Plan     `json:"plan,omitempty"`
	Transfer      *shopping.Transfer `json:"transfer,omitempty"`
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
	out := pluginSnapshot{Preferences: prefs, Stores: []pluginStore{}, SetupURL: "http://" + s.config.Listen + "/plugin", Demo: true}
	for _, c := range s.config.Clients {
		out.BrowserOnline = out.BrowserOnline || s.browser.Online(c.ID)
	}
	if root := s.config.PluginDir(); root != "" {
		marketplace := plugin.MarketplacePath(root)
		if _, err := os.Stat(marketplace); err == nil {
			out.PluginPath = filepath.Join(root, "ship-it")
			out.InstallURL = "codex://plugins/ship-it?marketplacePath=" + queryEscape(marketplace)
			prompt := "[@Ship It](plugin://ship-it@ship-it-local) " + prefs.Draft
			out.ChatURL = "codex://new?path=" + queryEscape(root) + "&prompt=" + queryEscape(prompt)
		}
	}
	sessions := s.browser.Sessions()
	for _, r := range s.config.Retailers {
		store := pluginStore{ID: r.ID, Name: r.Name, URL: r.BasketURL, Mode: r.Mode, Selected: slices.Contains(prefs.RetailerIDs, r.ID)}
		if r.Mode != "mock" {
			out.Demo = false
		}
		for _, session := range sessions {
			if session.RetailerID == r.ID {
				value := session
				store.Session = &value
			}
		}
		out.Stores = append(out.Stores, store)
	}
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
	if prefs.TransferID != "" && planID == prefs.PlanID {
		transfer, err := s.transfers.Get(ctx, prefs.TransferID)
		if err == nil {
			out.Transfer = &transfer
		} else if !errors.Is(err, shopping.ErrNotFound) {
			return out, err
		}
	}
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

func (s *Server) pluginBaskets(ids []string) ([]shopping.Basket, map[string]shopping.Catalog, error) {
	baskets := []shopping.Basket{}
	catalogs := map[string]shopping.Catalog{}
	for _, id := range ids {
		if _, ok := catalogs[id]; ok {
			return nil, nil, shopping.ErrInvalid
		}
		i := slices.IndexFunc(s.config.Retailers, func(r RetailerConfig) bool { return r.ID == id })
		if i < 0 {
			return nil, nil, shopping.ErrInvalid
		}
		r := s.config.Retailers[i]
		storeContext := r.Context()
		if r.Mode == "browser" {
			session, err := s.browser.Session(id)
			if err != nil {
				return nil, nil, err
			}
			storeContext = session.Context
			catalogs[id] = browserCatalog{s.browser, id}
		} else {
			catalogs[id] = s.catalogs[id]
		}
		baskets = append(baskets, shopping.Basket{RetailerID: id, Context: storeContext})
	}
	if len(baskets) == 0 {
		return nil, nil, errors.New("Choose and connect at least one store in Ship It.")
	}
	return baskets, catalogs, nil
}

func (s *Server) createPluginPlan(ctx context.Context, req pluginPlanRequest) (shopping.Plan, error) {
	if !validText(req.Text) || len(req.Items) < 1 || len(req.Items) > 100 || req.BudgetMinor < 0 || req.BudgetMinor > 100000000 {
		return shopping.Plan{}, shopping.ErrInvalid
	}
	for _, item := range req.Items {
		if !interpret.ValidItem(item) {
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
	baskets, catalogs, err := s.pluginBaskets(req.RetailerIDs)
	if err != nil {
		return shopping.Plan{}, err
	}
	plan := shopping.Plan{PlanRequest: shopping.PlanRequest{Text: req.Text, BudgetMinor: req.BudgetMinor}, InterpreterMode: "chatgpt", Baskets: baskets, Warnings: []string{}, Lines: make([]shopping.PlanLine, len(req.Items))}
	for i, item := range req.Items {
		plan.Lines[i] = shopping.PlanLine{ID: fmt.Sprintf("line-%d", i+1), Item: item}
	}
	plan.Lines, err = s.searchLinesUsing(ctx, baskets, plan.Lines, false, ignoreProgress, catalogs, nil, 20, false)
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
		p.TransferID = ""
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

func (s *Server) searchPluginLines(ctx context.Context, plan shopping.Plan, lines []shopping.PlanLine, preserve bool) ([]shopping.PlanLine, error) {
	ids := make([]string, len(plan.Baskets))
	for i, b := range plan.Baskets {
		ids[i] = b.RetailerID
	}
	baskets, catalogs, err := s.pluginBaskets(ids)
	if err != nil {
		return nil, err
	}
	for i, b := range baskets {
		if !sameStore(b.Context, plan.Baskets[i].Context) {
			return nil, errors.New("The store or delivery settings changed. Refresh prices before editing groceries.")
		}
	}
	return s.searchLinesUsing(ctx, baskets, lines, preserve, ignoreProgress, catalogs, nil, 20, false)
}

func sameStore(a, b shopping.StoreContext) bool {
	return retailer.SameContext(a, b)
}

func (s *Server) pluginHTML(local bool) string {
	page, _ := pluginFiles.ReadFile("plugin/index.html")
	css, _ := pluginFiles.ReadFile("plugin/app.css")
	js, _ := pluginFiles.ReadFile("plugin/app.js")
	config := "{}"
	if local {
		raw, _ := json.Marshal(map[string]string{"token": s.config.APIToken})
		config = string(raw)
	}
	return strings.NewReplacer("{{STYLE}}", string(css), "{{SCRIPT}}", string(js), "{{CONFIG}}", config).Replace(string(page))
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
	mux.Handle("/mcp", s.user(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP))
	mux.HandleFunc("GET /plugin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
		fmt.Fprint(w, s.pluginHTML(true))
	})
	mux.HandleFunc("POST /v1/plugin/tools/{name}", s.user(func(w http.ResponseWriter, r *http.Request) {
		var args json.RawMessage
		if !decode(w, r, &args) {
			return
		}
		result, err := s.callPluginTool(r.Context(), r.PathValue("name"), args)
		if err != nil {
			respond(w, 400, map[string]string{"error": err.Error()})
			return
		}
		respond(w, 200, result)
	}))
	return nil
}

func (s *Server) mcpServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ship-it", Version: "0.2.0"}, &mcp.ServerOptions{Instructions: "Use open_ship_it first to resume setup or the current shopping plan. The user and ChatGPT edit the same plan. Create plans from structured ingredients; no external AI key is required. Candidate prices and quantities are calculated by Ship It. Select only matching candidates from returned IDs, respect dietary requirements, and explain unresolved items. Filling a basket changes the retailer cart; do it only when the user requests it. Checkout stays at the retailer."})
	server.AddResource(&mcp.Resource{URI: pluginURI, Name: "Ship It", MIMEType: pluginMIME}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: pluginURI, MIMEType: pluginMIME, Text: s.pluginHTML(false), Meta: map[string]any{"ui": map[string]any{"prefersBorder": false}}}}}, nil
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
	if s.Transfer != nil {
		return "Basket transfer: " + s.Transfer.State + ". Review the result in Ship It."
	}
	if s.Plan != nil {
		return fmt.Sprintf("Plan %s, revision %d. %d ingredients. Choose matching products from the returned candidates; read the latest revision before editing.", s.Plan.ID, s.Plan.Revision, len(s.Plan.Lines))
	}
	if !s.BrowserOnline && !s.Demo {
		return "Open Ship It setup to connect the browser extension, then choose a store. The shopping request is saved."
	}
	return "Ship It is open. Choose and check a store to start shopping."
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
		{Name: "open_ship_it", Title: "Open Ship It", Description: "Open the shared grocery planner, resume onboarding, or read the current plan and revision. Returns real browser and store connection status.", InputSchema: schema(map[string]any{"plan_id": str})},
		{Name: "configure_ship_it", Title: "Choose stores", Description: "Save the user's chosen stores and shopping request during setup. Does not connect stores or change retailer baskets.", InputSchema: schema(map[string]any{"retailer_ids": map[string]any{"type": "array", "items": str}, "draft": str})},
		{Name: "connect_store", Title: "Check store connection", Description: "Open a selected store in the paired browser and check its current store or delivery context. User may need to sign in or choose a store there, then retry. Never invent a successful connection.", InputSchema: schema(map[string]any{"retailer_id": str}, "retailer_id")},
		{Name: "create_shopping_plan", Title: "Compare groceries", Description: "Create a comparison from concrete ingredients using connected store sessions. ChatGPT supplies ingredients and chooses matching products from returned candidates. No backend AI calls. Required traits belong only on ingredients the user constrained. Prices exclude delivery.", InputSchema: schema(map[string]any{"text": str, "items": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": schema(map[string]any{"label": str, "query": str, "amount": schema(map[string]any{"quantity": integer, "unit": map[string]any{"type": "string", "enum": []string{"g", "ml", "each", "pack"}}}, "quantity", "unit"), "required_traits": map[string]any{"type": "array", "items": str}}, "label", "query", "amount", "required_traits")}, "budget_minor": integer, "retailer_ids": map[string]any{"type": "array", "items": str}}, "text", "items")},
		{Name: "select_store", Title: "Choose comparison", Description: "Choose a store in an existing shopping comparison. Requires its current revision.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "retailer_id": str}, "plan_id", "revision", "retailer_id")},
		{Name: "select_products", Title: "Match products across stores", Description: "Select matching eligible product IDs for multiple ingredients and stores in one atomic plan revision. Use this after creating a plan to build comparable basket totals at every store. Leave uncertain matches unselected for user review. Never invent product IDs.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "selections": map[string]any{"type": "array", "minItems": 1, "maxItems": 600, "items": schema(map[string]any{"line_id": str, "retailer_id": str, "product_id": str}, "line_id", "retailer_id", "product_id")}}, "plan_id", "revision", "selections")},
		{Name: "update_plan_line", Title: "Update grocery", Description: "Choose a matching candidate by product_id, skip or restore an ingredient, or change and search an item. Requires the current plan revision. Product IDs must come from this plan's returned eligible candidates.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "line_id": str, "product_id": str, "skipped": boolean, "item": schema(map[string]any{"label": str, "query": str, "amount": schema(map[string]any{"quantity": integer, "unit": str}, "quantity", "unit"), "required_traits": map[string]any{"type": "array", "items": str}}, "label", "query", "amount", "required_traits")}, "plan_id", "revision", "line_id")},
		{Name: "refresh_plan", Title: "Refresh prices", Description: "Read fresh product prices and availability in the same store context, preserving available selections. Returns a new revision.", InputSchema: schema(id, "plan_id", "revision")},
		{Name: "fill_basket", Title: "Fill retailer basket", Description: "Change the user's retailer basket to add the reviewed plan. Only call when the user requests filling it. Unresolved lines must first be selected or explicitly skipped. Requires current revision and a stable request_id for retry safety. Never checks out or pays.", InputSchema: schema(map[string]any{"plan_id": str, "revision": integer, "request_id": str, "acknowledge_warnings": boolean, "allow_over_budget": boolean}, "plan_id", "revision", "request_id")},
	}
	for _, t := range tools {
		read := t.Name == "open_ship_it"
		world := t.Name == "connect_store" || t.Name == "create_shopping_plan" || t.Name == "refresh_plan" || t.Name == "fill_basket" || t.Name == "update_plan_line"
		destructive := t.Name == "fill_basket"
		t.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: read, DestructiveHint: &destructive, OpenWorldHint: &world, IdempotentHint: read || t.Name == "fill_basket"}
		t.Meta = map[string]any{"ui": map[string]any{"resourceUri": pluginURI}, "openai/ui": map[string]any{}}
		if read {
			t.Meta["openai/ui"] = map[string]any{"entrypoints": []map[string]string{{"type": "global"}, {"type": "thread"}}}
		}
	}
	return tools
}

func (s *Server) callPluginTool(ctx context.Context, name string, raw json.RawMessage) (pluginSnapshot, error) {
	var args struct {
		PlanID              string             `json:"plan_id"`
		Revision            int                `json:"revision"`
		LineID              string             `json:"line_id"`
		RetailerID          string             `json:"retailer_id"`
		RetailerIDs         []string           `json:"retailer_ids"`
		Draft               *string            `json:"draft"`
		ProductID           *string            `json:"product_id"`
		Skipped             *bool              `json:"skipped"`
		Item                *shopping.Item     `json:"item"`
		Text                string             `json:"text"`
		Items               []shopping.Item    `json:"items"`
		Selections          []productSelection `json:"selections"`
		BudgetMinor         int64              `json:"budget_minor"`
		RequestID           string             `json:"request_id"`
		AcknowledgeWarnings bool               `json:"acknowledge_warnings"`
		AllowOverBudget     bool               `json:"allow_over_budget"`
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
	case "open_ship_it":
	case "configure_ship_it":
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
	case "fill_basket":
		var plan shopping.Plan
		plan, err = s.plans.Get(ctx, args.PlanID)
		if err != nil {
			break
		}
		basket, ok := chosenPlanBasket(plan)
		if !ok || basket.Missing > 0 {
			err = errors.New("Choose or skip every unresolved grocery before filling the basket.")
			break
		}
		clientID := s.config.Clients[0].ID
		for _, r := range s.config.Retailers {
			if r.ID == basket.RetailerID && r.Mode == "browser" {
				var session browser.Session
				session, err = s.connectStore(ctx, r.ID)
				if err == nil {
					clientID = session.ClientID
					if session.Status != "ready" {
						err = errors.New("Choose delivery or pickup at the store, then reconnect in Ship It.")
					} else if !sameStore(session.Context, basket.Context) {
						err = errors.New("The store or delivery settings changed. Refresh prices before filling the basket.")
					}
				}
				break
			}
		}
		if err != nil {
			break
		}
		var transfer shopping.Transfer
		transfer, _, err = s.fillPlan(ctx, args.PlanID, transferOptions{Revision: args.Revision, ClientID: clientID, AcknowledgeWarnings: args.AcknowledgeWarnings, AllowOverBudget: args.AllowOverBudget}, args.RequestID)
		if err == nil {
			err = s.plugin.state.Update(func(p *pluginPreferences) error { p.PlanID = args.PlanID; p.TransferID = transfer.ID; return nil })
		}
	default:
		err = shopping.ErrNotFound
	}
	if err != nil {
		return pluginSnapshot{}, err
	}
	if name == "select_store" || name == "select_products" || name == "update_plan_line" || name == "refresh_plan" {
		if err := s.plugin.state.Update(func(p *pluginPreferences) error { p.PlanID = args.PlanID; p.TransferID = ""; return nil }); err != nil {
			return pluginSnapshot{}, err
		}
	}
	return s.snapshot(ctx, args.PlanID)
}

func chosenPlanBasket(plan shopping.Plan) (shopping.Basket, bool) {
	for _, b := range plan.Baskets {
		if b.RetailerID == plan.RetailerID {
			return b, true
		}
	}
	return shopping.Basket{}, false
}
