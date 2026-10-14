package main

import (
	"flag"
	"log"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/localserver"
	"github.com/kristalalfred/ship-it/internal/transfers"
)

func main() {
	path := flag.String("config", "config/dev.json", "scaffold configuration file")
	flag.Parse()
	config, err := backend.LoadConfig(*path)
	if err != nil {
		log.Fatal(err)
	}
	handler, err := backend.New(config, transfers.NewMemory())
	if err != nil {
		log.Fatal(err)
	}
	if err := localserver.Run(config.Listen, handler); err != nil {
		log.Fatal(err)
	}
}
