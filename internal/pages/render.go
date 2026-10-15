package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
	"github.com/Namarusa/PocketbasePlus/internal/session"
)

// Bindings saved by the editor as attributes of the page HTML.
const (
	attrList   = "data-pb-list"   // collection name: the element repeats its item for each record
	attrFilter = "data-pb-filter" // list: PocketBase filter ({{record.x}}, {{query.x}} are bound as params)
	attrSort   = "data-pb-sort"   // list: e.g. "-created,name"
	attrLimit  = "data-pb-limit"  // list: number of records (12 by default)
	attrItem   = "data-pb-item"   // list child repeated per record
	attrEmpty  = "data-pb-empty"  // list child shown when there are no records
	attrField  = "data-pb-field"  // shows a field of the current record ("brand.name": through relations)
	attrHref   = "data-pb-href"   // link: its target is a URL, email or file field (path)
	attrFormat = "data-pb-format" // date fields: date | datetime | time
	attrLink   = "data-pb-link"   // href to the record page with this slug (/{slug}/{id}); "slug:brand" for the related record
	attrForm   = "data-pb-form"   // iframe: embeds the form with this slug
	attrPart   = "data-pb-part"   // placeholder of the site part with this slug
	attrAccess = "data-pb-access" // list: "rules" applies the API rules of the collection for the visitor

	// members and actions
	attrVisible = "data-pb-visible" // shown to: "guests", "users", "role:<id>" (any of them)
	attrUser    = "data-pb-user"    // shows a field of the signed-in visitor
	attrAction  = "data-pb-action"  // form (or button): runs the page action flow with this id
	attrLogout  = "data-pb-logout"  // form (or button): signs out, then opens this path ("/" by default)
	attrMessage = "data-pb-message" // shows the message of the last action
)

const (
	defaultLimit = 12
	maxLimit     = 100
	maxRecords   = 1000 // per page view
	maxDepth     = 4    // nested lists
	queryTimeout = 5 * time.Second
)

type htmlNode = xhtml.Node

// scope is the record the fields refer to: the record of a record page, or
// the current item of a list.
type scope struct {
	col   *core.Collection
	rec   *core.Record
	self  bool // the record of the visitor: their email is shown
	rules bool // read with the API rules of the visitor: the fields they can't read are hidden
}

type renderer struct {
	app     core.App
	ctx     context.Context
	query   url.Values
	preview bool // superuser preview: binding errors are shown in the page
	budget  int

	visitor    visitor
	info       *core.RequestInfo // the visitor as PocketBase sees a request of theirs (API rules)
	path       string            // of the page (the actions come back to it)
	flash      *session.Flash
	flashShown bool
	widgets    bool // charts or calendars: the pages script and styles are loaded
	charts     int
	related    map[relatedKey]*core.Record // records reached through relations (nil: not shown)
	formulas   map[string]*template.Template
	lists      *listState // pagination and search (paging.go)
	pagerCSS   bool
	filterCSS  bool
}

// pageView is what's needed to render a page.
type pageView struct {
	Title       string
	Description string
	HTML        string
	CSS         string
	Scope       scope  // record pages
	Kit         *theme // Basecoat pages: the site theme
	Visitor     visitor
	Path        string         // the URL path and query of the page
	Flash       *session.Flash // message of the last action
}

// interactiveClasses are the Basecoat components that need its script
// (dialogs, accordions and tooltips work without it).
var interactiveClasses = map[string]bool{
	"tabs": true, "dropdown-menu": true, "popover": true, "select": true, "combobox": true,
	"toaster": true, "sidebar": true, "command": true,
}

