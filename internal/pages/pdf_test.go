package pages

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/pdfview"
)

var pdfOptsRegex = regexp.MustCompile(`data-gp-pdf="([^"]*)"`)

// pdfOptions returns the options of the first viewer of a rendered page.
func pdfOptions(t *testing.T, out string) map[string]any {
	t.Helper()
	m := pdfOptsRegex.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no viewer in\n%.2000s", out)
	}
	var opts map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &opts); err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestPDFViewer(t *testing.T) {
	f := setup(t)
	view := func(src string, s scope) (string, needs) {
		t.Helper()
		out, n, err := render(t.Context(), f.app, pageView{HTML: src, Scope: s}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		return out, n
	}

	// an address, the default controls
	out, n := view(`<div id="v" class="mine" style="margin:8px" data-pb-pdf="url" data-pb-pdf-src="https://example.com/files/guide.pdf"></div>`, scope{})
	if !n.fetch || !n.script {
		t.Errorf("a viewer needs its script and to read its file: %+v", n)
	}
	for _, want := range []string{`class="mine gp-pdf"`, `id="v"`, `style="margin:8px"`, `role="region"`, `aria-label="PDF document"`,
		`/p/_static/gp-pdf-`, `<a href="https://example.com/files/guide.pdf" rel="noopener" target="_blank">Open the PDF</a>`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%.2000s", want, out)
		}
	}
	if strings.Contains(out, "data-pb-") || strings.Contains(out, "/p/_static/gp-ui-") {
		t.Errorf("unexpected binding attributes or chart script:\n%.2000s", out)
	}
	opts := pdfOptions(t, out)
	if opts["src"] != "https://example.com/files/guide.pdf" || opts["lib"] != "/p/_pdfjs/"+pdfview.Version+"/" || opts["toolbar"] != "top" ||
		opts["zoom"] != "width" || opts["layout"] != "scroll" || opts["sidebar"] != "none" || opts["name"] != "guide.pdf" ||
		opts["text"] != true || opts["links"] != true || opts["frame"] != "shadow" || opts["background"] != "muted" {
		t.Errorf("unexpected default options %v", opts)
	}
	// the sidebar button only goes with a sidebar
	if tools := opts["tools"].([]any); len(tools) != len(pdfTools)-1 || slices.Contains(tools, any("sidebar")) {
		t.Errorf("unexpected tools %v", tools)
	}

	// every control chosen
	out, _ = view(`<div data-pb-pdf="url" data-pb-pdf-src="/files/a.pdf" data-pb-pdf-toolbar="bottom" data-pb-pdf-tools="pages, download,nope,pages"`+
		` data-pb-pdf-sidebar="open" data-pb-pdf-zoom="900" data-pb-pdf-layout="single" data-pb-pdf-page="3" data-pb-pdf-height="480px"`+
		` data-pb-pdf-background="dark" data-pb-pdf-frame="none" data-pb-pdf-text="false" data-pb-pdf-links="false" data-pb-pdf-name="Guide: 2026"`+
		` data-pb-pdf-label="User guide"></div>`, scope{})
	opts = pdfOptions(t, out)
	if strings.Join(toStrings(opts["tools"]), ",") != "pages,download" || opts["toolbar"] != "bottom" || opts["sidebar"] != "open" ||
		opts["zoom"] != "400" || opts["layout"] != "single" || opts["page"] != 3.0 || opts["height"] != 480.0 || opts["background"] != "dark" ||
		opts["frame"] != "none" || opts["text"] != false || opts["links"] != false || opts["name"] != "Guide 2026.pdf" || opts["label"] != "User guide" {
		t.Errorf("unexpected options %v", opts)
	}
	if !strings.Contains(out, `aria-label="User guide"`) {
		t.Error("expected the label of the viewer")
	}
	out, _ = view(`<div data-pb-pdf="url" data-pb-pdf-src="/a.pdf" data-pb-pdf-height="auto" data-pb-pdf-zoom="page" data-pb-pdf-tools="" data-pb-pdf-toolbar="none"></div>`, scope{})
	if opts = pdfOptions(t, out); opts["fit"] != true || opts["zoom"] != "page" || len(toStrings(opts["tools"])) != 0 || opts["toolbar"] != "none" {
		t.Errorf("unexpected options %v", opts)
	}

	// a formula of the record, and the addresses refused
	out, _ = view(`<div data-pb-pdf="url" data-pb-pdf-src="/docs/{{record.price}}.pdf"></div>`, scope{col: f.products, rec: f.lamp})
	if pdfOptions(t, out)["src"] != "/docs/12.5.pdf" {
		t.Errorf("formula: %v", pdfOptions(t, out))
	}
	for _, bad := range []string{"javascript:alert(1)", "data:application/pdf;base64,AA", "//evil.com/x.pdf", "mailto:a@b.c", "{{record.site}}"} {
		out, _ = view(`<div data-pb-pdf="url" data-pb-pdf-src="`+html.EscapeString(bad)+`"></div>`, scope{col: f.products, rec: f.lamp})
		if opts := pdfOptions(t, out); opts["src"] != "" || !strings.Contains(out, "isn&#39;t an http(s) address") || !strings.Contains(out, `class="gp-pdf-empty"`) {
			t.Errorf("%s: expected no address, got %v\n%.1500s", bad, opts, out)
		}
	}
	t.Run("offline", func(t *testing.T) {
		t.Setenv("POCKETBASEPLUS_OFFLINE", "1")
		out, _ := view(`<div data-pb-pdf="url" data-pb-pdf-src="https://example.com/a.pdf"></div>`, scope{})
		if pdfOptions(t, out)["src"] != "" || !strings.Contains(out, "offline mode") {
			t.Errorf("offline: %.1500s", out)
		}
	})

	// a file field of the record: its first PDF, named without PocketBase's suffix
	rec := f.lamp.Clone()
	rec.Set("photo", "manual_a1b2c3d4e5.pdf")
	out, _ = view(`<div data-pb-pdf="field" data-pb-pdf-field="photo"></div>`, scope{col: f.products, rec: rec})
	opts = pdfOptions(t, out)
	if opts["src"] != "/api/files/"+f.products.Id+"/"+lampID+"/manual_a1b2c3d4e5.pdf" || opts["name"] != "manual.pdf" {
		t.Errorf("field: %v", opts)
	}
	// a record without a file: the empty message
	out, _ = view(`<div data-pb-pdf="field" data-pb-pdf-field="photo" data-pb-pdf-empty="No manual yet"></div>`, scope{col: f.products, rec: f.chair})
	if pdfOptions(t, out)["src"] != "" || !strings.Contains(out, `<div class="gp-pdf-empty">No manual yet</div>`) {
		t.Errorf("no file: %.1500s", out)
	}
	out, _ = view(`<div data-pb-pdf="field" data-pb-pdf-field="photo"></div>`, scope{})
	if !strings.Contains(out, "a file field needs a record") {
		t.Errorf("field without a record: %.1500s", out)
	}
	// protected files are never linked
	f.products.Fields.GetByName("photo").(*core.FileField).Protected = true
	out, _ = view(`<div data-pb-pdf="field" data-pb-pdf-field="photo"></div>`, scope{col: f.products, rec: rec})
	f.products.Fields.GetByName("photo").(*core.FileField).Protected = false
	if pdfOptions(t, out)["src"] != "" || !strings.Contains(out, "protected file field") {
		t.Errorf("protected: %.1500s", out)
	}

	// a document template: a signed link for the record
	out, _ = view(`<div data-pb-pdf="document" data-pb-pdf-document="sheet"></div>`, scope{col: f.products, rec: f.lamp})
	src, _ := pdfOptions(t, out)["src"].(string)
	if !strings.HasPrefix(src, pdfview.ViewPath+"sheet/products:"+lampID+"?exp=") || !strings.Contains(src, "&sig=") {
		t.Errorf("document: %s", src)
	}
	out, _ = view(`<div data-pb-pdf="document"></div>`, scope{})
	if !strings.Contains(out, "choose the document template") {
		t.Errorf("document without a template: %.1500s", out)
	}

	// a document link: a download of the record's document; a button becomes a link
	out, _ = view(`<button type="button" class="btn" data-pb-document="sheet">Get the <b>sheet</b></button>`, scope{col: f.products, rec: f.lamp})
	if !regexp.MustCompile(`<a class="btn" download="" href="` + regexp.QuoteMeta(pdfview.ViewPath) + `sheet/products:` + lampID + `\?exp=\d+&amp;sig=[^"&]+&amp;download=1">Get the <b>sheet</b></a>`).MatchString(out) {
		t.Errorf("document link: %.1500s", out)
	}
	out, _ = view(`<a data-pb-document="sheet" data-pb-document-mode="open">Open</a>`, scope{})
	if !strings.Contains(out, `href="`+pdfview.ViewPath+`sheet/:-?exp=`) || !strings.Contains(out, `target="_blank" rel="noopener"`) || strings.Contains(out, "download") || strings.Contains(out, "data-pb-") {
		t.Errorf("document link in a tab: %.1500s", out)
	}
	if out, _ = view(`<a data-pb-document="">PDF</a>`, scope{}); !strings.Contains(out, "choose the document template") {
		t.Errorf("document link without a template: %.1500s", out)
	}

	// the CSP lets the viewers read their file
	if csp := pageCSP(needs{script: true, fetch: true}, false); !strings.Contains(csp, "connect-src 'self' https:;") {
		t.Errorf("csp: %s", csp)
	}
	if csp := pageCSP(needs{script: true}, false); !strings.Contains(csp, "connect-src 'none'") {
		t.Errorf("csp without viewer: %s", csp)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestPDFFiles(t *testing.T) {
	factory := func(t testing.TB) *tests.TestApp {
		f := setup(t)
		f.page(t, map[string]any{"html": `<div data-pb-pdf="url" data-pb-pdf-src="/a.pdf"></div>`})
		return f.app
	}
	scenarios := []tests.ApiScenario{
		{Name: "the page", Method: http.MethodGet, URL: "/shop", ExpectedStatus: 200, ExpectedContent: []string{"data-gp-pdf", "/p/_static/gp-pdf-"},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self'") || !strings.Contains(csp, "script-src 'self'") {
					t.Errorf("csp: %s", csp)
				}
			}},
		{Name: "script", Method: http.MethodGet, URL: "/p/_static/" + pdfJS, ExpectedStatus: 200, ExpectedContent: []string{"window.gpPDF"}},
		{Name: "styles", Method: http.MethodGet, URL: "/p/_static/" + pdfCSS, ExpectedStatus: 200, ExpectedContent: []string{".gp-pdf-toolbar"}},
		{Name: "pdf.js", Method: http.MethodGet, URL: "/p/_pdfjs/" + pdfview.Version + "/pdf.min.mjs", ExpectedStatus: 200, ExpectedContent: []string{"GlobalWorkerOptions"}},
		{Name: "a character map", Method: http.MethodGet, URL: "/p/_pdfjs/" + pdfview.Version + "/cmaps/UniJIS-UCS2-H.bcmap", ExpectedStatus: 200, NotExpectedContent: []string{"wasn't found"}},
		{Name: "another version", Method: http.MethodGet, URL: "/p/_pdfjs/1.0.0/pdf.min.mjs", ExpectedStatus: 404, ExpectedContent: []string{"wasn't found"}},
	}
	for _, s := range scenarios {
		s.TestAppFactory = factory
		s.Test(t)
	}
	// the pages take PDF files
	f := setup(t)
	pages, _ := f.app.FindCollectionByNameOrId(Collection)
	assets := pages.Fields.GetByName("assets").(*core.FileField)
	if !slices.Contains(assets.MimeTypes, "application/pdf") || assets.MaxSize < 30<<20 || !slices.Contains(assets.MimeTypes, "image/png") {
		t.Errorf("assets: %v %d", assets.MimeTypes, assets.MaxSize)
	}
}
