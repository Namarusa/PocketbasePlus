package pages

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
)

// A formula shows a computed text in an element:
//
//	{{formatDate "DD/MM/YYYY" record.created}}
//	{{currency "EUR" (multiply record.price record.qty)}}
//	Hello {{default "guest" auth.name}}, {{count record.tags}} tags
//
// record is the current record (of the record page or the list item), auth
// the signed-in visitor (empty for guests) and query the page URL parameters.
// They hold the fields the visitor may see, as the field bindings do.
// Relations used in the formula ("record.brand.name") are the related
// records (a list of them for multiple relations: pluck "name"
// record.tags); a related record alone prints its name. The functions are
// those of internal/formula; the result is text (HTML is escaped).
const attrFormula = "data-pb-formula"

const maxFormula = 4 << 10

var formulaRoots = []string{"record", "auth", "query", "error"}

// relation chains of a formula: record.brand.country.name: [brand country]
var formulaPathRegex = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.$])\$?\.?(record|auth)((?:\.[A-Za-z0-9_]+)+)`)

// formula fills the element with the result of its formula.
func (r *renderer) formula(n *xhtml.Node, s scope, src string) {
	clearChildren(n)
	out, err := r.evalFormula(s, src)
	if err != nil {
		r.fail(n, "formula: "+err.Error())
		return
	}
	n.AppendChild(textNode(out))
}

// Dynamic values: a formula for an attribute or a style of any element
// (data-pb-attr-src="{{record.image}}/high.webp",
// data-pb-style-color="{{if gt record.stock 0}}green{{else}}red{{end}}").
// The result replaces the attribute (class: added to the element's classes;
// empty: the attribute is removed) or sets the style property.
const (
	attrDynamic  = "data-pb-attr-"
	styleDynamic = "data-pb-style-"
)

// the attributes and the style properties a formula may set
var (
	dynamicAttrs = map[string]bool{
		"src": true, "href": true, "alt": true, "title": true, "class": true, "width": true, "height": true,
		"placeholder": true, "value": true, "aria-label": true, "poster": true,
	}
	dynamicStyles = map[string]bool{
		"color": true, "background-color": true, "border-color": true, "width": true, "height": true,
		"max-width": true, "min-height": true, "font-size": true, "font-weight": true, "opacity": true,
		"display": true, "text-align": true, "padding": true, "margin": true, "border-radius": true,
		"border-width": true, "gap": true, "transform": true,
	}
	// a style value: no other declaration, no rule, no quote, no escape
	styleValueRegex = regexp.MustCompile(`^[#%(),.\-+*/!\w\s]*$`)
	classRegex      = regexp.MustCompile(`[^A-Za-z0-9_\-: ]`)
)

// hasDynamic reports whether the element has dynamic values.
func hasDynamic(n *xhtml.Node) bool {
	for _, a := range n.Attr {
		if strings.HasPrefix(a.Key, attrDynamic) || strings.HasPrefix(a.Key, styleDynamic) {
			return true
		}
	}
	return false
}

// dynamic sets the attributes and styles of the element computed by formulas.
func (r *renderer) dynamic(n *xhtml.Node, s scope) {
	var styles []string
	tag := strings.ToLower(n.Data)
	for _, a := range append([]xhtml.Attribute(nil), n.Attr...) {
		attr, isAttr := strings.CutPrefix(a.Key, attrDynamic)
		prop, isStyle := strings.CutPrefix(a.Key, styleDynamic)
		if (!isAttr || !dynamicAttrs[attr]) && (!isStyle || !dynamicStyles[prop]) || strings.TrimSpace(a.Val) == "" {
			continue
		}
		v, err := r.evalFormula(s, a.Val)
		if err != nil {
			r.app.Logger().Warn("Page binding error", "error", "formula of "+attr+prop+": "+err.Error())
			if r.preview {
				setAttr(n, "title", "⚠ formula of "+attr+prop+": "+err.Error())
			}
			continue
		}
		v = strings.TrimSpace(v)
		if isStyle {
			if v != "" && dynamicStyle(v) {
				styles = append(styles, prop+":"+v)
			}
			continue
		}
		switch {
		case attr == "class":
			if v = strings.Join(strings.Fields(classRegex.ReplaceAllString(v, "")), " "); v != "" {
				if old := getAttr(n, "class"); old != "" {
					v = old + " " + v
				}
				setAttr(n, "class", v)
			}
			continue
		case v == "":
			removeAttr(n, attr)
			continue
		case urlAttrs[attr]:
			if !safeURL(v, attr == "src" && tag == "img") {
				removeAttr(n, attr)
				continue
			}
			if attr == "href" {
				v = currentPath(v)
			}
		}
		setAttr(n, attr, v)
	}
	if len(styles) > 0 {
		style := strings.TrimRight(strings.TrimSpace(getAttr(n, "style")), ";")
		if style != "" {
			style += ";"
		}
		setAttr(n, "style", style+strings.Join(styles, ";"))
	}
}

