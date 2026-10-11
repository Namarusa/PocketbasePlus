package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the dashboards (insights) collections. Superuser-only (nil API
// rules): role users read their dashboards through the /api/dashboards routes.
func init() {
	m.Register(func(app core.App) error {
		roles, err := app.FindCollectionByNameOrId("roles")
		if err != nil {
			return err
		}

		dashboards := core.NewBaseCollection("dashboards")
		dashboards.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.TextField{Name: "icon", Max: 100},
			&core.TextField{Name: "note", Max: 2000},
			// roles allowed to view the dashboard (superusers always can)
			&core.RelationField{Name: "roles", CollectionId: roles.Id, MaxSelect: 999},
			// auto refresh interval in seconds (0 = off)
			&core.NumberField{Name: "refresh", OnlyInt: true, Min: ptr(0.0), Max: ptr(86400.0)},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(dashboards); err != nil {
			return err
		}

		panels := core.NewBaseCollection("dashboard_panels")
		panels.Fields.Add(
			&core.RelationField{Name: "dashboard", CollectionId: dashboards.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
			&core.SelectField{Name: "type", Required: true, MaxSelect: 1, Values: []string{"metric", "time_series", "bar", "pie", "meter", "list", "label"}},
			&core.TextField{Name: "name", Max: 200},
			&core.TextField{Name: "icon", Max: 100},
			&core.TextField{Name: "color", Max: 50},
			&core.BoolField{Name: "show_header"},
			&core.TextField{Name: "note", Max: 1000},
			// position and size in grid units
			&core.NumberField{Name: "x", OnlyInt: true},
			&core.NumberField{Name: "y", OnlyInt: true},
			&core.NumberField{Name: "w", OnlyInt: true},
			&core.NumberField{Name: "h", OnlyInt: true},
			&core.JSONField{Name: "options", MaxSize: 1 << 20},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		panels.AddIndex("idx_dashboard_panels_dashboard", false, "dashboard", "")
		return app.Save(panels)
	}, func(app core.App) error {
		for _, name := range []string{"dashboard_panels", "dashboards"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func ptr[T any](v T) *T {
	return &v
}
