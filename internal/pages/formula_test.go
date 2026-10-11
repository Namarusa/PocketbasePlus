package pages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestFormulas(t *testing.T) {
	prev := formula.Now
	formula.Now = func() time.Time { return time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { formula.Now = prev })

	f := setup(t)
	tags := testutil.CreateCollection(t, f.app, "tags", &core.TextField{Name: "name", Presentable: true}, &core.TextField{Name: "code", Hidden: true})
	products, _ := f.app.FindCollectionByNameOrId("products")
	products.Fields.Add(&core.RelationField{Name: "tags", CollectionId: tags.Id, MaxSelect: 5}, &core.JSONField{Name: "meta"})
	if err := f.app.Save(products); err != nil {
		t.Fatal(err)
	}
	a := testutil.Create(t, f.app, "tags", map[string]any{"name": "Light", "code": "x1"})
	b := testutil.Create(t, f.app, "tags", map[string]any{"name": "Desk", "code": "x2"})
	lamp, _ := f.app.FindRecordById("products", lampID)
	lamp.Set("tags", []string{a.Id, b.Id})
	lamp.Set("meta", map[string]any{"watts": 1500000})
	if err := f.app.Save(lamp); err != nil {
		t.Fatal(err)
	}

	item := `<article data-pb-item>` +
		`<p data-pb-formula="{{upper record.name}} costs {{currency &#34;EUR&#34; (multiply record.price 2)}}">sample</p>` +
		`<p data-pb-formula="{{formatDate &#34;DD/MM/YYYY&#34; record.launch}} ({{timeAgo record.launch}})"></p>` +
		`<p data-pb-formula="{{record.brand}} / {{lower record.brand.name}} / {{record.tags}} / {{join &#34;+&#34; (pluck &#34;name&#34; record.tags)}}"></p>` +
		`<p data-pb-formula="[{{record.secret}}{{pluck &#34;code&#34; record.tags}}]"></p>` +
		`<p data-pb-formula="{{formatNumber record.meta.watts}} W, {{if gt record.price 10}}premium{{else}}cheap{{end}}"></p>` +
		`<p data-pb-formula="{{query.q}}|{{default &#34;guest&#34; auth.name}}"></p>` +
		`<p data-pb-formula="{{divide 1 0}}">x</p>` +
		`</article>`
	out := f.render(t, `<body><div data-pb-list="products" data-pb-filter="id = '`+lampID+`'">`+item+`</div></body>`, scope{}, url.Values{"q": {"<i>hi</i>"}})
	for _, want := range []string{
		"<p>LAMP &lt;B&gt; costs €25.00</p>", // escaped
		"<p>04/03/2026 (10 days ago)</p>",
		"<p>Acme / acme / Light, Desk / Light+Desk</p>", // relations print their names
		"<p>[, ]</p>", // hidden fields aren't there, through relations too
		"<p>1,500,000 W, premium</p>",
		"<p>&lt;i&gt;hi&lt;/i&gt;|guest</p>",
		"<p></p>", // errors render nothing (shown in previews)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-pb-formula") || strings.Contains(out, "s3cret") || strings.Contains(out, "x1") {
		t.Errorf("unexpected formula or hidden value in\n%s", out)
	}

	// previews show the errors
	preview, _, err := render(t.Context(), f.app, pageView{Title: "T", HTML: `<body><p data-pb-formula="{{divide 1 0}}"></p><p data-pb-formula="{{upper"></p></body>`}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "formula: divide: division by zero") || !strings.Contains(preview, "formula: 1: unclosed action") {
		t.Errorf("expected the errors in the preview\n%s", preview)
	}
}

func TestFormulaPreviewRoute(t *testing.T) {
	var f *fixture
	var pageID string
	headers := map[string]string{"Content-Type": "application/json"}
	factory := func(t testing.TB) *tests.TestApp {
		f = setup(t)
		pageID = f.page(t, map[string]any{}).Id
		return f.app
	}
	admin := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		su := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
		headers["Authorization"] = testutil.AuthToken(t, su)
	}
	run := func(name, body string, status int, want ...string) {
		s := &tests.ApiScenario{Name: name, Method: http.MethodPost, Headers: headers, TestAppFactory: factory, ExpectedStatus: status, ExpectedContent: want}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			admin(t, app, e)
			s.URL = "/api/pages/" + pageID + "/formula"
			s.Body = strings.NewReader(body)
		}
		s.Test(t)
	}
	run("sample record", `{"formula":"{{upper record.name}} {{record.brand.name}}","collection":"products","record":"`+lampID+`"}`, 200, `"text":"LAMP <B> Acme"`)
	run("error", `{"formula":"{{nope}}"}`, 200, `"error":`, `function \"nope\" not defined`)
	(&tests.ApiScenario{Name: "superusers only", Method: http.MethodPost, URL: "/api/pages/x/formula", Body: strings.NewReader(`{}`),
		TestAppFactory: factory, ExpectedStatus: 401, ExpectedContent: []string{`"data":{}`}}).Test(t)
}
