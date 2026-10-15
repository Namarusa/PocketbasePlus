package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Email templates: a folder (to group them in the list), and the account
// emails of PocketBase (verification, password reset...) a template can
// replace, for one auth collection or all of them.
func init() {
	m.Register(func(app core.App) error {
		templates, err := app.FindCollectionByNameOrId("email_templates")
		if err != nil {
			return err
		}
		templates.Fields.Add(
			&core.TextField{Name: "folder", Max: 100},
			&core.SelectField{Name: "account_email", MaxSelect: 1, Values: []string{"verification", "password_reset", "email_change", "otp", "login_alert"}},
			&core.TextField{Name: "account_collection", Max: 100}, // auth collection name, "" = all of them (but the superusers)
		)
		return app.Save(templates)
	}, func(app core.App) error {
		templates, err := app.FindCollectionByNameOrId("email_templates")
		if err != nil {
			return err
		}
		templates.Fields.RemoveByName("folder")
		templates.Fields.RemoveByName("account_email")
		templates.Fields.RemoveByName("account_collection")
		return app.Save(templates)
	})
}
