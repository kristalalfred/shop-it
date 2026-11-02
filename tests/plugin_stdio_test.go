package tests_test

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/backend"
	"github.com/kristalalfred/shop-it/internal/release"
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
	build := exec.Command(binary, "-config", path, "-plugin")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("package: %v %s", err, output)
	}
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	raw, err = os.ReadFile(filepath.Join(dir, "plugin", ".agents", "plugins", "marketplace.json"))
	if err != nil || json.Unmarshal(raw, &marketplace) != nil || marketplace.Name != "shop-it-local" || len(marketplace.Plugins) != 1 {
		t.Fatalf("marketplace: %v %s", err, raw)
	}
	packageDir := filepath.Join(dir, "plugin", filepath.FromSlash(marketplace.Plugins[0].Source.Path))
	for _, name := range []string{"plugin.json", "mcp.json", "skills/setup/SKILL.md", "bin/shop-it"} {
		info, err := os.Stat(filepath.Join(packageDir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("package missing %s", name)
		}
		if name == "bin/shop-it" && info.Mode().Perm()&0111 == 0 {
			t.Fatal("packaged executable lost its permissions")
		}
	}
	manifest, err := os.ReadFile(filepath.Join(packageDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte(config.APIToken)) {
		t.Fatal("credentials were copied into the install package")
	}
	var servers struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Cwd     string   `json:"cwd"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(manifest, &servers); err != nil {
		t.Fatal(err)
	}
	server := servers.MCPServers["shop-it"]
	if !strings.HasPrefix(server.Command, "./") || server.Cwd != "${PLUGIN_ROOT}" {
		t.Fatalf("Codex only launches commands and working directories inside the plugin: %+v", server)
	}
	if len(server.Args) != 5 || server.Args[3] != "-service" || server.Args[4] != binary {
		t.Fatalf("the plugin does not start the installed binary: %v", server.Args)
	}
	command := exec.Command(filepath.Join(packageDir, server.Command), server.Args...)
	command.Dir = packageDir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "installed-plugin-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: command, TerminateDuration: 2 * time.Second}, nil)
	if err != nil {
		t.Fatalf("connect installed binary: %v %s", err, stderr.String())
	}
	defer session.Close()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 8 {
		t.Fatalf("tools: %v %s", err, stderr.String())
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "open_shop_it", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("open: %v %+v", err, result)
	}
	resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "ui://shop-it/planner-v1.html"})
	if err != nil || len(resource.Contents) != 1 || resource.Contents[0].MIMEType != "text/html;profile=mcp-app" {
		t.Fatalf("UI resource: %v %+v", err, resource)
	}
	if strings.Contains(resource.Contents[0].Text, config.APIToken) {
		t.Fatal("credentials leaked into the ChatGPT UI")
	}
}

func TestPluginStartsTheInstalledServiceAndReportsItsVersion(t *testing.T) {
	output, err := exec.Command(binary, "-version").Output()
	if err != nil || string(output) != "shop-it "+release.Version+"\n" {
		t.Fatalf("version: %q %v", output, err)
	}
	config, err := backend.LoadConfig("../config/dev.json")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config.Listen = listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	config.DataDir = filepath.Join(dir, "data")
	path := filepath.Join(dir, "config.json")
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "-config", path, "-plugin").CombinedOutput(); err != nil {
		t.Fatalf("package: %v %s", err, output)
	}
	marker := filepath.Join(dir, "service.pid")
	installed := filepath.Join(dir, "installed-shop-it")
	script := "#!/bin/sh\necho $$ > " + strconv.Quote(marker) + "\nexec " + strconv.Quote(binary) + " \"$@\"\n"
	if err := os.WriteFile(installed, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(dir, "plugin", "shop-it")
	command := exec.Command(filepath.Join(packageDir, "bin", "shop-it"), "-mcp", "-config", path, "-service", installed)
	command.Dir = packageDir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	session, err := mcp.NewClient(&mcp.Implementation{Name: "upgrade-test", Version: "1"}, nil).Connect(t.Context(), &mcp.CommandTransport{Command: command, TerminateDuration: 2 * time.Second}, nil)
	if err != nil {
		t.Fatalf("connect: %v %s", err, stderr.String())
	}
	defer session.Close()
	pid, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the plugin did not start the installed binary: %v", err)
	}
	t.Cleanup(func() {
		if id, err := strconv.Atoi(strings.TrimSpace(string(pid))); err == nil {
			if process, err := os.FindProcess(id); err == nil {
				_ = process.Signal(os.Interrupt)
			}
		}
	})
	if tools, err := session.ListTools(t.Context(), nil); err != nil || len(tools.Tools) != 8 {
		t.Fatalf("tools through the started service: %v", err)
	}
}
