package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/httpcall"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/pdfview"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app      *tests.TestApp
	engine   *flows.Engine
	mail     *mail.Engine
	order    *core.Record
	customer *core.Record
}

func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	m := mail.Register(app)
	m.DisableKick = true
	f := &fixture{app: app, mail: m, engine: flows.Register(app, m)}
	f.engine.Sync = true
	t.Cleanup(func() { f.engine.Wait(time.Minute) })
	Bind(app)

	customers := testutil.CreateCollection(t, app, "customers", &core.TextField{Name: "name", Presentable: true}, &core.EmailField{Name: "email"})
	orders := testutil.CreateCollection(t, app, "orders",
		&core.TextField{Name: "number", Presentable: true},
		&core.RelationField{Name: "customer", CollectionId: customers.Id, MaxSelect: 1},
		&core.NumberField{Name: "total"},
		&core.BoolField{Name: "paid"},
		&core.FileField{Name: "invoice", MaxSelect: 1, MaxSize: 10 << 20},
	)
	testutil.CreateCollection(t, app, "order_items",
		&core.RelationField{Name: "order", CollectionId: orders.Id, MaxSelect: 1},
		&core.TextField{Name: "name", Presentable: true},
		&core.NumberField{Name: "price"},
		&core.NumberField{Name: "quantity"},
	)
	f.customer = testutil.Create(t, app, "customers", map[string]any{"name": "Ada Lovelace", "email": "ada@test.com"})
	f.order = testutil.Create(t, app, "orders", map[string]any{"number": "F-001", "customer": f.customer.Id, "total": 37.5})
	for i, it := range []struct {
		name  string
		price float64
	}{{"Chaise é", 12.5}, {"Lampe", 25}} {
		testutil.Create(t, app, "order_items", map[string]any{"order": f.order.Id, "name": it.name, "price": it.price, "quantity": i + 1})
	}
	return f
}

func (f *fixture) template(t testing.TB, data map[string]any) *core.Record {
	base := map[string]any{
		"name": "Invoice", "slug": "invoice", "format": "a4", "orientation": "portrait", "collection": "orders",
		"lists":  []List{{Name: "items", Collection: "order_items", Filter: "order = {{record.id}}", Sort: "name"}},
		"footer": []Block{{ID: "f", Type: "text", Text: "Page {{page}} / {{pages}}", Align: "center"}},
		"body": []Block{
			{ID: "h", Type: "heading", Level: 1, Text: "Invoice {{record.number}}"},
			{ID: "c", Type: "text", Text: "**{{record.customer}}** · {{record.customer.email}}"},
			{ID: "paid", Type: "text", Text: "PAID", ShowIf: "{{record.paid}}"},
			{ID: "t", Type: "table", Items: "{{lists.items}}", Cells: []TableCol{
				{Header: "Item", Value: "{{index}}. {{row}}"},
				{Header: "Amount", Value: `{{currency "EUR" (multiply row.price row.quantity)}}`, Align: "right"},
			}, Totals: []Row{{Label: "Total", Value: `{{currency "EUR" record.total}}`}}},
		},
		"filename": "invoice {{record.number}}",
	}
	for k, v := range data {
		base[k] = v
	}
	return testutil.Create(t, f.app, TemplatesCollection, base)
}

