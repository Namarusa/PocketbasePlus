package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// AI providers (TypeSafe, OpenRouter) and the models used by the flows
// "AI decision" step (Settings → AI). Superusers only.
func init() {
	m.Register(func(app core.App) error {
		providers := core.NewBaseCollection("ai_providers")
		providers.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.SelectField{Name: "kind", Required: true, MaxSelect: 1, Values: []string{"typesafe", "openrouter"}},
			// never returned by the API (see the key_hint custom field)
			&core.TextField{Name: "api_key", Hidden: true, Max: 500},
			&core.URLField{Name: "base_url"}, // empty = the provider default
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		providers.AddIndex("idx_ai_providers_name", true, "name", "")
		if err := app.Save(providers); err != nil {
			return err
		}

		models := core.NewBaseCollection("ai_models")
		models.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.SelectField{Name: "type", Required: true, MaxSelect: 1, Values: []string{"system_one", "standard"}},
			&core.RelationField{Name: "provider", Required: true, MaxSelect: 1, CollectionId: providers.Id},
			&core.TextField{Name: "model", Required: true, Max: 200}, // provider model id
			&core.TextField{Name: "description", Max: 1000},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		models.AddIndex("idx_ai_models_name", true, "name", "")
		if err := app.Save(models); err != nil {
			return err
		}
		return markSystem(app, true, "ai_providers", "ai_models")
	}, func(app core.App) error {
		if err := markSystem(app, false, "ai_providers", "ai_models"); err != nil {
			return err
		}
		for _, name := range []string{"ai_models", "ai_providers"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
