// Package assistant is the AI assistant of the dashboard: a standard model
// (Settings → AI assistant) answering the superusers in a chat, with tools
// reading the app (schema, records, aggregates, templates...) and proposing
// changes (dashboards, email templates, campaigns, records). Proposals are
// only applied when the superuser clicks "Apply".
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
)

const (
	settingsCollection      = "ai_settings"
	conversationsCollection = "ai_conversations"

	maxSteps          = 14              // model calls per user message
	maxHistory        = 60              // messages sent to the model
	maxToolResult     = 12000           // characters of a tool result
	chatTimeout       = 3 * time.Minute // per user message
	maxMessageLength  = 8000            // characters of a user message
	conversationLimit = 200             // messages kept per conversation
)

// Assistant runs the conversations.
type Assistant struct {
	app   core.App
	mail  *mail.Engine
	flows flowRunner // nil without the flows engine

	// newModel loads the assistant model (replaced by tests).
	newModel func(app core.App) (*ai.Model, ai.Chatter, error)
}

// Register binds the assistant routes. flowsEngine runs the test runs of
// flows (nil: no test runs).
func Register(app core.App, mailEngine *mail.Engine, flowsEngine *flows.Engine) *Assistant {
	a := &Assistant{app: app, mail: mailEngine, newModel: loadModel}
	if flowsEngine != nil {
		a.flows = flowsEngine
	}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		g := se.Router.Group("/api/assistant")
		g.Bind(apis.RequireSuperuserAuth())
		g.GET("/status", a.status)
		g.POST("/chat", a.chat)
		g.POST("/conversations/{id}/actions/{action}/apply", a.applyRoute)
		g.POST("/conversations/{id}/actions/{action}/discard", a.discardRoute)
		return se.Next()
	})
	return a
}

// Settings returns the settings record (created on first use).
func Settings(app core.App) (*core.Record, error) {
	list, err := app.FindRecordsByFilter(settingsCollection, "", "created", 1, 0)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list[0], nil
	}
	col, err := app.FindCollectionByNameOrId(settingsCollection)
	if err != nil {
		return nil, err
	}
	r := core.NewRecord(col)
	return r, app.Save(r)
}

func loadModel(app core.App) (*ai.Model, ai.Chatter, error) {
	s, err := Settings(app)
	if err != nil {
		return nil, nil, err
	}
	if !s.GetBool("assistant_enabled") {
		return nil, nil, errors.New("The assistant is disabled (Settings → AI assistant).")
	}
	m, err := ai.LoadModel(app, s.GetString("assistant_model"))
	if err != nil {
		return nil, nil, err
	}
	chatter, ok := m.Provider.(ai.Chatter)
	if !ok {
		return nil, nil, fmt.Errorf("%s is a System One model: the assistant needs a Standard model (Settings → AI assistant)", m.Name)
	}
	return m, chatter, nil
}

func (a *Assistant) status(re *core.RequestEvent) error {
	m, _, err := a.newModel(re.App)
	if err != nil {
		return re.JSON(http.StatusOK, map[string]any{"ready": false, "error": err.Error()})
	}
	return re.JSON(http.StatusOK, map[string]any{"ready": true, "model": m.Name})
}

// --- conversations -------------------------------------------------------------

type conversation struct {
	rec      *core.Record
	Messages []ai.Message
	Actions  []*Action
}

func (a *Assistant) loadConversation(app core.App, auth *core.Record, id string) (*conversation, error) {
	if id == "" {
		col, err := app.FindCollectionByNameOrId(conversationsCollection)
		if err != nil {
			return nil, err
		}
		rec := core.NewRecord(col)
		rec.Set("owner", auth.Id)
		return &conversation{rec: rec}, nil
	}
	rec, err := app.FindRecordById(conversationsCollection, id)
	if err != nil || rec.GetString("owner") != auth.Id {
		return nil, errors.New("conversation not found")
	}
	c := &conversation{rec: rec}
	_ = rec.UnmarshalJSONField("messages", &c.Messages)
	_ = rec.UnmarshalJSONField("actions", &c.Actions)
	return c, nil
}

func (c *conversation) save(app core.App) error {
	if len(c.Messages) > conversationLimit {
		c.Messages = trimHistory(c.Messages, conversationLimit)
	}
	if c.rec.GetString("title") == "" {
		for _, m := range c.Messages {
			if m.Role == "user" {
				c.rec.Set("title", limitText(strings.Join(strings.Fields(m.Content), " "), 80))
				break
			}
		}
	}
	c.rec.Set("messages", c.Messages)
	c.rec.Set("actions", c.Actions)
	return app.Save(c.rec)
}

func (c *conversation) export() map[string]any {
	messages := c.Messages
	if messages == nil {
		messages = []ai.Message{}
	}
	actions := c.Actions
	if actions == nil {
		actions = []*Action{}
	}
	return map[string]any{
		"id":       c.rec.Id,
		"title":    c.rec.GetString("title"),
		"messages": messages,
		"actions":  actions,
	}
}

func (c *conversation) action(id string) *Action {
	for _, act := range c.Actions {
		if act.ID == id {
			return act
		}
	}
	return nil
}

// trimHistory keeps the last n messages, starting at a user message so that
// tool results never lose their tool call.
func trimHistory(messages []ai.Message, n int) []ai.Message {
	if len(messages) <= n {
		return messages
	}
	out := messages[len(messages)-n:]
	for i, m := range out {
		if m.Role == "user" {
			return out[i:]
		}
	}
	return out
}

