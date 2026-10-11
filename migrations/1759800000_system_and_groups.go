package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// the collections of the PocketBasePlus modules, listed in the dashboard "System" group
var moduleCollections = []any{
	"roles", "permissions", "rbac_collections",
	"email_templates", "email_automations", "email_messages",
	"flows", "flow_runs",
	"dashboards", "dashboard_panels",
	"collection_groups",
}

// Marks the module collections as system collections (shown in the
// dashboard "System" group, protected from rename, deletion and rules
// changes) and creates the collection_groups collection (custom groups of
// the collections sidebar).
func init() {
	m.Register(func(app core.App) error {
		groups := core.NewBaseCollection("collection_groups")
		groups.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.NumberField{Name: "order", OnlyInt: true},
			// collection ids, in display order
			&core.JSONField{Name: "collections", MaxSize: 1 << 16},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(groups); err != nil {
			return err
		}

		return setSystem(app, true)
	}, func(app core.App) error {
		if err := setSystem(app, false); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId("collection_groups"); err == nil {
			return app.Delete(c)
		}
		return nil
	})
}

// setSystem changes the system flag directly: the collection validator
// forbids changing it on existing collections.
func setSystem(app core.App, system bool) error {
	return markSystem(app, system, moduleCollections...)
}

// markSystem sets the system flag of the named collections.
func markSystem(app core.App, system bool, names ...any) error {
	_, err := app.DB().Update(
		"_collections",
		dbx.Params{"system": system},
		dbx.In("name", names...),
	).Execute()
	if err != nil {
		return err
	}
	return app.ReloadCachedCollections()
}
