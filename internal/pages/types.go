package pages

import (
	"slices"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The types of pages: a single page (kind ""), a collection page (kind
// "collection": a page listing the records of list_collection, served like a
// single page), a single item page (record_collection: one record per
// address, /slug/{id}) and a site part (kind "part").
const (
	typesMigrationFile = "1762600000_pages_types.go"
	kindCollection     = "collection"
)

func init() {
	m.Register(func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		if f, ok := pages.Fields.GetByName("kind").(*core.SelectField); ok && !slices.Contains(f.Values, kindCollection) {
			f.Values = append(f.Values, kindCollection)
		}
		if pages.Fields.GetByName("list_collection") == nil {
			pages.Fields.Add(&core.TextField{Name: "list_collection"}) // collection id
		}
		return app.Save(pages)
	}, func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return nil
		}
		if _, err := app.DB().NewQuery("UPDATE {{" + Collection + "}} SET [[kind]] = '' WHERE [[kind]] = 'collection'").Execute(); err != nil {
			return err
		}
		if f, ok := pages.Fields.GetByName("kind").(*core.SelectField); ok {
			f.Values = slices.DeleteFunc(f.Values, func(v string) bool { return v == kindCollection })
		}
		pages.Fields.RemoveByName("list_collection")
		return app.Save(pages)
	}, typesMigrationFile)
}

// validateType checks the collection of a collection page.
func validateType(app core.App, rec *core.Record, errs validation.Errors) {
	if rec.GetString("kind") != kindCollection {
		rec.Set("list_collection", "")
		return
	}
	if rec.GetString("record_collection") != "" {
		errs["record_collection"] = validation.NewError("validation_collection_record", "A collection page lists records: it can't be a single item page.")
	}
	id := rec.GetString("list_collection")
	if c, err := app.FindCachedCollectionByNameOrId(id); err != nil || id == "" {
		errs["list_collection"] = validation.NewError("validation_collection_required", "Choose the collection the page lists.")
	} else {
		rec.Set("list_collection", c.Id)
	}
}
