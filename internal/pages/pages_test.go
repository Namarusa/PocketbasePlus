package pages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

const (
	lampID  = "lamp00000000001"
	chairID = "chair0000000001"
)

type fixture struct {
	app      *tests.TestApp
	products *core.Collection
	acme     *core.Record
	lamp     *core.Record
	chair    *core.Record
}

// setup creates a closed "products" collection (no API rules: the page is the gate).
func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	Bind(app)
	f := &fixture{app: app}
	brands := testutil.CreateCollection(t, app, "brands", &core.TextField{Name: "name", Presentable: true})
	f.acme = testutil.Create(t, app, "brands", map[string]any{"name": "Acme"})
	f.products = testutil.CreateCollection(t, app, "products",
		&core.TextField{Name: "name"},
		&core.NumberField{Name: "price"},
		&core.BoolField{Name: "published"},
		&core.EditorField{Name: "body"},
		&core.TextField{Name: "secret", Hidden: true},
		&core.TextField{Name: "site"}, // a text field: URL fields already refuse javascript:
		&core.DateField{Name: "launch"},
		&core.FileField{Name: "photo", MaxSelect: 1, MaxSize: 1 << 20},
		&core.RelationField{Name: "brand", CollectionId: brands.Id, MaxSelect: 1},
	)
	f.lamp = testutil.Create(t, app, "products", map[string]any{
		"id": lampID, "name": "Lamp <b>", "price": 12.5, "published": true, "secret": "s3cret", "brand": f.acme.Id,
		"body": `<p>Bright<script>alert(1)</script><img src=x onerror="alert(2)"></p>`,
		"site": "javascript:alert(3)", "launch": "2026-03-04 10:00:00.000Z",
	})
	f.chair = testutil.Create(t, app, "products", map[string]any{"id": chairID, "name": "Chair", "price": 40, "published": false})
	return f
}

func (f *fixture) page(t testing.TB, data map[string]any) *core.Record {
	t.Helper()
	base := map[string]any{"title": "Shop", "slug": "shop", "published": true}
	for k, v := range data {
		base[k] = v
	}
	return testutil.Create(t, f.app, Collection, base)
}

