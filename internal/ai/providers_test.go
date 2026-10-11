package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recorder is a fake provider recording the requests and replying with the
// next canned answer.
type recorder struct {
	mu       sync.Mutex
	requests []map[string]any
	paths    []string
	headers  []http.Header
	replies  []string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(req.Body).Decode(&body)
	r.requests = append(r.requests, body)
	r.paths = append(r.paths, req.URL.Path)
	r.headers = append(r.headers, req.Header.Clone())
	reply := `{}`
	if len(r.replies) > 0 {
		reply, r.replies = r.replies[0], r.replies[1:]
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(reply, "401:") {
		w.WriteHeader(http.StatusUnauthorized)
		reply = strings.TrimPrefix(reply, "401:")
	}
	_, _ = w.Write([]byte(reply))
}

func chatReply(content string, extra string) string {
	return `{"model": "m", "choices": [{"finish_reason": "stop", "message": {"content": ` + content + extra + `}}], "usage": {"prompt_tokens": 10, "completion_tokens": 5}}`
}

func noulRequest() Request {
	return Request{State: "Help!", Questions: []Question{{Key: "urgent", Type: QuestionNoul, Instructions: "Urgent?"}}}
}

func TestCompatOpenAI(t *testing.T) {
	rec := &recorder{replies: []string{chatReply(`"{\"urgent\": {\"p_yes\": 0.8}}"`, `, "reasoning_content": "hidden"`)}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := &Compat{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "sk"}
	res, err := p.Decide(context.Background(), "gpt-5-mini", noulRequest())
	if err != nil || res.Answers["urgent"].Noul != 0.8 {
		t.Fatalf("decide: %+v %v", res, err)
	}
	body := rec.requests[0]
	format := body["response_format"].(map[string]any)
	if format["type"] != "json_schema" || body["temperature"] != nil || body["provider"] != nil {
		t.Errorf("unexpected OpenAI request: %v", body)
	}
	if rec.headers[0].Get("Authorization") != "Bearer sk" || rec.paths[0] != "/chat/completions" {
		t.Errorf("unexpected auth/path: %v %s", rec.headers[0], rec.paths[0])
	}

	// max_completion_tokens; reasoning is never sent to OpenAI
	rec.replies = []string{chatReply(`"ok"`, "")}
	_, err = p.Chat(context.Background(), "gpt-5-mini", ChatRequest{MaxTokens: 100, Messages: []Message{
		{Role: "user", Content: "hi"}, {Role: "assistant", Content: "x", Reasoning: "secret", Native: json.RawMessage(`{"a":1}`)}, {Role: "user", Content: "again"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body = rec.requests[1]
	b, _ := json.Marshal(body["messages"])
	if body["max_completion_tokens"] != 100.0 || body["max_tokens"] != nil || strings.Contains(string(b), "secret") || strings.Contains(string(b), "native") {
		t.Errorf("unexpected request: %v", body)
	}
}

func TestCompatGLMJsonModeAndReasoning(t *testing.T) {
	rec := &recorder{replies: []string{
		chatReply(`"`+"```json\\n{\\\"urgent\\\": {\\\"p_yes\\\": 0.3}}\\n```"+`"`, ""),
		chatReply(`""`, `, "reasoning_content": "I should look it up", "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "count", "arguments": "{}"}}]`),
		chatReply(`"done"`, ""),
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := &Compat{Kind: KindGLM, BaseURL: srv.URL, APIKey: "k"}

	res, err := p.Decide(context.Background(), "glm-5", noulRequest())
	if err != nil || res.Answers["urgent"].Noul != 0.3 {
		t.Fatalf("decide: %+v %v", res, err)
	}
	body := rec.requests[0]
	if body["response_format"].(map[string]any)["type"] != "json_object" {
		t.Errorf("GLM must use JSON mode: %v", body["response_format"])
	}
	system := body["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "exactly following this JSON schema") || !strings.Contains(system, `"p_yes"`) {
		t.Errorf("the schema must be in the instructions: %s", system)
	}

	// tool loop: the reasoning goes back with the assistant message
	tools := []Tool{{Name: "count", Description: "Count", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}
	history := []Message{{Role: "user", Content: "how many?"}}
	r1, err := p.Chat(context.Background(), "glm-5", ChatRequest{Messages: history, Tools: tools})
	if err != nil || r1.Message.Reasoning != "I should look it up" || len(r1.Message.ToolCalls) != 1 {
		t.Fatalf("tool call: %+v %v", r1, err)
	}
	history = append(history, r1.Message, Message{Role: "tool", ToolCallID: "c1", Content: `{"n": 3}`})
	if _, err := p.Chat(context.Background(), "glm-5", ChatRequest{Messages: history, Tools: tools}); err != nil {
		t.Fatal(err)
	}
	assistant := rec.requests[2]["messages"].([]any)[1].(map[string]any)
	if assistant["reasoning_content"] != "I should look it up" || assistant["tool_calls"] == nil {
		t.Errorf("unexpected assistant message: %v", assistant)
	}
}

func TestCompatModels(t *testing.T) {
	// OpenRouter: key check, then the models with their context and prices (per million tokens)
	orRec := &recorder{replies: []string{`{"data": {"label": "k"}}`, `{"data": [{"id": "anthropic/claude-sonnet-5.5", "name": "Anthropic: Claude Sonnet 5.5", "context_length": 1000000, "pricing": {"prompt": "0.000002", "completion": "0.00001"}}]}`}}
	orSrv := httptest.NewServer(orRec)
	defer orSrv.Close()
	models, err := (&OpenRouter{BaseURL: orSrv.URL, APIKey: "k"}).Models(context.Background())
	if err != nil || len(models) != 1 || models[0].Context != 1000000 || models[0].InputPrice != 2 || models[0].OutputPrice != 10 {
		t.Fatalf("openrouter models: %+v %v", models, err)
	}
	if orRec.paths[0] != "/key" || orRec.paths[1] != "/models" {
		t.Errorf("paths: %v", orRec.paths)
	}

	rec := &recorder{replies: []string{`{"data": [{"id": "models/gemini-3.8-flash"}, {"id": "models/gemini-embedding-2"}, {"id": "models/gemini-3.8-pro"}]}`}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	list, err := (&Compat{Kind: KindGemini, BaseURL: srv.URL, APIKey: "k"}).Models(context.Background())
	if err != nil || len(list) != 2 || list[0].ID != "gemini-3.8-flash" || rec.paths[0] != "/models" {
		t.Fatalf("gemini models: %+v %v %v", list, err, rec.paths)
	}

	// GLM: no models list, the key is checked with a one token answer
	rec.replies = []string{chatReply(`"p"`, "")}
	list, err = (&Compat{Kind: KindGLM, BaseURL: srv.URL, APIKey: "k"}).Models(context.Background())
	if err != nil || len(list) == 0 || rec.requests[1]["max_tokens"] != 1.0 {
		t.Fatalf("glm models: %+v %v", list, err)
	}
	rec.replies = []string{`401:{"error": {"message": "invalid api key"}}`}
	if _, err := (&Compat{Kind: KindGLM, BaseURL: srv.URL, APIKey: "bad"}).Models(context.Background()); !IsAPIStatus(err, 401) {
		t.Errorf("expected 401, got %v", err)
	}
}

func anthropicReply(content string, stop string) string {
	return `{"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "content": ` + content +
		`, "stop_reason": "` + stop + `", "usage": {"input_tokens": 100, "output_tokens": 20}}`
}

func TestAnthropicToolLoopKeepsThinking(t *testing.T) {
	rec := &recorder{replies: []string{
		anthropicReply(`[{"type": "thinking", "thinking": "", "signature": "sig-123"}, {"type": "text", "text": "Let me count."},
			{"type": "tool_use", "id": "toolu_1", "name": "count", "input": {"collection": "orders"}}]`, "tool_use"),
		anthropicReply(`[{"type": "text", "text": "There are 3 orders."}]`, "end_turn"),
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := &Anthropic{BaseURL: srv.URL, APIKey: "sk-ant"}
	temperature := 0.2
	tools := []Tool{{Name: "count", Description: "Count records", Parameters: map[string]any{
		"type": "object", "properties": map[string]any{"collection": map[string]any{"type": "string"}}, "required": []string{"collection"},
	}}}
	history := []Message{{Role: "system", Content: "You are the assistant."}, {Role: "user", Content: "How many orders?"}}

	r1, err := p.Chat(context.Background(), "claude-opus-5-5", ChatRequest{Messages: history, Tools: tools, Temperature: &temperature})
	if err != nil {
		t.Fatal(err)
	}
	if r1.FinishReason != "tool_calls" || r1.Message.Content != "Let me count." || len(r1.Message.ToolCalls) != 1 ||
		r1.Message.ToolCalls[0].Function.Arguments != `{"collection": "orders"}` || len(r1.Message.Native) == 0 {
		t.Fatalf("unexpected result: %+v", r1)
	}
	body := rec.requests[0]
	if rec.paths[0] != "/v1/messages" || rec.headers[0].Get("X-Api-Key") != "sk-ant" || rec.headers[0].Get("Anthropic-Version") == "" {
		t.Errorf("unexpected request: %s %v", rec.paths[0], rec.headers[0])
	}
	if body["temperature"] != nil || body["max_tokens"] != 16000.0 || body["model"] != "claude-opus-5-5" {
		t.Errorf("unexpected params: %v", body)
	}
	if sys, _ := json.Marshal(body["system"]); !strings.Contains(string(sys), "You are the assistant.") {
		t.Errorf("system: %s", sys)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if tool["name"] != "count" || tool["input_schema"].(map[string]any)["required"].([]any)[0] != "collection" {
		t.Errorf("tool: %v", tool)
	}

	// second turn: the assistant turn is replayed unchanged (thinking signature), results in one user turn
	history = append(history, r1.Message, Message{Role: "tool", ToolCallID: "toolu_1", Content: `{"count": 3}`})
	r2, err := p.Chat(context.Background(), "claude-opus-5-5", ChatRequest{Messages: history, Tools: tools})
	if err != nil || r2.Message.Content != "There are 3 orders." {
		t.Fatalf("second turn: %+v %v", r2, err)
	}
	msgs := rec.requests[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected user, assistant, tool results: %v", msgs)
	}
	assistant, _ := json.Marshal(msgs[1])
	for _, want := range []string{`"signature":"sig-123"`, `"type":"thinking"`, `"id":"toolu_1"`, `"text":"Let me count."`} {
		if !strings.Contains(string(assistant), want) {
			t.Errorf("assistant turn misses %s: %s", want, assistant)
		}
	}
	results, _ := json.Marshal(msgs[2])
	if !strings.Contains(string(results), `"tool_use_id":"toolu_1"`) || !strings.Contains(string(results), `"type":"tool_result"`) {
		t.Errorf("tool results: %s", results)
	}
}

func TestAnthropicDecideAndErrors(t *testing.T) {
	rec := &recorder{replies: []string{
		anthropicReply(`[{"type": "text", "text": "{\"urgent\": {\"p_yes\": 0.9}}"}]`, "end_turn"),
		`401:{"type": "error", "error": {"type": "authentication_error", "message": "invalid x-api-key"}}`,
		`{"data": [{"id": "claude-opus-5-5", "display_name": "Claude Opus 5.5", "type": "model", "created_at": "2026-01-01T00:00:00Z"}], "has_more": false, "first_id": "claude-opus-5-5", "last_id": "claude-opus-5-5"}`,
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	p := &Anthropic{BaseURL: srv.URL, APIKey: "k"}

	res, err := p.Decide(context.Background(), "claude-opus-5-5", noulRequest())
	if err != nil || res.Answers["urgent"].Noul != 0.9 || res.Calibrated {
		t.Fatalf("decide: %+v %v", res, err)
	}
	format := rec.requests[0]["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil || rec.requests[0]["tool_choice"] != nil {
		t.Errorf("structured output expected: %v", rec.requests[0])
	}

	_, err = p.Chat(context.Background(), "claude-opus-5-5", ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if !IsAPIStatus(err, 401) || !strings.Contains(err.Error(), "invalid x-api-key") || !strings.Contains(err.Error(), "check the API key") {
		t.Errorf("expected a readable 401, got %v", err)
	}

	list, err := p.Models(context.Background())
	if err != nil || len(list) != 1 || list[0].Name != "Claude Opus 5.5" {
		t.Errorf("models: %+v %v", list, err)
	}
}