// render returns the full HTML document of the page, and whether it loads
// the Basecoat script (the CSP then allows the site's scripts).
func render(ctx context.Context, app core.App, p pageView, query url.Values, preview bool) (string, bool, error) {
	body, err := parseBody(p.HTML)
	if err != nil {
		return "", false, err
	}
	p.CSS += expandParts(app, body, preview)
	sanitize(body)
	r := &renderer{app: app, ctx: ctx, query: query, preview: preview, budget: maxRecords, visitor: p.Visitor, info: p.Visitor.requestInfo(), path: p.Path, flash: p.Flash}
	r.scanLists(body)
	r.walk(body, p.Scope, 0)
	if r.pagerCSS {
		p.CSS += pagerCSS
		if p.Kit == nil {
			p.CSS += pagerPlainCSS
		}
	}
	if r.filterCSS {
		p.CSS += filterCSS
		if p.Kit == nil {
			p.CSS += filterPlainCSS
		}
	}
	if r.flash != nil {
		if !r.flashShown {
			box := flashNode(r.flash)
			setAttr(box, "style", "margin:12px auto;max-width:960px")
			body.InsertBefore(box, body.FirstChild)
		}
		p.CSS += flashCSS
	}
	script := p.Kit != nil && find(body, func(n *xhtml.Node) bool {
		for _, c := range strings.Fields(getAttr(n, "class")) {
			if interactiveClasses[c] {
				return true
			}
		}
		return false
	}) != nil

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"")
	if p.Kit != nil {
		b.WriteString(` data-theme-mode="` + p.Kit.Mode + `"`)
		if p.Kit.Mode == "dark" {
			b.WriteString(` class="dark"`) // also without the script
		}
	}
	b.WriteString(">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>" + html.EscapeString(p.Title) + "</title>\n")
	if p.Description != "" {
		b.WriteString(`<meta name="description" content="` + html.EscapeString(p.Description) + `">` + "\n")
	}
	if preview {
		b.WriteString(`<meta name="robots" content="noindex">` + "\n")
	}
	if p.Kit != nil {
		b.WriteString(kitHead(*p.Kit))
	}
	if script {
		b.WriteString(`<script src="/p/_static/basecoat-` + basecoatVersion + `.js" defer></script>` + "\n")
	}
	if r.widgets {
		b.WriteString(`<link rel="stylesheet" href="/p/_static/` + uiCSS + `">` + "\n")
		b.WriteString(`<script src="/p/_static/` + uiJS + `" defer></script>` + "\n")
		script = true
	}
	// "</" can't close the style element early
	b.WriteString("<style>" + strings.ReplaceAll(p.CSS, "</", `<\/`) + "</style>\n</head>\n")
	if err := xhtml.Render(&b, body); err != nil {
		return "", false, err
	}
	b.WriteString("\n</html>\n")
	return b.String(), script, nil
}

// parseBody returns the body element of the editor HTML ("<body ...>...</body>"
// or a fragment).
func parseBody(src string) (*xhtml.Node, error) {
	doc, err := xhtml.Parse(strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	if body := find(doc, func(n *xhtml.Node) bool { return n.DataAtom == atom.Body }); body != nil {
		return body, nil
	}
	return nil, fmt.Errorf("invalid page HTML")
}

func find(n *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if n.Type == xhtml.ElementNode && match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := find(c, match); found != nil {
			return found
		}
	}
	return nil
}

// --- bindings --------------------------------------------------------------------------