// pdfText returns the texts drawn in an uncompressed PDF (the UTF-16 strings of the Tj operators).
func pdfText(pdf []byte) string {
	var out []string
	for _, m := range regexp.MustCompile(`\(((?:\\.|[^\\)])*)\)Tj`).FindAllSubmatch(pdf, -1) {
		s := m[1]
		s = bytes.ReplaceAll(s, []byte(`\(`), []byte("("))
		s = bytes.ReplaceAll(s, []byte(`\)`), []byte(")"))
		s = bytes.ReplaceAll(s, []byte(`\\`), []byte(`\`))
		// UTF-16BE: drop the high bytes of the Latin characters
		var b strings.Builder
		for i := 0; i+1 < len(s); i += 2 {
			r := rune(s[i])<<8 | rune(s[i+1])
			b.WriteRune(r)
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "|")
}

func (f *fixture) make(t testing.TB, tpl *core.Record, rec *core.Record, values map[string]any) *Result {
	t.Helper()
	spec, err := specOf(tpl)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Make(context.Background(), f.app, Input{Template: tpl, Spec: spec, Name: tpl.GetString("slug"), Record: rec, Values: values})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestMakeDocument(t *testing.T) {
	f := setup(t)
	tpl := f.template(t, nil)
	res := f.make(t, tpl, f.order, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	text := pdfText(res.PDF)
	for _, want := range []string{"Invoice F-001", "Ada Lovelace", "ada@test.com", "1. Chaise é", "2. Lampe", "€12.50", "€50.00", "€37.50", "Page 1 / 1"} {
		if !strings.Contains(strings.ReplaceAll(text, "|", ""), want) && !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "PAID") {
		t.Errorf("the hidden block is shown")
	}
	if res.Filename != "invoice F-001.pdf" || res.Pages != 1 {
		t.Errorf("file %q, %d pages", res.Filename, res.Pages)
	}

	// formula errors are reported on their block
	tpl.Set("body", []Block{{ID: "bad", Type: "text", Text: "{{divide 1 0}}"}})
	res = f.make(t, tpl, f.order, nil)
	if len(res.Errors) != 1 || res.Errors[0].Block != "bad" || !strings.Contains(res.Errors[0].Error, "division") {
		t.Errorf("errors: %v", res.Errors)
	}
}

func TestLongTableAndFormats(t *testing.T) {
	f := setup(t)
	items := []any{}
	for i := range 120 {
		items = append(items, map[string]any{"name": fmt.Sprintf("Line %d", i+1)})
	}
	tpl := f.template(t, map[string]any{
		"slug": "long", "format": "a5", "orientation": "landscape", "collection": "", "lists": []List{},
		"body": []Block{{ID: "t", Type: "table", Items: "{{values.lines}}", Zebra: true, Cells: []TableCol{{Header: "Description", Value: "{{row.name}}"}}}},
	})
	res := f.make(t, tpl, nil, map[string]any{"lines": items})
	if res.Pages < 3 {
		t.Fatalf("pages: %d", res.Pages)
	}
	text := pdfText(res.PDF)
	if n := strings.Count(text, "Description"); n != res.Pages {
		t.Errorf("the header on each page: %d headers for %d pages", n, res.Pages)
	}
	if flat := strings.ReplaceAll(text, "|", ""); !strings.Contains(flat, "Line 120") || !strings.Contains(flat, fmt.Sprintf("Page %d / %d", res.Pages, res.Pages)) {
		t.Errorf("last line or page number missing")
	}
	// A5 landscape: 210 x 148 mm
	if !regexp.MustCompile(`/MediaBox \[0 0 595\.2\d+ 419\.5\d+\]`).Match(res.PDF) {
		t.Errorf("page size: %s", regexp.MustCompile(`/MediaBox \[[^\]]*\]`).Find(res.PDF))
	}
}

func TestValidation(t *testing.T) {
	f := setup(t)
	col, _ := f.app.FindCollectionByNameOrId(TemplatesCollection)
	for name, data := range map[string]map[string]any{
		"unknown block":  {"body": []Block{{Type: "video"}}},
		"bad formula":    {"body": []Block{{Type: "text", Text: "{{if}}"}}},
		"secret":         {"body": []Block{{Type: "text", Text: "{{secret.KEY}}"}}},
		"bad list":       {"lists": []List{{Name: "x", Collection: "nope"}}},
		"bad format":     {"format": "b7"},
		"custom too big": {"format": "custom", "width_mm": 5000, "height_mm": 100},
	} {
		rec := core.NewRecord(col)
		rec.Load(map[string]any{"name": "x", "slug": "x-" + strings.ReplaceAll(name, " ", "-"), "format": "a4"})
		rec.Load(data)
		if err := f.app.Save(rec); err == nil {
			t.Errorf("%s: saved", name)
		} else if name == "secret" && !strings.Contains(err.Error(), "secrets") {
			t.Errorf("secret: %v", err)
		}
	}
}

func TestFlowSteps(t *testing.T) {
	f := setup(t)
	tpl := f.template(t, nil)
	flow := testutil.Create(t, f.app, "flows", map[string]any{
		"name": "invoice", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{
			{"key": "invoice", "type": "generate_document", "config": map[string]any{"template": tpl.Id, "record": "{{.trigger.payload.order}}"}},
			{"key": "mail", "type": "send_email", "config": map[string]any{"template": "", "to": "ada@test.com", "attachments": []any{"{{.steps.invoice}}"}}},
			{"key": "save", "type": "update_record", "config": map[string]any{"collection": "orders", "recordId": "{{.trigger.payload.order}}", "data": map[string]any{"invoice": "{{.steps.invoice}}"}}},
		},
	})
	// the email template of the step
	emailTpl := testutil.Create(t, f.app, "email_templates", map[string]any{"name": "Invoice", "slug": "invoice-mail", "subject": "Your invoice", "html": "<p>Attached.</p>"})
	steps := flow.Get("steps")
	raw, _ := jsonMarshal(steps)
	flow.Set("steps", strings.Replace(string(raw), `"template":""`, `"template":"`+emailTpl.Id+`"`, 1))
	if err := f.app.Save(flow); err != nil {
		t.Fatal(err)
	}

	res := f.engine.Run(flow, map[string]any{"payload": map[string]any{"order": f.order.Id}}, flows.RunOptions{})
	if res.Status != "success" {
		t.Fatalf("run: %s %s", res.Status, res.Error)
	}
	docs, _ := f.app.FindAllRecords(DocumentsCollection)
	if len(docs) != 1 || docs[0].GetString("record_id") != f.order.Id || docs[0].GetInt("pages") != 1 || !strings.HasPrefix(docs[0].GetString("source"), "Flow: ") {
		t.Fatalf("documents: %v", docs)
	}

	// the email carries the PDF
	f.mail.ProcessQueue()
	msg := f.app.TestMailer.LastMessage()
	if len(msg.Attachments) != 1 || msg.Attachments["invoice F-001.pdf"] == nil {
		t.Fatalf("attachments: %v", msg.Attachments)
	}
	pdf, _ := io.ReadAll(msg.Attachments["invoice F-001.pdf"])
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("not a PDF")
	}

	// the order keeps it in its file field
	order, _ := f.app.FindRecordById("orders", f.order.Id)
	if name := order.GetString("invoice"); !strings.HasSuffix(name, ".pdf") {
		t.Errorf("file field: %q", name)
	}

	// a deleted template: the flow is refused; a used one can't be deleted
	if err := f.app.Delete(tpl); err == nil || !strings.Contains(err.Error(), "used by the flows") {
		t.Errorf("delete a used template: %v", err)
	}
}

func TestRespondDocumentAndRoutes(t *testing.T) {
	f := setup(t)
	tpl := f.template(t, nil)
	testutil.Create(t, f.app, "flows", map[string]any{
		"name": "pdf", "active": true, "trigger_type": "webhook",
		"trigger": map[string]any{"method": "GET", "wait": true, "access": "public", "slug": "invoice-pdf"},
		"steps": []map[string]any{
			{"key": "doc", "type": "generate_document", "config": map[string]any{"template": tpl.Id, "record": "{{.trigger.query.order}}"}},
			{"key": "reply", "type": "respond", "config": map[string]any{"format": "document", "body": "{{.steps.doc}}", "download": true}},
		},
	})
	su := testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	r, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatal(err)
	}
	var mux http.Handler
	if err := f.app.OnServe().Trigger(&core.ServeEvent{App: f.app, Router: r}, func(e *core.ServeEvent) error {
		mux, err = e.Router.BuildMux()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	do := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.Header.Set("Authorization", testutil.AuthToken(t, su))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	res := do("GET", "/hooks/invoice-pdf?order="+f.order.Id, "", false)
	if res.Code != 200 || res.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(res.Header().Get("Content-Disposition"), "attachment;") || !bytes.HasPrefix(res.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("webhook: %d %v %.200s", res.Code, res.Header(), res.Body.String())
	}

	// the preview of an unsaved template, read by the editor's frame
	res = do("POST", "/api/documents/preview", `{"name":"x","record":"orders","spec":{"format":"letter","body":[{"id":"a","type":"text","text":"Hi {{record.number}} {{divide 1 0}}"}]}}`, true)
	var out struct {
		URL    string       `json:"url"`
		Pages  int          `json:"pages"`
		Errors []BlockError `json:"errors"`
	}
	if err := jsonUnmarshal(res.Body.Bytes(), &out); err != nil || out.URL == "" || out.Pages != 1 || len(out.Errors) != 1 {
		t.Fatalf("preview: %s", res.Body.String())
	}
	if res := do("GET", out.URL, "", false); res.Code != 200 || !bytes.HasPrefix(res.Body.Bytes(), []byte("%PDF-")) {
		t.Errorf("preview file: %d", res.Code)
	}
	if res := do("GET", "/api/documents/preview/nope", "", false); res.Code != 404 {
		t.Errorf("unknown preview: %d", res.Code)
	}
	if res := do("POST", "/api/documents/preview", `{}`, false); res.Code != 401 {
		t.Errorf("preview without auth: %d", res.Code)
	}

	// generate from the dashboard
	res = do("POST", "/api/documents/"+tpl.Id+"/generate", `{"recordId":"`+f.order.Id+`"}`, true)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"filename":"invoice F-001.pdf"`) {
		t.Errorf("generate: %d %s", res.Code, res.Body.String())
	}
	// a document shown by a page: the signed link of the template and the record
	link := pdfview.ViewLink(f.app, tpl.Id, "orders", f.order.Id)
	res = do("GET", link, "", false)
	if res.Code != 200 || res.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(res.Body.Bytes(), []byte("%PDF-")) ||
		!strings.Contains(res.Header().Get("Content-Disposition"), "inline; filename*=UTF-8''invoice%20F-001.pdf") {
		t.Fatalf("view: %d %v %.200s", res.Code, res.Header(), res.Body.String())
	}
	if again := do("GET", link, "", false); !bytes.Equal(again.Body.Bytes(), res.Body.Bytes()) {
		t.Error("view: the document is made again (not cached)")
	}
	// by slug, without a record
	if res := do("GET", pdfview.ViewLink(f.app, "invoice", "", ""), "", false); res.Code != 200 {
		t.Errorf("view without a record: %d %s", res.Code, res.Body.String())
	}
	if res := do("GET", strings.Replace(link, f.order.Id, f.customer.Id, 1), "", false); res.Code != 403 {
		t.Errorf("view of another record: %d", res.Code)
	}
	if res := do("GET", pdfview.ViewLink(f.app, tpl.Id, "customers", f.customer.Id), "", false); res.Code != 400 {
		t.Errorf("view of a record of another collection: %d %s", res.Code, res.Body.String())
	}
	if res := do("GET", pdfview.ViewLink(f.app, "nope", "orders", f.order.Id), "", false); res.Code != 404 {
		t.Errorf("view of an unknown template: %d", res.Code)
	}

	// PDF.js, for the editor and the viewers of the pages
	res = do("GET", "/api/documents/pdfjs/"+pdfview.Version+"/pdf.min.mjs", "", false)
	if res.Code != 200 || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/javascript") || res.Body.Len() < 100_000 {
		t.Errorf("pdfjs: %d %v %d", res.Code, res.Header(), res.Body.Len())
	}
	if res := do("GET", "/api/documents/pdfjs/1.0.0/pdf.min.mjs", "", false); res.Code != 404 {
		t.Errorf("pdfjs of another version: %d", res.Code)
	}
	editor, _ := extFS.ReadFile("ext/editor.js")
	if !strings.Contains(string(editor), `"/api/documents/pdfjs/`+pdfview.Version+`/"`) {
		t.Error("the editor loads another version of PDF.js than internal/pdfview")
	}
}

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func TestImages(t *testing.T) {
	f := setup(t)
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	logo, _ := filesystem.NewFileFromBytes(buf.Bytes(), "logo.png")
	tpl := f.template(t, map[string]any{"slug": "img", "assets": []*filesystem.File{logo}})
	asset := tpl.GetStringSlice("assets")[0]
	photo, _ := filesystem.NewFileFromBytes(buf.Bytes(), "photo.png")
	f.order.Set("invoice", photo)
	if err := f.app.Save(f.order); err != nil {
		t.Fatal(err)
	}
	// an address: an http(s) image (private addresses allowed for the test
	// server) and a file of a record by its /api/files path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pics/"+f.order.Id+"/high.png" {
			http.NotFound(w, r)
			return
		}
		w.Write(buf.Bytes())
	}))
	defer srv.Close()
	local := "/api/files/" + f.order.Collection().Name + "/" + f.order.Id + "/" + f.order.GetString("invoice")
	tpl.Set("body", []Block{
		{ID: "a", Type: "image", Asset: asset, Width: 30, Align: "right"},
		{ID: "b", Type: "image", Field: "invoice", Width: 20},
		{ID: "c", Type: "image", Asset: "gone.png"},
		{ID: "d", Type: "image", URL: srv.URL + "/pics/{{record.id}}/high.png", Width: 20},
		{ID: "e", Type: "image", URL: local + "?thumb=100x100", Width: 20},
		{ID: "f", Type: "image", URL: srv.URL + "/missing.png"},
		{ID: "g", Type: "image", URL: "logo.png"},
	})
	t.Setenv(httpcall.AllowPrivateEnv, "1")
	res := f.make(t, tpl, f.order, nil)
	// the same picture: stored once per source, drawn each time
	if n := regexp.MustCompile(`/I[0-9a-f]+ Do`).FindAll(res.PDF, -1); len(n) != 4 {
		t.Errorf("images drawn: %d", len(n))
	}
	errs := map[string]string{}
	for _, e := range res.Errors {
		errs[e.Block] = e.Error
	}
	if len(errs) != 3 || errs["c"] == "" || !strings.Contains(errs["f"], "404") || !strings.Contains(errs["g"], "https://") {
		t.Errorf("errors: %v", res.Errors)
	}

	// without the setting, the private address is refused
	t.Setenv(httpcall.AllowPrivateEnv, "")
	srv.CloseClientConnections() // a kept-alive connection isn't dialed again
	tpl.Set("body", []Block{{ID: "d", Type: "image", URL: srv.URL + "/pics/{{record.id}}/high.png"}})
	res = f.make(t, tpl, f.order, nil)
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error, "private") {
		t.Errorf("private address: %v", res.Errors)
	}
}

