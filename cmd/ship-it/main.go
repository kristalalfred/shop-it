package main

import (
	"flag"
	"log"
	"path/filepath"

	"github.com/kristalalfred/ship-it/extension"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/localserver"
	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/transfers"
)

func main() {
	path := flag.String("config", ".data/local.json", "service configuration file")
	extensionDir := flag.String("extension-dir", "", "write Chromium and Firefox extension bundles and exit")
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
