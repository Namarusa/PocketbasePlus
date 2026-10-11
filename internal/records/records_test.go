package records_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/xuri/excelize/v2"

	"github.com/Namarusa/PocketbasePlus/internal/records"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app          *tests.TestApp
	admin, other *core.Record
	user         *core.Record
	items        []*core.Record
}

func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	records.Register(app)
	f := &fixture{app: app}
	testutil.CreateCollection(t, app, "products",
		&core.TextField{Name: "sku", Required: true},
		&core.TextField{Name: "name"},
		&core.NumberField{Name: "price"},
		&core.BoolField{Name: "active"},
		&core.SelectField{Name: "tags", MaxSelect: 3, Values: []string{"new", "sale", "eco"}},
	)
	products, _ := app.FindCollectionByNameOrId("products")
	products.AddIndex("idx_products_sku", true, "sku", "")
	if err := app.Save(products); err != nil {
		t.Fatal(err)
	}
	f.admin = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	f.other = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "other@test.com", "password": "1234567890"})
	f.user = testutil.Create(t, app, "users", map[string]any{"email": "user@test.com", "password": "1234567890"})
	for _, p := range []map[string]any{
		{"sku": "A1", "name": "Chair", "price": 49.9, "active": true, "tags": []string{"new", "eco"}},
		{"sku": "B2", "name": "Desk", "price": 199, "active": false},
		{"sku": "C3", "name": "Lamp", "price": 25, "active": true},
	} {
		f.items = append(f.items, testutil.Create(t, app, "products", p))
	}
	return f
}

type request struct {
	name        string
	method      string
	who         func(f *fixture) *core.Record
	url         func(f *fixture) string
	body        func(f *fixture) (io.Reader, string) // body + content type
	status      int
	expected    []string
	notExpected []string
	after       func(t testing.TB, f *fixture, res *http.Response)
}

func (r request) run(t *testing.T) {
	var f *fixture
	headers := map[string]string{}
	scenario := &tests.ApiScenario{
		Name:               r.name,
		Method:             r.method,
		Headers:            headers,
		ExpectedStatus:     r.status,
		ExpectedContent:    r.expected,
		NotExpectedContent: r.notExpected,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			return f.app
		},
	}
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		scenario.URL = r.url(f)
		who := r.who
		if who == nil {
			who = func(f *fixture) *core.Record { return f.admin }
		}
		headers["Authorization"] = testutil.AuthToken(t, who(f))
		if r.body != nil {
			body, contentType := r.body(f)
			scenario.Body = body
			headers["Content-Type"] = contentType
		}
	}
	if r.after != nil {
		scenario.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) { r.after(t, f, res) }
	}
	scenario.Test(t)
}

func static(url string) func(*fixture) string { return func(*fixture) string { return url } }

func upload(filename, content string, options map[string]any) func(*fixture) (io.Reader, string) {
	return func(*fixture) (io.Reader, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("file", filename)
		fw.Write([]byte(content))
		if options != nil {
			raw, _ := json.Marshal(options)
			w.WriteField("options", string(raw))
		}
		w.Close()
		return &buf, w.FormDataContentType()
	}
}

func jsonBody(v any) func(*fixture) (io.Reader, string) {
	return func(*fixture) (io.Reader, string) {
		raw, _ := json.Marshal(v)
		return bytes.NewReader(raw), "application/json"
	}
}

