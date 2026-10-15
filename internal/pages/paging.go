package pages

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
)

// Pagination and search of the collection lists, without scripts: the page
// address holds the state (?page=2&q=pika), links and a GET form change it.
//
//	<div data-pb-list="cards" data-pb-paginate="pages" data-pb-key="cards">…</div>
//	<form data-pb-search="cards" data-pb-search-fields="name,brand.name"><input type="search"><button>Search</button></form>
//	<nav data-pb-pager="cards"></nav>  (optional: else the pager follows the list)
//
// A list is named by its key (data-pb-key, else its collection name). When a
// single list of the page pages or searches, its address parameters are
// "page" and "q"; with several, "{key}_page" and "{key}_q".
const (
	attrPaginate     = "data-pb-paginate"      // list: "pages" (numbered) or "more" (a "Show more" link)
	attrKey          = "data-pb-key"           // list: its name in the address
	attrSearch       = "data-pb-search"        // form: searches the list with this key
	attrSearchFields = "data-pb-search-fields" // form: the fields searched, comma separated ("brand.name": a related record's)
	attrPager        = "data-pb-pager"         // element: where the pager of the list with this key goes

	maxPage        = 10000
	maxSearchWords = 5
	maxSearchLen   = 100
)

// listState is what the page address and the page's search forms say about
// the lists.
type listState struct {
	prefixed bool                // several lists page, search or filter: "{key}_page"
	fields   map[string][]string // list key → searched fields
	pagers   map[string]*xhtml.Node
	nodes    map[string]*xhtml.Node  // list key → the (top level) list
	filters  map[string][]string     // list key → filtered fields (filters.go)
	sorts    map[string][]sortOption // list key → the sort options of its filters
}

// listKey is the name of a list in the address.
func listKey(n *xhtml.Node) string {
	if k := cleanKey(getAttr(n, attrKey)); k != "" {
		return k
	}
	return cleanKey(getAttr(n, attrList))
}

func cleanKey(k string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, k)
}

// scanLists finds the lists that page or search (top level lists only: a
// nested list repeats with each record), the search forms and the pagers.
func (r *renderer) scanLists(body *xhtml.Node) {
	st := &listState{fields: map[string][]string{}, pagers: map[string]*xhtml.Node{}, nodes: map[string]*xhtml.Node{},
		filters: map[string][]string{}, sorts: map[string][]sortOption{}}
	keys := map[string]bool{}
	sortSrc := map[string]string{}
	var visit func(n *xhtml.Node, inList bool)
	visit = func(n *xhtml.Node, inList bool) {
		if n.Type == xhtml.ElementNode {
			if getAttr(n, attrList) != "" {
				if !inList {
					if getAttr(n, attrPaginate) != "" {
						keys[listKey(n)] = true
					}
					if st.nodes[listKey(n)] == nil {
						st.nodes[listKey(n)] = n
					}
				}
				inList = true
			}
			if hasAttr(n, attrFilters) && !inList {
				k := cleanKey(getAttr(n, attrFilters))
				keys[k] = true
				for _, f := range strings.Split(getAttr(n, attrFilterFields), ",") {
					if f = strings.TrimSpace(f); f != "" && len(st.filters[k]) < maxFilterFields && !slices.Contains(st.filters[k], f) {
						st.filters[k] = append(st.filters[k], f)
					}
				}
				sortSrc[k] = getAttr(n, attrSortOptions)
			}
			if hasAttr(n, attrSearch) && !inList {
				k := cleanKey(getAttr(n, attrSearch))
				keys[k] = true
				for _, f := range strings.Split(getAttr(n, attrSearchFields), ",") {
					if f = strings.TrimSpace(f); f != "" {
						st.fields[k] = append(st.fields[k], f)
					}
				}
			}
			if hasAttr(n, attrPager) && !inList {
				st.pagers[cleanKey(getAttr(n, attrPager))] = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, inList)
		}
	}
	visit(body, false)
	st.prefixed = len(keys) > 1
	for k, src := range sortSrc {
		if list := st.nodes[k]; list != nil && src != "" {
			col, _ := listCollection(r.app, getAttr(list, attrList))
			st.sorts[k] = r.parseSortOptions(col, src)
		}
	}
	r.lists = st
}

// param is the address parameter of a list: "page", or "cards_page".
func (r *renderer) param(key, name string) string {
	if r.lists != nil && r.lists.prefixed {
		return key + "_" + name
	}
	return name
}