func (r *renderer) walk(n *xhtml.Node, s scope, depth int) {
	if n.Type == xhtml.ElementNode {
		if hasAttr(n, attrConditions) && !r.conditions(n, s) {
			if n.Parent != nil {
				n.Parent.RemoveChild(n)
			}
			return
		}
		if hasAttr(n, attrVisible) && !r.visitor.matches(getAttr(n, attrVisible)) {
			if n.Parent != nil {
				n.Parent.RemoveChild(n)
			}
			return
		}
		if hasAttr(n, attrAction) || hasAttr(n, attrLogout) {
			n = r.actionForm(n, s)
		}
		if hasAttr(n, attrMessage) {
			r.message(n)
		}
		if getAttr(n, attrChart) != "" {
			r.chart(n, s)
			removeBindingAttrs(n)
			return
		}
		if hasAttr(n, attrCalendar) {
			r.calendar(n)
			removeBindingAttrs(n)
			return
		}
		if getAttr(n, attrList) != "" {
			if hasDynamic(n) {
				r.dynamic(n, s)
			}
			r.list(n, s, depth)
			removeBindingAttrs(n)
			return
		}
		if hasAttr(n, attrSearch) && n.DataAtom == atom.Form {
			r.searchForm(n)
		}
		if hasAttr(n, attrFilters) && n.DataAtom == atom.Form {
			r.filterForm(n, s)
			removeBindingAttrs(n)
			return
		}
		if slug := getAttr(n, attrForm); slug != "" {
			setAttr(n, "src", "/f/"+url.PathEscape(slug)+"/embed")
		}
		replaced := false
		if hasAttr(n, attrFormula) {
			if src := getAttr(n, attrFormula); src != "" {
				r.formula(n, s, src)
			} else {
				clearChildren(n) // not written yet
			}
			replaced = true
		} else if name := getAttr(n, attrField); name != "" {
			replaced = r.field(n, s, name)
		} else if name := getAttr(n, attrUser); name != "" {
			var self scope
			if u := r.visitor.user; u != nil {
				self = scope{col: u.Collection(), rec: u, self: true}
			}
			replaced = r.field(n, self, name)
		}
		if link := getAttr(n, attrLink); link != "" && s.rec != nil {
			r.recordLink(n, s, link)
		}
		if path := getAttr(n, attrHref); path != "" {
			r.href(n, s, path)
		}
		if hasDynamic(n) {
			r.dynamic(n, s)
		}
		removeBindingAttrs(n)
		if replaced {
			return
		}
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		r.walk(c, s, depth)
		c = next
	}
}

// list repeats the item of the element for each record of the collection.
func (r *renderer) list(n *xhtml.Node, s scope, depth int) {
	var item, empty *xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		switch {
		case hasAttr(c, attrEmpty):
			empty = c
		case hasAttr(c, attrItem) && (item == nil || !hasAttr(item, attrItem)):
			item = c
		case item == nil:
			item = c
		}
	}
	clearChildren(n)

	name := getAttr(n, attrList)
	if depth >= maxDepth {
		r.fail(n, "lists are nested too deep")
		return
	}
	col, err := listCollection(r.app, name)
	if err != nil {
		r.fail(n, err.Error())
		return
	}
	limit := defaultLimit
	if v, err := strconv.Atoi(getAttr(n, attrLimit)); err == nil && v > 0 {
		limit = min(v, maxLimit)
	}
	limit = min(limit, r.budget)
	if limit <= 0 || item == nil {
		return
	}

	filter, params := r.bind(getAttr(n, attrFilter), s)
	rules := getAttr(n, attrAccess) == "rules" && !r.visitor.all
	// pagination and search of the top level lists (paging.go)
	key, mode := listKey(n), getAttr(n, attrPaginate)
	if depth > 0 || (mode != "pages" && mode != "more") {
		mode = ""
	}
	sort := getAttr(n, attrSort)
	if depth == 0 {
		// the visitor's search and filters (paging.go, filters.go)
		for _, extra := range []string{r.searchFilter(col, key, params), r.filterExpr(col, key, params)} {
			if extra == "" {
				continue
			}
			if strings.TrimSpace(filter) != "" {
				filter = "(" + filter + ") && (" + extra + ")"
			} else {
				filter = extra
			}
		}
		if s := r.sortOf(key); s != "" {
			sort = s
		}
	}
	page, count, offset := 1, limit, 0
	switch mode {
	case "pages":
		page = r.pageOf(key)
		offset = (page - 1) * limit
	case "more":
		page = r.pageOf(key)
		count = min(limit*page, r.budget)
	}
	records, err := r.find(col, filter, sort, count, offset, params, rules)
	if err != nil {
		r.fail(n, err.Error())
		return
	}
	if mode != "" {
		total, err := r.count(col, filter, params, rules)
		if err != nil {
			r.fail(n, err.Error())
			return
		}
		defer r.pager(n, key, mode, page, limit, total)
	}
	r.budget -= len(records)
	scopes := make([]scope, len(records))
	for i, rec := range records {
		scopes[i] = scope{col: col, rec: rec, rules: rules}
	}
	r.prefetch(item, scopes)

	for _, rec := range records {
		clone := cloneNode(item)
		r.walk(clone, scope{col: col, rec: rec, rules: rules}, depth+1)
		n.AppendChild(clone)
	}
	if len(records) == 0 && empty != nil {
		r.walk(empty, s, depth+1)
		n.AppendChild(empty)
	}
}

