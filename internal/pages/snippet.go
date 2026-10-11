package pages

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"
)

// snippet renders an element of the editor as the published page shows it
// (charts with their data, calendars), for the canvas of the builder. The
// "as" visitor is the "View as" choice of the builder.
func snippet(e *core.RequestEvent) error {
	page, err := e.App.FindRecordById(Collection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("", nil)
	}
	var body struct {
		HTML string `json:"html"`
		As   string `json:"as"`
	}
	if err := e.BindBody(&body); err != nil || len(body.HTML) > 64<<10 || !validAs(body.As) {
		return e.BadRequestError("Invalid element.", err)
	}
	root, err := parseBody("<body>" + body.HTML + "</body>")
	if err != nil {
		return e.BadRequestError("Invalid element.", err)
	}
	sanitize(root)
	v := previewVisitor(e.App, body.As)
	s := scope{}
	// a record page shows its first record
	if colID := page.GetString("record_collection"); colID != "" {
		if col, err := listCollection(e.App, colID); err == nil {
			filter, params := bindPlaceholders(page.GetString("record_filter"), scope{}, nil, v.user)
			if strings.TrimSpace(filter) == "" {
				filter = "id != ''"
			}
			if list, err := e.App.FindRecordsByFilter(col, filter, "-created", 1, 0, params); err == nil && len(list) > 0 {
				s = scope{col: col, rec: list[0]}
			}
		}
	}
	r := &renderer{app: e.App, ctx: e.Request.Context(), query: e.Request.URL.Query(), preview: true, budget: maxRecords,
		visitor: v, info: v.requestInfo(), path: "/"}
	r.walk(root, s, 0)
	var b strings.Builder
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&b, c); err != nil {
			return e.InternalServerError("", err)
		}
	}
	return e.JSON(200, map[string]string{"html": b.String()})
}
