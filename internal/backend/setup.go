package backend

import (
	"bufio"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kristalalfred/shop-it/internal/browser"
	"github.com/kristalalfred/shop-it/plugin"
)

type setupState struct {
	Stores           []pluginStore `json:"stores"`
	BrowserOnline    bool          `json:"browser_online"`
	ChatGPTConnected bool          `json:"chatgpt_connected"`
	SetupURL         string        `json:"setup_url"`
	InstallURL       string        `json:"install_url,omitempty"`
	ChatURL          string        `json:"chat_url,omitempty"`
	ExtensionDir     string        `json:"extension_dir,omitempty"`
	Demo             bool          `json:"demo"`
}

type pluginStore struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	URL      string           `json:"url"`
	Mode     string           `json:"mode"`
	Selected bool             `json:"selected"`
	Session  *browser.Session `json:"session,omitempty"`
}

func (s *Server) setup(prefs pluginPreferences) setupState {
	out := setupState{Stores: []pluginStore{}, SetupURL: "http://" + s.config.Listen + "/", ExtensionDir: s.config.ExtensionDir(), Demo: true, ChatGPTConnected: prefs.ChatGPTConnected || codexHasPlugin()}
	for _, c := range s.config.Clients {
		out.BrowserOnline = out.BrowserOnline || s.browser.Online(c.ID)
	}
	if root := s.config.PluginDir(); root != "" {
		if _, err := os.Stat(plugin.MarketplacePath(root)); err == nil {
			out.InstallURL = "codex://plugins/shop-it?marketplacePath=" + queryEscape(plugin.MarketplacePath(root))
			out.ChatURL = "codex://new?path=" + queryEscape(root) + "&prompt=" + queryEscape("[@Shop It](plugin://shop-it@shop-it-local) "+prefs.Draft)
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
	return out
}

// Claude: Codex starts a plugin's MCP server only when a thread starts, so an installed plugin is read from the Codex configuration.
func codexHasPlugin() bool {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		home = filepath.Join(dir, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return false
	}
	table := false
	for lines := bufio.NewScanner(bytes.NewReader(data)); lines.Scan(); {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") {
			table = line == `[plugins."shop-it@shop-it-local"]`
		} else if table && strings.ReplaceAll(line, " ", "") == "enabled=true" {
			return true
		}
	}
	return false
}

func (s *Server) registerSetup(mux *http.ServeMux) {
	respondSetup := func(w http.ResponseWriter) {
		prefs, err := s.plugin.state.Read()
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, http.StatusOK, s.setup(prefs))
	}
	mux.HandleFunc("GET /v1/setup", s.user(func(w http.ResponseWriter, r *http.Request) { respondSetup(w) }))
	mux.HandleFunc("PUT /v1/setup/stores", s.user(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RetailerIDs []string `json:"retailer_ids"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.RetailerIDs == nil {
			req.RetailerIDs = []string{}
		}
		if err := s.configurePlugin(req.RetailerIDs, nil); err != nil {
			fail(w, err)
			return
		}
		respondSetup(w)
	}))
	mux.HandleFunc("POST /v1/setup/stores/{id}/check", s.user(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.connectStore(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		respondSetup(w)
	}))
}
