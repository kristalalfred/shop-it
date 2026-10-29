package interpret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

func eventStream(events ...any) string {
	var stream strings.Builder
	for _, event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintf(&stream, "event: %s\ndata: %s\n\n", event.(map[string]any)["type"], data)
	}
	return stream.String()
}

func completed(output ...any) map[string]any {
	return map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": output}}
}

func message(text string) map[string]any {
	return map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}
}

func TestStructuredInterpretationContract(t *testing.T) {
	text := `{"items":[{"label":"milk","query":"mjölk","amount":{"quantity":1000,"unit":"ml"},"required_traits":["organic"]}],"warnings":["Assumed breakfast for two."]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || req["model"] != "test-model" || req["store"] != false || req["stream"] != true || req["input"] != `{"text":"organic breakfast for two"}` {
			t.Errorf("wrong interpreter request: %v", req)
		}
		if _, capped := req["max_output_tokens"]; capped {
			t.Errorf("output is capped: %v", req)
		}
		if req["reasoning"].(map[string]any)["summary"] != "auto" {
			t.Errorf("missing reasoning summaries: %v", req)
		}
		format := req["text"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Errorf("missing structured output: %v", format)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, eventStream(
			map[string]any{"type": "response.created"},
			map[string]any{"type": "response.reasoning_summary_text.done", "text": "**Planning breakfast**\n\nTwo people need milk."},
			map[string]any{"type": "response.reasoning_summary_text.done", "text": "**Choosing organic milk**"},
			map[string]any{"type": "response.output_text.delta", "delta": `{"items"`},
			completed(map[string]any{"type": "reasoning"}, message(text)),
		))
	}))
	defer server.Close()
	provider := NewOpenAI("test-key", "test-model")
	provider.URL = server.URL
	var notes []string
	result, err := provider.Interpret(context.Background(), shopping.InterpretationRequest{Text: "organic breakfast for two"}, func(note string) { notes = append(notes, note) })
	if err != nil || result.Mode != "openai" || len(result.Items) != 1 || result.Items[0].Amount.Quantity != 1000 || len(result.Warnings) != 1 {
		t.Fatalf("result: %+v, %v", result, err)
	}
	if !slices.Equal(notes, []string{"Planning breakfast", "Choosing organic milk"}) {
		t.Fatalf("reasoning notes: %q", notes)
	}
}

func TestProviderFailureIsNotAListFallback(t *testing.T) {
	invalid := `{"items":[{"label":"milk","query":"mjölk","amount":{"quantity":-1,"unit":"ml"},"required_traits":[]}],"warnings":[]}`
	for _, body := range []string{
		eventStream(map[string]any{"type": "response.incomplete", "response": map[string]any{"status": "incomplete"}}),
		eventStream(map[string]any{"type": "response.failed", "response": map[string]any{"status": "failed"}}),
		eventStream(map[string]any{"type": "error", "message": "rate limited"}),
		eventStream(map[string]any{"type": "response.created"}),
		eventStream(completed(map[string]any{"type": "message", "content": []any{map[string]any{"type": "refusal"}}})),
		eventStream(completed(message(invalid))),
		eventStream(completed(message(`{}`), message(`{}`))),
		"data: {broken\n\n",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		provider := NewOpenAI("test-key", "test-model")
		provider.URL = server.URL
		_, err := provider.Interpret(context.Background(), shopping.InterpretationRequest{Text: "milk"}, nil)
		server.Close()
		if err == nil || !errors.Is(err, shopping.ErrUpstream) {
			t.Fatalf("invalid provider response accepted: %s, %v", body, err)
		}
	}
}

func TestProviderTimeoutDuringStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, eventStream(map[string]any{"type": "response.created"}))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	provider := NewOpenAI("test-key", "test-model")
	provider.URL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := provider.Interpret(ctx, shopping.InterpretationRequest{Text: "milk"}, nil); !errors.Is(err, shopping.ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
}
