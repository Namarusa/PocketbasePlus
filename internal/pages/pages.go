// Package pages is an optional module: a visual page builder (GrapesJS) in
// the dashboard, whose blocks are bound to collections (record lists, fields,
// record pages, forms), and the public pages it publishes, rendered by the
// server at / (home page), /{slug} and /{slug}/{id} (record pages). The old
// /p/{slug} addresses redirect there; the pages' files stay under /p/_…
//
// Everything lives in this package: the collection migration, the routes, the
// dashboard extension (its own UI extension) and the vendored editor. To remove
// the feature, run "pocketbaseplus pages uninstall" (deletes the pages) and remove
// the pages.Register line from main.go.
package pages

import (
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	"github.com/spf13/cobra"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
	"github.com/Namarusa/PocketbasePlus/internal/offline"
	"github.com/Namarusa/PocketbasePlus/internal/session"
)

const (
	// Collection stores the pages.
	Collection = "pages"
	// ExtName is the dashboard extension of the module (/_/extensions/pocketbaseplus-pages/).
	ExtName = "pocketbaseplus-pages"

	previewTTL = time.Hour
)

//go:embed all:ext
var extFS embed.FS

// Register adds the Pages module: dashboard tab, public pages and the
// "pages uninstall" command.
func Register(app *pocketbase.PocketBase) {
	app.RootCmd.AddCommand(command(app))
	Bind(app)
}

