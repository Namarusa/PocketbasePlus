package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Public forms built from the fields of a collection (full page and iframe
// embed). Submissions create records of that collection.
func init() {
	m.Register(func(app core.App) error {
		forms := core.NewBaseCollection("forms")
		forms.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.TextField{Name: "slug", Required: true, Max: 100, Pattern: `^[a-z0-9][a-z0-9-]*$`},
			&core.TextField{Name: "collection", Required: true}, // collection id
			&core.BoolField{Name: "active"},
			&core.JSONField{Name: "fields"}, // [{field, label, help, placeholder, required, width, listRecords}]
			&core.JSONField{Name: "fixed"},  // {field: value or macro}
			&core.TextField{Name: "title", Max: 300},
			&core.TextField{Name: "description", Max: 5000},
			&core.TextField{Name: "submit_label", Max: 100},
			&core.TextField{Name: "success_message", Max: 2000},
			&core.URLField{Name: "redirect_url"},
			&core.JSONField{Name: "allowed_domains"}, // embed: allowed parent origins
			&core.BoolField{Name: "require_auth"},
			&core.NumberField{Name: "rate_limit", OnlyInt: true, Min: ptr(0.0)}, // submissions per IP and hour (0 = unlimited)
			&core.SelectField{Name: "theme", MaxSelect: 1, Values: []string{"auto", "light", "dark"}},
			&core.TextField{Name: "accent", Max: 50},
			// signs the submit tokens of the form
			&core.TextField{Name: "secret", Hidden: true, AutogeneratePattern: `[a-zA-Z0-9]{40}`, Max: 100},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		forms.AddIndex("idx_forms_slug", true, "slug", "")
		if err := app.Save(forms); err != nil {
			return err
		}

		// form submissions appear in the revisions with their own author type
		if revisions, err := app.FindCollectionByNameOrId("revisions"); err == nil {
			if f, ok := revisions.Fields.GetByName("actor_type").(*core.SelectField); ok && !slices.Contains(f.Values, "form") {
				f.Values = append(f.Values, "form")
				if err := app.Save(revisions); err != nil {
					return err
				}
			}
		}
		return markSystem(app, true, "forms")
	}, func(app core.App) error {
		if err := markSystem(app, false, "forms"); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId("forms"); err == nil {
			return app.Delete(c)
		}
		return nil
	})
}