// listCollection returns a collection the pages can show.
func listCollection(app core.App, name string) (*core.Collection, error) {
	col, err := app.FindCachedCollectionByNameOrId(name)
	if err != nil || internalcols.Is(col) {
		return nil, fmt.Errorf("unknown collection %q", name)
	}
	return col, nil
}

// find returns the records matching the filter. The filter and sort are set by
// the superuser who built the page, visitors only provide bound values
// ({{query.x}}). Without rules the page is the access gate (like forms); with
// rules the collection's list rule is applied for the visitor, as the REST API
// does, and the fields they can't read are hidden.
func (r *renderer) find(col *core.Collection, filter, sort string, limit, offset int, params dbx.Params, rules bool) ([]*core.Record, error) {
	if rules && col.ListRule == nil {
		return nil, nil // superusers only
	}
	ctx, cancel := context.WithTimeout(r.ctx, queryTimeout)
	defer cancel()
	q, info, err := r.listQuery(col, filter, sort, params, rules)
	if err != nil {
		return nil, err
	}
	var records []*core.Record
	if err := q.WithContext(ctx).Limit(int64(limit)).Offset(int64(offset)).All(&records); err != nil {
		return nil, err
	}
	if rules {
		enrich(r.app, info, records)
	}
	return records, nil
}

// count returns the number of records of a list (its pagination).
func (r *renderer) count(col *core.Collection, filter string, params dbx.Params, rules bool) (int, error) {
	if rules && col.ListRule == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(r.ctx, queryTimeout)
	defer cancel()
	q, _, err := r.listQuery(col, filter, "", params, rules)
	if err != nil {
		return 0, err
	}
	var total int
	err = q.WithContext(ctx).Select("COUNT(DISTINCT [[" + col.Name + ".id]])").OrderBy().Row(&total)
	return total, err
}

// listQuery is the query of the records of a list.
func (r *renderer) listQuery(col *core.Collection, filter, sort string, params dbx.Params, rules bool) (*dbx.SelectQuery, *core.RequestInfo, error) {
	q := r.app.RecordQuery(col)
	var info *core.RequestInfo
	if rules {
		info = r.info
	}
	resolver := core.NewRecordFieldResolver(r.app, col, info, true)
	if rules && *col.ListRule != "" {
		expr, err := search.FilterData(*col.ListRule).BuildExpr(resolver)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid list rule: %w", err)
		}
		q.AndWhere(expr)
	}
	if strings.TrimSpace(filter) != "" {
		expr, err := search.FilterData(filter).BuildExpr(resolver, params)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid filter: %w", err)
		}
		q.AndWhere(expr)
	}
	for _, sf := range sortFields(sort) {
		expr, err := sf.BuildExpr(resolver)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid sort: %w", err)
		}
		if expr != "" {
			q.AndOrderBy(expr)
		}
	}
	if err := resolver.UpdateQuery(q); err != nil {
		return nil, nil, err
	}
	return q, info, nil
}

// enrich runs the record enrich hooks of a request of the visitor: the fields
// hidden from them by the app (e.g. the field permissions of the roles) are
// marked hidden.
func enrich(app core.App, info *core.RequestInfo, records []*core.Record) {
	for _, rec := range records {
		ev := &core.RecordEnrichEvent{App: app, RequestInfo: info}
		ev.Record = rec
		_ = app.OnRecordEnrich().Trigger(ev, func(*core.RecordEnrichEvent) error { return nil })
	}
}

