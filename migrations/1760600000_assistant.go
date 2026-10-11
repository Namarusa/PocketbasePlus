package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// AI assistant: its settings (one record) and the conversations of the
// superusers. Superusers only.
func init() {
	m.Register(func(app core.App) error {
		models, err := app.FindCollectionByNameOrId("ai_models")
		if err != nil {
			return err
		}
		settings := core.NewBaseCollection("ai_settings")
		settings.Fields.Add(
			&core.BoolField{Name: "assistant_enabled"},
			&core.RelationField{Name: "assistant_model", CollectionId: models.Id, MaxSelect: 1},
			&core.TextField{Name: "instructions", Max: 20000}, // business notes
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(settings); err != nil {
			return err
		}

		conversations := core.NewBaseCollection("ai_conversations")
		conversations.Fields.Add(
			&core.TextField{Name: "owner", Required: true}, // superuser id
			&core.TextField{Name: "title", Max: 200},
			&core.JSONField{Name: "messages", MaxSize: 5 << 20},
			&core.JSONField{Name: "actions", MaxSize: 5 << 20},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		conversations.AddIndex("idx_ai_conversations_owner", false, "owner, updated", "")
		if err := app.Save(conversations); err != nil {
			return err
		}
		return markSystem(app, true, "ai_settings", "ai_conversations")
	}, func(app core.App) error {
		if err := markSystem(app, false, "ai_settings", "ai_conversations"); err != nil {
			return err
		}
		for _, name := range []string{"ai_conversations", "ai_settings"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
