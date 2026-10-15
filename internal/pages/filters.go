package pages

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
)

// Filters of the collection lists, without scripts (like the search box,
// paging.go): a GET form filled at render with a control per field.
//
//	<form data-pb-filters="cards" data-pb-filter-fields="rarity,set,hp,released" data-pb-sort-options="-hp:Strongest,name:Name"></form>
//
// The address holds the choices: f.rarity=Rare (a choice, a related record,
// yes/no "1"/"0", one of the texts), f.name_has=pika (a text typed: the
// fields with too many values to choose from), f.hp_min / f.hp_max (numbers), f.released_from /
// f.released_to (dates, YYYY-MM-DD) and sort=-hp (one of the sort options),
// prefixed "{key}_" when several lists page, search or filter.
const (
	attrFilters      = "data-pb-filters"       // form: filters the list with this key
	attrFilterFields = "data-pb-filter-fields" // form: the fields, comma separated (own fields of the collection)
	attrSortOptions  = "data-pb-sort-options"  // form: "sort:Label" pairs, comma separated ("-hp:Strongest")

	maxFilterFields  = 12
	maxSortOptions   = 10
	maxFilterOptions = 200 // related records of a relation filter
	maxTextOptions   = 100 // distinct values of a text filter
	filterParam      = "f."
)

type sortOption struct{ sort, label string }

// filter kinds
const (
	filterChoice   = "choice"   // select field
	filterBool     = "bool"     // yes / no
	filterRelation = "relation" // a related record
	filterNumber   = "number"   // min / max
	filterDate     = "date"     // from / to
	filterText     = "text"     // one of the values
)

var (
	filterIDRegex   = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)
	filterDateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// filterKind returns the kind of filter of a field, "" when it can't filter:
// an own field a page may show.
func (r *renderer) filterKind(col *core.Collection, name string) (core.Field, string) {
	if col == nil || strings.Contains(name, ".") || !visible(col, nil, name) {
		return nil, ""
	}
	f := col.Fields.GetByName(name)
	switch f := f.(type) {
	case *core.SelectField:
		return f, filterChoice
	case *core.BoolField:
		return f, filterBool
	case *core.RelationField:
		if rel, err := r.app.FindCachedCollectionByNameOrId(f.CollectionId); err != nil || internalcols.Is(rel) {
			return nil, ""
		}
		return f, filterRelation
	case *core.NumberField:
		return f, filterNumber
	case *core.DateField, *core.AutodateField:
		return f, filterDate
	case *core.TextField, *core.EmailField, *core.URLField:
		if name == "id" {
			return nil, ""
		}
		return f, filterText
	}
	return nil, ""
}

func multiple(f core.Field) bool {
	switch f := f.(type) {
	case *core.SelectField:
		return f.IsMultiple()
	case *core.RelationField:
		return f.IsMultiple()
	}
	return false
}

// parseSortOptions reads "-hp:Strongest,name:Name": the sort of a field the
// list could show, and its label.
func (r *renderer) parseSortOptions(col *core.Collection, src string) []sortOption {
	var out []sortOption
	for _, part := range strings.Split(src, ",") {
		sort, label, _ := strings.Cut(strings.TrimSpace(part), ":")
		name := strings.TrimPrefix(sort, "-")
		if name == "" || len(out) >= maxSortOptions {
			continue
		}
		if col != nil && name != "created" && name != "updated" {
			if _, kind := r.filterKind(col, name); kind == "" || kind == filterRelation {
				continue
			}
		} else if col != nil && col.Fields.GetByName(name) == nil {
			continue
		}
		if label = strings.TrimSpace(label); label == "" {
			label = fieldLabel(name)
		}
		out = append(out, sortOption{sort, label})
	}
	return out
}

// sortOf returns the sort chosen by the visitor for a list ("" for the list's own).
func (r *renderer) sortOf(key string) string {
	if r.lists == nil {
		return ""
	}
	v := r.query.Get(r.param(key, "sort"))
	for _, o := range r.lists.sorts[key] {
		if o.sort == v {
			return v
		}
	}
	return ""
}

// filterValue is a choice of the visitor read from the address.
type filterValue struct {
	field, kind string
	param       string // the address parameter
	value       string
	bound       string // "min", "max", "from", "to" (numbers and dates), "has" (a typed text)
}

