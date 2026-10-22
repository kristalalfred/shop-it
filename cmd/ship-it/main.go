package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/kristalalfred/ship-it/extension"
	"github.com/kristalalfred/ship-it/internal/mcpproxy"
	"github.com/kristalalfred/ship-it/plugin"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/localserver"
	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/transfers"
)

func main() {
	path := flag.String("config", ".data/local.json", "service configuration file")
	extensionDir := flag.String("extension-dir", "", "write Chromium and Firefox extension bundles and exit")
	stdio := flag.Bool("mcp", false, "connect ChatGPT to the local service over MCP stdio")
	pluginDir := flag.String("plugin-dir", "", "write an installable desktop plugin package and exit")
	interpreter := flag.String("interpreter", "", "override the text interpreter: builtin, codex or openai")
	listen := flag.String("listen", "", "override the local listen address")
	flag.Parse()
	if *extensionDir != "" {
		if err := extension.Build(*extensionDir); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *path == ".data/local.json" {
		if err := backend.InitLocal(*path); err != nil {
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
	if *interpreter != "" {
		config.Interpreter.Mode = *interpreter
	}
	if err := config.Validate(); err != nil {
		log.Fatal(err)
	}
	if *pluginDir != "" {
		if err := plugin.Build(*pluginDir, *path, config.Listen); err != nil {
			log.Fatal(err)
		}
		if err := plugin.Archive(*pluginDir, *pluginDir+".zip"); err != nil {
			log.Fatal(err)
		}
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
