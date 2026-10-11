package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Providers of standard models with an OpenAI compatible chat completions
// API: OpenRouter, OpenAI, Google Gemini, DeepSeek, Kimi (Moonshot) and GLM
// (Z.ai). They differ by a few details, described by their profile.

// DefaultOpenRouterURL is the OpenRouter API (standard LLMs of many vendors).
const DefaultOpenRouterURL = "https://openrouter.ai/api/v1"

type compatProfile struct {
	label   string
	baseURL string
	// strict json_schema response format; otherwise json_object with the
	// schema written in the instructions
	jsonSchema bool
	// OpenRouter provider preferences (only route to providers supporting the parameters)
	routing bool
	// accepts temperature 0 for decisions (reasoning models often only accept the default)
	decisionTemperature bool
	// max_completion_tokens instead of max_tokens
	maxCompletionTokens bool
	// returns reasoning_content, passed back in tool loops
	reasoning bool
	// models list: path ("" = none), filter, static list
	modelsPath   string
	modelFilter  func(id string) bool
	staticModels []string
	// model used to check the key when there's no models list
	pingModel string
}

var compatProfiles = map[string]compatProfile{
	KindOpenRouter: {
		label: "OpenRouter", baseURL: DefaultOpenRouterURL, jsonSchema: true, routing: true, decisionTemperature: true,
		modelsPath: "/models?supported_parameters=structured_outputs",
	},
	KindOpenAI: {
		label: "OpenAI", baseURL: "https://api.openai.com/v1", jsonSchema: true, maxCompletionTokens: true,
		modelsPath: "/models", modelFilter: openAIChatModel,
	},
	KindGemini: {
		label: "Gemini", baseURL: "https://generativelanguage.googleapis.com/v1beta/openai", jsonSchema: true,
		modelsPath: "/models", modelFilter: geminiChatModel,
	},
	KindDeepSeek: {
		label: "DeepSeek", baseURL: "https://api.deepseek.com", reasoning: true, decisionTemperature: true,
		modelsPath: "/models",
	},
	KindKimi: {
		label: "Kimi", baseURL: "https://api.moonshot.ai/v1", jsonSchema: true, reasoning: true,
		modelsPath: "/models",
	},
	KindGLM: {
		label: "GLM", baseURL: "https://api.z.ai/api/paas/v4", reasoning: true,
		staticModels: []string{"glm-5.3", "glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.7-flash", "glm-4.6", "glm-4.5-air"},
		pingModel:    "glm-4.7-flash",
	},
}

func openAIChatModel(id string) bool {
	if !(strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") ||
		strings.HasPrefix(id, "o4") || strings.HasPrefix(id, "chatgpt-")) {
		return false
	}
	for _, skip := range []string{"audio", "realtime", "transcribe", "tts", "image", "search", "instruct", "embedding"} {
		if strings.Contains(id, skip) {
			return false
		}
	}
	return true
}

func geminiChatModel(id string) bool {
	if !strings.Contains(id, "gemini") {
		return false
	}
	for _, skip := range []string{"embedding", "image", "tts", "live", "audio"} {
		if strings.Contains(id, skip) {
			return false
		}
	}
	return true
}

// Compat is a client of an OpenAI compatible provider. Kind selects the
// profile (empty = OpenRouter).
type Compat struct {
	Kind    string
	BaseURL string
	APIKey  string
	AppName string // OpenRouter app attribution (X-Title)
	AppURL  string // OpenRouter app attribution (HTTP-Referer)
}

// OpenRouter is the OpenRouter client (a Compat without Kind).
type OpenRouter = Compat

func (p *Compat) profile() compatProfile {
	kind := p.Kind
	if kind == "" {
		kind = KindOpenRouter
	}
	return compatProfiles[kind]
}

func (p *Compat) url(path string) string {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = p.profile().baseURL
	}
	return base + path
}