// filterValues returns the valid choices of the address for a list.
func (r *renderer) filterValues(col *core.Collection, key string) []filterValue {
	if r.lists == nil {
		return nil
	}
	var out []filterValue
	for _, name := range r.lists.filters[key] {
		f, kind := r.filterKind(col, name)
		if f == nil {
			continue
		}
		read := func(suffix, bound string) {
			p := r.param(key, filterParam+name+suffix)
			v := strings.TrimSpace(r.query.Get(p))
			if v == "" {
				return
			}
			ok := false
			switch kind {
			case filterChoice:
				ok = slices.Contains(f.(*core.SelectField).Values, v)
			case filterBool:
				ok = v == "1" || v == "0"
			case filterRelation:
				ok = filterIDRegex.MatchString(v)
			case filterNumber:
				_, err := strconv.ParseFloat(v, 64)
				ok = err == nil
			case filterDate:
				_, err := time.Parse(time.DateOnly, v)
				ok = err == nil && filterDateRegex.MatchString(v)
			case filterText:
				ok = len(v) <= 200
			}
			if ok {
				out = append(out, filterValue{field: name, kind: kind, param: p, value: v, bound: bound})
			}
		}
		switch kind {
		case filterNumber:
			read("_min", "min")
			read("_max", "max")
		case filterDate:
			read("_from", "from")
			read("_to", "to")
		case filterText:
			read("", "")
			read("_has", "has")
		default:
			read("", "")
		}
	}
	return out
}

// filterExpr returns the filter of the visitor's choices on a list.
func (r *renderer) filterExpr(col *core.Collection, key string, params dbx.Params) string {
	var all []string
	for i, v := range r.filterValues(col, key) {
		p := "pbf" + strconv.Itoa(i)
		op := "="
		var value any = v.value
		switch v.kind {
		case filterChoice, filterRelation:
			if multiple(col.Fields.GetByName(v.field)) {
				op = "?="
			}
		case filterBool:
			value = v.value == "1"
		case filterText:
			if v.bound == "has" {
				op = "~"
			}
		case filterNumber:
			value, _ = strconv.ParseFloat(v.value, 64)
			op = map[string]string{"min": ">=", "max": "<="}[v.bound]
		case filterDate:
			d, _ := time.Parse(time.DateOnly, v.value)
			if v.bound == "to" {
				d = d.AddDate(0, 0, 1) // the whole day
				op = "<"
			} else {
				op = ">="
			}
			value = d.UTC().Format("2006-01-02 15:04:05.000Z")
		}
		params[p] = value
		all = append(all, v.field+" "+op+" {:"+p+"}")
	}
	return strings.Join(all, " && ")
}

