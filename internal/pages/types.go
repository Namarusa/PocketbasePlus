package pages

import (
	"slices"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The types of pages: a single page (kind ""), a collection page (kind
// "collection": a page listing the records of list_collection, served like a
// single page), a single item page (record_collection: one record per
// address, /slug/{id}), a site part (kind "part") and an error page (kind
// "error": shown instead of the default page of its error_code, errors.go).
const (
	typesMigrationFile  = "1762600000_pages_types.go"
	errorsMigrationFile = "1762700000_pages_errors.go"
	kindCollection      = "collection"
	kindError           = "error"
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

func init() {
	m.Register(func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		if f, ok := pages.Fields.GetByName("kind").(*core.SelectField); ok && !slices.Contains(f.Values, kindError) {
			f.Values = append(f.Values, kindError)
		}
		if pages.Fields.GetByName("error_code") == nil {
			pages.Fields.Add(&core.SelectField{Name: "error_code", MaxSelect: 1, Values: errorCodes})
		}
		return app.Save(pages)
	}, func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return nil
		}
		if _, err := app.DB().NewQuery("DELETE FROM {{" + Collection + "}} WHERE [[kind]] = 'error'").Execute(); err != nil {
			return err
		}
		if f, ok := pages.Fields.GetByName("kind").(*core.SelectField); ok {
			f.Values = slices.DeleteFunc(f.Values, func(v string) bool { return v == kindError })
		}
		pages.Fields.RemoveByName("error_code")
		return app.Save(pages)
	}, errorsMigrationFile)
}

// validateType checks the collection of a collection page and the code of
// an error page (one page per code).
func validateType(app core.App, rec *core.Record, errs validation.Errors) {
	if rec.GetString("kind") == kindError {
		code := rec.GetString("error_code")
		if !slices.Contains(errorCodes, code) {
			errs["error_code"] = validation.NewError("validation_error_code", "Choose the error the page is shown for.")
		} else if other, err := app.FindFirstRecordByFilter(Collection, "kind = 'error' && error_code = {:code} && id != {:id}", dbx.Params{"code": code, "id": rec.Id}); err == nil {
			errs["error_code"] = validation.NewError("validation_error_taken", "The "+code+" page is already “"+other.GetString("title")+"”: edit it, or delete it first.")
		}
		// shown to everyone, at any address
		rec.Set("home", false)
		rec.Set("access", accessPublic)
		rec.Set("record_collection", "")
	} else {
		rec.Set("error_code", "")
	}
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
