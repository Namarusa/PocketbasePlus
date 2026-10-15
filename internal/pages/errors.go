package pages

import (
	"html"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Error pages: what a visitor sees for an address without a page (404), a
// page they must sign in for (401, when there's no sign in page to send them
// to), a page their account can't open (403) and a page that failed (500).
// Each code can have one page of kind "error" (designed in the builder,
// shown once published); otherwise a default page with the site theme.
//
// The page reads the error with formulas: {{error.code}}, {{error.title}},
// {{error.message}}, {{error.path}} (the address asked) and
// {{error.signin}} (the sign in page, back to that address; empty without one).

var errorCodes = []string{"404", "401", "403", "500"}

var errorTexts = map[int][2]string{
	http.StatusNotFound:            {"Page not found", "This page doesn't exist or isn't published."},
	http.StatusUnauthorized:        {"Sign in required", "Sign in to view this page."},
	http.StatusForbidden:           {"No access", "Your account doesn't have access to this page."},
	http.StatusInternalServerError: {"Something went wrong", "The page couldn't be shown. Try again in a moment."},
}

// errorInfo is the {{error}} data of an error page.
func errorInfo(app core.App, status int, path string) map[string]any {
	texts := errorTexts[status]
	signin := ""
	if login := loginPage(app); login != "" {
		signin = pageURL(login, "") + "?next=" + url.QueryEscape(path)
	}
	return map[string]any{
		"code": strconv.Itoa(status), "title": texts[0], "message": texts[1], "path": path, "signin": signin,
	}
}

// defaultErrorHTML is the page of an error without its own page (and the
// layout of the new error pages): the code, the title, the message and what
// the visitor can do next.
const defaultErrorHTML = `<main style="min-height:70vh;display:flex;align-items:center;justify-content:center;padding:48px 24px;text-align:center">
<div style="max-width:520px;display:flex;flex-direction:column;align-items:center;gap:12px">
<p data-pb-formula="{{error.code}}" style="margin:0;font-size:64px;font-weight:700;letter-spacing:-0.03em;line-height:1;color:var(--muted-foreground)">404</p>
<h1 data-pb-formula="{{error.title}}" style="margin:0;font-size:28px;font-weight:700">Page not found</h1>
<p data-pb-formula="{{error.message}}" style="margin:0;color:var(--muted-foreground)">This page doesn't exist or isn't published.</p>
<div style="display:flex;gap:8px;flex-wrap:wrap;justify-content:center;margin-top:12px">
<a class="btn" href="#" data-pb-attr-href="{{error.signin}}" data-pb-conditions='[{"action":"show","when":[{"field":"error.code","op":"eq","value":"401"},{"field":"error.signin","op":"notEmpty"}]}]'>Sign in</a>
<form data-pb-logout="." data-pb-visible="users" data-pb-conditions='[{"action":"show","when":[{"field":"error.code","op":"eq","value":"403"}]}]'><button type="submit" class="btn" data-variant="outline">Sign in with another account</button></form>
<a class="btn" data-variant="outline" href="/">Home</a>
</div></div></main>`

// errorPage answers with the page of an error: the site's own error page for
// this code (published), else the default one with the site theme.
func errorPage(e *core.RequestEvent, status int) error {
	path := e.Request.URL.RequestURI()
	view := pageView{
		Visitor: visitorOf(e),
		Path:    path,
		Title:   errorTexts[status][0],
		HTML:    defaultErrorHTML,
		Error:   errorInfo(e.App, status, path),
	}
	t := loadTheme(e.App)
	view.Kit = &t
	if page, err := e.App.FindFirstRecordByFilter(Collection, "kind = 'error' && error_code = {:code} && published = true", dbx.Params{"code": strconv.Itoa(status)}); err == nil {
		view.Title = page.GetString("title")
		view.Description = page.GetString("description")
		view.HTML = page.GetString("html")
		view.CSS = page.GetString("css")
		if page.GetString("kit") != kitBasecoat {
			view.Kit = nil
		}
	}
	out, script, err := render(e.Request.Context(), e.App, view, e.Request.URL.Query(), false)
	h := e.Response.Header()
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Vary", "Cookie")
	h.Set("X-Content-Type-Options", "nosniff")
	if err != nil {
		// the error page itself failed: a plain one
		h.Set("Content-Security-Policy", pageCSP(false, false))
		texts := errorTexts[status]
		return e.HTML(status, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>`+html.EscapeString(texts[0])+
			`</title></head><body style="font-family:system-ui,sans-serif;text-align:center;padding:15vh 16px"><h1>`+html.EscapeString(texts[0])+
			`</h1><p>`+html.EscapeString(texts[1])+`</p><p><a href="/">Home</a></p></body></html>`)
	}
	h.Set("Content-Security-Policy", pageCSP(script, view.Kit != nil))
	return e.HTML(status, out)
}
