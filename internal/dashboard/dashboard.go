// Package dashboard extends the native PocketBase superuser dashboard (/_/)
// with the Permissions and Emails pages, using the PocketBase UI extensions API.
//
// The extension is plain JavaScript (no build step) written with the same
// Shablon toolkit and components as the dashboard itself, see ext/main.js.
package dashboard

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/offline"
)

// dashboardCSP is PocketBase's default dashboard policy (apis/serve.go, v0.40.4).
const dashboardCSP = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' http://127.0.0.1:* https://tile.openstreetmap.org data: blob:; connect-src 'self' http://127.0.0.1:* https://nominatim.openstreetmap.org; script-src 'self' http://127.0.0.1:*; frame-ancestors 'none'"

// Name is the extension name, its files are served at /_/extensions/{Name}/.
const Name = "pocketbaseplus"

//go:embed all:ext
var extFS embed.FS

// Register adds the extension to the superuser dashboard.
func Register(app core.App) {
	files, err := fs.Sub(extFS, "ext")
	if err != nil {
		panic(err)
	}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.UIExtensions = append(se.UIExtensions, core.UIExtension{Name: Name, FS: files})

		// the functions of the templates and formulas, for the pickers
		se.Router.GET("/api/gp/functions", func(e *core.RequestEvent) error {
			return e.JSON(200, map[string]any{"categories": formula.Categories, "functions": formula.Catalog()})
		}).Bind(apis.RequireSuperuserAuth())

		// The dashboard caches /_/ assets for 2 weeks (it only sets the header
		// when empty). Extension files are not content hashed, so make
		// browsers revalidate them to pick up changes after an upgrade.
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			p := e.Request.URL.Path
			if p == "/_/extensions.js" || strings.HasPrefix(p, "/_/extensions/"+Name+"/") {
				e.Response.Header().Set("Cache-Control", "no-cache")
			}
			// a map tile server set with POCKETBASEPLUS_MAP_TILES (e.g. on the local
			// network) must be allowed by the dashboard CSP (PocketBase only sets
			// its default policy when none is set)
			if origin := offline.MapTilesOrigin(); origin != "" && (p == "/_" || strings.HasPrefix(p, "/_/")) {
				e.Response.Header().Set("Content-Security-Policy", strings.Replace(dashboardCSP, "img-src 'self'", "img-src 'self' "+origin, 1))
			}
			return e.Next()
		})

		return se.Next()
	})
}
