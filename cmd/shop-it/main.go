package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kristalalfred/shop-it/extension"
	"github.com/kristalalfred/shop-it/internal/mcpproxy"
	"github.com/kristalalfred/shop-it/internal/release"
	"github.com/kristalalfred/shop-it/plugin"

	"github.com/kristalalfred/shop-it/internal/backend"
	"github.com/kristalalfred/shop-it/internal/localserver"
	"github.com/kristalalfred/shop-it/internal/persistence"
	"github.com/kristalalfred/shop-it/internal/transfers"
)

func main() {
	defaultConfig, err := backend.DefaultConfigPath()
	if err != nil {
		log.Fatal(err)
	}
	path := flag.String("config", defaultConfig, "service configuration file, created on first run")
	extensionDir := flag.String("extension-dir", "", "write Chromium and Firefox extension bundles and exit")
	stdio := flag.Bool("mcp", false, "connect ChatGPT to the local service over MCP stdio")
	service := flag.String("service", "", "with -mcp, the shop-it binary to start when the service is not running")
	writePlugin := flag.Bool("plugin", false, "write the ChatGPT plugin next to the configuration and exit")
	listen := flag.String("listen", "", "override the local listen address")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		fmt.Println("shop-it " + release.Version)
		return
	}
	if *extensionDir != "" {
		if err := extension.Build(*extensionDir); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *path == defaultConfig {
		if err := backend.EnsureConfig(*path); err != nil {
			log.Fatal(err)
		}
	}
	config, err := backend.LoadConfig(*path)
	if err != nil {
		log.Fatal(err)
	}
	if *listen != "" {
		config.Listen = *listen
	}
	if err := config.Validate(); err != nil {
		log.Fatal(err)
	}
	if *writePlugin {
		if err := plugin.Build(config.PluginDir(), *path); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ChatGPT plugin written to %s\n", config.PluginDir())
		return
	}
	if *stdio {
		if err := mcpproxy.Run(context.Background(), config.Listen, config.APIToken, *path, *service); err != nil {
			log.New(os.Stderr, "", 0).Fatal(err)
		}
		return
	}
	interactive := terminal()
	page := "http://" + config.Listen + "/"
	if running(page) {
		fmt.Printf("Shop It is already running at %s\n", page)
		if interactive {
			openBrowser(page)
		}
		return
	}
	unlock, err := persistence.Lock(config.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer unlock()
	if dir := config.PluginDir(); dir != "" {
		if err := plugin.Build(dir, *path); err != nil {
			log.Fatal(err)
		}
	}
	if dir := config.ExtensionDir(); dir != "" {
		if err := extension.Build(dir); err != nil {
			log.Fatal(err)
		}
	}
	transferPath := ""
	if config.DataDir != "" {
		transferPath = filepath.Join(config.DataDir, "transfers.json")
	}
	store, err := transfers.Open(transferPath)
	if err != nil {
		log.Fatal(err)
	}
	handler, err := backend.New(config, store)
	if err != nil {
		log.Fatal(err)
	}
	ready := func(string) {}
	if interactive {
		ready = openBrowser
	}
	if err := localserver.Run(config.Listen, handler, ready); err != nil {
		log.Fatal(err)
	}
}

func terminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func running(page string) bool {
	client := http.Client{Timeout: time.Second}
	response, err := client.Get(page + "healthz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var health struct {
		Mode string `json:"mode"`
	}
	return response.StatusCode == http.StatusOK && json.NewDecoder(response.Body).Decode(&health) == nil && health.Mode == "service"
}

func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err == nil {
		go command.Wait()
	}
}
