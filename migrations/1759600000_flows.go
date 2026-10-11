package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the flows (automation) collections. Superuser-only (nil API rules).
func init() {
	m.Register(func(app core.App) error {
		flows := core.NewBaseCollection("flows")
		flows.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.TextField{Name: "description", Max: 2000},
			&core.BoolField{Name: "active"},
			&core.SelectField{Name: "trigger_type", Required: true, MaxSelect: 1, Values: []string{"record", "manual", "webhook", "schedule", "flow"}},
			&core.JSONField{Name: "trigger"},
			&core.JSONField{Name: "steps", MaxSize: 2 << 20},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(flows); err != nil {
			return err
		}

		runs := core.NewBaseCollection("flow_runs")
		runs.Fields.Add(
			&core.RelationField{Name: "flow", CollectionId: flows.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
			&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"running", "success", "failed", "rejected"}},
			&core.TextField{Name: "trigger_type"},
			&core.JSONField{Name: "input", MaxSize: 2 << 20},
			&core.JSONField{Name: "steps", MaxSize: 5 << 20},
			&core.JSONField{Name: "output", MaxSize: 2 << 20},
			&core.TextField{Name: "error", Max: 5000},
			&core.NumberField{Name: "duration_ms", OnlyInt: true},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		runs.AddIndex("idx_flow_runs_flow", false, "flow, created", "")
		if err := app.Save(runs); err != nil {
			return err
		}

		// lets the email log show which flow queued a message
		messages, err := app.FindCollectionByNameOrId("email_messages")
		if err != nil {
			return err
		}
		messages.Fields.Add(&core.TextField{Name: "source", Max: 300})
		return app.Save(messages)
	}, func(app core.App) error {
		if messages, err := app.FindCollectionByNameOrId("email_messages"); err == nil {
			messages.Fields.RemoveByName("source")
			if err := app.Save(messages); err != nil {
				return err
			}
		}
		for _, name := range []string{"flow_runs", "flows"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
