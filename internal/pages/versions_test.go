package pages

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func versionCount(t testing.TB, app core.App, pageID string) int {
	t.Helper()
	n, err := app.CountRecords(VersionsCollection, dbx.HashExp{"page": pageID})
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestVersions(t *testing.T) {
	f := setup(t)
	p := f.page(t, map[string]any{"html": "<p>A</p>"})
	save := func(data map[string]any) {
		t.Helper()
		p.Load(data)
		if err := f.app.Save(p); err != nil {
			t.Fatal(err)
		}
		if err := snapshot(f.app, p, "me@test.com"); err != nil {
			t.Fatal(err)
		}
	}
	save(nil)
	save(nil)                                // same design: no new version
	save(map[string]any{"title": "Renamed"}) // settings only: no new version
	if n := versionCount(t, f.app, p.Id); n != 1 {
		t.Fatalf("expected 1 version, got %d", n)
	}
	save(map[string]any{"html": "<p>B</p>"})
	if n := versionCount(t, f.app, p.Id); n != 2 {
		t.Fatalf("expected 2 versions, got %d", n)
	}
	for i := range maxVersions + 5 {
		save(map[string]any{"css": strings.Repeat("x", i+1)})
	}
	if n := versionCount(t, f.app, p.Id); n != maxVersions {
		t.Fatalf("expected %d versions, got %d", maxVersions, n)
	}
	latest, _ := f.app.FindRecordsByFilter(VersionsCollection, "page = {:p}", "-@rowid", 1, 0, dbx.Params{"p": p.Id})
	if latest[0].GetString("css") != strings.Repeat("x", maxVersions+5) || latest[0].GetString("by") != "me@test.com" {
		t.Errorf("unexpected latest version %v", latest[0].PublicExport())
	}
	// the versions go with their page
	if err := f.app.Delete(p); err != nil {
		t.Fatal(err)
	}
	if n := versionCount(t, f.app, p.Id); n != 0 {
		t.Errorf("expected the versions to be deleted, got %d", n)
	}
}

func TestVersionsOnSave(t *testing.T) {
	const pageID = "pagesaved000001"
	var admin *core.Record
	headers := map[string]string{}
	(&tests.ApiScenario{
		Name:   "saving from the dashboard keeps a version",
		Method: http.MethodPatch,
		URL:    "/api/collections/pages/records/" + pageID,
		Body:   strings.NewReader(`{"html":"<p>New</p>"}`),
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f := setup(t)
			f.page(t, map[string]any{"id": pageID, "html": "<p>Old</p>"})
			admin = testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
			return f.app
		},
		Headers: headers,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			headers["Authorization"] = testutil.AuthToken(t, admin)
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"html":"<p>New</p>"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			list, err := app.FindRecordsByFilter(VersionsCollection, "page = {:p}", "", 0, 0, dbx.Params{"p": pageID})
			if err != nil || len(list) != 1 || list[0].GetString("html") != "<p>New</p>" || list[0].GetString("by") != "admin@test.com" {
				t.Errorf("expected one version by the superuser, got %v %v", len(list), err)
			}
		},
	}).Test(t)
}

func TestVersionPreview(t *testing.T) {
	f := setup(t)
	draft := f.page(t, map[string]any{"slug": "draft", "published": false, "html": "<p>Old</p>"})
	if err := snapshot(f.app, draft, ""); err != nil {
		t.Fatal(err)
	}
	old, _ := f.app.FindFirstRecordByFilter(VersionsCollection, "page = {:p}", dbx.Params{"p": draft.Id})
	other := f.page(t, map[string]any{"slug": "other", "html": "<p>Other</p>"})
	if err := snapshot(f.app, other, ""); err != nil {
		t.Fatal(err)
	}
	otherVersion, _ := f.app.FindFirstRecordByFilter(VersionsCollection, "page = {:p}", dbx.Params{"p": other.Id})

	future := "9999999999"
	token := future + "." + previewSig(f.app, draft.Id, old.Id, future)
	if !validPreview(f.app, draft, token, old.Id) {
		t.Error("expected a valid version token")
	}
	if validPreview(f.app, draft, token, "") || validPreview(f.app, draft, future+"."+previewSig(f.app, draft.Id, "", future), old.Id) {
		t.Error("a token is bound to its version")
	}
	p, err := withVersion(f.app, draft, old.Id)
	if err != nil || p.GetString("html") != "<p>Old</p>" || draft.GetString("html") != "<p>Old</p>" {
		t.Errorf("unexpected version page %v", err)
	}
	if _, err := withVersion(f.app, draft, otherVersion.Id); err == nil {
		t.Error("a version of another page must be refused")
	}
}

