package interpret

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

const providerTimeout = 90 * time.Second

type OpenAI struct {
	URL    string
	Key    string
	Model  string
	Client *http.Client
}

func NewOpenAI(key, model string) *OpenAI {
	return &OpenAI{URL: "https://api.openai.com/v1/responses", Key: key, Model: model, Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (o *OpenAI) Interpret(ctx context.Context, req shopping.InterpretationRequest, notify shopping.Notify) (shopping.Interpretation, error) {
	return interpret(ctx, o, "openai", req, notify)
}

func (o *OpenAI) generate(ctx context.Context, instructions, input string, schema map[string]any, notify shopping.Notify) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"model": o.Model, "store": false, "stream": true,
		"instructions": instructions, "reasoning": map[string]any{"summary": "auto"},
		"input": input, "text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "grocery_result", "strict": true, "schema": schema}},
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, shopping.ErrUpstream
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.Key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	res, err := o.Client.Do(httpReq)
	if err != nil {
		return nil, providerError(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, shopping.ErrUpstream
	}
	data, err := readResponseStream(res.Body, notify)
	if err != nil && ctx.Err() != nil {
		return nil, providerError(ctx.Err())
	}
	return data, err
}

func readResponseStream(body io.Reader, notify shopping.Notify) ([]byte, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		data, ok := bytes.CutPrefix(scanner.Bytes(), []byte("data:"))
		if !ok {
			continue
		}
		var event struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(data, &event) != nil {
			return nil, shopping.ErrUpstream
		}
		switch event.Type {
		case "response.reasoning_summary_text.done":
			if title := noteTitle(event.Text); notify != nil && title != "" {
				notify(title)
			}
		case "response.completed":
			return outputText(event.Response)
		case "response.failed", "response.incomplete", "error":
			return nil, shopping.ErrUpstream
		}
	}
	return nil, shopping.ErrUpstream
}

func outputText(data []byte) ([]byte, error) {
	var response struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if json.Unmarshal(data, &response) != nil || response.Status != "completed" {
		return nil, shopping.ErrUpstream
	}
	var result []byte
	count := 0
	for _, output := range response.Output {
		for _, content := range output.Content {
			if content.Type == "refusal" {
				return nil, shopping.ErrUpstream
			}
			if output.Type == "message" && content.Type == "output_text" {
				count++
				result = []byte(content.Text)
			}
		}
	}
	if count != 1 || len(result) > 1<<20 {
		return nil, shopping.ErrUpstream
	}
	return result, nil
}

func providerError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return shopping.ErrTimeout
	}
	return shopping.ErrUpstream
}
