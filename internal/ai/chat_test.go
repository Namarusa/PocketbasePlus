package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateFieldsAndText(t *testing.T) {
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body)
		content := "Hello Ada!"
		if body["response_format"] != nil {
			content = `{"subject": "Your order", "tags": ["vip", "fr"], "score": 4}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "openai/gpt-5-mini",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
			"usage":   map[string]any{"prompt_tokens": 50, "completion_tokens": 10},
		})
	}))
	defer srv.Close()
	m := &Model{Name: "GPT", Model: "openai/gpt-5-mini", Provider: &OpenRouter{BaseURL: srv.URL, APIKey: "k"}}

	res, err := m.Generate(context.Background(), GenerateRequest{System: "Be brief.", Prompt: "Greet Ada"})
	if err != nil || res.Text != "Hello Ada!" || res.Usage.InputTokens != 50 {
		t.Fatalf("text: %+v %v", res, err)
	}
	msgs := sent[0]["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || sent[0]["provider"] != nil {
		t.Errorf("unexpected text request: %v", sent[0])
	}

	res, err = m.Generate(context.Background(), GenerateRequest{Prompt: "Write", Fields: []Field{
		{Name: "subject", Type: "text", Description: "Email subject"}, {Name: "tags", Type: "list"}, {Name: "score", Type: "number"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fields["subject"] != "Your order" || res.Fields["score"] != 4.0 || len(res.Fields["tags"].([]any)) != 2 {
		t.Errorf("fields: %v", res.Fields)
	}
	b, _ := json.Marshal(sent[1]["response_format"])
	if !strings.Contains(string(b), `"strict":true`) || !strings.Contains(string(b), `"tags":{"items":{"type":"string"},"type":"array"}`) ||
		sent[1]["provider"].(map[string]any)["require_parameters"] != true {
		t.Errorf("unexpected schema: %s", b)
	}

	// System One models don't generate
	jev := &Model{Name: "Jev", Provider: &TypeSafe{}}
	if _, err := jev.Generate(context.Background(), GenerateRequest{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "Standard") {
		t.Errorf("expected a System One error, got %v", err)
	}
}

func TestChatToolCalls(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`{"model": "m", "choices": [{"finish_reason": "tool_calls", "message": {"content": null,
			"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "count", "arguments": "{\"collection\":\"orders\"}"}}]}}]}`))
	}))
	defer srv.Close()
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "k"}
	res, err := p.Chat(context.Background(), "m", ChatRequest{
		Messages: []Message{{Role: "user", Content: "How many orders?"}},
		Tools:    []Tool{{Name: "count", Description: "Count records", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Function.Name != "count" || res.Message.Content != "" {
		t.Errorf("unexpected result: %+v", res)
	}
	tools := sent["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "function" || tools[0].(map[string]any)["function"].(map[string]any)["name"] != "count" {
		t.Errorf("unexpected tools: %v", tools)
	}
}
