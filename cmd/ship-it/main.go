package main

import (
	"flag"
	"log"
	"path/filepath"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/localserver"
	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/transfers"
)

func main() {
	path := flag.String("config", "config/dev.json", "service configuration file")
	flag.Parse()
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
