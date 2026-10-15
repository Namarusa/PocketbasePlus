package pages

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestPaginationAndSearch(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"Desk", "Shelf", "Stool"} {
		testutil.Create(t, f.app, "products", map[string]any{"name": name, "price": 5})
	}
	// 5 products: Chair, Desk, Lamp <b>, Shelf, Stool
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
	list := func(attrs string) string {
		return `<div id="grid" data-pb-list="products" data-pb-sort="name" data-pb-limit="2" ` + attrs + `>` +
			`<article data-pb-item><h2 data-pb-field="name">x</h2></article><p data-pb-empty>None</p></div>`
	}

	// numbered pages
	src := `<body>` + list(`data-pb-paginate="pages"`) + `</body>`
	out := render(src, nil)
	if got := names(out); got != "Chair,Desk" {
		t.Errorf("page 1: %s", got)
	}
	check("page 1", out, `<nav class="pb-pager" aria-label="Pages">`, `<span class="pb-pager-btn btn" aria-disabled="true" data-variant="ghost">‹ Previous</span>`,
		`href="/shop?page=2#grid" data-variant="ghost" rel="next">Next ›</a>`, `data-variant="outline" aria-current="page">1</a>`, `1–2 of 5`, `.pb-pager{`)
	out = render(src, url.Values{"page": {"3"}, "x": {"1"}})
	if got := names(out); got != "Stool" {
		t.Errorf("page 3: %s", got)
	}
	check("page 3", out, `href="/shop?page=2&amp;x=1#grid" data-variant="ghost" rel="prev">‹ Previous</a>`, `href="/shop?x=1#grid" data-variant="ghost">1</a>`, `5–5 of 5`, `aria-disabled="true" data-variant="ghost">Next ›</span>`)
	if out = render(src, url.Values{"page": {"9"}}); names(out) != "" || !strings.Contains(out, "<p>None</p>") {
		t.Errorf("past the last page: the empty state\n%s", out)
	}

	// show more
	out = render(`<body>`+list(`data-pb-paginate="more"`)+`</body>`, url.Values{"page": {"2"}})
	if got := names(out); got != "Chair,Desk,Lamp &lt;b&gt;,Shelf" {
		t.Errorf("more: %s", got)
	}
	check("more", out, `href="/shop?page=3#grid" data-variant="ghost" rel="next">Show more</a>`)
	if out = render(`<body>`+list(`data-pb-paginate="more"`)+`</body>`, url.Values{"page": {"3"}}); strings.Contains(out, "pb-pager") {
		t.Errorf("everything shown: no pager\n%s", out)
	}

	// search, through a relation; hidden fields can't be searched
	form := `<form data-pb-search="products" data-pb-search-fields="name, brand.name, secret, nope"><input type="search" placeholder="Search"><button>Go</button></form>`
	src = `<body>` + form + list(`data-pb-paginate="pages"`) + `<nav data-pb-pager="products" class="here"></nav></body>`
	out = render(src, url.Values{"q": {"ACM"}, "page": {"1"}, "x": {"1"}})
	if got := names(out); got != "Lamp &lt;b&gt;" {
		t.Errorf("search: %s", got)
	}
	check("search form", out, `<form method="get" action="/shop" role="search"><input type="search" placeholder="Search" name="q" value="ACM"/><button>Go</button><input type="hidden" name="x" value="1"/></form>`,
		`<nav class="here"></nav>`) // a single page: an empty pager element
	if names(render(src, url.Values{"q": {"s3cret"}})) != "" {
		t.Error("expected no result for a hidden field")
	}
	if got := names(render(src, url.Values{"q": {"st oo"}})); got != "Stool" {
		t.Errorf("every word: %s", got)
	}
	out = render(src, url.Values{"q": {"s"}}) // Desk, Shelf, Stool
	check("pager element", out, `<nav class="here"><nav class="pb-pager"`, `href="/shop?page=2&amp;q=s#grid"`)

	// two lists: their own parameters
	src = `<body>` + list(`data-pb-paginate="pages"`) + `<ul data-pb-list="brands" data-pb-key="b" data-pb-paginate="pages" data-pb-limit="1"><li data-pb-item data-pb-field="name"></li></ul></body>`
	out = render(src, url.Values{"products_page": {"2"}})
	if got := names(out); got != "Lamp &lt;b&gt;,Shelf" {
		t.Errorf("two lists: %s", got)
	}
	check("two lists", out, `href="/shop?products_page=3#grid"`)
	if strings.Contains(out, "data-pb-") {
		t.Errorf("bindings left in\n%s", out)
	}
}
