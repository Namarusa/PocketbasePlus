package documents

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The migration lives in the module (not in migrations/) so removing the
// module removes it too. Its name keeps the order of the project migrations.
const migrationFile = "1762900000_documents.go"

func init() {
	m.Register(func(app core.App) error {
		templates := core.NewBaseCollection(TemplatesCollection)
		templates.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			&core.TextField{Name: "slug", Required: true, Max: 100, Pattern: `^[a-z0-9_-]+$`},
			&core.TextField{Name: "folder", Max: 100},
			&core.TextField{Name: "description", Max: 1000},
			&core.SelectField{Name: "format", MaxSelect: 1, Values: []string{"a4", "a5", "a3", "letter", "legal", "custom"}},
			&core.SelectField{Name: "orientation", MaxSelect: 1, Values: []string{"portrait", "landscape"}},
			&core.NumberField{Name: "width_mm"},
			&core.NumberField{Name: "height_mm"},
			&core.JSONField{Name: "margins", MaxSize: 1 << 10},
			&core.NumberField{Name: "font_size"},
			&core.TextField{Name: "accent_color", Max: 7},
			&core.TextField{Name: "collection", Max: 100}, // the record the document is about
			&core.TextField{Name: "sample_record", Max: 50},
			&core.JSONField{Name: "lists", MaxSize: 64 << 10},
			&core.JSONField{Name: "header", MaxSize: maxSpecBytes},
			&core.JSONField{Name: "body", MaxSize: maxSpecBytes},
			&core.JSONField{Name: "footer", MaxSize: maxSpecBytes},
			&core.TextField{Name: "filename", Max: 500},
			&core.FileField{Name: "assets", MaxSelect: 20, MaxSize: maxAssetBytes, MimeTypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp"}},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		templates.AddIndex("idx_document_templates_slug", true, "slug", "")
		if err := app.Save(templates); err != nil {
			return err
		}

		docs := core.NewBaseCollection(DocumentsCollection)
		docs.Fields.Add(
			&core.RelationField{Name: "template", CollectionId: templates.Id, MaxSelect: 1},
			&core.FileField{Name: "file", MaxSelect: 1, MaxSize: 100 << 20, Protected: true},
			&core.TextField{Name: "filename", Max: 200},
			&core.TextField{Name: "collection", Max: 100},
			&core.TextField{Name: "record_id", Max: 50},
			&core.NumberField{Name: "pages", OnlyInt: true},
			&core.NumberField{Name: "size", OnlyInt: true},
			&core.TextField{Name: "source", Max: 200}, // dashboard, or the flow
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		docs.AddIndex("idx_documents_record", false, "collection, record_id", "")
		if err := app.Save(docs); err != nil {
			return err
		}
		return markSystem(app, true, TemplatesCollection, DocumentsCollection)
	}, func(app core.App) error {
		return dropCollections(app)
	}, migrationFile)
}

func dropCollections(app core.App) error {
	if err := markSystem(app, false, TemplatesCollection, DocumentsCollection); err != nil {
		return err
	}
	for _, name := range []string{DocumentsCollection, TemplatesCollection} {
		if c, err := app.FindCollectionByNameOrId(name); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}
	}
	return nil
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
