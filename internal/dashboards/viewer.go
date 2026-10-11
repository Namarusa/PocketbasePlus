package dashboards

import (
	"net/http"
	"sync"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/publicpage"
)

// ViewerPath is where role users open their dashboards (they can't use /_/).
const ViewerPath = "/dashboards"

var (
	viewerOnce sync.Once
	viewerHTML string
)

// registerViewer serves the read-only viewer page. It reuses the dashboard
// styles and UI toolkit, and the same panel rendering code as the dashboard extension.
func registerViewer(se *core.ServeEvent) {
	handler := func(e *core.RequestEvent) error {
		viewerOnce.Do(func() {
			viewerHTML = publicpage.HTML(publicpage.Options{
				Title:     "Dashboards",
				BodyClass: "gp-viewer-body",
				Scripts:   []string{"/_/extensions/pocketbaseplus/vendor/echarts.min.js"},
				Module:    "/_/extensions/pocketbaseplus/viewer.js",
			})
		})
		h := e.Response.Header()
		h.Set("Content-Security-Policy", publicpage.CSP("'none'"))
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Frame-Options", "DENY")
		return e.HTML(http.StatusOK, viewerHTML)
	}
	se.Router.GET(ViewerPath, handler)
	se.Router.GET(ViewerPath+"/", handler)
}
