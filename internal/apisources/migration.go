package apisources

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The migration lives in the module (not in migrations/) so removing the
// module removes it too. Its name keeps the order of the project migrations.
const (
	migrationFile     = "1761600000_api_sources.go"
	varsMigrationFile = "1761700000_api_sources_variables.go"
)

func init() {
	m.Register(func(app core.App) error {
		sources := core.NewBaseCollection(SourcesCollection)
		sources.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.TextField{Name: "description", Max: 1000},
			&core.TextField{Name: "base_url", Max: 2000}, // e.g. https://api.example.com/v1
			&core.SelectField{Name: "auth", MaxSelect: 1, Values: authTypes},
			&core.TextField{Name: "auth_name", Max: 100}, // header or query parameter of an API key
			&core.TextField{Name: "username", Max: 200},  // basic auth
			// never returned by the API (see the secret_hint custom field)
			&core.TextField{Name: "secret", Hidden: true, Max: 4000},
			&core.JSONField{Name: "headers", MaxSize: 64 << 10}, // shared by the calls, not secret
			&core.NumberField{Name: "timeout_seconds", OnlyInt: true, Min: ptr(1.0), Max: ptr(120.0)},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		sources.AddIndex("idx_api_sources_name", true, "name", "")
		if err := app.Save(sources); err != nil {
			return err
		}

		calls := core.NewBaseCollection(CallsCollection)
		calls.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.RelationField{Name: "source", Required: true, MaxSelect: 1, CollectionId: sources.Id},
			&core.TextField{Name: "description", Max: 1000},
			&core.SelectField{Name: "method", MaxSelect: 1, Values: methods},
			&core.TextField{Name: "path", Max: 2000}, // after the base URL, or a full URL
			&core.JSONField{Name: "query", MaxSize: 64 << 10},
			&core.JSONField{Name: "headers", MaxSize: 64 << 10},
			&core.SelectField{Name: "body_type", MaxSelect: 1, Values: bodyTypes},
			&core.TextField{Name: "body", Max: 1 << 20},      // json, xml, text
			&core.JSONField{Name: "form", MaxSize: 64 << 10}, // form key/values
			&core.JSONField{Name: "variables", MaxSize: 64 << 10},
			&core.SelectField{Name: "response_format", MaxSelect: 1, Values: []string{"auto", "json", "xml", "text"}},
			&core.JSONField{Name: "outputs", MaxSize: 64 << 10},
			&core.NumberField{Name: "retries", OnlyInt: true, Min: ptr(0.0), Max: ptr(3.0)},
			&core.BoolField{Name: "retry_unsafe"},
			&core.JSONField{Name: "sample", MaxSize: 256 << 10}, // the last test response (shortened)
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		calls.AddIndex("idx_api_calls_source_name", true, "source, name", "")
		if err := app.Save(calls); err != nil {
			return err
		}
		return markSystem(app, true, SourcesCollection, CallsCollection)
	}, func(app core.App) error {
		return dropCollections(app)
	}, migrationFile)
}

// the variables of an API: used in its base address, shared headers and
// sign-in, filled by the steps like the variables of its calls
func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(SourcesCollection)
		if err != nil {
			return err
		}
		c.Fields.Add(&core.JSONField{Name: "variables", MaxSize: 64 << 10})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(SourcesCollection)
		if err != nil {
			return nil
		}
		c.Fields.RemoveByName("variables")
		return app.Save(c)
	}, varsMigrationFile)
}

func dropCollections(app core.App) error {
	if err := markSystem(app, false, SourcesCollection, CallsCollection); err != nil {
		return err
	}
	for _, name := range []string{CallsCollection, SourcesCollection} {
		if c, err := app.FindCollectionByNameOrId(name); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// markSystem flags collections as system ones (listed in the System group of
// the sidebar, can't be renamed or deleted from the dashboard).
func markSystem(app core.App, system bool, names ...string) error {
	in := make([]any, len(names))
	for i, n := range names {
		in[i] = n
	}
	_, err := app.DB().Update("_collections", dbx.Params{"system": system}, dbx.In("name", in...)).Execute()
	if err != nil {
		return err
	}
	return app.ReloadCachedCollections()
}
