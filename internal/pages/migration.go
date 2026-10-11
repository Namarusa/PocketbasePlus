package pages

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The migration lives in the module (not in migrations/) so removing the
// module removes it too. Its name keeps the order of the project migrations.
const migrationFile = "1760800000_pages.go"

func init() {
	m.Register(func(app core.App) error {
		pages := core.NewBaseCollection(Collection)
		pages.Fields.Add(
			&core.TextField{Name: "title", Required: true, Max: 200},
			&core.TextField{Name: "slug", Required: true, Max: 100, Pattern: `^[a-z0-9][a-z0-9-]*$`},
			&core.BoolField{Name: "published"},
			&core.BoolField{Name: "home"}, // served at "/" (one page at most)
			&core.TextField{Name: "description", Max: 500},
			// record pages: one record of this collection per URL (/{slug}/{id})
			&core.TextField{Name: "record_collection"}, // collection id
			&core.TextField{Name: "record_filter", Max: 2000},
			&core.JSONField{Name: "project", MaxSize: 10 << 20}, // GrapesJS project data (editor state)
			&core.TextField{Name: "html", Max: 2_000_000},       // exported by the editor, rendered by the server
			&core.TextField{Name: "css", Max: 1_000_000},
			&core.FileField{Name: "assets", MaxSelect: 999, MaxSize: 10 << 20, MimeTypes: []string{
				"image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "image/svg+xml",
				"video/mp4", "video/webm",
			}},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		pages.AddIndex("idx_pages_slug", true, "slug", "")
		if err := app.Save(pages); err != nil {
			return err
		}
		return markSystem(app, true, Collection)
	}, func(app core.App) error {
		if err := markSystem(app, false, Collection); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId(Collection); err == nil {
			return app.Delete(c)
		}
		return nil
	}, migrationFile)
}

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
