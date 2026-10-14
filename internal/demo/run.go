package demo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/mockio"
	"github.com/kristalalfred/ship-it/internal/persistence"
	"github.com/kristalalfred/ship-it/internal/transfers"
)

func Run(ctx context.Context, config backend.Config, scenario mockio.Scenario) error {
	unlock, err := persistence.Lock(config.DataDir)
	if err != nil {
		return err
	}
	defer unlock()
	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	gateway := &http.Server{Handler: mockio.RetailerHandler(scenario), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = gateway.Serve(gatewayListener) }()
	defer gateway.Close()
	gatewayURL := "http://" + gatewayListener.Addr().String()
	config.Retailers = slices.Clone(config.Retailers)
	for i := range config.Retailers {
		if config.Retailers[i].Mode != "mock" {
			return fmt.Errorf("demo requires mock retailer modes")
		}
		config.Retailers[i].GatewayURL = gatewayURL + "/retailers/" + config.Retailers[i].ID
	}
	path := ""
	if config.DataDir != "" {
		path = filepath.Join(config.DataDir, "transfers.json")
	}
	store, err := transfers.Open(path)
	if err != nil {
		return err
	}
	handler, err := backend.New(config, store)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, len(config.Clients)+1)
	go func() { finished <- server.Serve(listener) }()
	url := "http://" + listener.Addr().String()
	fmt.Printf("Local demo ready: %s\nRetailer catalogs and basket operations are simulated.\nPress Ctrl+C to stop.\n", url)
	for _, client := range config.Clients {
		go func() { finished <- mockio.NewBrowser(scenario.Browser).Run(ctx, url, client.Token) }()
	}
	var result error
	select {
	case <-ctx.Done():
	case result = <-finished:
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	result = errors.Join(result, server.Shutdown(shutdown))
	if errors.Is(result, http.ErrServerClosed) || errors.Is(result, context.Canceled) {
		return nil
	}
	return result
}
