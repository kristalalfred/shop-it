package tests_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBuiltPluginPackageConnectsOverStdio(t *testing.T) {
	system := startSystem(t, demoScenario(t))
	config, err := backend.LoadConfig("../config/dev.json")
	if err != nil {
		t.Fatal(err)
	}
	config.Listen = strings.TrimPrefix(system.url, "http://")
	config.APIToken = userToken
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(dir, "plugin")
	build := exec.Command(binary, "-config", path, "-plugin-dir", packageDir)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("package: %v %s", err, output)
	}
	archive, err := zip.OpenReader(packageDir + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	names := map[string]bool{}
	for _, f := range archive.File {
		names[f.Name] = true
		if f.Name == "bin/ship-it" && f.Mode().Perm()&0111 == 0 {
			t.Fatal("packaged executable lost its permissions")
		}
	}
	for _, name := range []string{"plugin.json", "mcp.json", "skills/setup/SKILL.md", "bin/ship-it"} {
		if !names[name] {
			t.Fatalf("package missing %s", name)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(packageDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte(config.APIToken)) {
		t.Fatal("credentials were copied into the install package")
	}
	command := exec.Command(filepath.Join(packageDir, "bin", "ship-it"), "-mcp", "-config", path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "installed-plugin-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: command, TerminateDuration: 2 * time.Second}, nil)
	if err != nil {
		t.Fatalf("connect installed binary: %v %s", err, stderr.String())
	}
	defer session.Close()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 9 {
		t.Fatalf("tools: %v %s", err, stderr.String())
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "open_ship_it", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("open: %v %+v", err, result)
	}
	resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "ui://ship-it/planner-v1.html"})
	if err != nil || len(resource.Contents) != 1 || resource.Contents[0].MIMEType != "text/html;profile=mcp-app" {
		t.Fatalf("UI resource: %v %+v", err, resource)
	}
	if strings.Contains(resource.Contents[0].Text, config.APIToken) {
		t.Fatal("credentials leaked into the ChatGPT UI")
	}
}