func TestBoxes(t *testing.T) {
	f := setup(t)
	tpl := f.template(t, map[string]any{"slug": "boxes"})
	tpl.Set("header", []Block{{ID: "h", Type: "text", Text: "Header"}})
	tpl.Set("body", []Block{
		{ID: "t", Type: "heading", Text: "Title"},
		{ID: "c", Type: "columns", Columns: []Column{{Blocks: []Block{{ID: "c1", Type: "text", Text: "Left"}}}, {Blocks: []Block{{ID: "c2", Type: "text", Text: "Right"}}}}},
		{ID: "s", Type: "spacer", Height: 200},
		{ID: "x", Type: "text", Text: "{{repeat 30 \"Line\\n\"}}"},
		{ID: "hidden", Type: "text", Text: "No", ShowIf: "{{false}}"},
	})
	res := f.make(t, tpl, f.order, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	boxes := map[string][]Box{}
	for _, b := range res.Boxes {
		boxes[b.Block] = append(boxes[b.Block], b)
	}
	if res.PageW != 210 || res.PageH != 297 {
		t.Errorf("page: %v × %v", res.PageW, res.PageH)
	}
	if len(boxes["h"]) != res.Pages || res.Pages < 2 {
		t.Errorf("a header box per page: %d boxes, %d pages", len(boxes["h"]), res.Pages)
	}
	if b := boxes["t"]; len(b) != 1 || b[0].Page != 1 || b[0].X != 18 || b[0].W != 174 || b[0].H <= 0 {
		t.Errorf("title: %+v", b)
	}
	if l, r := boxes["c1"], boxes["c2"]; len(l) != 1 || len(r) != 1 || l[0].Y != r[0].Y || r[0].X <= l[0].X || l[0].W >= 174/2 {
		t.Errorf("columns: %+v %+v", l, r)
	}
	if len(boxes["hidden"]) != 0 {
		t.Errorf("a hidden block has no box")
	}
	// the text goes on after the spacer: a box on each page
	if b := boxes["x"]; len(b) < 1 || b[len(b)-1].Page != res.Pages {
		t.Errorf("text: %+v (%d pages)", b, res.Pages)
	}
}
