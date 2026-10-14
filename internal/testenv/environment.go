package testenv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/mockio"
	"github.com/kristalalfred/ship-it/internal/shopping"
)

const (
	UserToken   = "local-demo-api-token"
	ClientToken = "local-demo-client-token"
	OtherToken  = "other-client-token"
)

type Options struct {
	Binary   string
	Config   backend.Config
	Scenario mockio.Scenario
}

type Environment struct {
	URL        string
	IOURL      string
	Browser    *mockio.Browser
	Client     *http.Client
	config     backend.Config
	dir        string
	server     *http.Server
	command    *exec.Cmd
	done       chan struct{}
	processErr error
	logs       lockedBuffer
	mu         sync.Mutex
	calls      []shopping.SearchRequest
	closeOnce  sync.Once
	closeErr   error
}

func Start(ctx context.Context, options Options) (_ *Environment, err error) {
	e := &Environment{
		Browser: mockio.NewBrowser(options.Scenario.Browser),
		Client:  &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}},
		calls:   make([]shopping.SearchRequest, 0),
	}
	e.dir, err = os.MkdirTemp("", "ship-it-testenv-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, e.Close())
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	e.IOURL = "http://" + listener.Addr().String()
	gateway := mockio.RetailerHandler(options.Scenario)
	mux := http.NewServeMux()
	mux.Handle("/retailers/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var req shopping.SearchRequest
		if json.Unmarshal(body, &req) == nil {
			e.mu.Lock()
			e.calls = append(e.calls, req)
			e.mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		gateway.ServeHTTP(w, r)
	}))
	e.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = e.server.Serve(listener) }()
	config := options.Config
	config.Listen = "127.0.0.1:0"
	config.APIToken = UserToken
	config.AdapterTimeoutMS = 200
	config.Clients = []backend.ClientConfig{{ID: "demo-browser", Token: ClientToken}, {ID: "other-browser", Token: OtherToken}}
	config.Retailers = slices.Clone(config.Retailers)
	for i := range config.Retailers {
		config.Retailers[i].GatewayURL = e.IOURL + "/retailers/" + config.Retailers[i].ID
	}
	e.config = config
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(e.dir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	e.command = exec.Command(options.Binary, "-config", path)
	e.command.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
	e.command.Stderr = &e.logs
	stdout, err := e.command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := e.command.Start(); err != nil {
		return nil, err
	}
	ready := make(chan string, 1)
	e.done = make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = e.logs.Write([]byte(line + "\n"))
			if strings.HasPrefix(line, "listening on ") {
				ready <- strings.TrimPrefix(line, "listening on ")
			}
		}
	}()
	go func() { e.processErr = e.command.Wait(); close(e.done) }()
	select {
	case e.URL = <-ready:
	case <-e.done:
		return nil, fmt.Errorf("backend did not start: %v\n%s", e.processErr, e.logs.String())
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("backend startup timed out")
	}
	e.registerControls(mux)
	return e, nil
}

func (e *Environment) CatalogRequests() []shopping.SearchRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

func (e *Environment) Variables() map[string]string {
	return map[string]string{
		"base_url": e.URL, "io_url": e.IOURL,
		"user_token": UserToken, "client_token": ClientToken, "other_token": OtherToken,
	}
}

func (e *Environment) Close() error {
	e.closeOnce.Do(func() {
		e.Client.CloseIdleConnections()
		if e.done != nil {
			_ = e.command.Process.Signal(os.Interrupt)
			select {
			case <-e.done:
				if e.processErr != nil {
					e.closeErr = fmt.Errorf("backend exited: %v\n%s", e.processErr, e.logs.String())
				}
			case <-time.After(6 * time.Second):
				_ = e.command.Process.Kill()
				<-e.done
				e.closeErr = fmt.Errorf("backend did not shut down\n%s", e.logs.String())
			}
		}
		if e.server != nil {
			e.closeErr = errors.Join(e.closeErr, e.server.Close())
		}
		if e.dir != "" {
			e.closeErr = errors.Join(e.closeErr, os.RemoveAll(e.dir))
		}
	})
	return e.closeErr
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
