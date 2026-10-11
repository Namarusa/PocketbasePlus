package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Finer permissions: a "custom" scope with a filter, the readable/writable
// fields of a grant and the default values of created records (presets).
func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("permissions")
		if err != nil {
			return err
		}
		if scope, ok := c.Fields.GetByName("scope").(*core.SelectField); ok && !slices.Contains(scope.Values, "custom") {
			scope.Values = append(scope.Values, "custom")
		}
		c.Fields.Add(
			&core.TextField{Name: "filter", Max: 5000}, // custom scope filter
			&core.JSONField{Name: "fields"},            // allowed fields (empty = all)
			&core.JSONField{Name: "presets"},           // create: default values
		)
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("permissions")
		if err != nil {
			return err
		}
		for _, name := range []string{"filter", "fields", "presets"} {
			c.Fields.RemoveByName(name)
		}
		if scope, ok := c.Fields.GetByName("scope").(*core.SelectField); ok {
			scope.Values = slices.DeleteFunc(scope.Values, func(v string) bool { return v == "custom" })
		}
		return app.Save(c)
	})
}
