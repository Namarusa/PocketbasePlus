package media

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/Namarusa/PocketbasePlus/internal/medialib"
)

// The migration lives in the module (not in migrations/) so removing the
// module removes it too. Its name keeps the order of the project migrations.
const migrationFile = "1763100000_media.go"

// thumbs are the thumbnails of the dashboard (PocketBase makes them).
var thumbs = []string{"320x320f", "100x100f"}

func init() {
	m.Register(func(app core.App) error {
		c := core.NewBaseCollection(medialib.Collection)
		// superusers only: the files themselves are public (their address)
		c.Fields.Add(
			&core.FileField{Name: "file", Required: true, MaxSelect: 1, MaxSize: MaxBytes, MimeTypes: mimeTypes, Thumbs: thumbs},
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.TextField{Name: "folder", Max: 100},
			&core.TextField{Name: "alt", Max: 500},
			&core.SelectField{Name: "kind", MaxSelect: 1, Values: kinds},
			&core.TextField{Name: "mime", Max: 200},
			&core.NumberField{Name: "size", OnlyInt: true},
			&core.NumberField{Name: "width", OnlyInt: true},
			&core.NumberField{Name: "height", OnlyInt: true},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		c.AddIndex("idx_media_assets_folder", false, "folder", "")
		if err := app.Save(c); err != nil {
			return err
		}
		return markSystem(app, true)
	}, func(app core.App) error {
		return dropCollection(app)
	}, migrationFile)
}

func dropCollection(app core.App) error {
	if err := markSystem(app, false); err != nil {
		return err
	}
	if c, err := app.FindCollectionByNameOrId(medialib.Collection); err == nil {
		return app.Delete(c)
	}
	return nil
}

// markSystem flags the collection as a system one (the System group of the
// sidebar; it can't be renamed or deleted from the dashboard).
func markSystem(app core.App, system bool) error {
	if _, err := app.DB().Update("_collections", dbx.Params{"system": system}, dbx.HashExp{"name": medialib.Collection}).Execute(); err != nil {
		return err
	}
	return app.ReloadCachedCollections()
}
