package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the collections used by the RBAC and mail modules.
// All of them keep nil API rules, i.e. they are accessible only by superusers.
func init() {
	m.Register(func(app core.App) error {
		// --- RBAC -----------------------------------------------------------

		roles := core.NewBaseCollection("roles")
		roles.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.TextField{Name: "description", Max: 500},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		roles.AddIndex("idx_roles_name", true, "name", "")
		if err := app.Save(roles); err != nil {
			return err
		}

		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		users.Fields.Add(&core.RelationField{
			Name:         "roles",
			CollectionId: roles.Id,
			MaxSelect:    999,
		})
		if err := app.Save(users); err != nil {
			return err
		}

		rbacCollections := core.NewBaseCollection("rbac_collections")
		rbacCollections.Fields.Add(
			// stores the collection id so that renames don't break the mapping
			&core.TextField{Name: "collection", Required: true},
			&core.TextField{Name: "owner_field"},
			&core.BoolField{Name: "managed"},
		)
		rbacCollections.AddIndex("idx_rbac_collections_collection", true, "collection", "")
		if err := app.Save(rbacCollections); err != nil {
			return err
		}

		permissions := core.NewBaseCollection("permissions")
		permissions.Fields.Add(
			&core.RelationField{Name: "role", CollectionId: roles.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
			&core.TextField{Name: "collection", Required: true},
			&core.SelectField{Name: "action", Required: true, MaxSelect: 1, Values: []string{"list", "view", "create", "update", "delete"}},
			&core.SelectField{Name: "scope", Required: true, MaxSelect: 1, Values: []string{"own", "all"}},
		)
		permissions.AddIndex("idx_permissions_unique", true, "role, collection, action", "")
		if err := app.Save(permissions); err != nil {
			return err
		}

		// --- Mail -----------------------------------------------------------

		templates := core.NewBaseCollection("email_templates")
		templates.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.TextField{Name: "slug", Required: true, Max: 100, Pattern: `^[a-z0-9_-]+$`},
			&core.TextField{Name: "subject", Required: true, Max: 1000},
			&core.TextField{Name: "html", Max: 500000},
			&core.TextField{Name: "text", Max: 100000},
			&core.TextField{Name: "sample_collection"},
			&core.JSONField{Name: "sample_data"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		templates.AddIndex("idx_email_templates_slug", true, "slug", "")
		if err := app.Save(templates); err != nil {
			return err
		}

		automations := core.NewBaseCollection("email_automations")
		automations.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.BoolField{Name: "active"},
			&core.RelationField{Name: "template", CollectionId: templates.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
			&core.SelectField{Name: "trigger", Required: true, MaxSelect: 1, Values: []string{"record_event", "schedule", "delayed"}},
			&core.TextField{Name: "collection"}, // collection name
			&core.SelectField{Name: "events", MaxSelect: 3, Values: []string{"create", "update", "delete"}},
			&core.TextField{Name: "filter", Max: 2000},
			&core.TextField{Name: "cron", Max: 100},
			&core.NumberField{Name: "delay_minutes", OnlyInt: true},
			&core.BoolField{Name: "recheck_filter"},
			&core.TextField{Name: "recipient_field", Max: 200},
			&core.TextField{Name: "static_recipients", Max: 5000},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(automations); err != nil {
			return err
		}

		messages := core.NewBaseCollection("email_messages")
		messages.Fields.Add(
			&core.RelationField{Name: "template", CollectionId: templates.Id, MaxSelect: 1},
			&core.RelationField{Name: "automation", CollectionId: automations.Id, MaxSelect: 1},
			&core.TextField{Name: "collection"},
			&core.TextField{Name: "record_id"},
			&core.TextField{Name: "to", Required: true},
			&core.TextField{Name: "subject"},
			&core.TextField{Name: "html", Max: 1000000},
			&core.TextField{Name: "text", Max: 200000},
			&core.DateField{Name: "send_at", Required: true},
			&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"pending", "sent", "failed", "cancelled", "skipped"}},
			&core.NumberField{Name: "attempts", OnlyInt: true},
			&core.TextField{Name: "error", Max: 5000},
			&core.DateField{Name: "sent_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		messages.AddIndex("idx_email_messages_queue", false, "status, send_at", "")
		return app.Save(messages)
	}, func(app core.App) error {
		for _, name := range []string{"email_messages", "email_automations", "email_templates", "permissions", "rbac_collections"} {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			if err := app.Delete(c); err != nil {
				return err
			}
		}

		if users, err := app.FindCollectionByNameOrId("users"); err == nil {
			users.Fields.RemoveByName("roles")
			if err := app.Save(users); err != nil {
				return err
			}
		}

		if roles, err := app.FindCollectionByNameOrId("roles"); err == nil {
			return app.Delete(roles)
		}
		return nil
	})
}
