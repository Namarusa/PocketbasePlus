package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic calls Claude models with the Messages API (official SDK).
//
// Assistant turns are kept in their native form (Message.Native) and sent
// back unchanged, thinking blocks included, as the API requires in tool
// loops. Sampling parameters (temperature) are left to the model defaults:
// current Claude models reject them.
type Anthropic struct {
	BaseURL string // empty = https://api.anthropic.com
	APIKey  string
}

const anthropicMaxTokens = 16000

func (p *Anthropic) client() anthropic.Client {
	opts := []option.RequestOption{option.WithAPIKey(p.APIKey), option.WithMaxRetries(2)}
	if p.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(p.BaseURL))
	}
	return anthropic.NewClient(opts...)
}

// anthropicError converts SDK errors to APIError (same messages as the other providers).
func anthropicError(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		return &APIError{Status: apierr.StatusCode, Message: errorMessage([]byte(apierr.RawJSON()))}
	}
	return err
}

// anthropicMessages converts the history: system messages become the
// system prompt, tool results are grouped in one user turn.
func anthropicMessages(messages []Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam, error) {
	var system []string
	var out []anthropic.MessageParam
	var results []anthropic.ContentBlockParamUnion
	flush := func() {
		if len(results) > 0 {
			out = append(out, anthropic.NewUserMessage(results...))
			results = nil
		}
	}
	for _, m := range messages {
		switch m.Role {
		case "system":
			system = append(system, m.Content)
		case "tool":
			// the assistant tools report failures as {"error": ...}
			results = append(results, anthropic.NewToolResultBlock(m.ToolCallID, m.Content, strings.HasPrefix(m.Content, `{"error"`)))
		case "user":
			flush()
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			flush()
			if len(m.Native) > 0 {
				var native anthropic.MessageParam
				if err := json.Unmarshal(m.Native, &native); err == nil {
					out = append(out, native)
					continue
				}
			}
			// from another provider: rebuild the text and the tool calls
			var blocks []anthropic.ContentBlockParamUnion
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, call := range m.ToolCalls {
				var input any = map[string]any{}
				if call.Function.Arguments != "" {
					input = json.RawMessage(call.Function.Arguments)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, input, call.Function.Name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}
		}
	}
	flush()
	var systemBlocks []anthropic.TextBlockParam
	if len(system) > 0 {
		systemBlocks = []anthropic.TextBlockParam{{Text: strings.Join(system, "\n\n")}}
	}
	return systemBlocks, out, nil
}

func (p *Anthropic) Chat(ctx context.Context, model string, req ChatRequest) (*ChatResult, error) {
	if model == "" {
		return nil, errors.New("no model set (e.g. claude-opus-5-5)")
	}
	system, messages, err := anthropicMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicMaxTokens
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		System:    system,
		Messages:  messages,
	}
	for _, t := range req.Tools {
		schema := anthropic.ToolInputSchemaParam{Properties: t.Parameters["properties"]}
		if req, ok := t.Parameters["required"].([]string); ok {
			schema.Required = req
		}
		tool := anthropic.ToolParam{Name: t.Name, Description: anthropic.String(t.Description), InputSchema: schema}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}
	if req.Schema != nil {
		// structured outputs (forced tool use is rejected by current models)
		params.OutputConfig = anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: req.Schema}}
	}

	client := p.client()
	resp, err := client.Messages.New(ctx, params)
	if err != nil {
		return nil, anthropicError(err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		reason := "the model declined the request"
		if resp.StopDetails.Category != "" {
			reason += " (" + string(resp.StopDetails.Category) + ")"
		}
		return nil, errors.New(reason)
	}

	msg := Message{Role: "assistant"}
	var texts []string
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			texts = append(texts, b.Text)
		case anthropic.ToolUseBlock:
			call := ToolCall{ID: b.ID, Type: "function"}
			call.Function.Name = b.Name
			call.Function.Arguments = b.JSON.Input.Raw()
			msg.ToolCalls = append(msg.ToolCalls, call)
		}
	}
	msg.Content = strings.Join(texts, "")
	if native, err := json.Marshal(resp.ToParam()); err == nil {
		msg.Native = native
	}

	finish := "stop"
	switch resp.StopReason {
	case anthropic.StopReasonMaxTokens:
		finish = "length"
	case anthropic.StopReasonToolUse:
		finish = "tool_calls"
	}
	return &ChatResult{
		Model:        string(resp.Model),
		FinishReason: finish,
		Message:      msg,
		Usage:        Usage{InputTokens: int(resp.Usage.InputTokens), OutputTokens: int(resp.Usage.OutputTokens)},
	}, nil
}

func (p *Anthropic) Decide(ctx context.Context, model string, req Request) (*Response, error) {
	return decideWithChat(ctx, p, model, req, nil)
}

func (p *Anthropic) Models(ctx context.Context) ([]ModelInfo, error) {
	client := p.client()
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	var list []ModelInfo
	for pager.Next() {
		m := pager.Current()
		list = append(list, ModelInfo{ID: m.ID, Name: m.DisplayName, Context: int(m.MaxInputTokens)})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("listing the models: %w", anthropicError(err))
	}
	return list, nil
}