// --- chat ------------------------------------------------------------------------

type chatBody struct {
	Conversation string      `json:"conversation"`
	Message      string      `json:"message"`
	Page         PageContext `json:"page"`
}

// PageContext describes the dashboard page of the superuser.
type PageContext struct {
	Path       string `json:"path"`       // e.g. "#/dashboards/abc"
	Collection string `json:"collection"` // open collection
	Record     string `json:"record"`     // open record
	Dashboard  string `json:"dashboard"`  // open dashboard
	Flow       string `json:"flow"`       // open flow
	Page       string `json:"page"`       // open page (Pages module)
	Template   string `json:"template"`   // open email template
}

func (a *Assistant) chat(re *core.RequestEvent) error {
	var body chatBody
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" {
		return re.BadRequestError("The message is empty.", nil)
	}
	if len(body.Message) > maxMessageLength {
		return re.BadRequestError(fmt.Sprintf("The message is too long (max %d characters).", maxMessageLength), nil)
	}

	model, chatter, err := a.newModel(re.App)
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	conv, err := a.loadConversation(re.App, re.Auth, body.Conversation)
	if err != nil {
		return re.NotFoundError(err.Error(), nil)
	}

	ctx, cancel := context.WithTimeout(re.Request.Context(), chatTimeout)
	defer cancel()

	conv.Messages = append(conv.Messages, ai.Message{Role: "user", Content: body.Message})
	t := &toolbox{a: a, app: re.App, auth: re.Auth, ctx: ctx, conv: conv}
	runErr := a.run(ctx, t, model, chatter, body.Page)
	if err := conv.save(re.App); err != nil {
		return re.BadRequestError("Failed to save the conversation: "+err.Error(), nil)
	}
	out := conv.export()
	if runErr != nil {
		out["error"] = runErr.Error()
	}
	return re.JSON(http.StatusOK, out)
}

// run calls the model until it answers without calling tools.
func (a *Assistant) run(ctx context.Context, t *toolbox, model *ai.Model, chatter ai.Chatter, page PageContext) error {
	system := systemPrompt(t, page)
	tools := toolDefinitions()
	temperature := 0.2
	for step := 0; step < maxSteps; step++ {
		messages := append([]ai.Message{{Role: "system", Content: system}}, trimHistory(t.conv.Messages, maxHistory)...)
		res, err := chatter.Chat(ctx, model.Model, ai.ChatRequest{Messages: messages, Tools: tools, Temperature: &temperature})
		if err != nil {
			t.conv.Messages = append(t.conv.Messages, ai.Message{Role: "assistant", Content: "⚠️ " + err.Error()})
			return err
		}
		msg := res.Message
		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				msg.Content = "(no answer)"
			}
			t.conv.Messages = append(t.conv.Messages, msg)
			return nil
		}
		t.conv.Messages = append(t.conv.Messages, msg)
		for _, call := range msg.ToolCalls {
			result := t.call(call.Function.Name, call.Function.Arguments)
			t.conv.Messages = append(t.conv.Messages, ai.Message{Role: "tool", ToolCallID: call.ID, Content: result})
		}
		// the schema or the actions may have changed
		system = systemPrompt(t, page)
	}
	t.conv.Messages = append(t.conv.Messages, ai.Message{Role: "assistant",
		Content: fmt.Sprintf("I stopped after %d steps. Tell me how to continue, or ask a narrower question.", maxSteps)})
	return nil
}

// --- actions routes -----------------------------------------------------------

func (a *Assistant) applyRoute(re *core.RequestEvent) error {
	conv, err := a.loadConversation(re.App, re.Auth, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError(err.Error(), nil)
	}
	act := conv.action(re.Request.PathValue("action"))
	if act == nil {
		return re.NotFoundError("Action not found.", nil)
	}
	if act.Status != StatusPending && act.Status != StatusFailed {
		return re.BadRequestError("This action was already "+act.Status+".", nil)
	}
	result, err := a.apply(re, act)
	if err != nil {
		act.Status = StatusFailed
		act.Error = err.Error()
	} else {
		act.Status = StatusApplied
		act.Error = ""
		act.Result = result
	}
	if err := conv.save(re.App); err != nil {
		return err
	}
	out := conv.export()
	if act.Status == StatusFailed {
		out["error"] = act.Error
	}
	return re.JSON(http.StatusOK, out)
}

func (a *Assistant) discardRoute(re *core.RequestEvent) error {
	conv, err := a.loadConversation(re.App, re.Auth, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError(err.Error(), nil)
	}
	act := conv.action(re.Request.PathValue("action"))
	if act == nil {
		return re.NotFoundError("Action not found.", nil)
	}
	if act.Status == StatusPending || act.Status == StatusFailed {
		act.Status = StatusDiscarded
	}
	if err := conv.save(re.App); err != nil {
		return err
	}
	return re.JSON(http.StatusOK, conv.export())
}

// --- helpers --------------------------------------------------------------------

func limitText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error": "failed to encode the result"}`
	}
	return string(b)
}

// SetModel replaces the assistant model (tests, also of the modules adding
// tools).
func (a *Assistant) SetModel(fn func(app core.App) (*ai.Model, ai.Chatter, error)) {
	a.newModel = fn
}
