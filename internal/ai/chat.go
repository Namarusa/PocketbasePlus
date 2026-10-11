package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Generation and tool calling with standard models.

// Message is a chat message (OpenAI format).
type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Reasoning is the reasoning text of some providers (DeepSeek, Kimi,
	// GLM), passed back to them in tool loops.
	Reasoning string `json:"reasoning_content,omitempty"`
	// Native is the provider's own form of an assistant message (Anthropic:
	// the content blocks, thinking included), replayed unchanged.
	Native json.RawMessage `json:"native,omitempty"`
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // function
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON
	} `json:"function"`
}

// Tool is a function the model can call.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON schema
}

// ChatRequest is one chat completion request.
type ChatRequest struct {
	Messages    []Message
	Tools       []Tool
	Temperature *float64
	MaxTokens   int
	// Schema forces a JSON answer following this strict JSON schema.
	Schema map[string]any
}

// ChatResult is the answer of the model.
type ChatResult struct {
	Model        string
	Message      Message
	FinishReason string
	Usage        Usage
}

// Chatter is implemented by the providers of standard models.
type Chatter interface {
	Chat(ctx context.Context, model string, req ChatRequest) (*ChatResult, error)
}

// Field is a named output of a generation.
type Field struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"` // text | number | boolean | list (of texts)
}

// GenerateRequest asks a standard model for a text, or for named fields.
type GenerateRequest struct {
	System      string
	Prompt      string
	Fields      []Field // empty = a plain text answer
	Temperature *float64
	MaxTokens   int
}

// GenerateResponse is the generated text (or the fields).
type GenerateResponse struct {
	Model  string         `json:"model"`
	Text   string         `json:"text,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
	Usage  Usage          `json:"usage"`
}

var fieldTypes = map[string]map[string]any{
	"text":    {"type": "string"},
	"number":  {"type": "number"},
	"boolean": {"type": "boolean"},
	"list":    {"type": "array", "items": map[string]any{"type": "string"}},
}

// Generate runs a generation with the model (standard models only).
func (m *Model) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	chatter, ok := m.Provider.(Chatter)
	if !ok {
		return nil, fmt.Errorf("%s is a System One model: it answers decisions, not text. Select a Standard model", m.Name)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("the prompt is empty")
	}

	chat := ChatRequest{Temperature: req.Temperature, MaxTokens: req.MaxTokens}
	if strings.TrimSpace(req.System) != "" {
		chat.Messages = append(chat.Messages, Message{Role: "system", Content: req.System})
	}
	chat.Messages = append(chat.Messages, Message{Role: "user", Content: req.Prompt})

	if len(req.Fields) > 0 {
		props := ordered{}
		required := []string{}
		for _, f := range req.Fields {
			if !KeyRegex.MatchString(f.Name) {
				return nil, fmt.Errorf("output field %q: use only letters, digits and _", f.Name)
			}
			t, ok := fieldTypes[f.Type]
			if !ok {
				t = fieldTypes["text"]
			}
			prop := map[string]any{}
			for k, v := range t {
				prop[k] = v
			}
			if f.Description != "" {
				prop["description"] = f.Description
			}
			props = append(props, kv{f.Name, prop})
			required = append(required, f.Name)
		}
		chat.Schema = map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		res, err := chatter.Chat(ctx, m.Model, chat)
		if err != nil {
			return nil, err
		}
		out := &GenerateResponse{Model: res.Model, Usage: res.Usage}
		if res.FinishReason == "length" && len(req.Fields) > 0 {
			return nil, errors.New("the model ran out of output tokens (raise the max tokens)")
		}
		if len(req.Fields) == 0 {
			out.Text = res.Message.Content
			return out, nil
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(stripFences(res.Message.Content)), &fields); err != nil {
			lastErr = fmt.Errorf("the model didn't return valid JSON (%s); pick a model supporting structured outputs", limit(res.Message.Content, 200))
			continue
		}
		out.Fields = map[string]any{}
		for _, f := range req.Fields {
			out.Fields[f.Name] = fields[f.Name]
		}
		return out, nil
	}
	return nil, lastErr
}
