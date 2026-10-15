package pages

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestConditions(t *testing.T) {
	f := setup(t)
	cond := func(json string) string { return html.EscapeString(json) }
	item := `<article data-pb-item>` +
		`<p data-pb-field="name" data-pb-conditions="` + cond(`[{"action":"set","prop":"data-pb-style-color","value":"gold","when":[{"field":"record.price","op":"gt","value":"20"}]},`+
		`{"action":"set","prop":"data-pb-attr-class","value":"brand-{{lower record.brand.name}}","when":[{"field":"record.brand.name","op":"eq","value":"acme"}]}]`) + `">x</p>` +
		`<span class="draft" data-pb-conditions="` + cond(`[{"action":"show","when":[{"field":"record.published","op":"isFalse"}]}]`) + `">Draft</span>` +
		`<em data-pb-conditions="` + cond(`[{"action":"hide","when":[{"field":"record.photo","op":"empty"}]}]`) + `">photo</em>` +
		`<b data-pb-conditions="` + cond(`[{"action":"set","prop":"data-pb-formula","value":"Sold out","match":"any","when":[{"field":"record.name","op":"contains","value":"CHA"},{"formula":"{{gt record.price 100}}"}]}]`) + `">In stock</b>` +
		`<i data-pb-conditions="` + cond(`[{"action":"hide","when":[{"field":"query.q","op":"notEmpty"}]}]`) + `">all</i>` +
		`<s data-pb-conditions="` + cond(`[{"action":"hide","when":[{"field":"record.secret","op":"notEmpty"}]}]`) + `">secret?</s>` +
		`</article>`
	out := f.render(t, `<body><div data-pb-list="products" data-pb-sort="name">`+item+`</div></body>`, scope{}, url.Values{})
	for _, want := range []string{
		`<article><p style="color:gold">Chair</p><span class="draft">Draft</span><b>Sold out</b><i>all</i><s>secret?</s></article>`,
		`<article><p class="brand-acme">Lamp &lt;b&gt;</p><b>In stock</b><i>all</i><s>secret?</s></article>`, // hidden fields are empty
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-pb-") {
		t.Errorf("bindings left in\n%s", out)
	}
	out = f.render(t, `<body><div data-pb-list="products">`+item+`</div></body>`, scope{}, url.Values{"q": {"x"}})
	if strings.Contains(out, "<i>") {
		t.Errorf("expected the address condition to hide\n%s", out)
	}
}

func TestConditionsPreviewRoute(t *testing.T) {
	var f *fixture
	var pageID string
	headers := map[string]string{"Content-Type": "application/json"}
	s := &tests.ApiScenario{Name: "rules", Method: http.MethodPost, Headers: headers, ExpectedStatus: 200,
		ExpectedContent: []string{`"visible":false`, `"rules":[{"matched":true},{"matched":false,"error":"conditions: choose a field"}]`, `"sets":[]`}}
	s.TestAppFactory = func(t testing.TB) *tests.TestApp {
		f = setup(t)
		pageID = f.page(t, map[string]any{}).Id
		return f.app
	}
	s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		su := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
		headers["Authorization"] = testutil.AuthToken(t, su)
		s.URL = "/api/pages/" + pageID + "/conditions"
		s.Body = strings.NewReader(`{"collection":"products","record":"` + lampID + `","conditions":[` +
			`{"action":"hide","when":[{"field":"record.brand.name","op":"startsWith","value":"ac"}]},{"action":"show","when":[{"field":"","op":"eq"}]}]}`)
	}
	s.Test(t)
}