func (p *Compat) headers() map[string]string {
	h := map[string]string{"Authorization": "Bearer " + p.APIKey}
	if p.profile().routing {
		if p.AppName != "" {
			h["X-Title"] = p.AppName
		}
		if p.AppURL != "" {
			h["HTTP-Referer"] = p.AppURL
		}
	}
	return h
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content          json.RawMessage `json:"content"`
			Refusal          string          `json:"refusal"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []ToolCall      `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// wireMessages converts the messages to the provider format.
func (p *Compat) wireMessages(messages []Message, schemaNote string) []map[string]any {
	prof := p.profile()
	out := make([]map[string]any, 0, len(messages)+1)
	noted := false
	for _, m := range messages {
		w := map[string]any{"role": m.Role, "content": m.Content}
		if m.Role == "system" && schemaNote != "" && !noted {
			w["content"] = m.Content + "\n\n" + schemaNote
			noted = true
		}
		if len(m.ToolCalls) > 0 {
			w["tool_calls"] = m.ToolCalls
		}
		if m.ToolCallID != "" {
			w["tool_call_id"] = m.ToolCallID
		}
		if prof.reasoning && m.Role == "assistant" && m.Reasoning != "" {
			w["reasoning_content"] = m.Reasoning
		}
		out = append(out, w)
	}
	if schemaNote != "" && !noted {
		out = append([]map[string]any{{"role": "system", "content": schemaNote}}, out...)
	}
	return out
}

func (p *Compat) Chat(ctx context.Context, model string, req ChatRequest) (*ChatResult, error) {
	if model == "" {
		return nil, errors.New("no model set")
	}
	prof := p.profile()
	body := map[string]any{"model": model}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		if prof.maxCompletionTokens {
			body["max_completion_tokens"] = req.MaxTokens
		} else {
			body["max_tokens"] = req.MaxTokens
		}
	}
	required := false
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": t})
		}
		body["tools"] = tools
		required = true
	}
	schemaNote := ""
	if req.Schema != nil {
		if prof.jsonSchema {
			body["response_format"] = map[string]any{
				"type":        "json_schema",
				"json_schema": map[string]any{"name": "answer", "strict": true, "schema": req.Schema},
			}
		} else {
			// JSON mode only: the schema goes in the instructions
			schema, err := json.Marshal(req.Schema)
			if err != nil {
				return nil, err
			}
			body["response_format"] = map[string]any{"type": "json_object"}
			schemaNote = "Answer with one JSON object only (no Markdown, no comments), exactly following this JSON schema:\n" + string(schema)
		}
		required = true
	}
	body["messages"] = p.wireMessages(req.Messages, schemaNote)
	if required && prof.routing {
		body["provider"] = map[string]any{"require_parameters": true}
	}

	var res chatResponse
	if err := doJSON(ctx, http.MethodPost, p.url("/chat/completions"), p.headers(), body, &res); err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, fmt.Errorf("the AI provider returned an error: %s", res.Error.Message)
	}
	if len(res.Choices) == 0 {
		return nil, errors.New("the model returned no answer")
	}
	c := res.Choices[0]
	if c.Message.Refusal != "" {
		return nil, fmt.Errorf("the model refused to answer: %s", limit(c.Message.Refusal, 300))
	}
	msg := Message{Role: "assistant", Content: messageText(c.Message.Content), ToolCalls: c.Message.ToolCalls}
	if prof.reasoning {
		msg.Reasoning = c.Message.ReasoningContent
	}
	return &ChatResult{
		Model:        res.Model,
		FinishReason: c.FinishReason,
		Message:      msg,
		Usage:        Usage{InputTokens: res.Usage.PromptTokens, OutputTokens: res.Usage.CompletionTokens},
	}, nil
}

func (p *Compat) Decide(ctx context.Context, model string, req Request) (*Response, error) {
	var temperature *float64
	if p.profile().decisionTemperature {
		zero := 0.0
		temperature = &zero
	}
	return decideWithChat(ctx, p, model, req, temperature)
}

func (p *Compat) Models(ctx context.Context) ([]ModelInfo, error) {
	prof := p.profile()
	if prof.routing {
		// the OpenRouter models list is public: check the key first
		if err := doJSON(ctx, http.MethodGet, p.url("/key"), p.headers(), nil, nil); err != nil {
			return nil, err
		}
	}
	if prof.modelsPath == "" {
		// no models list: check the key with a one token answer
		if prof.pingModel != "" {
			ping := map[string]any{"model": prof.pingModel, "max_tokens": 1, "messages": []map[string]any{{"role": "user", "content": "ping"}}}
			if err := doJSON(ctx, http.MethodPost, p.url("/chat/completions"), p.headers(), ping, nil); err != nil {
				return nil, err
			}
		}
		list := make([]ModelInfo, 0, len(prof.staticModels))
		for _, id := range prof.staticModels {
			list = append(list, ModelInfo{ID: id, Name: id})
		}
		return list, nil
	}

	var res struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			DisplayName   string `json:"display_name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     any `json:"prompt"`
				Completion any `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := doJSON(ctx, http.MethodGet, p.url(prof.modelsPath), p.headers(), nil, &res); err != nil {
		return nil, err
	}
	list := make([]ModelInfo, 0, len(res.Data))
	for _, m := range res.Data {
		id := strings.TrimPrefix(m.ID, "models/") // Gemini
		if prof.modelFilter != nil && !prof.modelFilter(id) {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.DisplayName
		}
		if name == "" {
			name = id
		}
		info := ModelInfo{ID: id, Name: name, Description: limit(m.Description, 200), Context: m.ContextLength}
		// OpenRouter prices are per token
		if v, ok := toFloat(m.Pricing.Prompt); ok && v > 0 {
			info.InputPrice = v * 1e6
		}
		if v, ok := toFloat(m.Pricing.Completion); ok && v > 0 {
			info.OutputPrice = v * 1e6
		}
		list = append(list, info)
	}
	if !prof.routing {
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	}
	return list, nil
}
