package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kristalalfred/ship-it/internal/localserver"
	"github.com/kristalalfred/ship-it/internal/mockio"
)

func main() {
	mode := flag.String("mode", "retailers", "mock mode: retailers or browser")
	listen := flag.String("listen", "127.0.0.1:18081", "mock retailer listen address")
	scenarioPath := flag.String("scenario", "testdata/demo.json", "synthetic I/O scenario")
	backendURL := flag.String("backend", "http://127.0.0.1:18080", "backend URL for browser polling")
	token := flag.String("token", "local-demo-client-token", "paired browser token")
	flag.Parse()
	scenario, err := mockio.LoadScenario(*scenarioPath)
	if err != nil {
		log.Fatal(err)
	}
	switch *mode {
	case "retailers":
		err = localserver.Run(*listen, mockio.RetailerHandler(scenario))
	case "browser":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = mockio.NewBrowser(scenario.Browser).Run(ctx, *backendURL, *token)
		if errors.Is(err, context.Canceled) {
			err = nil
		}
	default:
		log.Fatalf("unknown mock mode %q", *mode)
	}
	if err != nil {
		log.Fatal(err)
	}
}
