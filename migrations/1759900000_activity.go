package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

var activityCollections = []any{"revisions", "comments", "notifications"}

// Creates the activity collections: record revisions, record comments and
// superuser notifications (all system collections, superusers only).
func init() {
	m.Register(func(app core.App) error {
		revisions := core.NewBaseCollection("revisions")
		revisions.Fields.Add(
			&core.TextField{Name: "collection", Required: true}, // collection id
			&core.TextField{Name: "collection_name"},
			&core.TextField{Name: "record_id", Required: true},
			&core.SelectField{Name: "action", Required: true, MaxSelect: 1, Values: []string{"create", "update", "delete", "revert"}},
			&core.SelectField{Name: "actor_type", MaxSelect: 1, Values: []string{"superuser", "user", "flow", "system"}},
			&core.TextField{Name: "actor_id"},
			&core.TextField{Name: "actor_label"},
			&core.JSONField{Name: "data", MaxSize: 5 << 20},  // record state after the change (before it for deletes)
			&core.JSONField{Name: "delta", MaxSize: 5 << 20}, // {field: [old, new]}
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		revisions.AddIndex("idx_revisions_record", false, "collection, record_id, created", "")
		revisions.AddIndex("idx_revisions_created", false, "created", "")
		if err := app.Save(revisions); err != nil {
			return err
		}

		comments := core.NewBaseCollection("comments")
		comments.Fields.Add(
			&core.TextField{Name: "collection", Required: true},
			&core.TextField{Name: "record_id", Required: true},
			&core.TextField{Name: "author_id"},
			&core.TextField{Name: "author_label"},
			&core.TextField{Name: "text", Required: true, Max: 10000},
			&core.JSONField{Name: "mentions"}, // superuser ids
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		comments.AddIndex("idx_comments_record", false, "collection, record_id, created", "")
		if err := app.Save(comments); err != nil {
			return err
		}

		notifications := core.NewBaseCollection("notifications")
		notifications.Fields.Add(
			&core.TextField{Name: "recipient", Required: true}, // superuser id
			&core.SelectField{Name: "kind", MaxSelect: 1, Values: []string{"mention", "flow", "system"}},
			&core.TextField{Name: "subject", Required: true, Max: 300},
			&core.TextField{Name: "message", Max: 5000},
			&core.TextField{Name: "link", Max: 2000},
			&core.BoolField{Name: "read"},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		notifications.AddIndex("idx_notifications_recipient", false, "recipient, read, created", "")
		if err := app.Save(notifications); err != nil {
			return err
		}

		return markSystem(app, true, activityCollections...)
	}, func(app core.App) error {
		if err := markSystem(app, false, activityCollections...); err != nil {
			return err
		}
		for _, name := range []string{"notifications", "comments", "revisions"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