func (f *fixture) render(t testing.TB, src string, s scope, query url.Values) string {
	t.Helper()
	out, _, err := render(t.Context(), f.app, pageView{Title: "T", HTML: src, Scope: s}, query, false)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestListBindings(t *testing.T) {
	f := setup(t)
	out := f.render(t, `<body><div data-pb-list="products" data-pb-filter="published = true" data-pb-sort="name">`+
		`<article data-pb-item><h2 data-pb-field="name">Sample</h2><span data-pb-field="price">0</span>`+
		`<span data-pb-field="brand"></span><span data-pb-field="secret">x</span><span data-pb-field="launch"></span>`+
		`<p data-pb-field="body"></p><a data-pb-link="product">More</a><img data-pb-field="site"></article>`+
		`<p data-pb-empty>Nothing</p></div></body>`, scope{}, nil)

	for _, want := range []string{
		"<h2>Lamp &lt;b&gt;</h2>", "<span>12.5</span>", "<span>Acme</span>", "<span></span>", // secret is hidden
		"Mar 4, 2026", "<div><p>Bright<img src=\"x\" loading=\"lazy\"/></p></div>", `href="/product/` + f.lamp.Id + `"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Chair", "s3cret", "script", "onerror", "javascript:", "data-pb-", "Nothing", "Sample"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q in\n%s", unwanted, out)
		}
	}

	// empty state
	out = f.render(t, `<div data-pb-list="products" data-pb-filter="price > 1000"><b data-pb-item>x</b><p data-pb-empty>Nothing</p></div>`, scope{}, nil)
	if !strings.Contains(out, "<p>Nothing</p>") || strings.Contains(out, "<b>") {
		t.Errorf("expected the empty state, got\n%s", out)
	}
}

func TestPlaceholdersAreBound(t *testing.T) {
	f := setup(t)
	src := `<ul data-pb-list="products" data-pb-filter="name = {{query.q}}"><li data-pb-field="name"></li></ul>`
	out := f.render(t, src, scope{}, url.Values{"q": {"Chair"}})
	if !strings.Contains(out, "<li>Chair</li>") || strings.Contains(out, "Lamp") {
		t.Errorf("expected only Chair, got\n%s", out)
	}
	// injection attempts stay a value
	out = f.render(t, src, scope{}, url.Values{"q": {`x" || id != "`}})
	if strings.Contains(out, "<li>") {
		t.Errorf("the query value changed the filter:\n%s", out)
	}

	// {{record.x}}: lists of the current record (products of a brand page)
	brands, _ := f.app.FindCollectionByNameOrId("brands")
	out = f.render(t, `<h1 data-pb-field="name"></h1><ul data-pb-list="products" data-pb-filter="brand = {{record.id}}"><li data-pb-field="name"></li></ul>`,
		scope{col: brands, rec: f.acme}, nil)
	if !strings.Contains(out, "<h1>Acme</h1>") || !strings.Contains(out, "<li>Lamp &lt;b&gt;</li>") || strings.Contains(out, "Chair") {
		t.Errorf("unexpected brand page\n%s", out)
	}
}

func TestSanitize(t *testing.T) {
	f := setup(t)
	out := f.render(t, `<div onclick="x()"><script>alert(1)</script><a href=" javascript:alert(1)">a</a>`+
		`<img src="data:image/png;base64,AA" srcset="javascript:x 1x"><iframe src="https://video.example/embed"></iframe>`+
		`<iframe data-pb-form="contact"></iframe><!-- secret comment --><svg><script>alert(1)</script></svg></div>`, scope{}, nil)
	for _, unwanted := range []string{"onclick", "alert", "javascript", "srcset", "secret comment"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q in\n%s", unwanted, out)
		}
	}
	// links saved before the pages moved to the root
	old := f.render(t, `<a href="/p/about?x=1#top">a</a><a href="/p/_assets/x/a.png">b</a><a href="/p/api">c</a>`+
		`<form action="/p/contact/abc"></form><a href="/pp/x">d</a>`, scope{}, nil)
	for _, want := range []string{`href="/about?x=1#top"`, `href="/p/_assets/x/a.png"`, `href="/p/api"`, `action="/contact/abc"`, `href="/pp/x"`} {
		if !strings.Contains(old, want) {
			t.Errorf("missing %q in\n%s", want, old)
		}
	}
	for _, want := range []string{`src="data:image/png;base64,AA"`, `sandbox="allow-scripts`, `src="/f/contact/embed"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// CSS can't close the style element
	html, _, _ := render(t.Context(), f.app, pageView{CSS: "</style><script>alert(1)</script>"}, nil, false)
	if strings.Contains(html, "</style><script>") {
		t.Errorf("the CSS closed the style element:\n%s", html)
	}
}

func TestValidation(t *testing.T) {
	f := setup(t)
	bad := []map[string]any{
		{"slug": "bad", "html": `<div data-pb-list="nope"></div>`},
		{"slug": "bad", "html": `<div data-pb-list="products" data-pb-filter="unknown_field = 1"></div>`},
		{"slug": "bad", "html": `<div data-pb-list="pages"></div>`}, // module collections are out of reach
		{"slug": "bad", "record_collection": "nope"},
		{"slug": "Bad Slug"},
		{"slug": "api"}, // the address of the app's routes
		{"slug": "dashboards"},
	}
	for i, data := range bad {
		col, _ := f.app.FindCollectionByNameOrId(Collection)
		r := core.NewRecord(col)
		r.Load(map[string]any{"title": "x"})
		r.Load(data)
		if err := f.app.Save(r); err == nil {
			t.Errorf("%d: expected a validation error", i)
		}
	}

	// one home page
	a := f.page(t, map[string]any{"slug": "a", "home": true})
	f.page(t, map[string]any{"slug": "b", "home": true})
	a, _ = f.app.FindRecordById(Collection, a.Id)
	if a.GetBool("home") {
		t.Error("expected the previous home page to be unset")
	}
}

func TestRoutes(t *testing.T) {
	list := `<div data-pb-list="products"><a data-pb-item data-pb-link="product" data-pb-field="name"></a></div>`
	factory := func(t testing.TB) *tests.TestApp {
		f := setup(t)
		f.page(t, map[string]any{"slug": "shop", "home": true, "html": list})
		f.page(t, map[string]any{"slug": "product", "title": "Product", "html": `<h1 data-pb-field="name"></h1>`,
			"record_collection": "products", "record_filter": "published = true"})
		f.page(t, map[string]any{"slug": "draft", "published": false, "html": "<p>Draft</p>"})
		return f.app
	}

	notFound := []string{"Page not found"}
	scenarios := []tests.ApiScenario{
		{Name: "home page", Method: http.MethodGet, URL: "/", ExpectedStatus: 200,
			ExpectedContent: []string{"Lamp &lt;b&gt;", "Chair", `href="/product/`}},
		{Name: "unknown API route", Method: http.MethodGet, URL: "/api/nope", ExpectedStatus: 404, ExpectedContent: []string{`"status":404`}},
		{Name: "reserved first segment", Method: http.MethodGet, URL: "/email", ExpectedStatus: 404, ExpectedContent: notFound},
		{Name: "too deep", Method: http.MethodGet, URL: "/product/a/b", ExpectedStatus: 404, ExpectedContent: notFound},
		{Name: "kit files stay under /p/", Method: http.MethodGet, URL: "/p/_theme.css", ExpectedStatus: 200, ExpectedContent: []string{"@font-face"}},
		{Name: "draft hidden", Method: http.MethodGet, URL: "/draft", ExpectedStatus: 404, ExpectedContent: notFound},
		{Name: "unknown page", Method: http.MethodGet, URL: "/nope", ExpectedStatus: 404, ExpectedContent: notFound},
		{Name: "record page needs an id", Method: http.MethodGet, URL: "/product", ExpectedStatus: 404, ExpectedContent: notFound},
		{Name: "list page takes no id", Method: http.MethodGet, URL: "/shop/abc", ExpectedStatus: 404, ExpectedContent: notFound},
	}
	for _, s := range scenarios {
		s.TestAppFactory = factory
		s.Test(t)
	}

	// redirects: one address for the home page, old /p/ addresses
	for url, location := range map[string]string{
		"/shop?x=1":          "/?x=1",
		"/p/product/abc?x=1": "/product/abc?x=1",
		"/p/draft":           "/draft",
	} {
		(&tests.ApiScenario{Name: "redirect " + url, Method: http.MethodGet, URL: url, TestAppFactory: factory, ExpectedStatus: 301,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if got := res.Header.Get("Location"); got != location {
					t.Errorf("%s: expected the location %q, got %q", url, location, got)
				}
			},
		}).Test(t)
	}

	// record page: matching record, record outside the filter, bad token
	(&tests.ApiScenario{Name: "record page", Method: http.MethodGet, TestAppFactory: factory,
		URL:             "/product/" + lampID,
		ExpectedStatus:  200,
		ExpectedContent: []string{"<h1>Lamp &lt;b&gt;</h1>", "<title>Lamp &lt;b&gt; · Product</title>"},
	}).Test(t)
	(&tests.ApiScenario{Name: "record outside the filter", Method: http.MethodGet, TestAppFactory: factory,
		URL: "/product/" + chairID, ExpectedStatus: 404, ExpectedContent: notFound,
	}).Test(t)
	(&tests.ApiScenario{Name: "forged preview", Method: http.MethodGet, TestAppFactory: factory,
		URL: "/draft?preview=9999999999.abc", ExpectedStatus: 404, ExpectedContent: notFound,
	}).Test(t)
}

func TestPreviewTokens(t *testing.T) {
	f := setup(t)
	draft := f.page(t, map[string]any{"slug": "draft", "published": false})
	other := f.page(t, map[string]any{"slug": "other", "published": false})
	future, past := "9999999999", "1000000000"
	if !validPreview(f.app, draft, future+"."+previewSig(f.app, draft.Id, "", future), "") {
		t.Error("expected a valid token")
	}
	for _, token := range []string{
		"", "abc", future + ".abc",
		past + "." + previewSig(f.app, draft.Id, "", past),     // expired
		future + "." + previewSig(f.app, other.Id, "", future), // another page
	} {
		if validPreview(f.app, draft, token, "") {
			t.Errorf("token %q accepted", token)
		}
	}
	out, _, err := render(t.Context(), f.app, pageView{HTML: `<p data-pb-field="name"></p><div data-pb-list="nope"></div>`}, nil, true)
	if err != nil || !strings.Contains(out, "noindex") || !strings.Contains(out, "{name}") || !strings.Contains(out, "unknown collection") {
		t.Errorf("unexpected preview %v\n%s", err, out)
	}
}

func TestKitShell(t *testing.T) {
	f := setup(t)
	th := theme{Style: "nova", Mode: "dark"}
	out, script, err := render(t.Context(), f.app, pageView{HTML: `<a class="btn" data-variant="outline" href="/x">Go</a>`, Kit: &th}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<html lang="en" data-theme-mode="dark" class="dark">`, "<script>" + kitScript + "</script>", `/p/_static/basecoat-1.0.2-nova.css`, `/p/_theme.css?v=`, `data-variant="outline"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if script || strings.Contains(out, "<script src") {
		t.Error("a page without interactive components must not load the Basecoat script")
	}

	// interactive components load the embedded script; dialogs use native commands
	src := `<div class="tabs"><nav role="tablist"><button role="tab" aria-controls="p1">A</button></nav><div role="tabpanel" id="p1" hidden>x</div></div>` +
		`<button command="show-modal" commandfor="d1" onclick="alert(1)">Open</button><dialog id="d1" class="dialog" closedby="any"><div><button command="close" commandfor="d1">x</button></div></dialog>`
	out, script, _ = render(t.Context(), f.app, pageView{HTML: src, Kit: &th}, nil, false)
	if !script || !strings.Contains(out, `<script src="/p/_static/basecoat-1.0.2.js" defer></script>`) {
		t.Errorf("expected the Basecoat script\n%s", out)
	}
	for _, want := range []string{`command="show-modal" commandfor="d1"`, `closedby="any"`, `role="tabpanel"`, "hidden"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "onclick") {
		t.Error("event handlers must be removed")
	}
	// without the kit: no Basecoat files, even with the same classes
	out, script, _ = render(t.Context(), f.app, pageView{HTML: src}, nil, false)
	if script || strings.Contains(out, "basecoat") || strings.Contains(out, "_theme.css") {
		t.Errorf("unexpected kit files without the kit\n%s", out)
	}

	if !strings.Contains(pageCSP(true, false), "script-src 'self';") || !strings.Contains(pageCSP(false, false), "script-src 'none'") ||
		!strings.Contains(pageCSP(false, true), "script-src "+kitScriptHash+";") || !strings.Contains(pageCSP(true, true), "script-src 'self' 'sha256-") {
		t.Error("unexpected CSP")
	}
	// auto mode: no class, the script picks the mode
	th.Mode = "auto"
	out, _, _ = render(t.Context(), f.app, pageView{HTML: "<p>x</p>", Kit: &th}, nil, false)
	if !strings.Contains(out, `<html lang="en" data-theme-mode="auto">`) {
		t.Errorf("unexpected auto mode shell\n%s", out)
	}
	t.Setenv("POCKETBASEPLUS_OFFLINE", "1")
	if strings.Contains(pageCSP(false, true), "https:") {
		t.Errorf("offline pages must only load the app files: %s", pageCSP(false, true))
	}
}

func TestThemeSettings(t *testing.T) {
	f := setup(t)
	rec, err := f.app.FindFirstRecordByFilter(SettingsCollection, "id != ''")
	if err != nil {
		t.Fatal("expected the settings record", err)
	}
	if rec.GetString("style") != "vega" {
		t.Errorf("default style %q", rec.GetString("style"))
	}
	for _, bad := range []map[string]any{{"primary": "red;}body{display:none"}, {"radius": "1rem;x"}, {"style": "nope"}} {
		r, _ := f.app.FindRecordById(SettingsCollection, rec.Id)
		r.Load(bad)
		if err := f.app.Save(r); err == nil {
			t.Errorf("%v: expected a validation error", bad)
		}
	}
	rec.Load(map[string]any{"mode": "auto"})
	if err := f.app.Save(rec); err != nil || loadTheme(f.app).Mode != "auto" {
		t.Fatal("expected the auto mode", err)
	}
	rec.Load(map[string]any{"style": "maia", "mode": "dark", "font": "system", "primary": "#f5d90a", "radius": "0.5rem", "theme_css": ":root{--accent:red}"})
	if err := f.app.Save(rec); err != nil {
		t.Fatal(err)
	}
	css := loadTheme(f.app).css()
	for _, want := range []string{"--primary:#f5d90a;--primary-foreground:#111", "--radius:0.5rem", "--font-sans:ui-sans-serif", "--accent:red", "geist-sans.woff2"} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in\n%s", want, css)
		}
	}
	if foregroundFor("#1e3a8a") != "#fff" {
		t.Error("expected white text on a dark color")
	}
}

