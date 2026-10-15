package pages

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// Versions: every save that changes the design of a page keeps a copy (the
// last maxVersions per page), shown in the History of the builder.
const (
	// VersionsCollection holds the saved versions of the pages.
	VersionsCollection    = "pages_versions"
	versionsMigrationFile = "1761100000_pages_versions.go"
	maxVersions           = 30

	// kindPart marks the site parts: pieces of pages (header, footer, sidebar)
	// placed on several pages, never served on their own.
	kindPart = "part"
)

func init() {
	m.Register(func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		pages.Fields.Add(&core.SelectField{Name: "kind", MaxSelect: 1, Values: []string{kindPart}})
		if err := app.Save(pages); err != nil {
			return err
		}

		versions := core.NewBaseCollection(VersionsCollection)
		versions.Fields.Add(
			&core.RelationField{Name: "page", CollectionId: pages.Id, MaxSelect: 1, Required: true, CascadeDelete: true},
			&core.TextField{Name: "title", Max: 200},
			&core.TextField{Name: "html", Max: 2_000_000},
			&core.TextField{Name: "css", Max: 1_000_000},
			&core.JSONField{Name: "project", MaxSize: 10 << 20},
			&core.TextField{Name: "kit", Max: 20},
			&core.TextField{Name: "by", Max: 255}, // email of the superuser who saved
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		versions.AddIndex("idx_pages_versions_page", false, "page", "")
		if err := app.Save(versions); err != nil {
			return err
		}
		// the current state of the existing pages is their first version
		list, err := app.FindAllRecords(Collection)
		if err != nil {
			return err
		}
		for _, p := range list {
			if err := snapshot(app, p, ""); err != nil {
				return err
			}
		}
		return markSystem(app, true, VersionsCollection)
	}, func(app core.App) error {
		if err := markSystem(app, false, VersionsCollection); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId(VersionsCollection); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return nil
		}
		pages.Fields.RemoveByName("kind")
		return app.Save(pages)
	}, versionsMigrationFile)
}

func bindVersions(app core.App) {
	save := func(e *core.RecordRequestEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		by := ""
		if e.Auth != nil {
			by = e.Auth.Email()
		}
		if err := snapshot(e.App, e.Record, by); err != nil {
			e.App.Logger().Warn("pages: the version couldn't be saved", "page", e.Record.Id, "error", err)
		}
		return nil
	}
	app.OnRecordCreateRequest(Collection).BindFunc(save)
	app.OnRecordUpdateRequest(Collection).BindFunc(save)
}

// snapshot stores the design of a page as a new version, unless it's the same
// as the latest one, and deletes the versions beyond maxVersions.
func snapshot(app core.App, page *core.Record, by string) error {
	html, css := page.GetString("html"), page.GetString("css")
	latest, err := app.FindRecordsByFilter(VersionsCollection, "page = {:page}", "-@rowid", 1, 0, dbx.Params{"page": page.Id})
	if err != nil {
		return err
	}
	if len(latest) > 0 && latest[0].GetString("html") == html && latest[0].GetString("css") == css {
		return nil
	}
	col, err := app.FindCachedCollectionByNameOrId(VersionsCollection)
	if err != nil {
		return err
	}
	v := core.NewRecord(col)
	v.Load(map[string]any{
		"page": page.Id, "title": page.GetString("title"), "html": html, "css": css,
		"project": page.Get("project"), "kit": page.GetString("kit"), "by": by,
	})
	if err := app.Save(v); err != nil {
		return err
	}
	old, err := app.FindRecordsByFilter(VersionsCollection, "page = {:page}", "-@rowid", -1, maxVersions, dbx.Params{"page": page.Id})
	if err != nil {
		return err
	}
	for _, o := range old {
		if err := app.Delete(o); err != nil {
			return err
		}
	}
	return nil
}

// withVersion returns a copy of the page showing one of its versions.
func withVersion(app core.App, page *core.Record, versionID string) (*core.Record, error) {
	v, err := app.FindRecordById(VersionsCollection, versionID)
	if err != nil || v.GetString("page") != page.Id {
		return nil, fmt.Errorf("unknown version")
	}
	p := page.Clone()
	for _, k := range []string{"html", "css", "kit"} {
		p.Set(k, v.GetString(k))
	}
	return p, nil
}

// --- duplicate ----------------------------------------------------------------------------

// duplicatePage copies a page (unpublished) with its uploaded files.
func duplicatePage(e *core.RequestEvent) error {
	src, err := e.App.FindRecordById(Collection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("", nil)
	}
	dst := core.NewRecord(src.Collection())
	for _, k := range []string{"description", "record_collection", "list_collection", "record_filter", "record_rules", "access", "roles", "kit", "kind", "html", "css", "project"} {
		dst.Set(k, src.Get(k))
	}
	title := src.GetString("title") + " (copy)"
	if len(title) > 200 {
		title = title[:200]
	}
	dst.Set("title", title)
	dst.Set("slug", freeSlug(e.App, src.GetString("slug")+"-copy"))

	fsys, err := e.App.NewFilesystem()
	if err != nil {
		return e.InternalServerError("", err)
	}
	defer fsys.Close()
	names := src.GetStringSlice("assets")
	files := make([]*filesystem.File, 0, len(names))
	for _, name := range names {
		r, err := fsys.GetReader(src.BaseFilesPath() + "/" + name)
		if err != nil {
			continue // missing file: skipped
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return e.InternalServerError("", err)
		}
		f, err := filesystem.NewFileFromBytes(data, name)
		if err != nil {
			return e.InternalServerError("", err)
		}
		files = append(files, f)
	}
	dst.Set("assets", files)

	err = e.App.RunInTransaction(func(tx core.App) error {
		if err := tx.Save(dst); err != nil {
			return err
		}
		// the copied files have new names: the design points to them
		pairs := []string{}
		for _, f := range files {
			pairs = append(pairs, "/p/_assets/"+src.Id+"/"+f.OriginalName, "/p/_assets/"+dst.Id+"/"+f.Name)
		}
		if len(pairs) == 0 {
			return snapshot(tx, dst, authEmail(e))
		}
		r := strings.NewReplacer(pairs...)
		dst.Set("html", r.Replace(dst.GetString("html")))
		dst.Set("css", r.Replace(dst.GetString("css")))
		dst.Set("project", r.Replace(dst.GetString("project")))
		if err := tx.Save(dst); err != nil {
			return err
		}
		return snapshot(tx, dst, authEmail(e))
	})
	if err != nil {
		return e.BadRequestError("The page couldn't be copied.", err)
	}
	return e.JSON(http.StatusOK, map[string]string{"id": dst.Id})
}

func authEmail(e *core.RequestEvent) string {
	if e.Auth != nil {
		return e.Auth.Email()
	}
	return ""
}

// freeSlug returns slug, or slug-2, slug-3... the first one not used.
func freeSlug(app core.App, slug string) string {
	if len(slug) > 90 {
		slug = slug[:90]
	}
	s := slug
	for n := 2; ; n++ {
		if _, err := app.FindFirstRecordByData(Collection, "slug", s); err != nil {
			return s
		}
		s = fmt.Sprintf("%s-%d", slug, n)
	}
}
