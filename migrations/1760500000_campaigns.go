package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Marketing emails: a "marketing" flag on the templates (unsubscribe link,
// opt-out list) and the opt-out list itself.
func init() {
	m.Register(func(app core.App) error {
		templates, err := app.FindCollectionByNameOrId("email_templates")
		if err != nil {
			return err
		}
		templates.Fields.Add(&core.BoolField{Name: "marketing"})
		if err := app.Save(templates); err != nil {
			return err
		}

		unsubscribes := core.NewBaseCollection("email_unsubscribes")
		unsubscribes.Fields.Add(
			&core.TextField{Name: "email", Required: true, Max: 320}, // lowercased
			&core.TextField{Name: "source", Max: 100},                // link | one-click | manual | import...
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		unsubscribes.AddIndex("idx_email_unsubscribes_email", true, "email", "")
		if err := app.Save(unsubscribes); err != nil {
			return err
		}
		return markSystem(app, true, "email_unsubscribes")
	}, func(app core.App) error {
		if err := markSystem(app, false, "email_unsubscribes"); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId("email_unsubscribes"); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		templates, err := app.FindCollectionByNameOrId("email_templates")
		if err != nil {
			return err
		}
		templates.Fields.RemoveByName("marketing")
		return app.Save(templates)
	})
}
