package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/kristalalfred/shop-it/extension"
	"github.com/kristalalfred/shop-it/internal/mcpproxy"
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
	writePlugin := flag.Bool("plugin", false, "write the ChatGPT plugin next to the configuration and exit")
	listen := flag.String("listen", "", "override the local listen address")
	flag.Parse()
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
		fmt.Printf("ChatGPT plugin written to %s\nInstall it with:\n  codex plugin marketplace add %q\n  codex plugin add shop-it@shop-it-local\n", config.PluginDir(), config.PluginDir())
		return
	}
	if *stdio {
		if err := mcpproxy.Run(context.Background(), config.Listen, config.APIToken, *path); err != nil {
			log.New(os.Stderr, "", 0).Fatal(err)
		}
		return
	}
	unlock, err := persistence.Lock(config.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer unlock()
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
	if err := localserver.Run(config.Listen, handler); err != nil {
		log.Fatal(err)
	}
}
