// Package media is the optional Assets module: a library of files (images,
// videos, PDFs, fonts, documents) shared by the whole app, in the Assets menu
// of the dashboard. Each file has a stable public address, /media/{id}/{name}
// (replacing the file keeps it), and the pages, documents and emails pick
// their files from it.
//
// It is removable: run "pocketbaseplus media uninstall --yes" (deletes the
// library), then remove media.Register from main.go and this folder. The
// links to its files then answer 404.
package media

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/medialib"
)

// ExtName is the dashboard extension of the module (/_/extensions/{ExtName}/).
const ExtName = "pocketbaseplus-media"

// widths are the resized copies of the images (?w=640), for the srcset of
// the pages and lighter emails.
var widths = []int{160, 320, 640, 960, 1280, 1920}

//go:embed all:ext
var extFS embed.FS

// Register adds the module: hooks, routes, the dashboard pages and the
// "media uninstall" command.
func Register(app *pocketbase.PocketBase) {
	app.RootCmd.AddCommand(command(app))
	Bind(app)
}

// Bind adds the hooks and routes of the module.
func Bind(app core.App) {
	internalcols.Add(medialib.Collection)
	bindAssistant()

	files, err := fs.Sub(extFS, "ext")
	if err != nil {
		panic(err)
	}

	app.OnRecordValidate(medialib.Collection).BindFunc(func(e *core.RecordEvent) error {
		if err := prepare(e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.UIExtensions = append(se.UIExtensions, core.UIExtension{Name: ExtName, FS: files})
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			if strings.HasPrefix(e.Request.URL.Path, "/_/extensions/"+ExtName+"/") {
				e.Response.Header().Set("Cache-Control", "no-cache") // revalidated after upgrades
			}
			return e.Next()
		})
		se.Router.GET(medialib.Prefix+"{id}/{name...}", serve)
		se.Router.HEAD(medialib.Prefix+"{id}/{name...}", serve)
		g := se.Router.Group("/api/media")
		g.Bind(apis.RequireSuperuserAuth())
		g.GET("/usages/{id}", func(e *core.RequestEvent) error {
			list, err := Usages(e.App, e.Request.PathValue("id"))
			if err != nil {
				return e.BadRequestError(err.Error(), nil)
			}
			return e.JSON(http.StatusOK, list)
		})
		return se.Next()
	})
}

// prepare fills the name and the details of the file (type, size, image size).
func prepare(rec *core.Record) error {
	if files := rec.GetUnsavedFiles("file"); len(files) > 0 {
		m := fileMeta(files[0])
		rec.Set("mime", m.mime)
		rec.Set("kind", m.kind)
		rec.Set("size", m.size)
		rec.Set("width", m.width)
		rec.Set("height", m.height)
		if strings.TrimSpace(rec.GetString("name")) == "" {
			rec.Set("name", baseName(files[0].OriginalName))
		}
	}
	rec.Set("name", strings.Join(strings.Fields(rec.GetString("name")), " "))
	rec.Set("folder", strings.Join(strings.Fields(rec.GetString("folder")), " "))
	rec.Set("alt", strings.TrimSpace(rec.GetString("alt")))
	if rec.GetString("name") == "" && rec.GetString("file") != "" {
		rec.Set("name", "file")
	}
	if k := rec.GetString("kind"); k != "" && !slices.Contains(kinds, k) {
		return validation.Errors{"kind": validation.NewError("invalid_kind", "Unknown kind of file.")}
	}
	return nil
}

// --- the public files ---------------------------------------------------------------------

var thumbLocks sync.Map // thumb key → *sync.Mutex

