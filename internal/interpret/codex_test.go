package interpret

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

func TestCodexInterpretation(t *testing.T) {
	for _, mode := range []string{"valid", "failure", "invalid", "missing", "oversized", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("SHOP_IT_CODEX_HELPER", mode)
			t.Setenv("SHOP_IT_CODEX_CAPTURE", filepath.Join(dir, "capture.json"))
			binary := filepath.Join(dir, "codex")
			script := "#!/bin/sh\nexec " + strconv.Quote(os.Args[0]) + " -test.run=^TestCodexProcessHelper$ -- \"$@\"\n"
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			provider := &Codex{Binary: binary, Model: "test-model"}
			ctx := context.Background()
			if mode == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
			}
			var notes []string
			result, err := provider.Interpret(ctx, shopping.InterpretationRequest{Text: "Vegansk korvstroganoff för 5"}, func(note string) { notes = append(notes, note) })
			if mode == "valid" {
				if err != nil || result.Mode != "codex" || len(result.Items) != 1 || result.Items[0].Query != "vegokorv" {
					t.Fatalf("interpretation: %+v, %v", result, err)
				}
				if !slices.Equal(notes, []string{"Choosing a vegan stroganoff", "Estimating quantities"}) {
					t.Fatalf("reasoning notes: %q", notes)
				}
			} else if mode == "timeout" {
				if !errors.Is(err, shopping.ErrTimeout) {
					t.Fatalf("timeout: %v", err)
				}
			} else if !errors.Is(err, shopping.ErrUpstream) {
				t.Fatalf("failed interpretation fell back: %+v, %v", result, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "capture.json"))
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				Dir  string                         `json:"dir"`
				Args []string                       `json:"args"`
				Req  shopping.InterpretationRequest `json:"req"`
			}
			if err := json.Unmarshal(data, &capture); err != nil {
				t.Fatal(err)
			}
			if capture.Req.Text != "Vegansk korvstroganoff för 5" || !slices.Contains(capture.Args, "test-model") {
				t.Fatalf("wrong request: %+v", capture)
			}
			for _, arg := range []string{"--json", "--ignore-user-config", "--ephemeral", "read-only", "shell_tool", "apps", "plugins", "hooks", "multi_agent", `web_search="disabled"`} {
				if !slices.Contains(capture.Args, arg) {
					t.Errorf("missing argument %s", arg)
				}
			}
			if _, err := os.Stat(capture.Dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temporary interpretation directory remains: %s", capture.Dir)
			}
		})
	}
}

func TestCodexProcessHelper(t *testing.T) {
	mode := os.Getenv("SHOP_IT_CODEX_HELPER")
	if mode == "" {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	var req shopping.InterpretationRequest
	if json.Unmarshal(data, &req) != nil {
		os.Exit(2)
	}
	dir, _ := os.Getwd()
	capture, _ := json.Marshal(map[string]any{"dir": dir, "args": os.Args, "req": req})
	if os.WriteFile(os.Getenv("SHOP_IT_CODEX_CAPTURE"), capture, 0600) != nil {
		os.Exit(2)
	}
	output := ""
	for i, arg := range os.Args {
		if arg == "--output-last-message" {
			output = os.Args[i+1]
		}
		if arg == "--output-schema" {
			data, err := os.ReadFile(os.Args[i+1])
			if err != nil || !json.Valid(data) {
				os.Exit(2)
			}
		}
	}
	for _, event := range []string{
		`{"type":"thread.started","thread_id":"thread"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Choosing a vegan stroganoff**\n\nThe user wants five portions."}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","text":"**Not a note**"}}`,
		`not json`,
		`{"type":"item.completed","item":{"id":"item_2","type":"reasoning","text":"**Estimating quantities**"}}`,
	} {
		os.Stdout.WriteString(event + "\n")
	}
	text := `{"items":[{"label":"Vegokorv","query":"vegokorv","amount":{"quantity":750,"unit":"g"},"required_traits":["vegan"]}],"warnings":["Korvstroganoff för 5 portioner, mängderna är uppskattade."]}`
	switch mode {
	case "failure":
		os.Exit(1)
	case "invalid":
		text = strings.Replace(text, "750", "-1", 1)
	case "missing":
		os.Exit(0)
	case "oversized":
		text = strings.Repeat("x", (1<<20)+1)
	case "timeout":
		time.Sleep(time.Minute)
	}
	if output == "" || os.WriteFile(output, []byte(text), 0600) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
