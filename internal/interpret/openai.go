package interpret

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type OpenAI struct {
	URL    string
	Key    string
	Model  string
	Client *http.Client
}

func NewOpenAI(key, model string) *OpenAI {
	return &OpenAI{URL: "https://api.openai.com/v1/responses", Key: key, Model: model, Client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (o *OpenAI) Interpret(ctx context.Context, req shopping.InterpretationRequest, notify shopping.Notify) (shopping.Interpretation, error) {
	data, err := o.generate(ctx, groceryInstructions, req, grocerySchema(), notify)
	if err != nil {
		return shopping.Interpretation{}, err
	}
	var result shopping.Interpretation
	if decodeResult(data, &result) != nil || result.Items == nil || result.Warnings == nil {
		return shopping.Interpretation{}, shopping.ErrUpstream
	}
	result.Mode = "openai"
	return result, Validate(result)
}

func (o *OpenAI) generate(ctx context.Context, instructions string, request any, schema map[string]any, _ shopping.Notify) ([]byte, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"model": o.Model, "store": false, "max_output_tokens": 6000,
		"instructions": instructions,
		"input":        string(input), "text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "grocery_result", "strict": true, "schema": schema}},
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
	res, err := o.Client.Do(httpReq)
	if err != nil {
		return nil, providerError(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, shopping.ErrUpstream
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, providerError(err)
	}
	if len(data) > 1<<20 {
		return nil, shopping.ErrUpstream
	}
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
	if count != 1 {
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