func TestStaticFiles(t *testing.T) {
	scenarios := []tests.ApiScenario{
		{Name: "style (plain)", Method: http.MethodGet, URL: "/p/_static/basecoat-1.0.2-vega.css", ExpectedStatus: 200, ExpectedContent: []string{".btn"}},
		{Name: "script (gzip)", Method: http.MethodGet, URL: "/p/_static/basecoat-1.0.2.js", Headers: map[string]string{"Accept-Encoding": "gzip"}, ExpectedStatus: 200, ExpectedContent: []string{"\x1f\x8b"}},
		{Name: "font", Method: http.MethodGet, URL: "/p/_static/geist-sans.woff2", ExpectedStatus: 200, ExpectedContent: []string{"wOF2"}},
		// only the listed names: never the embedded files by their paths
		{Name: "unknown file", Method: http.MethodGet, URL: "/p/_static/vega.css.gz", ExpectedStatus: 404, ExpectedContent: []string{"wasn't found"}},
		{Name: "embedded path", Method: http.MethodGet, URL: "/p/_static/ext%2Fmain.js", ExpectedStatus: 404, ExpectedContent: []string{"wasn't found"}},
		{Name: "theme", Method: http.MethodGet, URL: "/p/_theme.css", ExpectedStatus: 200, ExpectedContent: []string{"Geist Sans"}},
	}
	for _, s := range scenarios {
		s.TestAppFactory = func(t testing.TB) *tests.TestApp { return setup(t).app }
		s.Test(t)
	}
}
