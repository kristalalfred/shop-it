package extension

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var reference = regexp.MustCompile(`(?:(?:src|href)="|(?:from|import) '\./)([^"']+)`)

func TestBundlesShipExactlyTheFilesTheExtensionUses(t *testing.T) {
	for _, browser := range []string{"chromium", "firefox"} {
		bundle := map[string][]byte{}
		if err := writeFiles(browser, func(name string, data []byte) error { bundle[name] = data; return nil }); err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Icons      map[string]string `json:"icons"`
			Background struct {
				ServiceWorker string   `json:"service_worker"`
				Scripts       []string `json:"scripts"`
			} `json:"background"`
			Action struct {
				Popup string            `json:"default_popup"`
				Icon  map[string]string `json:"default_icon"`
			} `json:"action"`
			ContentScripts []struct {
				JS []string `json:"js"`
			} `json:"content_scripts"`
		}
		if err := json.Unmarshal(bundle["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		pending := append([]string{"manifest.json", manifest.Background.ServiceWorker, manifest.Action.Popup}, manifest.Background.Scripts...)
		pending = slices.AppendSeq(pending, maps.Values(manifest.Icons))
		pending = slices.AppendSeq(pending, maps.Values(manifest.Action.Icon))
		for _, script := range manifest.ContentScripts {
			pending = append(pending, script.JS...)
		}
		used := map[string]bool{}
		for len(pending) > 0 {
			name := pending[0]
			pending = pending[1:]
			if name == "" || used[name] {
				continue
			}
			used[name] = true
			data, ok := bundle[name]
			if !ok {
				t.Errorf("%s bundle is missing %s", browser, name)
				continue
			}
			if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".html") {
				for _, match := range reference.FindAllSubmatch(data, -1) {
					pending = append(pending, string(match[1]))
				}
			}
		}
		for name := range bundle {
			if !used[name] {
				t.Errorf("%s bundle ships unused %s", browser, name)
			}
		}
	}
}

func TestBuildReplacesPreviousOutputOnly(t *testing.T) {
	dir := t.TempDir()
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "firefox", "removed.js")
	if err := os.WriteFile(stale, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file survived a rebuild: %v", err)
	}
	unrelated := t.TempDir()
	if err := os.Mkdir(filepath.Join(unrelated, "chromium"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "chromium", "notes.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Build(unrelated); err == nil {
		t.Fatal("replaced a directory that is not an extension build")
	}
	if _, err := os.Stat(filepath.Join(unrelated, "chromium", "notes.txt")); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}