// sortFields parses "-created,name" (ParseSortFromString returns an empty
// field for an empty string).
func sortFields(sort string) []search.SortField {
	var out []search.SortField
	for _, sf := range search.ParseSortFromString(sort) {
		if strings.TrimSpace(sf.Name) != "" {
			out = append(out, sf)
		}
	}
	return out
}

var placeholderRegex = regexp.MustCompile(`\{\{\s*(record|query|auth)\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+){0,2})\s*\}\}`)

// bindPlaceholders replaces {{record.field}} (the current record),
// {{query.param}} (the page URL) and {{auth.field}} (the signed-in visitor,
// empty for guests) with bound params.
func bindPlaceholders(filter string, s scope, query url.Values, auth *core.Record) (string, dbx.Params) {
	return bindPlaceholdersWith(filter, s, query, auth, nil)
}

// bind binds the placeholders of a filter of the page; paths through
// relations ({{record.brand.country}}) are resolved.
func (r *renderer) bind(filter string, s scope) (string, dbx.Params) {
	return bindPlaceholdersWith(filter, s, r.query, r.visitor.user, r)
}

func bindPlaceholdersWith(filter string, s scope, query url.Values, auth *core.Record, r *renderer) (string, dbx.Params) {
	params := dbx.Params{}
	i := 0
	bound := placeholderRegex.ReplaceAllStringFunc(filter, func(m string) string {
		parts := placeholderRegex.FindStringSubmatch(m)
		var v any = ""
		switch parts[1] {
		case "record":
			if strings.Contains(parts[2], ".") {
				if r != nil {
					v = r.pathValue(s, parts[2])
				}
			} else if s.rec != nil && canShow(s, parts[2]) {
				v = s.rec.Get(parts[2])
			}
		case "auth":
			self := scope{col: nil, rec: auth, self: true}
			if auth != nil {
				self.col = auth.Collection()
			}
			if strings.Contains(parts[2], ".") {
				if r != nil && auth != nil {
					v = r.pathValue(self, parts[2])
				}
			} else if auth != nil && (parts[2] == "id" || canShow(self, parts[2])) {
				v = auth.Get(parts[2])
			}
		case "query":
			if strings.Contains(parts[2], ".") {
				break
			}
			q := query.Get(parts[2])
			if len(q) > 200 {
				q = q[:200]
			}
			v = q
		}
		key := "pbv" + strconv.Itoa(i)
		i++
		params[key] = v
		return "{:" + key + "}"
	})
	return bound, params
}

// visible reports whether a field of the record can be shown on a page:
// never hidden fields, passwords and tokens, and the email of auth records only
// when it's public.
func visible(col *core.Collection, rec *core.Record, name string) bool {
	if col == nil {
		return false
	}
	f := col.Fields.GetByName(name)
	if f == nil || f.GetHidden() || f.Type() == core.FieldTypePassword {
		return false
	}
	if col.IsAuth() {
		switch name {
		case core.FieldNameTokenKey, core.FieldNamePassword:
			return false
		case core.FieldNameEmail:
			return rec != nil && rec.EmailVisibility()
		}
	}
	return true
}

// canShow reports whether a field of the scope record can be shown (the
// visitor's own email too).
func canShow(s scope, name string) bool {
	if s.self && name == core.FieldNameEmail && s.col != nil && s.col.IsAuth() {
		return true
	}
	if s.rules && s.rec != nil {
		if _, ok := s.rec.PublicExport()[name]; !ok {
			return false // hidden from the visitor by the app
		}
	}
	return visible(s.col, s.rec, name)
}

