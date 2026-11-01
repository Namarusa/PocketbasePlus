package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/Namarusa/PocketbasePlus/internal/assistant"
	"github.com/Namarusa/PocketbasePlus/internal/documents"
	"github.com/Namarusa/PocketbasePlus/internal/medialib"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func pngBytes(t testing.TB, w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func upload(t testing.TB, app core.App, name string, raw []byte, data map[string]any) (*core.Record, error) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(medialib.Collection)
	if err != nil {
		t.Fatal(err)
	}
	f, err := filesystem.NewFileFromBytes(raw, name)
	if err != nil {
		t.Fatal(err)
	}
	rec := core.NewRecord(col)
	rec.Load(data)
	rec.Set("file", f)
	return rec, app.Save(rec)
}

func setup(t *testing.T) (core.App, func(method, path string, auth bool) *httptest.ResponseRecorder) {
	app := testutil.NewApp(t)
	Bind(app)
	su := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	r, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	var mux http.Handler
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: r}, func(e *core.ServeEvent) error {
		mux, err = e.Router.BuildMux()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return app, func(method, path string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if auth {
			req.Header.Set("Authorization", testutil.AuthToken(t, su))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
}

func TestLibrary(t *testing.T) {
	app, do := setup(t)

	// an image: its name, type and size are read
	logo, err := upload(t, app, "Logo Été.png", pngBytes(t, 400, 200, color.RGBA{200, 0, 0, 255}), map[string]any{"folder": "  Brand   kit "})
	if err != nil {
		t.Fatal(err)
	}
	if logo.GetString("name") != "Logo Été" || logo.GetString("kind") != "image" || logo.GetString("mime") != "image/png" ||
		logo.GetInt("width") != 400 || logo.GetInt("height") != 200 || logo.GetInt("size") == 0 || logo.GetString("folder") != "Brand kit" {
		t.Errorf("image: %v", logo.PublicExport())
	}
	// an SVG (its size from its viewBox), a PDF, a font
	svg, err := upload(t, app, "icon.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 24"><script>alert(1)</script></svg>`), nil)
	if err != nil || svg.GetString("kind") != "image" || svg.GetInt("width") != 48 || svg.GetInt("height") != 24 {
		t.Errorf("svg: %v %v", err, svg.PublicExport())
	}
	pdf, err := upload(t, app, "guide.pdf", []byte("%PDF-1.4\n%%EOF\n"), map[string]any{"name": "User guide"})
	if err != nil || pdf.GetString("kind") != "pdf" || pdf.GetString("name") != "User guide" {
		t.Errorf("pdf: %v %v", err, pdf.PublicExport())
	}
	// HTML is refused (it would run in the app's pages)
	if _, err := upload(t, app, "page.html", []byte("<!doctype html><html><script>alert(1)</script></html>"), nil); err == nil {
		t.Error("an HTML file was taken")
	}

	// the public address: the file, sandboxed
	link := medialib.Link(logo.Id, "whatever.png")
	res := do("GET", link, false)
	if res.Code != 200 || res.Header().Get("Content-Type") != "image/png" || !strings.Contains(res.Header().Get("Content-Security-Policy"), "sandbox") ||
		!strings.Contains(res.Header().Get("Cache-Control"), "max-age=300") || res.Header().Get("ETag") == "" {
		t.Fatalf("serve: %d %v", res.Code, res.Header())
	}
	if !strings.Contains(res.Header().Get("Content-Disposition"), `"logo-ete.png"`) {
		t.Errorf("served name: %s", res.Header().Get("Content-Disposition"))
	}
	if again := do("GET", link, false); again.Code != 200 || !bytes.Equal(again.Body.Bytes(), res.Body.Bytes()) {
		t.Error("serve again")
	}
	// a narrower copy: the next width of the list
	small := do("GET", link+"?w=300", false)
	if cfg, _, err := image.DecodeConfig(small.Body); small.Code != 200 || err != nil || cfg.Width != 320 || cfg.Height != 160 {
		t.Errorf("resized: %d %v %+v", small.Code, err, cfg)
	}
	// wider than the image: the image itself
	if big := do("GET", link+"?w=1920", false); !bytes.Equal(big.Body.Bytes(), res.Body.Bytes()) {
		t.Error("an image isn't made wider")
	}
	if dl := do("GET", link+"?download=1", false); !strings.HasPrefix(dl.Header().Get("Content-Disposition"), "attachment;") {
		t.Errorf("download: %v", dl.Header())
	}
	if pdfRes := do("GET", medialib.Link(pdf.Id, "guide.pdf"), false); pdfRes.Code != 200 || strings.Contains(pdfRes.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("a PDF is shown by the browsers: %d %v", pdfRes.Code, pdfRes.Header())
	}
	for _, bad := range []string{"/media/nope/x.png", "/media/aaaaaaaaaaaaaaa/x.png"} {
		if res := do("GET", bad, false); res.Code != 404 {
			t.Errorf("%s: %d", bad, res.Code)
		}
	}
	// the list is for superusers only
	if res := do("GET", "/api/collections/"+medialib.Collection+"/records", false); res.Code == 200 && strings.Contains(res.Body.String(), logo.Id) {
		t.Error("the library is listed to guests")
	}

	// replaced: the same address, the new file
	f, _ := filesystem.NewFileFromBytes(pngBytes(t, 50, 50, color.RGBA{0, 0, 200, 255}), "new.png")
	logo.Set("file", f)
	if err := app.Save(logo); err != nil {
		t.Fatal(err)
	}
	if logo.GetInt("width") != 50 || logo.GetString("name") != "Logo Été" {
		t.Errorf("replaced: %v", logo.PublicExport())
	}
	if res := do("GET", link, false); res.Code != 200 || bytes.Equal(res.Body.Bytes(), small.Body.Bytes()) {
		t.Errorf("replaced: %d", res.Code)
	} else if cfg, _, _ := image.DecodeConfig(res.Body); cfg.Width != 50 {
		t.Errorf("replaced: still the old file (%d px)", cfg.Width)
	}

	// where a file is used
	posts := testutil.CreateCollection(t, app, "posts", &core.TextField{Name: "title"}, &core.EditorField{Name: "body"})
	_ = posts
	post := testutil.Create(t, app, "posts", map[string]any{"title": "Hello", "body": `<p><img src="` + link + `"></p>`})
	testutil.Create(t, app, "posts", map[string]any{"title": "Other", "body": `<p>none</p>`})
	res = do("GET", "/api/media/usages/"+logo.Id, true)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"id":"`+post.Id+`"`) || !strings.Contains(res.Body.String(), `"label":"Hello"`) ||
		!strings.Contains(res.Body.String(), `"field":"body"`) || strings.Count(res.Body.String(), `"collection"`) != 1 {
		t.Errorf("usages: %d %s", res.Code, res.Body.String())
	}
	if res := do("GET", "/api/media/usages/"+logo.Id, false); res.Code != 401 {
		t.Errorf("usages without auth: %d", res.Code)
	}

	// the assistant lists them, with their address
	got, err := toolListAssets(&assistant.ToolContext{App: app}, map[string]any{"kind": "image", "search": "logo"})
	files, _ := got.(map[string]any)["files"].([]map[string]any)
	if err != nil || len(files) != 1 || files[0]["address"] != medialib.Link(logo.Id, "logo-ete.png") || files[0]["width"] != 50 || files[0]["folder"] != "Brand kit" {
		t.Errorf("list_assets: %v %v", err, got)
	}

	// the documents read the images of the library
	spec := &documents.Spec{Body: []documents.Block{{ID: "i", Type: "image", URL: "{{values.logo}}", Width: 30}}}
	for _, src := range []string{link, "https://app.example.com" + link} {
		app.Settings().Meta.AppURL = "https://app.example.com"
		out, err := documents.Make(context.Background(), app, documents.Input{Spec: spec, Name: "x", Values: map[string]any{"logo": src}})
		if err != nil || len(out.Errors) > 0 || !bytes.Contains(out.PDF, []byte("/Subtype /Image")) {
			t.Errorf("document image %s: %v %v", src, err, out)
		}
	}
}

func TestLinks(t *testing.T) {
	app := testutil.NewApp(t)
	app.Settings().Meta.AppURL = "https://app.example.com"
	for in, want := range map[string]string{
		"/media/abcdefghijklmno/logo.png":                         "abcdefghijklmno",
		"/media/abcdefghijklmno/logo.png?w=640":                   "abcdefghijklmno",
		"https://APP.example.com/media/abcdefghijklmno/logo.png":  "abcdefghijklmno",
		"https://evil.example.com/media/abcdefghijklmno/logo.png": "",
		"/media/short/logo.png":                                   "",
		"/api/files/x/y/z.png":                                    "",
	} {
		if got, ok := medialib.ID(app, in); got != want || ok != (want != "") {
			t.Errorf("%s: %q %v", in, got, ok)
		}
	}
	if l := medialib.Link("abcdefghijklmno", "a b.png"); l != "/media/abcdefghijklmno/a%20b.png" {
		t.Error(l)
	}
	if n := linkName("Logo  Été / dark!", "x_abc.PNG"); n != "logo-ete-dark.png" {
		t.Error(n)
	}
}
