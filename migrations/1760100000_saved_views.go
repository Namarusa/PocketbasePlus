package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Saved views of the collection pages (filter, sort, columns, layout).
func init() {
	m.Register(func(app core.App) error {
		views := core.NewBaseCollection("saved_views")
		views.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.TextField{Name: "collection", Required: true}, // collection id
			&core.JSONField{Name: "query"},                      // {filter, sort, columns, layout}
			&core.TextField{Name: "owner"},                      // superuser id
			&core.BoolField{Name: "shared"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		views.AddIndex("idx_saved_views_collection", false, "collection", "")
		if err := app.Save(views); err != nil {
			return err
		}
		return markSystem(app, true, "saved_views")
	}, func(app core.App) error {
		if err := markSystem(app, false, "saved_views"); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId("saved_views"); err == nil {
			return app.Delete(c)
		}
		return nil
	})
}