// serve sends a file of the library (or a narrower copy of an image: ?w=640).
func serve(e *core.RequestEvent) error {
	rec, err := medialib.Find(e.App, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("", nil)
	}
	file := rec.GetString("file")
	if file == "" {
		return e.NotFoundError("", nil)
	}
	fsys, err := e.App.NewFilesystem()
	if err != nil {
		return e.InternalServerError("", err)
	}
	defer fsys.Close()
	key := rec.BaseFilesPath() + "/" + file
	served := key
	if w, _ := strconv.Atoi(e.Request.URL.Query().Get("w")); w > 0 && resizable(rec) && w < rec.GetInt("width") {
		// the next width of the list (a few copies per image)
		i := slices.IndexFunc(widths, func(x int) bool { return x >= w })
		if i >= 0 && widths[i] < rec.GetInt("width") {
			size := strconv.Itoa(widths[i]) + "x0"
			thumb := rec.BaseFilesPath() + "/thumbs_" + file + "/" + size + "_" + file
			if ok, _ := fsys.Exists(thumb); !ok {
				l, _ := thumbLocks.LoadOrStore(thumb, &sync.Mutex{})
				mu := l.(*sync.Mutex)
				mu.Lock()
				if ok, _ := fsys.Exists(thumb); !ok {
					err = fsys.CreateThumb(key, thumb, size)
				}
				mu.Unlock()
				thumbLocks.Delete(thumb)
			}
			if err == nil {
				served = thumb
			} else {
				e.App.Logger().Warn("Assets: the image couldn't be resized", "file", rec.GetString("name"), "error", err)
			}
		}
	}
	h := e.Response.Header()
	// a file of the library never runs in the app's pages (an SVG opened alone)
	if rec.GetString("kind") == "pdf" {
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'")
	} else {
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	// the address stays when the file is replaced: short cache, revalidated
	h.Set("Cache-Control", "public, max-age=300, stale-while-revalidate=86400")
	h.Set("ETag", strconv.Quote(path.Base(served)))
	h.Set("Access-Control-Allow-Origin", "*") // fonts and PDFs used by other sites
	if e.Request.URL.Query().Get("download") == "1" {
		h.Set("Content-Disposition", "attachment; filename="+strconv.Quote(linkName(rec.GetString("name"), file)))
	}
	return fsys.Serve(e.Response, e.Request, served, linkName(rec.GetString("name"), file))
}

// resizable tells the images PocketBase can resize.
func resizable(rec *core.Record) bool {
	switch rec.GetString("mime") {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// --- where the files are used -------------------------------------------------------------

// Usage is a record linking to a file of the library.
type Usage struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
	Label      string `json:"label"`
	Field      string `json:"field"`
}

// collections whose content isn't a use of a file (logs, history)
var notUses = map[string]bool{
	medialib.Collection: true, "email_messages": true, "flow_runs": true, "revisions": true, "notifications": true,
	"ai_conversations": true, "pages_versions": true, "documents": true,
}

const maxUsages = 50

// Usages lists the records whose texts link to a file of the library: the
// pages, documents and email templates, and the text, rich text, URL and
// JSON fields of the app's collections.
func Usages(app core.App, id string) ([]Usage, error) {
	if _, ok := medialib.ID(app, medialib.Prefix+id+"/"); !ok {
		return nil, fmt.Errorf("invalid id")
	}
	needle := "%" + medialib.Prefix + id + "/%"
	cols, err := app.FindAllCollections()
	if err != nil {
		return nil, err
	}
	out := []Usage{}
	for _, c := range cols {
		if c.IsView() || strings.HasPrefix(c.Name, "_") || notUses[c.Name] {
			continue
		}
		var fields []string
		for _, f := range c.Fields {
			switch f.Type() {
			case core.FieldTypeText, core.FieldTypeEditor, core.FieldTypeURL, core.FieldTypeJSON:
				fields = append(fields, f.GetName())
			}
		}
		if len(fields) == 0 {
			continue
		}
		var exps []dbx.Expression
		for _, f := range fields {
			exps = append(exps, dbx.NewExp("[["+f+"]] LIKE {:needle}", dbx.Params{"needle": needle}))
		}
		var recs []*core.Record
		if err := app.RecordQuery(c).AndWhere(dbx.Or(exps...)).Limit(maxUsages).All(&recs); err != nil {
			app.Logger().Warn("Assets: usages", "collection", c.Name, "error", err)
			continue
		}
		for _, r := range recs {
			u := Usage{Collection: c.Name, ID: r.Id, Label: label(r)}
			for _, f := range fields {
				if strings.Contains(r.GetString(f), medialib.Prefix+id+"/") {
					u.Field = f
					break
				}
			}
			out = append(out, u)
			if len(out) >= maxUsages {
				return out, nil
			}
		}
	}
	return out, nil
}

func label(r *core.Record) string {
	for _, f := range []string{"name", "title", "subject", "slug", "label"} {
		if v := strings.TrimSpace(r.GetString(f)); v != "" {
			return v
		}
	}
	return r.Id
}

// --- uninstall ----------------------------------------------------------------------------

func command(app *pocketbase.PocketBase) *cobra.Command {
	cmd := &cobra.Command{Use: "media", Short: "Assets module (the library of files)"}
	var yes bool
	uninstall := &cobra.Command{
		Use:          "uninstall",
		SilenceUsage: true,
		Short:        "Deletes the library and its files (before removing the module from main.go)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := app.FindCollectionByNameOrId(medialib.Collection); err != nil {
				fmt.Println("The library doesn't exist: nothing to uninstall.")
				return nil
			}
			n, _ := app.CountRecords(medialib.Collection)
			if !yes {
				fmt.Printf("This deletes the %d files of the Assets library (the pages, documents and emails linking to them lose them).\n"+
					"Back up pb_data first, then run again with --yes.\n", n)
				return nil
			}
			err := app.RunInTransaction(func(tx core.App) error {
				if err := dropCollection(tx); err != nil {
					return err
				}
				_, err := tx.DB().Delete("_migrations", dbx.HashExp{"file": migrationFile}).Execute()
				return err
			})
			if err != nil {
				return err
			}
			fmt.Printf("Deleted the %d files. Now remove media.Register(app) from main.go and the internal/media folder.\n", n)
			return nil
		},
	}
	uninstall.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	cmd.AddCommand(uninstall)
	return cmd
}
