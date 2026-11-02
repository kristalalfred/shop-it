package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/browser"
	"github.com/kristalalfred/shop-it/internal/shopping"
	"github.com/kristalalfred/shop-it/internal/transfers"
	"github.com/kristalalfred/shop-it/plugin"
)

func setupTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	raw, _ := json.Marshal(Config{Listen: "127.0.0.1:24816", APIToken: "user-secret", AdapterTimeoutMS: 1000, DataDir: "data", Clients: []ClientConfig{{ID: "browser", Token: "client-secret"}}, Retailers: []RetailerConfig{
		{ID: "mathem", Name: "Mathem", Mode: "browser", BasketURL: BasketURLs["mathem"]},
		{ID: "willys", Name: "Willys", Mode: "browser", BasketURL: BasketURLs["willys"]},
	}})
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plugin.MarketplacePath(config.PluginDir())), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugin.MarketplacePath(config.PluginDir()), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := transfers.Open("")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(config, store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, dir
}

func setupCall(t *testing.T, url, path, method string, body any) setupState {
	t.Helper()
	status, raw := browserRequest(t, t.Context(), url, path, method, "user-secret", body)
	if status != http.StatusOK {
		t.Fatalf("setup request: %d %s", status, raw)
	}
	var state setupState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSetupFollowsTheExtensionChatGPTAndStoreChecks(t *testing.T) {
	server, dir := setupTestServer(t)
	get := func() setupState {
		return setupCall(t, server.URL, "/v1/setup", "GET", nil)
	}
	fresh := get()
	marketplace := filepath.Join(dir, "plugin", ".agents", "plugins", "marketplace.json")
	if fresh.BrowserOnline || fresh.ChatGPTConnected || fresh.Demo || len(fresh.Stores) != 2 || fresh.ExtensionDir != filepath.Join(dir, "extension") || fresh.InstallURL != "codex://plugins/shop-it?marketplacePath="+queryEscape(marketplace) || fresh.ChatURL != "codex://new?path="+queryEscape(filepath.Join(dir, "plugin"))+"&prompt=%5B%40Shop%20It%5D%28plugin%3A%2F%2Fshop-it%40shop-it-local%29%20" {
		t.Fatalf("fresh setup: %+v", fresh)
	}
	if status, _ := browserRequest(t, t.Context(), server.URL, "/v1/setup", "GET", "client-secret", nil); status != http.StatusUnauthorized {
		t.Fatalf("the extension token read setup: %d", status)
	}

	codex := filepath.Join(dir, "codex", "config.toml")
	os.MkdirAll(filepath.Dir(codex), 0700)
	os.WriteFile(codex, []byte("[plugins.\"shop-it@shop-it-local\"]\nenabled = false\n\n[plugins.\"other@market\"]\nenabled = true\n"), 0600)
	if get().ChatGPTConnected {
		t.Fatal("a disabled plugin counted as added to ChatGPT")
	}
	os.WriteFile(codex, []byte("[marketplaces.shop-it-local]\nsource = \"/plugin\"\n\n[plugins.\"shop-it@shop-it-local\"]\nenabled = true\n"), 0600)
	if !get().ChatGPTConnected {
		t.Fatal("the plugin enabled in Codex was not detected")
	}
	os.Remove(codex)
	if get().ChatGPTConnected {
		t.Fatal("ChatGPT counted as connected without the plugin")
	}
	pluginClient(t, server.URL).ListTools(t.Context(), nil)
	if !get().ChatGPTConnected {
		t.Fatal("a plugin call did not mark ChatGPT as connected")
	}

	if status, _ := browserRequest(t, t.Context(), server.URL, "/v1/setup/stores", "PUT", "user-secret", map[string]any{"retailer_ids": []string{"ica"}}); status != http.StatusBadRequest {
		t.Fatalf("unknown store accepted: %d", status)
	}
	chosen := setupCall(t, server.URL, "/v1/setup/stores", "PUT", map[string]any{"retailer_ids": []string{"willys"}})
	if chosen.Stores[0].Selected || !chosen.Stores[1].Selected {
		t.Fatalf("store choice: %+v", chosen.Stores)
	}
	status, raw := browserRequest(t, t.Context(), server.URL, "/v1/setup/stores/willys/check", "POST", "user-secret", nil)
	if status != http.StatusConflict || !json.Valid(raw) || string(raw) != `{"error":"Connect the Shop It browser extension, then try again."}`+"\n" {
		t.Fatalf("check without the extension: %d %s", status, raw)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			status, raw := browserRequest(t, ctx, server.URL, "/v1/client/commands?wait=1", "POST", "client-secret", nil)
			if status != http.StatusOK {
				return
			}
			var batch struct {
				Commands []browser.Command `json:"commands"`
			}
			json.Unmarshal(raw, &batch)
			for _, command := range batch.Commands {
				result, _ := json.Marshal(browser.Session{Status: "ready", Label: "Willys Fältöversten", Context: shopping.StoreContext{StoreID: "2110", PostalCode: "11122", Fulfillment: "pickup"}})
				browserRequest(t, ctx, server.URL, "/v1/client/commands/"+command.ID+"/result", "PUT", "client-secret", browser.Reply{Result: result})
			}
		}
	}()
	for !get().BrowserOnline {
		time.Sleep(5 * time.Millisecond)
	}
	checked := setupCall(t, server.URL, "/v1/setup/stores/willys/check", "POST", nil)
	if !checked.BrowserOnline || checked.Stores[1].Session == nil || checked.Stores[1].Session.Status != "ready" || checked.Stores[1].Session.Label != "Willys Fältöversten" {
		t.Fatalf("checked store: %+v", checked)
	}
	if prefs := get(); !prefs.Stores[1].Selected || prefs.Stores[1].Session == nil {
		t.Fatalf("setup forgot the checked store: %+v", prefs)
	}
}
