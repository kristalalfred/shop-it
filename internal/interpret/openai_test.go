package interpret

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kristalalfred/ship-it/internal/shopping"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStructuredInterpretationContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || req["model"] != "test-model" || req["store"] != false || req["input"] != `{"text":"breakfast for two","required_traits":["organic"]}` {
			t.Errorf("wrong interpreter request: %v", req)
		}
		format := req["text"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Errorf("missing structured output: %v", format)
		}
		text := `{"items":[{"label":"milk","query":"mjölk","amount":{"quantity":1000,"unit":"ml"},"required_traits":["organic"]}],"warnings":["Assumed breakfast for two."]}`
		json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "reasoning"}, map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}}})
	}))
	defer server.Close()
	provider := NewOpenAI("test-key", "test-model")
	provider.URL = server.URL
	result, err := provider.Interpret(context.Background(), shopping.InterpretationRequest{Text: "breakfast for two", RequiredTraits: []string{"organic"}}, nil)
	if err != nil || result.Mode != "openai" || len(result.Items) != 1 || result.Items[0].Amount.Quantity != 1000 || len(result.Warnings) != 1 {
		t.Fatalf("result: %+v, %v", result, err)
	}
}

func TestProviderFailureIsNotAListFallback(t *testing.T) {
	for _, body := range []string{
		`{"status":"incomplete","output":[]}`,
		`{"status":"completed","output":[{"type":"message","content":[{"type":"refusal"}]}]}`,
		`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"items\":[{\"label\":\"milk\",\"query\":\"mjölk\",\"amount\":{\"quantity\":-1,\"unit\":\"ml\"},\"required_traits\":[]}],\"warnings\":[]}"}]}]}`,
		`{broken`,
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
