package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Model is a configured model (Settings → AI models) with its provider client.
type Model struct {
	ID           string
	Name         string
	Model        string // provider model id, e.g. "jev-latest" or "openai/gpt-5-mini"
	Type         string // system_one | standard
	ProviderName string
	Kind         string
	Provider     Provider
}

// Decide runs a decision with the model.
func (m *Model) Decide(ctx context.Context, req Request) (*Response, error) {
	return m.Provider.Decide(ctx, m.Model, req)
}

// newProvider returns the client of a provider record.
func newProvider(app core.App, p *core.Record) (Provider, error) {
	key := p.GetString("api_key")
	if key == "" {
		return nil, fmt.Errorf("the AI provider %q has no API key (Settings → AI providers)", p.GetString("name"))
	}
	switch p.GetString("kind") {
	case KindTypeSafe:
		return &TypeSafe{BaseURL: p.GetString("base_url"), APIKey: key}, nil
	case KindAnthropic:
		return &Anthropic{BaseURL: p.GetString("base_url"), APIKey: key}, nil
	case KindOpenRouter, KindOpenAI, KindGemini, KindDeepSeek, KindKimi, KindGLM:
		return &Compat{
			Kind:    p.GetString("kind"),
			BaseURL: p.GetString("base_url"),
			APIKey:  key,
			AppName: app.Settings().Meta.AppName,
			AppURL:  app.Settings().Meta.AppURL,
		}, nil
	}
	return nil, fmt.Errorf("unknown AI provider kind %q", p.GetString("kind"))
}

// LoadModel loads a configured model by id.
func LoadModel(app core.App, id string) (*Model, error) {
	if id == "" {
		return nil, errors.New("select an AI model (configure them in Settings → AI models)")
	}
	m, err := app.FindRecordById(ModelsCollection, id)
	if err != nil {
		return nil, fmt.Errorf("the AI model %q doesn't exist anymore (Settings → AI models)", id)
	}
	p, err := app.FindRecordById(ProvidersCollection, m.GetString("provider"))
	if err != nil {
		return nil, fmt.Errorf("the provider of the AI model %q doesn't exist anymore", m.GetString("name"))
	}
	client, err := newProvider(app, p)
	if err != nil {
		return nil, err
	}
	return &Model{
		ID:           m.Id,
		Name:         m.GetString("name"),
		Model:        m.GetString("model"),
		Type:         m.GetString("type"),
		ProviderName: p.GetString("name"),
		Kind:         p.GetString("kind"),
		Provider:     client,
	}, nil
}

// keyHint shows the end of an API key ("…1a2b").
func keyHint(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "…"
	}
	return "…" + key[len(key)-4:]
}

