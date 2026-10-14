package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/mockio"
	"github.com/kristalalfred/ship-it/internal/testenv"
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
		err = demo(ctx, *configPath)
	default:
		err = fmt.Errorf("unknown harness mode %q", *mode)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func demo(ctx context.Context, configPath string) (err error) {
	config, err := backend.LoadConfig(configPath)
	if err != nil {
		return err
	}
	scenario, err := mockio.LoadScenario("testdata/demo.json")
	if err != nil {
		return err
	}
	env, err := testenv.Start(ctx, testenv.Options{Binary: "./bin/ship-it", Config: config, Scenario: scenario})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, env.Close()) }()
	fmt.Printf("Local demo ready\nBackend: %s\nFake I/O: %s\nPress Ctrl+C to stop.\n", env.URL, env.IOURL)
	return env.Browser.Run(ctx, env.URL, testenv.ClientToken)
}