// field shows a field of the scope record in the element: the source of
// images and videos, the target of links, the content of the other elements.
// The path may go through relations ("brand.logo"): several related records
// show their values separated by commas (images and links: the first one).
// It reports whether the element's children were replaced.
func (r *renderer) field(n *xhtml.Node, s scope, path string) bool {
	tag := strings.ToLower(n.Data)
	scopes, name := r.resolve(s, path)
	var shown []scope
	for _, sc := range scopes {
		if canShow(sc, name) {
			shown = append(shown, sc)
		}
	}
	if len(shown) == 0 {
		// nothing to show (no record, unknown or private field, no related record)
		switch tag {
		case "img", "video", "audio", "source":
			removeAttr(n, "src")
		case "a":
			if f := fieldOf(scopes, name); f != nil && isLinkField(f) {
				removeAttr(n, "href")
				return false
			}
			clearChildren(n)
		default:
			clearChildren(n)
			if r.preview && s.rec == nil {
				n.AppendChild(textNode("{" + path + "}"))
			}
		}
		return true
	}
	first := shown[0]
	f := first.col.Fields.GetByName(name)

	switch tag {
	case "img", "video", "audio", "source":
		u := ""
		for _, sc := range shown {
			if u = fieldURL(sc, sc.col.Fields.GetByName(name)); u != "" {
				first = sc
				break
			}
		}
		if u != "" {
			setAttr(n, "src", u)
		} else {
			removeAttr(n, "src")
		}
		if tag == "img" && getAttr(n, "alt") == "" {
			setAttr(n, "alt", labels.Of(first.rec, labels.Fields(first.col, false)))
		}
		return true
	case "a":
		// links to URLs, files and emails keep their content (which may show
		// fields too); other fields are the text of the link
		if isLinkField(f) {
			if u := fieldURL(first, f); u != "" {
				setAttr(n, "href", u)
			} else {
				removeAttr(n, "href")
			}
			return false
		}
	}

	clearChildren(n)
	if f.Type() == core.FieldTypeEditor {
		if n.DataAtom == atom.P || n.DataAtom == atom.Span {
			// rich text has paragraphs: they can't be in a paragraph or span
			n.Data, n.DataAtom = "div", atom.Div
		}
		for _, c := range parseHTMLFragment(first.rec.GetString(name)) {
			n.AppendChild(c)
		}
		return true
	}
	var texts []string
	for _, sc := range shown {
		if t := r.text(sc, sc.col.Fields.GetByName(name), getAttr(n, attrFormat)); t != "" {
			texts = append(texts, t)
		}
	}
	n.AppendChild(textNode(strings.Join(texts, ", ")))
	return true
}

func fieldOf(scopes []scope, name string) core.Field {
	for _, sc := range scopes {
		if f := sc.col.Fields.GetByName(name); f != nil {
			return f
		}
	}
	return nil
}

func isLinkField(f core.Field) bool {
	switch f.Type() {
	case core.FieldTypeURL, core.FieldTypeFile, core.FieldTypeEmail:
		return true
	}
	return false
}

// href sets the target of a link from a URL, email or file field (the
// content of the link stays). Without a value the link has no target.
func (r *renderer) href(n *xhtml.Node, s scope, path string) {
	scopes, name := r.resolve(s, path)
	for _, sc := range scopes {
		if !canShow(sc, name) {
			continue
		}
		if f := sc.col.Fields.GetByName(name); f != nil && isLinkField(f) {
			if u := fieldURL(sc, f); u != "" {
				setAttr(n, "href", u)
				return
			}
		}
	}
	removeAttr(n, "href")
}

// recordLink sets the target of a link to a record page: of the scope record
// ("slug"), or of a related record ("slug:brand", the first one).
func (r *renderer) recordLink(n *xhtml.Node, s scope, link string) {
	slug, via, _ := strings.Cut(link, ":")
	if via == "" {
		setAttr(n, "href", pageURL(slug, s.rec.Id))
		return
	}
	hops, last, ok := splitPath(via)
	if ok {
		if scopes := r.follow(s, append(hops, last)); len(scopes) > 0 {
			setAttr(n, "href", pageURL(slug, scopes[0].rec.Id))
			return
		}
	}
	removeAttr(n, "href")
}