// dynamicStyle reports whether a computed style value is safe: one value,
// no url() or image (no request to another site).
func dynamicStyle(v string) bool {
	l := strings.ToLower(v)
	return len(v) <= 200 && styleValueRegex.MatchString(v) &&
		!strings.Contains(l, "url") && !strings.Contains(l, "image") && !strings.Contains(l, "expression")
}

func (r *renderer) evalFormula(s scope, src string) (string, error) {
	if len(src) > maxFormula {
		return "", errFormulaTooLong
	}
	if r.formulas == nil {
		r.formulas = map[string]*template.Template{}
	}
	t, ok := r.formulas[src]
	if !ok {
		var err error
		if t, err = formula.Parse(src, formulaRoots...); err != nil {
			return "", cleanParseError(err)
		}
		r.formulas[src] = t
	}
	return formula.Execute(t, r.formulaData(s, src))
}

type formulaError string

func (e formulaError) Error() string { return string(e) }

const errFormulaTooLong = formulaError("the formula is too long")

// cleanParseError drops the "template: formula:1:" prefix of parse errors.
func cleanParseError(err error) error {
	msg := strings.TrimPrefix(err.Error(), "template: formula:")
	return formulaError(msg)
}

func (r *renderer) formulaData(s scope, src string) map[string]any {
	chains := map[string][][]string{}
	for _, m := range formulaPathRegex.FindAllStringSubmatch(src, -1) {
		parts := strings.Split(strings.TrimPrefix(m[2], "."), ".")
		if len(parts) > 1 {
			hops := parts[:min(len(parts)-1, maxHops)]
			chains[m[1]] = append(chains[m[1]], hops)
		} else {
			chains[m[1]] = append(chains[m[1]], parts) // the relation itself: its records
		}
	}
	var self scope
	if u := r.visitor.user; u != nil {
		self = scope{col: u.Collection(), rec: u, self: true}
	}
	query := map[string]any{}
	for k, v := range r.query {
		if len(v) > 0 && k != "preview" && k != "as" {
			q := v[0]
			if len(q) > 200 {
				q = q[:200]
			}
			query[k] = q
		}
	}
	errInfo := r.errorInfo
	if errInfo == nil {
		errInfo = map[string]any{}
	}
	return map[string]any{
		"record": r.recordMap(s, chains["record"]),
		"auth":   r.recordMap(self, chains["auth"]),
		"query":  query,
		"error":  errInfo,
	}
}

// relatedRecord is a record reached through a relation: it prints its name.
type relatedRecord map[string]any

func (rr relatedRecord) String() string {
	if l, ok := rr["@label"].(string); ok {
		return l
	}
	return ""
}

// recordMap returns the fields of a record the visitor may see; the relations
// of the chains are their related records.
func (r *renderer) recordMap(s scope, chains [][]string) map[string]any {
	m := map[string]any{}
	if s.rec == nil || s.col == nil {
		return m
	}
	m["id"] = s.rec.Id
	m["collectionName"] = s.col.Name
	for _, f := range s.col.Fields {
		name := f.GetName()
		if !canShow(s, name) {
			continue
		}
		m[name] = fieldValue(s.rec, f)
	}
	next := map[string][][]string{}
	for _, c := range chains {
		if len(c) > 0 {
			next[c[0]] = append(next[c[0]], c[1:])
		}
	}
	for hop, rest := range next {
		f, col := r.relationTarget(s, hop)
		if col == nil {
			continue
		}
		var items []any
		for _, sc := range r.hop(s, hop) {
			rm := relatedRecord(r.recordMap(sc, rest))
			rm["@label"] = labels.Of(sc.rec, labels.Fields(sc.col, false))
			items = append(items, rm)
		}
		if f.IsMultiple() {
			m[hop] = formula.List(items)
		} else if len(items) > 0 {
			m[hop] = items[0]
		} else {
			m[hop] = nil
		}
	}
	return m
}