// fieldLabel writes a field name for people: "released_at" → "Released at".
func fieldLabel(name string) string {
	s := strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(name))
	if s == "" {
		return name
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type filterOption struct{ value, label string }

// filterOptions returns the choices of a field: its values, the related
// records the visitor may list (always with the API rules of their
// collection: the page shows the list's collection, not all of the related
// one), the distinct texts of the records the list may show (its filter).
func (r *renderer) filterOptions(col *core.Collection, f core.Field, kind string, rules bool, listFilter string, listParams dbx.Params) []filterOption {
	switch kind {
	case filterChoice:
		var out []filterOption
		for _, v := range f.(*core.SelectField).Values {
			out = append(out, filterOption{v, v})
		}
		return out
	case filterBool:
		return []filterOption{{"1", "Yes"}, {"0", "No"}}
	case filterRelation:
		rel, err := r.app.FindCachedCollectionByNameOrId(f.(*core.RelationField).CollectionId)
		if err != nil || rel.ListRule == nil {
			return nil
		}
		fields := labels.Fields(rel, false)
		sort := ""
		if len(fields) > 0 {
			sort = fields[0]
		}
		records, err := r.find(rel, "", sort, maxFilterOptions, 0, dbx.Params{}, !r.visitor.all)
		if err != nil {
			return nil
		}
		var out []filterOption
		for _, rec := range records {
			out = append(out, filterOption{rec.Id, labels.Of(rec, fields)})
		}
		return out
	case filterText:
		if rules && col.ListRule == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(r.ctx, queryTimeout)
		defer cancel()
		q, _, err := r.listQuery(col, listFilter, "", listParams, rules)
		if err != nil {
			return nil
		}
		var values []string
		name := f.GetName()
		err = q.WithContext(ctx).Distinct(true).Select("[[" + col.Name + "." + name + "]]").
			AndWhere(dbx.NewExp("[[" + col.Name + "." + name + "]] != ''")).
			OrderBy(col.Name + "." + name).Limit(maxTextOptions + 1).Column(&values)
		if err != nil || len(values) > maxTextOptions {
			return nil // too many to choose from: the search box fits better
		}
		out := make([]filterOption, len(values))
		for i, v := range values {
			out[i] = filterOption{v, v}
		}
		return out
	}
	return nil
}

// filterForm fills a filters form of the page: a GET form of the list's
// parameters with the visitor's choices, keeping the other parameters.
func (r *renderer) filterForm(n *xhtml.Node, s scope) {
	key := cleanKey(getAttr(n, attrFilters))
	list := r.lists.nodes[key]
	if list == nil {
		r.fail(n, "choose the list the filters apply to")
		return
	}
	col, err := listCollection(r.app, getAttr(list, attrList))
	if err != nil {
		r.fail(n, err.Error())
		return
	}
	rules := getAttr(list, attrAccess) == "rules" && !r.visitor.all
	listFilter, listParams := r.bind(getAttr(list, attrFilter), s)
	path, _, _ := strings.Cut(r.path, "?")
	if path == "" {
		path = "/"
	}
	clearChildren(n)
	setAttr(n, "method", "get")
	setAttr(n, "action", path)
	setAttr(n, "role", "search")
	setAttr(n, "aria-label", "Filters")
	removeAttr(n, "enctype")
	if c := getAttr(n, "class"); !strings.Contains(" "+c+" ", " pb-filters ") {
		setAttr(n, "class", strings.TrimSpace(c+" pb-filters"))
	}
	r.filterCSS = true

	anchor := getAttr(list, "id")
	pageParam := r.param(key, "page")
	active := r.filterValues(col, key)
	chosen := map[string]string{}
	for _, v := range active {
		chosen[v.param] = v.value
	}

	// the choices made, each with a link removing it
	fields := elem("div", "class", "pb-filter-fields")
	var chips []*xhtml.Node
	for _, name := range r.lists.filters[key] {
		f, kind := r.filterKind(col, name)
		if f == nil {
			continue
		}
		label := fieldLabel(name)
		box := elem("label", "class", "pb-filter-field")
		title := elem("span", "class", "pb-filter-label label")
		title.AppendChild(textNode(label))
		box.AppendChild(title)
		switch kind {
		case filterNumber, filterDate:
			typ, lo, hi := "number", "_min", "_max"
			loText, hiText := "Min", "Max"
			if kind == filterDate {
				typ, lo, hi = "date", "_from", "_to"
				loText, hiText = "From", "To"
			}
			rng := elem("span", "class", "pb-filter-range")
			for _, b := range []struct{ suffix, text string }{{lo, loText}, {hi, hiText}} {
				p := r.param(key, filterParam+name+b.suffix)
				in := elem("input", "type", typ, "class", "input", "name", p, "placeholder", b.text, "aria-label", label+" "+strings.ToLower(b.text))
				if typ == "number" {
					setAttr(in, "step", "any")
				}
				if v := chosen[p]; v != "" {
					setAttr(in, "value", v)
					chips = append(chips, r.filterChip(label+" "+strings.ToLower(b.text)+": "+v, p, pageParam, anchor))
				}
				rng.AppendChild(in)
			}
			box.AppendChild(rng)
		default:
			options := r.filterOptions(col, f, kind, rules, listFilter, listParams)
			p := r.param(key, filterParam+name)
			if kind == filterText && options == nil {
				// too many values: the visitor types a part of one
				p += "_has"
				in := elem("input", "type", "search", "class", "input", "name", p, "placeholder", "Contains…")
				if v := chosen[p]; v != "" {
					setAttr(in, "value", v)
					chips = append(chips, r.filterChip(label+" contains: "+v, p, pageParam, anchor))
				}
				box.AppendChild(in)
				break
			}
			sel := elem("select", "class", "select", "name", p)
			anyOpt := elem("option", "value", "")
			anyOpt.AppendChild(textNode("Any"))
			sel.AppendChild(anyOpt)
			for _, o := range options {
				opt := elem("option", "value", o.value)
				if chosen[p] == o.value {
					setAttr(opt, "selected", "")
					chips = append(chips, r.filterChip(label+": "+o.label, p, pageParam, anchor))
				}
				opt.AppendChild(textNode(o.label))
				sel.AppendChild(opt)
			}
			box.AppendChild(sel)
		}
		fields.AppendChild(box)
	}

	// the sort options
	sortParam := r.param(key, "sort")
	if opts := r.lists.sorts[key]; len(opts) > 0 {
		box := elem("label", "class", "pb-filter-field")
		title := elem("span", "class", "pb-filter-label label")
		title.AppendChild(textNode("Sort by"))
		box.AppendChild(title)
		sel := elem("select", "class", "select", "name", sortParam)
		def := elem("option", "value", "")
		def.AppendChild(textNode("Default"))
		sel.AppendChild(def)
		current := r.sortOf(key)
		for _, o := range opts {
			opt := elem("option", "value", o.sort)
			if o.sort == current {
				setAttr(opt, "selected", "")
			}
			opt.AppendChild(textNode(o.label))
			sel.AppendChild(opt)
		}
		box.AppendChild(sel)
		fields.AppendChild(box)
	}

	if len(chips) > 0 {
		row := elem("div", "class", "pb-filter-chips")
		for _, c := range chips {
			row.AppendChild(c)
		}
		n.AppendChild(row)
	}
	n.AppendChild(fields)

	actions := elem("div", "class", "pb-filter-actions")
	apply := elem("button", "type", "submit", "class", "btn")
	apply.AppendChild(textNode("Apply"))
	actions.AppendChild(apply)
	if len(active) > 0 || r.sortOf(key) != "" {
		clear := map[string]string{pageParam: "", sortParam: ""}
		for _, v := range active {
			clear[v.param] = ""
		}
		a := elem("a", "class", "btn", "data-variant", "ghost", "href", r.linkWith(clear, anchor))
		a.AppendChild(textNode("Clear"))
		actions.AppendChild(a)
	}
	n.AppendChild(actions)

	// the other parameters stay (the search, other lists...), not the page
	own := r.param(key, filterParam)
	for k, vs := range r.query {
		if strings.HasPrefix(k, own) || k == sortParam || k == pageParam || (!r.preview && (k == "preview" || k == "as")) {
			continue
		}
		for _, v := range vs {
			if v != "" { // the empty fields of another form
				n.AppendChild(elem("input", "type", "hidden", "name", k, "value", v))
			}
		}
	}
}

// filterChip is a link removing a choice of the visitor.
func (r *renderer) filterChip(text, param, pageParam, anchor string) *xhtml.Node {
	a := elem("a", "class", "pb-filter-chip badge", "data-variant", "secondary",
		"href", r.linkWith(map[string]string{param: "", pageParam: ""}, anchor), "aria-label", "Remove the filter "+text)
	a.AppendChild(textNode(text + " ✕"))
	return a
}

// filterCSS lays the filters out (the kit styles the controls).
const filterCSS = `
.pb-filters{display:flex;flex-direction:column;gap:10px;margin:16px 0}
.pb-filter-fields{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:10px}
.pb-filter-field{display:flex;flex-direction:column;gap:4px;min-width:0}
.pb-filter-field select,.pb-filter-field input{width:100%;min-width:0}
.pb-filter-range{display:flex;gap:6px}
.pb-filter-chips{display:flex;flex-wrap:wrap;gap:6px}
.pb-filter-chip{text-decoration:none}
.pb-filter-actions{display:flex;gap:8px;align-items:center}
`

// filterPlainCSS styles the filters of the pages without the UI kit.
const filterPlainCSS = `
.pb-filter-label{font-size:.85em;font-weight:600}
.pb-filters select,.pb-filters input{padding:6px 8px;border:1px solid #d4d4d8;border-radius:6px;font:inherit;background:transparent;color:inherit}
.pb-filters .btn{display:inline-flex;align-items:center;padding:6px 14px;border:1px solid currentColor;border-radius:6px;font:inherit;font-weight:600;color:inherit;background:transparent;cursor:pointer;text-decoration:none}
.pb-filters .btn[data-variant=ghost]{border-color:transparent}
.pb-filter-chip{padding:2px 10px;border-radius:999px;background:rgba(127,127,127,.15);color:inherit;font-size:.85em}
`
