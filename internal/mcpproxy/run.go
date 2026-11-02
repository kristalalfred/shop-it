package mcpproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kristalalfred/shop-it/internal/release"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authenticated struct{ token string }

func (a authenticated) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func Run(ctx context.Context, address, token, configPath, service string) error {
	client := &http.Client{Transport: authenticated{token}, Timeout: 200 * time.Second}
	endpoint := "http://" + address
	if err := ensureService(ctx, client, endpoint, address, configPath, service); err != nil {
		return err
	}
	remote, err := mcp.NewClient(&mcp.Implementation{Name: "shop-it-desktop", Version: release.Version}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint + "/mcp", HTTPClient: client}, nil)
	if err != nil {
		return err
	}
	defer remote.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "shop-it", Version: release.Version}, &mcp.ServerOptions{Instructions: "Use open_shop_it to resume setup or the current grocery plan. ChatGPT supplies the shopping reasoning and structured ingredients. Select matching candidate IDs from the returned plan. Only fill a retailer basket when requested by the user; checkout stays at the store."})
	tools, err := remote.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	for _, tool := range tools.Tools {
		server.AddTool(tool, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return remote.CallTool(ctx, &mcp.CallToolParams{Name: r.Params.Name, Arguments: r.Params.Arguments})
		})
	}
	resources, err := remote.ListResources(ctx, nil)
	if err != nil {
		return err
	}
	for _, resource := range resources.Resources {
		server.AddResource(resource, func(ctx context.Context, r *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return remote.ReadResource(ctx, r.Params)
		})
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

func ensureService(ctx context.Context, client *http.Client, endpoint, address, configPath, service string) error {
	ready := func() bool {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(probe, http.MethodPost, endpoint+"/mcp", nil)
		response, err := client.Do(req)
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode != 404 && response.StatusCode != 401 && response.StatusCode != 403
	}
	if ready() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if info, err := os.Stat(service); service != "" && err == nil && info.Mode().IsRegular() {
		exe = service
	}
	path, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(path), "plugin.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(exe, "-config", path, "-listen", address)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return fmt.Errorf("Shop It could not start: %v. Check %s", err, log.Name())
		case <-timeout.C:
			_ = command.Process.Kill()
			return errors.New("Shop It did not start within ten seconds")
		case <-ticker.C:
			if ready() {
				return nil
			}
		}
	}
}
