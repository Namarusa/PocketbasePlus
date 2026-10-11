// Package publicpage renders the public pages served outside of the
// superuser dashboard (dashboards viewer, forms): the same stylesheet and UI
// toolkit as the dashboard, and a strict content security policy.
package publicpage

import (
	"html"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/ui"
)

var cssRegex = regexp.MustCompile(`href="\./(assets/index-[^"]+\.css)"`)

var (
	cssOnce sync.Once
	cssPath string
)

// DashboardCSS returns the path of the dashboard stylesheet (its name is
// content hashed and changes with PocketBase releases).
func DashboardCSS() string {
	cssOnce.Do(func() {
		if ui.DistDirFS == nil {
			return
		}
		index, err := fs.ReadFile(ui.DistDirFS, "index.html")
		if err != nil {
			return
		}
		if m := cssRegex.FindSubmatch(index); m != nil {
			cssPath = "/_/" + string(m[1])
		}
	})
	return cssPath
}

// CSP returns the content security policy of a public page, with the
// frame-ancestors directive (e.g. "'none'" or "'self' https://example.com").
func CSP(frameAncestors string) string {
	return "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; script-src 'self'; frame-ancestors " + frameAncestors
}

// Options describe a public page.
type Options struct {
	Title     string
	BodyClass string
	BodyAttrs map[string]string // data-* attributes read by the page script
	Scripts   []string          // classic scripts (head)
	Module    string            // the page ES module
}

// HTML renders the page shell.
func HTML(o Options) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"UTF-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1.0">` + "\n")
	b.WriteString("<title>" + html.EscapeString(o.Title) + "</title>\n")
	b.WriteString(`<link rel="icon" type="image/png" href="/_/images/favicon.png">` + "\n")
	if css := DashboardCSS(); css != "" {
		b.WriteString(`<link rel="stylesheet" href="` + css + `">` + "\n")
	}
	b.WriteString(`<link rel="stylesheet" href="/_/extensions/pocketbaseplus/style.css">` + "\n")
	b.WriteString(`<script src="/_/libs/shablon/shablon.iife.js"></script>` + "\n")
	for _, s := range o.Scripts {
		b.WriteString(`<script src="` + html.EscapeString(s) + `"></script>` + "\n")
	}
	b.WriteString("</head>\n<body class=\"" + html.EscapeString(o.BodyClass) + "\"")
	for k, v := range o.BodyAttrs {
		b.WriteString(" data-" + html.EscapeString(k) + "=\"" + html.EscapeString(v) + "\"")
	}
	b.WriteString(">\n")
	if o.Module != "" {
		b.WriteString(`<script type="module" src="` + html.EscapeString(o.Module) + `"></script>` + "\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}