// text returns the readable value of a field.
func (r *renderer) text(s scope, f core.Field, format string) string {
	name := f.GetName()
	switch f := f.(type) {
	case *core.DateField, *core.AutodateField:
		d := s.rec.GetDateTime(name)
		if d.IsZero() {
			return ""
		}
		switch format {
		case "datetime":
			return d.Time().UTC().Format("Jan 2, 2006 15:04")
		case "time":
			return d.Time().UTC().Format("15:04")
		default:
			return d.Time().UTC().Format("Jan 2, 2006")
		}
	case *core.BoolField:
		if s.rec.GetBool(name) {
			return "Yes"
		}
		return "No"
	case *core.NumberField:
		return strconv.FormatFloat(s.rec.GetFloat(name), 'f', -1, 64)
	case *core.SelectField:
		return strings.Join(s.rec.GetStringSlice(name), ", ")
	case *core.FileField:
		return strings.Join(s.rec.GetStringSlice(name), ", ")
	case *core.RelationField:
		return r.relationLabels(f, s.rec.GetStringSlice(name), s.rules)
	case *core.JSONField:
		raw, _ := json.Marshal(s.rec.Get(name))
		return string(raw)
	case *core.GeoPointField:
		raw, _ := json.Marshal(s.rec.Get(name))
		var p struct{ Lon, Lat float64 }
		_ = json.Unmarshal(raw, &p)
		return strconv.FormatFloat(p.Lat, 'f', -1, 64) + ", " + strconv.FormatFloat(p.Lon, 'f', -1, 64)
	}
	return s.rec.GetString(name)
}

// relationLabels returns the names of the related records.
func (r *renderer) relationLabels(f *core.RelationField, ids []string, rules bool) string {
	if len(ids) == 0 {
		return ""
	}
	col, err := r.app.FindCachedCollectionByNameOrId(f.CollectionId)
	if err != nil {
		return ""
	}
	// the related records the visitor can't view (with the rules) aren't named
	r.loadRelated(col, ids, rules)
	fields := labels.Fields(col, false)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if rec := r.related[relatedKey{col.Id, id, rules}]; rec != nil {
			names = append(names, labels.Of(rec, fields))
		}
	}
	return strings.Join(names, ", ")
}

// fieldURL returns the URL a field points to: its first file, a URL, an
// email (mailto:). Protected files need a token and are never linked.
func fieldURL(s scope, f core.Field) string {
	name := f.GetName()
	switch f := f.(type) {
	case *core.FileField:
		if f.Protected {
			return ""
		}
		files := s.rec.GetStringSlice(name)
		if len(files) == 0 {
			return ""
		}
		return "/api/files/" + url.PathEscape(s.col.Id) + "/" + url.PathEscape(s.rec.Id) + "/" + url.PathEscape(files[0])
	case *core.EmailField:
		if v := s.rec.GetString(name); v != "" {
			return "mailto:" + v
		}
		return ""
	}
	if v := s.rec.GetString(name); safeURL(v, false) {
		return v
	}
	return ""
}

func (r *renderer) fail(n *xhtml.Node, msg string) {
	r.app.Logger().Warn("Page binding error", "error", msg)
	if r.preview {
		p := &xhtml.Node{
			Type: xhtml.ElementNode, Data: "p", DataAtom: atom.P,
			Attr: []xhtml.Attribute{{Key: "style", Val: "color:#c00;font:14px sans-serif;border:1px dashed #c00;padding:8px"}},
		}
		p.AppendChild(textNode("⚠ " + msg + " (only shown in previews)"))
		n.AppendChild(p)
	}
}

// --- sanitizing --------------------------------------------------------------------------

// dropped elements (scripts are also blocked by the page CSP)
var droppedTags = map[string]bool{
	"script": true, "noscript": true, "object": true, "embed": true, "applet": true, "base": true,
	"meta": true, "link": true, "frame": true, "frameset": true, "template": true, "title": true,
	"head": true, "portal": true,
}

var urlAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true, "poster": true, "xlink:href": true,
	"background": true, "cite": true, "data": true,
}

// sanitize removes scripts, event handlers and unsafe URLs from the HTML.
func sanitize(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch c.Type {
		case xhtml.ElementNode:
			if droppedTags[strings.ToLower(c.Data)] {
				n.RemoveChild(c)
			} else {
				sanitizeAttrs(c)
				sanitize(c)
			}
		case xhtml.CommentNode:
			n.RemoveChild(c)
		}
		c = next
	}
}

func sanitizeAttrs(n *xhtml.Node) {
	tag := strings.ToLower(n.Data)
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" {
			key = strings.ToLower(a.Namespace) + ":" + key
		}
		switch {
		case strings.HasPrefix(key, "on"), key == "srcdoc", key == "srcset" && !safeSrcset(a.Val):
			continue
		case urlAttrs[key] && !safeURL(a.Val, key == "src" && tag == "img"):
			continue
		case key == "href" || key == "action" || key == "formaction" || key == "data-pb-logout":
			a.Val = currentPath(a.Val)
		}
		kept = append(kept, a)
	}
	n.Attr = kept
	// third-party embeds (videos, maps) run sandboxed
	if tag == "iframe" && !hasAttr(n, attrForm) && !hasAttr(n, "sandbox") {
		n.Attr = append(n.Attr, xhtml.Attribute{Key: "sandbox", Val: "allow-scripts allow-same-origin allow-popups allow-presentation"})
	}
}

// currentPath turns an old /p/{slug} address (saved in the pages before the
// pages moved to the root) into the page's address, without a redirect.
func currentPath(v string) string {
	rest, ok := strings.CutPrefix(v, "/p/")
	if !ok || strings.HasPrefix(rest, "_") {
		return v
	}
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	if slug := rest[:end]; slug == "" || reservedSlugs[slug] || !rootPages {
		return v
	}
	return "/" + rest
}

// safeURL accepts relative URLs and the http(s), mailto and tel schemes
// (data:image/ for images).
func safeURL(v string, image bool) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1 // browsers ignore control characters in schemes
		}
		return r
	}, s)
	i := strings.IndexAny(s, ":/?#")
	if i < 0 || s[i] != ':' {
		return true // relative
	}
	switch s[:i] {
	case "http", "https", "mailto", "tel":
		return true
	case "data":
		return image && strings.HasPrefix(s, "data:image/") && !strings.HasPrefix(s, "data:image/svg")
	}
	return false
}

func safeSrcset(v string) bool {
	for _, part := range strings.Split(v, ",") {
		if u, _, _ := strings.Cut(strings.TrimSpace(part), " "); !safeURL(u, true) {
			return false
		}
	}
	return true
}

// --- node helpers ------------------------------------------------------------------------

func getAttr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func hasAttr(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return true
		}
	}
	return false
}

func setAttr(n *xhtml.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, xhtml.Attribute{Key: key, Val: val})
}

func removeAttr(n *xhtml.Node, key string) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Namespace != "" || a.Key != key {
			kept = append(kept, a)
		}
	}
	n.Attr = kept
}

// removeBindingAttrs keeps the bindings (e.g. the filters) out of the page.
func removeBindingAttrs(n *xhtml.Node) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if !strings.HasPrefix(a.Key, "data-pb-") {
			kept = append(kept, a)
		}
	}
	n.Attr = kept
}

func clearChildren(n *xhtml.Node) {
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
}

func textNode(s string) *xhtml.Node {
	return &xhtml.Node{Type: xhtml.TextNode, Data: s}
}

func cloneNode(n *xhtml.Node) *xhtml.Node {
	c := &xhtml.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	c.Attr = append([]xhtml.Attribute(nil), n.Attr...)
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.AppendChild(cloneNode(ch))
	}
	return c
}