// searchFilter returns the filter of the visitor's search on a list: every
// word in one of the searched fields. Hidden fields can't be searched.
func (r *renderer) searchFilter(col *core.Collection, key string, params dbx.Params) string {
	if r.lists == nil {
		return ""
	}
	q := strings.TrimSpace(r.query.Get(r.param(key, "q")))
	if q == "" {
		return ""
	}
	if len(q) > maxSearchLen {
		q = q[:maxSearchLen]
	}
	var fields []string
	for _, f := range r.lists.fields[key] {
		if r.searchable(col, f) {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return ""
	}
	words := strings.Fields(q)
	if len(words) > maxSearchWords {
		words = words[:maxSearchWords]
	}
	var all []string
	for i, w := range words {
		p := "pbsearch" + strconv.Itoa(i)
		params[p] = w
		var any []string
		for _, f := range fields {
			any = append(any, f+" ~ {:"+p+"}")
		}
		all = append(all, "("+strings.Join(any, " || ")+")")
	}
	return strings.Join(all, " && ")
}

// searchable reports whether a field (or a related record's: "brand.name")
// can be searched: a field a page may show.
func (r *renderer) searchable(col *core.Collection, path string) bool {
	parts := strings.Split(path, ".")
	if len(parts) > 2 {
		return false
	}
	for i, p := range parts {
		if col == nil || !visible(col, nil, p) {
			return false
		}
		f := col.Fields.GetByName(p)
		if i == len(parts)-1 {
			switch f.Type() {
			case core.FieldTypeRelation, core.FieldTypeFile, core.FieldTypeJSON, core.FieldTypeGeoPoint:
				return false
			}
			return true
		}
		rel, ok := f.(*core.RelationField)
		if !ok {
			return false
		}
		col, _ = r.app.FindCachedCollectionByNameOrId(rel.CollectionId)
		if col != nil && internalcols.Is(col) {
			return false
		}
	}
	return true
}

// pageOf returns the page of a list asked by the address (1 for the first).
func (r *renderer) pageOf(key string) int {
	p, err := strconv.Atoi(r.query.Get(r.param(key, "page")))
	if err != nil || p < 1 {
		return 1
	}
	return min(p, maxPage)
}

// linkWith returns the address of the page with these parameters changed
// ("" removes one), and the list as anchor.
func (r *renderer) linkWith(set map[string]string, anchor string) string {
	q := url.Values{}
	for k, v := range r.query {
		if k != "preview" && k != "as" || r.preview {
			q[k] = v
		}
	}
	for k, v := range set {
		if v == "" {
			q.Del(k)
		} else {
			q.Set(k, v)
		}
	}
	path, _, _ := strings.Cut(r.path, "?")
	if path == "" {
		path = "/"
	}
	out := path
	if enc := q.Encode(); enc != "" {
		out += "?" + enc
	}
	if anchor != "" {
		out += "#" + url.PathEscape(anchor)
	}
	return out
}

// pager adds the pager of a list: in its pager element, else after it.
func (r *renderer) pager(n *xhtml.Node, key, mode string, page, perPage, total int) {
	pages := (total + perPage - 1) / perPage
	if (mode == "more" && total <= perPage*page) || (mode == "pages" && pages <= 1 && page <= 1) {
		r.placePager(n, key, nil) // everything is shown
		return
	}
	anchor := getAttr(n, "id")
	link := func(p int) string {
		v := strconv.Itoa(p)
		if p <= 1 {
			v = ""
		}
		return r.linkWith(map[string]string{r.param(key, "page"): v}, anchor)
	}
	nav := elem("nav", "class", "pb-pager", "aria-label", "Pages")
	r.pagerCSS = true
	btn := func(label, href, rel string, current bool) *xhtml.Node {
		var a *xhtml.Node
		class := "pb-pager-btn btn"
		if _, err := strconv.Atoi(label); err == nil {
			class += " pb-pager-num" // hidden on phones (the current page shows "3 / 56")
		}
		if href == "" {
			a = elem("span", "class", class, "aria-disabled", "true", "data-variant", "ghost")
		} else {
			a = elem("a", "class", class, "href", href, "data-variant", "ghost")
			if rel != "" {
				setAttr(a, "rel", rel)
			}
			if current {
				setAttr(a, "aria-current", "page")
				setAttr(a, "data-variant", "outline")
			}
		}
		// "‹ Previous": the word is hidden on phones
		if arrow, word, ok := strings.Cut(label, " "); ok && (arrow == "‹" || word == "›") {
			if arrow == "‹" {
				a.AppendChild(textNode("‹ "))
				w := elem("span", "class", "pb-pager-word")
				w.AppendChild(textNode(word))
				a.AppendChild(w)
			} else {
				w := elem("span", "class", "pb-pager-word")
				w.AppendChild(textNode(arrow))
				a.AppendChild(w)
				a.AppendChild(textNode(" ›"))
			}
			setAttr(a, "aria-label", map[bool]string{true: "Previous page", false: "Next page"}[arrow == "‹"])
			return a
		}
		a.AppendChild(textNode(label))
		return a
	}
	if mode == "more" {
		if total > perPage*page {
			nav.AppendChild(btn("Show more", link(page+1), "next", false))
		}
		r.placePager(n, key, nav)
		return
	}
	prev, next := "", ""
	if page > 1 {
		prev = link(page - 1)
	}
	if page < pages {
		next = link(page + 1)
	}
	nav.AppendChild(btn("‹ Previous", prev, "prev", false))
	last := 0
	for p := 1; p <= pages; p++ {
		if p != 1 && p != pages && (p < page-1 || p > page+1) {
			continue
		}
		if last != 0 && p > last+1 {
			gap := elem("span", "class", "pb-pager-gap")
			gap.AppendChild(textNode("…"))
			nav.AppendChild(gap)
		}
		nav.AppendChild(btn(strconv.Itoa(p), link(p), "", p == page))
		last = p
	}
	current := elem("span", "class", "pb-pager-current")
	current.AppendChild(textNode(strconv.Itoa(page) + " / " + strconv.Itoa(pages)))
	nav.AppendChild(current)
	nav.AppendChild(btn("Next ›", next, "next", false))
	info := elem("span", "class", "pb-pager-info")
	from := (page-1)*perPage + 1
	to := min(page*perPage, total)
	if from > total {
		info.AppendChild(textNode(strconv.Itoa(total) + " results"))
	} else {
		info.AppendChild(textNode(strconv.Itoa(from) + "–" + strconv.Itoa(to) + " of " + strconv.Itoa(total)))
	}
	nav.AppendChild(info)
	r.placePager(n, key, nav)
}

// placePager puts the pager in the list's pager element (emptied), else after
// the list.
func (r *renderer) placePager(list *xhtml.Node, key string, nav *xhtml.Node) {
	if el := r.lists.pagers[key]; el != nil {
		clearChildren(el)
		removeBindingAttrs(el)
		if nav != nil {
			el.AppendChild(nav)
		}
		return
	}
	if nav != nil && list.Parent != nil {
		list.Parent.InsertBefore(nav, list.NextSibling)
	}
}

// searchForm makes a search form of the page a GET form of the list's
// parameter, with the current search, keeping the other parameters.
func (r *renderer) searchForm(n *xhtml.Node) {
	key := cleanKey(getAttr(n, attrSearch))
	name := r.param(key, "q")
	path, _, _ := strings.Cut(r.path, "?")
	if path == "" {
		path = "/"
	}
	setAttr(n, "method", "get")
	setAttr(n, "action", path)
	setAttr(n, "role", "search")
	removeAttr(n, "enctype")
	input := find(n, func(c *xhtml.Node) bool {
		t := strings.ToLower(getAttr(c, "type"))
		return c.DataAtom == atom.Input && (t == "" || t == "search" || t == "text")
	})
	if input == nil {
		input = elem("input", "type", "search")
		n.InsertBefore(input, n.FirstChild)
	}
	setAttr(input, "name", name)
	setAttr(input, "value", r.query.Get(name))
	if getAttr(input, "aria-label") == "" && getAttr(input, "placeholder") == "" {
		setAttr(input, "aria-label", "Search")
	}
	// the other parameters stay (other lists, the address's own), not the page
	for k, vs := range r.query {
		if k == name || k == r.param(key, "page") || (!r.preview && (k == "preview" || k == "as")) {
			continue
		}
		for _, v := range vs {
			if v != "" { // the empty fields of another form
				n.AppendChild(elem("input", "type", "hidden", "name", k, "value", v))
			}
		}
	}
}

func elem(tag string, attrs ...string) *xhtml.Node {
	n := &xhtml.Node{Type: xhtml.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
	for i := 0; i+1 < len(attrs); i += 2 {
		n.Attr = append(n.Attr, xhtml.Attribute{Key: attrs[i], Val: attrs[i+1]})
	}
	return n
}

// pagerCSS lays the pagers out (the kit's buttons style them).
const pagerCSS = `
.pb-pager{display:flex;flex-wrap:wrap;align-items:center;justify-content:center;gap:4px;margin:16px 0}
.pb-pager-btn{min-width:2.25rem;justify-content:center}
.pb-pager-btn[aria-disabled]{opacity:.45;pointer-events:none}
.pb-pager-gap{padding:0 4px;opacity:.6}
.pb-pager-info{width:100%;text-align:center;font-size:.85em;opacity:.7}
.pb-pager-current{display:none;padding:0 8px;font-weight:600}
@media (max-width:480px){.pb-pager{flex-wrap:nowrap}.pb-pager .pb-pager-num,.pb-pager .pb-pager-gap,.pb-pager .pb-pager-word,.pb-pager .pb-pager-info{display:none}.pb-pager .pb-pager-current{display:inline}}
`

// pagerPlainCSS styles the pagers of the pages without the UI kit.
const pagerPlainCSS = `
.pb-pager-btn{display:inline-flex;align-items:center;padding:6px 10px;border:1px solid transparent;border-radius:6px;color:inherit;text-decoration:none}
.pb-pager-btn:hover{background:rgba(127,127,127,.12)}
.pb-pager-btn[aria-current]{border-color:currentColor;font-weight:600}
`
