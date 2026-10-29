package interpret

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

type Codex struct {
	Binary string
	Model  string
}

func NewCodex(model string) (*Codex, error) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return nil, fmt.Errorf("codex interpreter requires the Codex CLI: %w", err)
	}
	return &Codex{Binary: binary, Model: model}, nil
}

func (c *Codex) Interpret(ctx context.Context, req shopping.InterpretationRequest, notify shopping.Notify) (shopping.Interpretation, error) {
	return interpret(ctx, c, "codex", req, notify)
}

func (c *Codex) generate(ctx context.Context, instructions, input string, schemaObject map[string]any, notify shopping.Notify) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "shop-it-interpret-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	schema, err := json.Marshal(schemaObject)
	if err != nil {
		return nil, err
	}
	schemaPath, outputPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "result.json")
	if err := os.WriteFile(schemaPath, schema, 0600); err != nil {
		return nil, err
	}
	encodedInstructions, _ := json.Marshal(instructions)
	args := []string{
		"exec", "--json", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only",
		"--disable", "shell_tool", "--disable", "apps", "--disable", "plugins", "--disable", "hooks", "--disable", "multi_agent",
		"--disable", "browser_use", "--disable", "computer_use",
		"-c", `web_search="disabled"`, "-c", `approval_policy="never"`, "-c", "project_doc_max_bytes=0", "-c", `model_reasoning_summary="concise"`,
		"-c", "developer_instructions=" + string(encodedInstructions),
		"--output-schema", schemaPath, "--output-last-message", outputPath,
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	args = append(args, "-")
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = time.Second
	if notify != nil {
		cmd.Stdout = &reasoningNotes{notify: notify}
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, providerError(ctx.Err())
		}
		return nil, fmt.Errorf("%w: Codex interpretation failed; check codex login status", shopping.ErrUpstream)
	}
	file, err := os.Open(outputPath)
	if err != nil {
		return nil, shopping.ErrUpstream
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, shopping.ErrUpstream
	}
	return data, nil
}

func decodeResult(data []byte, result any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(result) != nil || decoder.Decode(new(any)) != io.EOF {
		return shopping.ErrUpstream
	}
	return nil
}

type reasoningNotes struct {
	notify shopping.Notify
	line   []byte
}

func (r *reasoningNotes) Write(data []byte) (int, error) {
	written := len(data)
	for {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			r.keep(data)
			return written, nil
		}
		r.keep(data[:end])
		r.emit()
		r.line, data = r.line[:0], data[end+1:]
	}
}

func (r *reasoningNotes) keep(data []byte) {
	if len(r.line) < 64<<10 {
		r.line = append(r.line, data...)
	}
}

func (r *reasoningNotes) emit() {
	var event struct {
		Type string `json:"type"`
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if json.Unmarshal(r.line, &event) != nil || event.Type != "item.completed" || event.Item.Type != "reasoning" {
		return
	}
	if title := noteTitle(event.Item.Text); title != "" {
		r.notify(title)
	}
}

func noteTitle(text string) string {
	title, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	title = strings.TrimSpace(strings.Trim(strings.TrimSpace(title), "*"))
	if runes := []rune(title); len(runes) > 120 {
		title = string(runes[:119]) + "…"
	}
	return title
}
