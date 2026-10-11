package pages

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestRelatedBindings(t *testing.T) {
	f := setup(t)
	brands, _ := f.app.FindCollectionByNameOrId("brands")
	countries := testutil.CreateCollection(t, f.app, "countries", &core.TextField{Name: "name"})
	brands.Fields.Add(
		&core.URLField{Name: "site"},
		&core.TextField{Name: "motto", Hidden: true},
		&core.RelationField{Name: "country", CollectionId: countries.Id, MaxSelect: 1},
	)
	if err := f.app.Save(brands); err != nil {
		t.Fatal(err)
	}
	fr := testutil.Create(t, f.app, "countries", map[string]any{"name": "France"})
	acme, _ := f.app.FindRecordById("brands", f.acme.Id) // with the new fields
	acme.Load(map[string]any{"site": "https://acme.test", "motto": "secret motto", "country": fr.Id})
	if err := f.app.Save(acme); err != nil {
		t.Fatal(err)
	}
	f.page(t, map[string]any{"slug": "brand", "record_collection": brands.Id})

	item := `<article data-pb-item><b data-pb-field="name"></b>` +
		`<i data-pb-field="brand.name"></i><u data-pb-field="brand.country.name"></u><s data-pb-field="brand.motto">x</s>` +
		`<a class="btn" data-pb-href="brand.site">Website</a><a data-pb-link="brand:brand">Brand</a>` +
		`<ul data-pb-list="brands" data-pb-filter="country.name = {{record.brand.country.name}}"><li data-pb-item data-pb-field="name"></li></ul></article>`

	// the page as the gate: the related records are shown
	out := f.render(t, `<body><div data-pb-list="products" data-pb-filter="id = '`+lampID+`'">`+item+`</div></body>`, scope{}, nil)
	for _, want := range []string{
		"<i>Acme</i>", "<u>France</u>", "<s></s>", // hidden fields stay hidden through relations
		`<a class="btn" href="https://acme.test">Website</a>`,
		`<a href="/brand/` + f.acme.Id + `">Brand</a>`,
		`<li>Acme</li>`, // {{record.brand.country.name}} in a nested filter
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret motto") || strings.Contains(out, "data-pb-") {
		t.Errorf("unexpected binding or hidden value in\n%s", out)
	}

	// the visitor's API rules: products are public, brands aren't viewable
	products, _ := f.app.FindCollectionByNameOrId("products")
	products.ListRule = new(string)
	products.ViewRule = new(string)
	if err := f.app.Save(products); err != nil {
		t.Fatal(err)
	}
	out = f.render(t, `<body><div data-pb-list="products" data-pb-access="rules" data-pb-filter="id = '`+lampID+`'">`+item+`</div></body>`, scope{}, nil)
	for _, want := range []string{"<i></i>", "<u></u>", `<a class="btn">Website</a>`, `<a>Brand</a>`} {
		if !strings.Contains(out, want) {
			t.Errorf("rules: missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "<i>Acme</i>") || strings.Contains(out, "acme.test") {
		t.Errorf("rules: the brand is shown to a guest\n%s", out)
	}
	// ... and once brands are public, they are
	brands.ViewRule = new(string)
	if err := f.app.Save(brands); err != nil {
		t.Fatal(err)
	}
	out = f.render(t, `<body><div data-pb-list="products" data-pb-access="rules" data-pb-filter="id = '`+lampID+`'">`+item+`</div></body>`, scope{}, nil)
	if !strings.Contains(out, "<i>Acme</i>") || !strings.Contains(out, `href="https://acme.test"`) || strings.Contains(out, "<u>France</u>") {
		t.Errorf("rules: expected the brand (not its country, still private)\n%s", out)
	}

	// a path is at most 2 relations
	if _, _, ok := splitPath("a.b.c.d"); ok {
		t.Error("expected a too long path to be refused")
	}
}