func TestExport(t *testing.T) {
	request{
		name: "csv with filter and fields", method: http.MethodGet,
		url:    static("/api/gp/export/products?format=csv&filter=active%3Dtrue&sort=sku&fields=sku,price,tags"),
		status: 200, expected: []string{"sku,price,tags", "A1,49.9,new;eco", "C3,25,"}, notExpected: []string{"B2", "Desk"},
	}.run(t)

	request{
		name: "json", method: http.MethodGet,
		url:    static("/api/gp/export/products?format=json&sort=-price"),
		status: 200, expected: []string{`"sku":"B2"`, `"tags":["new","eco"]`},
	}.run(t)

	request{
		name: "xlsx", method: http.MethodGet,
		url:    static("/api/gp/export/products?format=xlsx&sort=sku"),
		status: 200, expected: []string{"xl/workbook.xml"},
		after: func(t testing.TB, f *fixture, res *http.Response) {
			x, err := excelize.OpenReader(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := x.GetRows("products")
			if len(rows) != 4 || rows[1][1] != "A1" {
				t.Fatalf("unexpected xlsx rows %v", rows)
			}
		},
	}.run(t)

	request{name: "invalid filter", method: http.MethodGet, url: static("/api/gp/export/products?filter=nope%3D1"), status: 400, expected: []string{"Invalid filter"}}.run(t)
	request{name: "users can't export", method: http.MethodGet, who: func(f *fixture) *core.Record { return f.user }, url: static("/api/gp/export/products"), status: 403, expected: []string{`"data":{}`}}.run(t)
	request{name: "internal collections can't be exported", method: http.MethodGet, url: static("/api/gp/export/_superusers"), status: 404, expected: []string{`"data":{}`}}.run(t)
}

func countProducts(t testing.TB, app core.App) int {
	n, err := app.CountRecords("products")
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestImport(t *testing.T) {
	csvFile := "sku;name;price;active;tags\nD4;Sofa;899,5;yes;new|sale\nE5;Shelf;59;no;\n"

	request{
		name: "preview suggests the mapping", method: http.MethodPost,
		url: static("/api/gp/import/products/preview"), body: upload("items.csv", csvFile, nil),
		status: 200, expected: []string{`"total":2`, `"columns":["sku","name","price","active","tags"]`, `"price":"price"`},
	}.run(t)

	request{
		name: "dry run saves nothing", method: http.MethodPost,
		url: static("/api/gp/import/products"), body: upload("items.csv", csvFile, map[string]any{"dryRun": true}),
		status: 200, expected: []string{`"ok":true`, `"created":2`, `"saved":false`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			if n := countProducts(t, f.app); n != 3 {
				t.Fatalf("dry run must not save, got %d products", n)
			}
		},
	}.run(t)

	request{
		name: "import creates the records with typed values", method: http.MethodPost,
		url: static("/api/gp/import/products"), body: upload("items.csv", csvFile, nil),
		status: 200, expected: []string{`"saved":true`, `"created":2`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			sofa, err := f.app.FindFirstRecordByData("products", "sku", "D4")
			if err != nil || sofa.GetFloat("price") != 899.5 || !sofa.GetBool("active") || len(sofa.GetStringSlice("tags")) != 2 {
				t.Fatalf("unexpected imported record %v (%v)", sofa, err)
			}
		},
	}.run(t)

	request{
		name: "one invalid row rolls everything back", method: http.MethodPost,
		url: static("/api/gp/import/products"), body: upload("items.csv", "sku,name,price\nF6,Good,10\nA1,Duplicate sku,5\n,Missing sku,1\n", nil),
		status: 200, expected: []string{`"ok":false`, `"saved":false`, `"row":3`, `"row":4`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			if n := countProducts(t, f.app); n != 3 {
				t.Fatalf("expected the rollback, got %d products", n)
			}
		},
	}.run(t)

	request{
		name: "upsert by sku updates and creates", method: http.MethodPost,
		url: static("/api/gp/import/products"),
		body: upload("items.json", `[{"sku":"A1","price":55},{"sku":"G7","name":"Rug","price":120}]`, map[string]any{
			"mode": "upsert", "matchField": "sku", "mapping": map[string]string{"sku": "sku", "name": "name", "price": "price"},
		}),
		status: 200, expected: []string{`"updated":1`, `"created":1`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			chair, _ := f.app.FindFirstRecordByData("products", "sku", "A1")
			if chair.GetFloat("price") != 55 || chair.GetString("name") != "Chair" {
				t.Fatalf("expected only the price to change, got %v", chair.FieldsData())
			}
		},
	}.run(t)
}

func TestBulkUpdate(t *testing.T) {
	request{
		name: "bulk update", method: http.MethodPatch, url: static("/api/gp/bulk/products"),
		body: func(f *fixture) (io.Reader, string) {
			return jsonBody(map[string]any{"ids": []string{f.items[0].Id, f.items[1].Id}, "data": map[string]any{"active": false, "price": 10}})(f)
		},
		status: 200, expected: []string{`"updated":2`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			list, _ := f.app.FindRecordsByFilter("products", "price = 10 && active = false", "", 0, 0)
			if len(list) != 2 {
				t.Fatalf("expected 2 updated records, got %d", len(list))
			}
		},
	}.run(t)

	request{
		name: "an invalid value rolls back", method: http.MethodPatch, url: static("/api/gp/bulk/products"),
		body: func(f *fixture) (io.Reader, string) {
			return jsonBody(map[string]any{"ids": []string{f.items[0].Id, f.items[1].Id}, "data": map[string]any{"sku": "SAME"}})(f)
		},
		status: 400, expected: []string{"Failed to update"},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			if list, _ := f.app.FindRecordsByFilter("products", "sku = 'SAME'", "", 0, 0); len(list) != 0 {
				t.Fatal("expected the rollback")
			}
		},
	}.run(t)

	request{name: "id can't be bulk edited", method: http.MethodPatch, url: static("/api/gp/bulk/products"),
		body: jsonBody(map[string]any{"ids": []string{"x"}, "data": map[string]any{"id": "y"}}), status: 400, expected: []string{"can't be bulk edited"}}.run(t)
	request{name: "users can't bulk edit", method: http.MethodPatch, who: func(f *fixture) *core.Record { return f.user }, url: static("/api/gp/bulk/products"),
		body: jsonBody(map[string]any{"ids": []string{"x"}, "data": map[string]any{"name": "y"}}), status: 403, expected: []string{`"data":{}`}}.run(t)
}

func TestSavedViewsOwnership(t *testing.T) {
	request{
		name: "the owner is the creator", method: http.MethodPost, url: static("/api/collections/saved_views/records"),
		body:   jsonBody(map[string]any{"name": "Active", "collection": "products", "query": map[string]any{"filter": "active = true"}, "owner": "spoofed", "shared": true}),
		status: 200, expected: []string{`"name":"Active"`},
		after: func(t testing.TB, f *fixture, res *http.Response) {
			var v map[string]any
			json.NewDecoder(res.Body).Decode(&v)
			if v["owner"] != f.admin.Id {
				t.Fatalf("expected the creator as owner, got %v", v["owner"])
			}
		},
	}.run(t)

	request{
		name: "others can't change a shared view", method: http.MethodPatch, who: func(f *fixture) *core.Record { return f.other },
		url: func(f *fixture) string {
			v := testutil.Create(t, f.app, "saved_views", map[string]any{"name": "Mine", "collection": "products", "owner": f.admin.Id, "shared": true})
			return "/api/collections/saved_views/records/" + v.Id
		},
		body: jsonBody(map[string]any{"name": "Hacked"}), status: 403, expected: []string{"Only the owner"},
	}.run(t)
}
