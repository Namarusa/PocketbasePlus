package pages

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestListFilters(t *testing.T) {
	f := setup(t)
	f.products.Fields.Add(&core.SelectField{Name: "kind", MaxSelect: 1, Values: []string{"light", "seat", "table"}})
	if err := f.app.Save(f.products); err != nil {
		t.Fatal(err)
	}
	other := testutil.Create(t, f.app, "brands", map[string]any{"name": "Zed"})
	brands, _ := f.app.FindCollectionByNameOrId("brands")
	brands.ListRule = types.Pointer("") // the visitors may list the brands
	if err := f.app.Save(brands); err != nil {
		t.Fatal(err)
	}
	for _, p := range []map[string]any{
		{"name": "Desk", "price": 120, "kind": "table", "brand": other.Id, "published": true, "launch": "2026-05-01 09:00:00.000Z"},
		{"name": "Stool", "price": 25, "kind": "seat", "brand": f.acme.Id, "secret": "x"},
	} {
		testutil.Create(t, f.app, "products", p)
	}
	for id, kind := range map[string]string{f.lamp.Id: "light", f.chair.Id: "seat"} {
		r, _ := f.app.FindRecordById("products", id) // with the new field
		r.Set("kind", kind)
		if err := f.app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	// 4 products: Chair (40, seat), Desk (120, table, Zed, published, May), Lamp <b> (12.5, light, Acme, published, March), Stool (25, seat, Acme)
	render := func(src string, q url.Values) string {
		out, _, err := render(t.Context(), f.app, pageView{Title: "T", HTML: src, Path: "/shop?x=1"}, q, false)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	names := func(out string) string {
		var got []string
		for _, m := range regexp.MustCompile(`<h2>([^<]*)</h2>`).FindAllStringSubmatch(out, -1) {
			got = append(got, m[1])
		}
		return strings.Join(got, ",")
	}
	check := func(label, out string, wants ...string) {
		t.Helper()
		for _, w := range wants {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q in\n%s", label, w, out)
			}
		}
	}
	const form = `<form data-pb-filters="products" data-pb-filter-fields="kind,brand,published,price,launch,name,secret,body" data-pb-sort-options="-price:Most expensive,name:Name,secret:Secret"></form>`
	const list = `<div id="grid" data-pb-list="products" data-pb-sort="name" data-pb-paginate="pages" data-pb-limit="2"><article data-pb-item><h2 data-pb-field="name">x</h2></article><p data-pb-empty>None</p></div>`
	src := `<body>` + form + list + `</body>`

	out := render(src, nil)
	check("form", out,
		`<form method="get" action="/shop" role="search" aria-label="Filters" class="pb-filters">`,
		`<select class="select" name="f.kind"><option value="">Any</option><option value="light">light</option>`,
		`name="f.brand"><option value="">Any</option><option value="`+f.acme.Id+`">Acme</option><option value="`+other.Id+`">Zed</option></select>`,
		`<option value="1">Yes</option><option value="0">No</option>`,
		`<input type="number" class="input" name="f.price_min" placeholder="Min"`,
		`<input type="date" class="input" name="f.launch_to" placeholder="To"`,
		`<select class="select" name="f.name"><option value="">Any</option><option value="Chair">Chair</option>`,
		`<select class="select" name="sort"><option value="">Default</option><option value="-price">Most expensive</option><option value="name">Name</option></select>`,
		`.pb-filters{`)
	for _, bad := range []string{"f.secret", "f.body", `value="secret"`, "data-pb-filter", "Clear"} {
		if strings.Contains(out, bad) {
			t.Errorf("form: unexpected %q", bad)
		}
	}
	if got := names(out); got != "Chair,Desk" {
		t.Errorf("no filters: %s", got)
	}

	for _, c := range []struct {
		q    string
		want string
	}{
		{"f.kind=seat", "Chair,Stool"},
		{"f.brand=" + f.acme.Id, "Lamp &lt;b&gt;,Stool"},
		{"f.published=1", "Desk,Lamp &lt;b&gt;"},
		{"f.published=0", "Chair,Stool"},
		{"f.price_min=25&f.price_max=40", "Chair,Stool"},
		{"f.launch_from=2026-03-04&f.launch_to=2026-03-04", "Lamp &lt;b&gt;"},
		{"f.name=Desk", "Desk"},
		{"f.name_has=too", "Stool"},
		{"f.kind=seat&sort=-price", "Chair,Stool"},
		{"sort=-price", "Desk,Chair"},
		{"f.kind=nope&f.secret=x&f.price_min=abc&sort=secret", "Chair,Desk"}, // ignored
		{"f.kind=seat&page=2", "None"},                                       // 2 results: no page 2
	} {
		q, _ := url.ParseQuery(c.q)
		out := render(src, q)
		got := names(out)
		if got == "" && strings.Contains(out, ">None<") {
			got = "None"
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.q, got, c.want)
		}
	}

	// chips, clear and kept parameters
	q, _ := url.ParseQuery("f.kind=seat&q=ch&page=2&sort=-price")
	out = render(src, q)
	check("chosen", out,
		`<option value="seat" selected="">seat</option>`, `<option value="-price" selected="">Most expensive</option>`,
		`class="pb-filter-chip badge" data-variant="secondary" href="/shop?q=ch&amp;sort=-price#grid"`, `Kind: seat ✕`,
		`<a class="btn" data-variant="ghost" href="/shop?q=ch#grid">Clear</a>`,
		`<input type="hidden" name="q" value="ch"/>`)
	if strings.Contains(out, `name="page"`) {
		t.Error("the page is kept by the filter form")
	}

	// two lists: prefixed parameters
	src2 := `<body>` + form + list + `<div data-pb-list="brands" data-pb-paginate="pages"><b data-pb-item data-pb-field="name"></b></div></body>`
	q, _ = url.ParseQuery("products_f.kind=table&f.kind=seat")
	out = render(src2, q)
	check("two lists", out, `name="products_f.kind"`, `name="products_sort"`)
	if got := names(out); got != "Desk" {
		t.Errorf("two lists: %s", got)
	}

	// the texts offered are those of the records the list may show
	out = render(`<body>`+form+strings.Replace(list, `data-pb-sort="name"`, `data-pb-sort="name" data-pb-filter="published = true"`, 1)+`</body>`, nil)
	if strings.Contains(out, `<option value="Chair">`) || !strings.Contains(out, `<option value="Desk">`) {
		t.Error("the text options ignore the list's filter")
	}

	// related records the visitor can't list aren't offered
	brands.ListRule = nil
	if err := f.app.Save(brands); err != nil {
		t.Fatal(err)
	}
	if out := render(src, nil); strings.Contains(out, ">Acme<") {
		t.Error("private related records are offered")
	}

	// a form without a list
	out = render(`<body><form data-pb-filters="nope" data-pb-filter-fields="kind"></form></body>`, nil)
	if strings.Contains(out, "<select") {
		t.Error("a form without its list has controls")
	}
}