func TestDuplicate(t *testing.T) {
	const srcID = "pagesource00001"
	var admin *core.Record
	headers := map[string]string{}
	(&tests.ApiScenario{
		Name:   "duplicate a page with its files",
		Method: http.MethodPost,
		URL:    "/api/pages/" + srcID + "/duplicate",
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f := setup(t)
			img, _ := filesystem.NewFileFromBytes([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), "photo.gif")
			src := f.page(t, map[string]any{"id": srcID, "slug": "shop", "home": true, "assets": []*filesystem.File{img}})
			name := src.GetStringSlice("assets")[0]
			src.Set("html", `<img src="/p/_assets/`+srcID+`/`+name+`">`)
			src.Set("css", `.x{background:url(/p/_assets/`+srcID+`/`+name+`)}`)
			if err := f.app.Save(src); err != nil {
				t.Fatal(err)
			}
			f.page(t, map[string]any{"slug": "shop-copy", "published": false}) // taken: shop-copy-2
			admin = testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
			return f.app
		},
		Headers: headers,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			headers["Authorization"] = testutil.AuthToken(t, admin)
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"id":`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			var out struct{ ID string }
			json.NewDecoder(res.Body).Decode(&out)
			dst, err := app.FindRecordById(Collection, out.ID)
			if err != nil {
				t.Fatal(err)
			}
			assets := dst.GetStringSlice("assets")
			if dst.GetString("slug") != "shop-copy-2" || dst.GetBool("published") || dst.GetBool("home") || dst.GetString("title") != "Shop (copy)" || len(assets) != 1 {
				t.Fatalf("unexpected copy %v", dst.PublicExport())
			}
			want := "/p/_assets/" + dst.Id + "/" + assets[0]
			if !strings.Contains(dst.GetString("html"), want) || !strings.Contains(dst.GetString("css"), want) || strings.Contains(dst.GetString("html"), srcID) {
				t.Errorf("the copy must use its own files: %s %s", dst.GetString("html"), dst.GetString("css"))
			}
			fsys, _ := app.NewFilesystem()
			defer fsys.Close()
			if ok, _ := fsys.Exists(dst.BaseFilesPath() + "/" + assets[0]); !ok {
				t.Error("expected the copied file")
			}
			if versionCount(t, app, dst.Id) != 1 {
				t.Error("expected the first version of the copy")
			}
		},
	}).Test(t)

	(&tests.ApiScenario{
		Name: "guests can't duplicate", Method: http.MethodPost, URL: "/api/pages/" + srcID + "/duplicate",
		TestAppFactory: func(t testing.TB) *tests.TestApp { return setup(t).app },
		ExpectedStatus: 401, ExpectedContent: []string{`"data":{}`},
	}).Test(t)
}

func TestParts(t *testing.T) {
	f := setup(t)
	f.page(t, map[string]any{"slug": "header", "kind": kindPart, "published": false, "css": ".x{color:red}",
		"html": `<body><nav class="x"><a data-pb-field="name"></a><div data-pb-part="header"></div></nav></body>`})

	src := `<div data-pb-part="header" id="p1"></div><p>Body</p><div data-pb-part="header"></div><div data-pb-part="nope"></div>`
	out, _, err := render(t.Context(), f.app, pageView{HTML: src, Scope: scope{col: f.products, rec: f.lamp}}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, `<nav class="x"><a>Lamp &lt;b&gt;</a></nav>`) != 2 || !strings.Contains(out, "<p>Body</p>") {
		t.Errorf("expected the part twice, with the record of the page\n%s", out)
	}
	if strings.Count(out, ".x{color:red}") != 1 || strings.Contains(out, "data-pb-part") || strings.Contains(out, "Unknown site part") {
		t.Errorf("unexpected parts output\n%s", out)
	}
	out, _, _ = render(t.Context(), f.app, pageView{HTML: src}, nil, true)
	if !strings.Contains(out, "Unknown site part: nope") {
		t.Errorf("expected a hint in the preview\n%s", out)
	}
	// the Basecoat script is loaded for a sidebar in a part
	f.page(t, map[string]any{"slug": "side", "kind": kindPart, "published": false, "html": `<aside class="sidebar"><nav></nav></aside>`})
	th := theme{Style: "vega", Mode: "light"}
	if _, script, _ := render(t.Context(), f.app, pageView{HTML: `<div data-pb-part="side"></div><main></main>`, Kit: &th}, nil, false); !script {
		t.Error("expected the Basecoat script for the sidebar of a part")
	}

	// parts are never pages
	for _, data := range []map[string]any{{"published": true}, {"home": true}, {"record_collection": "products"}} {
		col, _ := f.app.FindCollectionByNameOrId(Collection)
		r := core.NewRecord(col)
		r.Load(map[string]any{"title": "x", "slug": "part-x", "kind": kindPart})
		r.Load(data)
		if err := f.app.Save(r); err == nil {
			t.Errorf("%v: expected a validation error", data)
		}
	}
	(&tests.ApiScenario{Name: "parts aren't served", Method: http.MethodGet, URL: "/header",
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f := setup(t)
			f.page(t, map[string]any{"slug": "header", "kind": kindPart, "published": false})
			return f.app
		},
		ExpectedStatus: 404, ExpectedContent: []string{"Page not found"},
	}).Test(t)
}