// Bind adds the hooks and routes of the module.
func Bind(app core.App) {
	internalcols.Add(Collection, SettingsCollection, VersionsCollection)

	files, err := fs.Sub(extFS, "ext")
	if err != nil {
		panic(err)
	}

	app.OnRecordValidate(Collection).BindFunc(func(e *core.RecordEvent) error {
		if err := validate(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordValidate(SettingsCollection).BindFunc(func(e *core.RecordEvent) error {
		if err := validateSettings(e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	bindVersions(app)
	bindLoginPage(app)
	bindAssistant()
	app.OnRecordAfterCreateSuccess(Collection).BindFunc(singleHome)
	app.OnRecordAfterUpdateSuccess(Collection).BindFunc(singleHome)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.UIExtensions = append(se.UIExtensions, core.UIExtension{Name: ExtName, FS: files})
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			// revalidate the extension files after upgrades (the editor is versioned in its path)
			p := e.Request.URL.Path
			if strings.HasPrefix(p, "/_/extensions/"+ExtName+"/") && !strings.Contains(p, "/vendor/") {
				e.Response.Header().Set("Cache-Control", "no-cache")
			}
			return e.Next()
		})

		se.Router.GET("/{$}", serveHome)
		// the pages at the root: one catch-all, so the routes of PocketBase and
		// of the other modules (more specific) always win
		rootPages = !se.Router.HasRoute(http.MethodGet, "/{path...}")
		if rootPages {
			se.Router.GET("/{path...}", servePath)
		} else {
			se.App.Logger().Warn("Pages: another route serves /{path...} (pb_public?): the pages are served at /p/{slug}")
		}
		se.Router.GET("/p/{slug}", serveLegacy)
		se.Router.GET("/p/{slug}/{id}", serveLegacy)
		se.Router.GET("/p/_assets/{page}/{file}", serveAsset)
		se.Router.GET("/p/_static/{file}", serveStatic)
		se.Router.GET("/p/_theme.css", serveTheme)
		se.Router.POST("/api/pages/{id}/preview", previewLink).Bind(apis.RequireSuperuserAuth())
		se.Router.POST("/api/pages/{id}/duplicate", duplicatePage).Bind(apis.RequireSuperuserAuth())
		se.Router.POST("/api/pages/{id}/snippet", snippet).Bind(apis.RequireSuperuserAuth())
		se.Router.POST("/api/pages/{id}/formula", formulaPreview).Bind(apis.RequireSuperuserAuth())
		se.Router.POST("/api/pages/{id}/conditions", conditionsPreview).Bind(apis.RequireSuperuserAuth())
		se.Router.GET("/api/pages/chart-palettes", chartPalettesRoute).Bind(apis.RequireSuperuserAuth())
		return se.Next()
	})
}

// --- validation -------------------------------------------------------------------------

func validate(app core.App, rec *core.Record) error {
	errs := validation.Errors{}
	var recordCol *core.Collection
	if id := rec.GetString("record_collection"); id != "" {
		c, err := listCollection(app, id)
		if err != nil {
			errs["record_collection"] = validation.NewError("validation_invalid_collection", "Unknown collection.")
		} else {
			recordCol = c
			rec.Set("record_collection", c.Id)
		}
	}
	if f := strings.TrimSpace(rec.GetString("record_filter")); f != "" && recordCol != nil {
		if err := checkFilter(app, recordCol, f, ""); err != nil {
			errs["record_filter"] = validation.NewError("validation_invalid_filter", err.Error())
		}
	}
	if reservedSlugs[rec.GetString("slug")] && (rec.IsNew() || rec.Original().GetString("slug") != rec.GetString("slug")) {
		errs["slug"] = validation.NewError("validation_slug_reserved", "This address is used by the app: choose another slug.")
	}
	if rec.GetString("kind") == kindPart {
		// parts are shown inside other pages only
		if rec.GetBool("published") {
			errs["published"] = validation.NewError("validation_part_published", "A site part can't be published on its own: place it on pages.")
		}
		if rec.GetBool("home") {
			errs["home"] = validation.NewError("validation_part_home", "A site part can't be the home page.")
		}
		if rec.GetString("record_collection") != "" {
			errs["record_collection"] = validation.NewError("validation_part_record", "A site part can't be a record page.")
		}
	}
	validateType(app, rec, errs)
	validateAccess(app, rec, errs)
	if err := checkBindings(app, rec.GetString("html")); err != nil {
		errs["html"] = validation.NewError("validation_invalid_bindings", err.Error())
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// checkFilter validates a filter (its placeholders bound to empty values).
func checkFilter(app core.App, col *core.Collection, filter, sort string) error {
	bound, params := bindPlaceholders(filter, scope{}, nil, nil)
	resolver := core.NewRecordFieldResolver(app, col, nil, true)
	if strings.TrimSpace(bound) != "" {
		if _, err := search.FilterData(bound).BuildExpr(resolver, params); err != nil {
			return fmt.Errorf("invalid filter %q: %w", filter, err)
		}
	}
	for _, sf := range sortFields(sort) {
		if _, err := sf.BuildExpr(resolver); err != nil {
			return fmt.Errorf("invalid sort %q: %w", sort, err)
		}
	}
	return nil
}

// checkBindings validates the lists of the page HTML (collections, filters, sorts).
func checkBindings(app core.App, src string) error {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	body, err := parseBody(src)
	if err != nil {
		return err
	}
	var errs []string
	var visit func(n *htmlNode)
	visit = func(n *htmlNode) {
		if getAttr(n, attrChart) != "" {
			if err := checkChart(app, n); err != nil {
				errs = append(errs, "chart: "+err.Error())
			}
		}
		if name := getAttr(n, attrList); name != "" {
			col, err := listCollection(app, name)
			if err != nil {
				errs = append(errs, "list: "+err.Error())
			} else if err := checkFilter(app, col, getAttr(n, attrFilter), getAttr(n, attrSort)); err != nil {
				errs = append(errs, "list of "+col.Name+": "+err.Error())
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(body)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// singleHome keeps one home page: saving a home page unsets the others.
func singleHome(e *core.RecordEvent) error {
	if e.Record.GetBool("home") {
		_, err := e.App.DB().Update(Collection, dbx.Params{"home": false},
			dbx.And(dbx.HashExp{"home": true}, dbx.Not(dbx.HashExp{"id": e.Record.Id}))).Execute()
		if err != nil {
			return err
		}
	}
	return e.Next()
}

// --- public pages -----------------------------------------------------------------------

// pageCSP returns the policy of the public pages: no scripts, except the
// embedded Basecoat script for pages with interactive components and the mode
// script of the pages with the kit (scripts in the page HTML are always removed). Styles, images, fonts and embeds may come
// from https sites (web fonts, remote images), unless the offline mode is on.
func pageCSP(script, kit bool) string {
	remote := " https:"
	if offline.Enabled() {
		remote = ""
	}
	var scripts []string
	if script {
		scripts = append(scripts, "'self'")
	}
	if kit {
		scripts = append(scripts, kitScriptHash)
	}
	if scripts == nil {
		scripts = []string{"'none'"}
	}
	return "default-src 'self'; script-src " + strings.Join(scripts, " ") + "; style-src 'self' 'unsafe-inline'" + remote + "; " +
		"img-src 'self' data: blob:" + remote + "; font-src 'self' data:" + remote + "; media-src 'self'" + remote + "; " +
		"frame-src 'self'" + remote + "; connect-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'self'"
}

func serveHome(e *core.RequestEvent) error {
	rec, err := e.App.FindFirstRecordByFilter(Collection, "home = true && published = true && kind != 'part'")
	if err != nil {
		return notFound(e)
	}
	v := visitorOf(e)
	if !v.canView(rec) {
		return denied(e, rec, v)
	}
	return respond(e, rec, nil, false, v)
}

// reservedSlugs are the first path segments of the app's own routes: no page
// takes them (a module serving a new root path adds it here).
var reservedSlugs = map[string]bool{"api": true, "p": true, "f": true, "dashboards": true, "email": true, "hooks": true}

// rootPages is false when another route serves the root paths (pb_public):
// the pages are then served at /p/{slug}.
var rootPages = true

// pageURL is the address of the page with this slug (and record id).
func pageURL(slug, id string) string {
	u := "/" + url.PathEscape(slug)
	if !rootPages || reservedSlugs[slug] {
		u = "/p" + u
	}
	if id != "" {
		u += "/" + url.PathEscape(id)
	}
	return u
}

// servePath serves /{slug} and /{slug}/{id}.
func servePath(e *core.RequestEvent) error {
	p := e.Request.PathValue("path")
	if first, _, _ := strings.Cut(p, "/"); first == "api" || first == "_" {
		return e.NotFoundError("", nil) // an unknown API route: PocketBase's answer
	}
	segs := strings.Split(p, "/")
	if len(segs) > 2 || slices.Contains(segs, "") || reservedSlugs[segs[0]] {
		return notFound(e)
	}
	e.Request.SetPathValue("slug", segs[0])
	if len(segs) == 2 {
		e.Request.SetPathValue("id", segs[1])
	}
	return servePage(e)
}

// serveLegacy serves the old /p/{slug} addresses: a redirect to the page's
// address (kept in links, flows and older versions of the pages).
func serveLegacy(e *core.RequestEvent) error {
	slug := e.Request.PathValue("slug")
	if !rootPages || reservedSlugs[slug] {
		return servePage(e)
	}
	u := pageURL(slug, e.Request.PathValue("id"))
	if q := e.Request.URL.RawQuery; q != "" {
		u += "?" + q
	}
	return e.Redirect(http.StatusMovedPermanently, u)
}

func servePage(e *core.RequestEvent) error {
	rec, err := e.App.FindFirstRecordByData(Collection, "slug", e.Request.PathValue("slug"))
	if err != nil || rec.GetString("kind") == kindPart {
		return notFound(e)
	}
	q := e.Request.URL.Query()
	version, as := q.Get("version"), q.Get("as")
	preview := validAs(as) && validPreview(e.App, rec, q.Get("preview"), previewScope(version, as))
	if !rec.GetBool("published") && !preview || rec.GetString("kind") == kindError && !preview {
		return notFound(e) // an error page is shown instead of other pages, not at its own address
	}
	if version != "" {
		if !preview {
			return notFound(e)
		}
		if rec, err = withVersion(e.App, rec, version); err != nil {
			return notFound(e)
		}
	}

	v := visitorOf(e)
	if preview {
		v = previewVisitor(e.App, as)
		if !v.canView(rec) {
			return deniedPage(e, http.StatusOK, "Preview as "+as, "This visitor can't open the page: guests are sent to the sign in page, the others see \"No access\".", "")
		}
	} else if !v.canView(rec) {
		return denied(e, rec, v)
	}

	id := e.Request.PathValue("id")
	if rec.GetBool("home") && id == "" && !preview {
		// one address for the home page
		u := "/"
		if q := e.Request.URL.RawQuery; q != "" {
			u += "?" + q
		}
		return e.Redirect(http.StatusMovedPermanently, u)
	}
	colID := rec.GetString("record_collection")
	if (id == "") != (colID == "") {
		return notFound(e) // a record page needs a record, the other pages don't take one
	}
	var record *core.Record
	if id != "" {
		record, err = pageRecord(e.App, rec, id, v.user)
		if err != nil {
			return notFound(e)
		}
		if !v.canViewRecord(e.App, rec, record) {
			if v.user == nil && !preview {
				return denied(e, rec, v) // maybe theirs once signed in
			}
			return notFound(e)
		}
	}
	return respond(e, rec, record, preview, v)
}

// pageRecord returns the record shown by a record page (matching its filter).
func pageRecord(app core.App, page *core.Record, id string, auth *core.Record) (*core.Record, error) {
	col, err := listCollection(app, page.GetString("record_collection"))
	if err != nil {
		return nil, err
	}
	filter := "id = {:id}"
	if f := strings.TrimSpace(page.GetString("record_filter")); f != "" {
		filter += " && (" + f + ")"
	}
	bound, params := bindPlaceholders(filter, scope{}, nil, auth)
	params["id"] = id
	return app.FindFirstRecordByFilter(col, bound, params)
}

func respond(e *core.RequestEvent, page, record *core.Record, preview bool, v visitor) error {
	view := pageView{
		Visitor:     v,
		Path:        e.Request.URL.RequestURI(),
		Flash:       session.TakeFlash(e),
		Title:       page.GetString("title"),
		Description: page.GetString("description"),
		HTML:        page.GetString("html"),
		CSS:         page.GetString("css"),
	}
	if page.GetString("kit") == kitBasecoat {
		t := loadTheme(e.App)
		view.Kit = &t
	}
	if record != nil {
		view.Scope = scope{col: record.Collection(), rec: record, rules: page.GetBool("record_rules") && !v.all}
		if view.Scope.rules {
			enrich(e.App, v.requestInfo(), []*core.Record{record})
		}
		if name := labels.Of(record, labels.Fields(record.Collection(), false)); name != record.Id {
			view.Title = name + " · " + view.Title
		}
	}
	if page.GetString("kind") == kindError {
		// the preview of an error page: its error, at a sample address
		code, _ := strconv.Atoi(page.GetString("error_code"))
		view.Error = errorInfo(e.App, code, "/an/address")
	}
	out, script, err := render(e.Request.Context(), e.App, view, e.Request.URL.Query(), preview)
	if err != nil {
		e.App.Logger().Error("Page rendering failed", "page", page.GetString("slug"), "error", err)
		return errorPage(e, http.StatusInternalServerError)
	}
	h := e.Response.Header()
	h.Set("Content-Security-Policy", pageCSP(script, view.Kit != nil))
	h.Set("Cache-Control", "private, no-cache") // the page may show the visitor
	h.Set("Vary", "Cookie")
	h.Set("X-Content-Type-Options", "nosniff")
	return e.HTML(http.StatusOK, out)
}

func notFound(e *core.RequestEvent) error {
	return errorPage(e, http.StatusNotFound)
}

// serveAsset serves a file uploaded in the editor (images of the page design).
// It's sandboxed: SVG files can't run scripts on the site.
func serveAsset(e *core.RequestEvent) error {
	rec, err := e.App.FindRecordById(Collection, e.Request.PathValue("page"))
	name := e.Request.PathValue("file")
	if err != nil || !slices.Contains(rec.GetStringSlice("assets"), name) || !assetVisible(e, rec) {
		return e.NotFoundError("", nil)
	}
	fsys, err := e.App.NewFilesystem()
	if err != nil {
		return e.InternalServerError("", err)
	}
	defer fsys.Close()
	h := e.Response.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "max-age=2592000")
	return fsys.Serve(e.Response, e.Request, rec.BaseFilesPath()+"/"+name, name)
}

// assetVisible reports whether the images of a page can be served: published
// pages and site parts (shown on the pages), drafts only to their previews
// (the preview link is the referrer of its images) and the page builder.
func assetVisible(e *core.RequestEvent, page *core.Record) bool {
	if page.GetBool("published") || page.GetString("kind") == kindPart {
		return true
	}
	if e.HasSuperuserAuth() {
		return true
	}
	ref, err := url.Parse(e.Request.Referer())
	if err != nil || ref.Host != e.Request.Host {
		return false
	}
	if ref.Path == "/_/" || strings.HasPrefix(ref.Path, "/_/") {
		return true // the page builder (dashboard)
	}
	q := ref.Query()
	return validPreview(e.App, page, q.Get("preview"), previewScope(q.Get("version"), q.Get("as")))
}

// --- previews ---------------------------------------------------------------------------

func previewKey(app core.App) []byte {
	base := ""
	if c, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers); err == nil {
		base = c.AuthToken.Secret
	}
	m := hmac.New(sha256.New, []byte(base))
	// the project's former name, kept so the links already sent stay valid
	m.Write([]byte("gopocket-pages-preview"))
	return m.Sum(nil)
}

// previewScope is what a preview token covers besides the page: a version
// and the visitor it shows the page to.
func previewScope(version, as string) string {
	if as != "" {
		return version + "|as=" + as
	}
	return version
}

// previewSig signs a preview of a page (of one of its versions when set).
func previewSig(app core.App, pageID, version, exp string) string {
	m := hmac.New(sha256.New, previewKey(app))
	if version != "" {
		pageID += ":" + version
	}
	m.Write([]byte(pageID + "." + exp))
	return hex.EncodeToString(m.Sum(nil))
}

func validPreview(app core.App, page *core.Record, token, version string) bool {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > ts {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(previewSig(app, page.Id, version, exp)))
}

// previewLink returns a link showing the saved page (published or not), or one
// of its versions (?version=id), for an hour. Record pages show their first
// record, or the requested one.
func previewLink(e *core.RequestEvent) error {
	page, err := e.App.FindRecordById(Collection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("", nil)
	}
	if page.GetString("kind") == kindPart {
		return e.BadRequestError("A site part is previewed on the pages using it.", nil)
	}
	version, as := e.Request.URL.Query().Get("version"), e.Request.URL.Query().Get("as")
	if !validAs(as) {
		return e.BadRequestError("Unknown visitor.", nil)
	}
	if version != "" {
		if _, err := withVersion(e.App, page, version); err != nil {
			return e.NotFoundError("", nil)
		}
	}
	path := pageURL(page.GetString("slug"), "")
	if colID := page.GetString("record_collection"); colID != "" {
		col, err := listCollection(e.App, colID)
		if err != nil {
			return e.BadRequestError(err.Error(), nil)
		}
		id := e.Request.URL.Query().Get("record")
		if id == "" {
			filter, params := bindPlaceholders(page.GetString("record_filter"), scope{}, nil, previewVisitor(e.App, as).user)
			if strings.TrimSpace(filter) == "" {
				filter = "id != ''"
			}
			first, err := e.App.FindRecordsByFilter(col, filter, "-created", 1, 0, params)
			if err != nil || len(first) == 0 {
				return e.BadRequestError("No record of "+col.Name+" matches the page to preview it.", nil)
			}
			id = first[0].Id
		}
		path += "/" + id
	}
	exp := strconv.FormatInt(time.Now().Add(previewTTL).Unix(), 10)
	link := path + "?preview=" + exp + "." + previewSig(e.App, page.Id, previewScope(version, as), exp)
	if version != "" {
		link += "&version=" + url.QueryEscape(version)
	}
	if as != "" {
		link += "&as=" + url.QueryEscape(as)
	}
	return e.JSON(http.StatusOK, map[string]string{"url": link})
}

// --- uninstall --------------------------------------------------------------------------

func command(app *pocketbase.PocketBase) *cobra.Command {
	cmd := &cobra.Command{Use: "pages", Short: "Pages module (visual page builder)"}
	var yes bool
	uninstall := &cobra.Command{
		Use:          "uninstall",
		SilenceUsage: true,
		Short:        "Deletes the pages, their files and the pages collection (before removing the module from main.go)",
		RunE: func(cmd *cobra.Command, args []string) error {
			col, err := app.FindCollectionByNameOrId(Collection)
			if err != nil {
				fmt.Println("The pages collection doesn't exist: nothing to uninstall.")
				return nil
			}
			total, _ := app.CountRecords(col)
			if !yes {
				fmt.Printf("This deletes the pages collection (%d pages), its uploaded files, the saved versions and the theme settings.\n"+
					"Back up pb_data first, then run again with --yes.\n", total)
				return nil
			}
			err = app.RunInTransaction(func(tx core.App) error {
				if err := markSystem(tx, false, Collection, SettingsCollection, VersionsCollection); err != nil {
					return err
				}
				// reloaded: system collections can't be deleted
				for _, name := range []string{VersionsCollection, SettingsCollection, Collection} {
					c, err := tx.FindCollectionByNameOrId(name)
					if err != nil {
						continue
					}
					if err := tx.Delete(c); err != nil {
						return err
					}
				}
				// forget the migrations (re-adding the module creates the collections again)
				_, err := tx.DB().Delete("_migrations", dbx.In("file", migrationFile, kitMigrationFile, modeMigrationFile, versionsMigrationFile, accessMigrationFile, rulesMigrationFile, chartsMigrationFile, typesMigrationFile, errorsMigrationFile)).Execute()
				return err
			})
			if err != nil {
				return err
			}
			if fsys, err := app.NewFilesystem(); err == nil {
				defer fsys.Close()
				fsys.DeletePrefix(col.Id + "/")
			}
			fmt.Printf("Deleted %d pages. Now remove pages.Register(app) from main.go and the internal/pages folder.\n", total)
			return nil
		},
	}
	uninstall.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	cmd.AddCommand(uninstall)
	return cmd
}