// fieldValue returns the value of a field for the formulas: numbers print
// without exponent, JSON is decoded, dates stay dates.
func fieldValue(rec *core.Record, f core.Field) any {
	name := f.GetName()
	switch f.(type) {
	case *core.NumberField:
		return formula.Number(rec.GetFloat(name))
	case *core.DateField, *core.AutodateField:
		d := rec.GetDateTime(name)
		if d.IsZero() {
			return ""
		}
		return d
	case *core.JSONField:
		return formula.Plain(rec.Get(name))
	}
	return rec.Get(name)
}

// formulaPaths returns the relation paths of the formula of an element, to
// load the related records of a list at once ("brand.id").
func formulaPaths(n *xhtml.Node) []string {
	var out []string
	src := getAttr(n, attrFormula) + " " + getAttr(n, attrConditions)
	for _, a := range n.Attr {
		if strings.HasPrefix(a.Key, attrDynamic) || strings.HasPrefix(a.Key, styleDynamic) {
			src += " " + a.Val
		}
	}
	for _, m := range formulaPathRegex.FindAllStringSubmatch(src, -1) {
		if m[1] != "record" {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(m[2], "."), ".")
		if len(parts) > 1 {
			out = append(out, strings.Join(parts[:min(len(parts)-1, maxHops)], ".")+".id")
		} else {
			out = append(out, parts[0]+".id")
		}
	}
	return out
}

// --- builder preview ------------------------------------------------------------------------

// previewBody is the sample of a builder preview: a record of the
// collection, seen as a visitor ("as").
type previewBody struct {
	Collection string `json:"collection"`
	Record     string `json:"record"`
	As         string `json:"as"`
}

// previewRenderer reads the body of a builder preview (dst embeds pb) and
// returns a renderer of the page with the sample record's scope.
func previewRenderer(e *core.RequestEvent, dst any, pb *previewBody) (*renderer, scope, error) {
	page, err := e.App.FindRecordById(Collection, e.Request.PathValue("id"))
	if err != nil {
		return nil, scope{}, e.NotFoundError("", nil)
	}
	if err := e.BindBody(dst); err != nil || !validAs(pb.As) {
		return nil, scope{}, e.BadRequestError("Invalid preview.", err)
	}
	v := previewVisitor(e.App, pb.As)
	s := scope{}
	if pb.Collection != "" && pb.Record != "" {
		if col, err := listCollection(e.App, pb.Collection); err == nil {
			if rec, err := e.App.FindRecordById(col, pb.Record); err == nil {
				s = scope{col: col, rec: rec}
			}
		}
	}
	r := &renderer{app: e.App, ctx: e.Request.Context(), query: url.Values{}, preview: true, budget: maxRecords,
		visitor: v, info: v.requestInfo(), path: "/"}
	if page.GetString("kind") == kindError {
		code, _ := strconv.Atoi(page.GetString("error_code"))
		r.errorInfo = errorInfo(e.App, code, "/an/address")
	}
	return r, s, nil
}

// formulaPreview runs a formula of the builder on a sample record
// (POST /api/pages/{id}/formula {formula, collection, record, as}).
func formulaPreview(e *core.RequestEvent) error {
	var body struct {
		Formula string `json:"formula"`
		previewBody
	}
	r, s, err := previewRenderer(e, &body, &body.previewBody)
	if err != nil {
		return err
	}
	out, err := r.evalFormula(s, body.Formula)
	if err != nil {
		return e.JSON(200, map[string]string{"error": err.Error()})
	}
	return e.JSON(200, map[string]string{"text": out})
}