// Register binds the settings validation and the /api/ai routes.
func Register(app core.App) {
	app.OnRecordValidate(ProvidersCollection).BindFunc(func(e *core.RecordEvent) error {
		r := e.Record
		r.Set("name", strings.TrimSpace(r.GetString("name")))
		r.Set("api_key", strings.TrimSpace(r.GetString("api_key")))
		r.Set("base_url", strings.TrimRight(strings.TrimSpace(r.GetString("base_url")), "/"))
		if !r.IsNew() && r.Original().GetString("kind") != r.GetString("kind") {
			n, _ := e.App.CountRecords(ModelsCollection, dbx.HashExp{"provider": r.Id})
			if n > 0 {
				return validation.Errors{"kind": validation.NewError("kind_in_use", "The provider has models: delete them before changing its kind.")}
			}
		}
		return e.Next()
	})

	app.OnRecordValidate(ModelsCollection).BindFunc(func(e *core.RecordEvent) error {
		r := e.Record
		r.Set("name", strings.TrimSpace(r.GetString("name")))
		r.Set("model", strings.TrimSpace(r.GetString("model")))
		p, err := e.App.FindRecordById(ProvidersCollection, r.GetString("provider"))
		if err != nil {
			return validation.Errors{"provider": validation.NewError("invalid_provider", "Select a provider.")}
		}
		if want := ModelType(p.GetString("kind")); want != r.GetString("type") {
			label := map[string]string{TypeSystemOne: "System One", TypeStandard: "Standard"}[want]
			return validation.Errors{"provider": validation.NewError("incompatible_provider",
				fmt.Sprintf("%s serves %s models only.", p.GetString("name"), label))}
		}
		return e.Next()
	})

	// a deleted model would break the flows using it
	app.OnRecordDelete(ModelsCollection).BindFunc(func(e *core.RecordEvent) error {
		var names []string
		_ = e.App.DB().Select("name").From("flows").
			Where(dbx.Like("steps", e.Record.Id)).
			Column(&names)
		if len(names) > 0 {
			return apis.NewBadRequestError(fmt.Sprintf("The model is used by the flows: %s. Change their AI steps first.", strings.Join(names, ", ")), nil)
		}
		return e.Next()
	})

	app.OnRecordDelete(ProvidersCollection).BindFunc(func(e *core.RecordEvent) error {
		n, _ := e.App.CountRecords(ModelsCollection, dbx.HashExp{"provider": e.Record.Id})
		if n > 0 {
			return apis.NewBadRequestError("Delete the models of this provider first.", nil)
		}
		return e.Next()
	})

	// the API key is never returned, even to superusers: show whether it's set and its end instead
	app.OnRecordEnrich(ProvidersCollection).BindFunc(func(e *core.RecordEnrichEvent) error {
		rec := e.Record
		rec.WithCustomData(true)
		rec.Set("key_hint", keyHint(rec.GetString("api_key")))
		// hidden fields are unhidden for superusers at the end of the enrich chain
		err := e.Next()
		rec.Hide("api_key")
		return err
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		g := se.Router.Group("/api/ai")
		g.Bind(apis.RequireSuperuserAuth())
		g.POST("/providers/{id}/models", providerModels)
		g.POST("/models/{id}/test", testModel)
		return se.Next()
	})
}

// providerModels checks the provider connection and lists its models.
func providerModels(re *core.RequestEvent) error {
	p, err := re.App.FindRecordById(ProvidersCollection, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError("", nil)
	}
	client, err := newProvider(re.App, p)
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	ctx, cancel := context.WithTimeout(re.Request.Context(), 20*time.Second)
	defer cancel()
	models, err := client.Models(ctx)
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	return re.JSON(http.StatusOK, map[string]any{"models": models})
}

// testModel asks a sample question with the model.
func testModel(re *core.RequestEvent) error {
	m, err := LoadModel(re.App, re.Request.PathValue("id"))
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	req := Request{
		State: "Hi! My payouts have been failing for 3 days and my customers are angry. Please help!",
		Questions: []Question{
			{Key: "urgent", Type: QuestionNoul, Instructions: "Does the customer need help urgently?"},
			{Key: "team", Type: QuestionChoice, Instructions: "Which team should handle this message?", Options: []Option{
				{Key: "billing", Description: "Payments, invoices, refunds, payouts"},
				{Key: "technical", Description: "Bugs, outages, integrations"},
				{Key: "sales", Description: "Pricing, upgrades, new accounts"},
			}},
			{Key: "mood", Type: QuestionScore, Instructions: "How frustrated is the customer?", Levels: []any{"Calm", "Annoyed", "Angry"}},
		},
	}
	ctx, cancel := context.WithTimeout(re.Request.Context(), 60*time.Second)
	defer cancel()
	started := time.Now()
	res, err := m.Decide(ctx, req)
	if err != nil {
		return re.BadRequestError(err.Error(), nil)
	}
	return re.JSON(http.StatusOK, map[string]any{
		"request":    req,
		"response":   res,
		"durationMs": time.Since(started).Milliseconds(),
	})
}
