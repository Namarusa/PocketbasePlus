package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Dashboard variables: inputs shown above a dashboard (date range, select,
// text, number) that the panel filters reference as {{$key}}.
func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("dashboards")
		if err != nil {
			return err
		}
		c.Fields.Add(&core.JSONField{Name: "variables"})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("dashboards")
		if err != nil {
			return err
		}
		c.Fields.RemoveByName("variables")
		return app.Save(c)
	})
}
