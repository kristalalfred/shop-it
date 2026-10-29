package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kristalalfred/shop-it/internal/backend"
	"github.com/kristalalfred/shop-it/internal/demo"
	"github.com/kristalalfred/shop-it/internal/mockio"
	"github.com/kristalalfred/shop-it/internal/testenv"
)

func main() {
	selection := flag.String("case", "all", "case name or glob, all, or concurrency")
	mode := flag.String("mode", "test", "harness mode: test or demo")
	configPath := flag.String("config", "config/dev.json", "development configuration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch *mode {
	case "test":
		err = testenv.Run(ctx, *selection, os.Stdout)
	case "demo":
		err = runDemo(ctx, *configPath)
	default:
		err = fmt.Errorf("unknown harness mode %q", *mode)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runDemo(ctx context.Context, configPath string) (err error) {
	config, err := backend.LoadConfig(configPath)
	if err != nil {
		return err
	}
	scenario, err := mockio.LoadScenario("testdata/demo.json")
	if err != nil {
		return err
	}
	return demo.Run(ctx, config, scenario)
}
