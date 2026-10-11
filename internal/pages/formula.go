package pages

import (
	"net/url"
	"regexp"
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

var formulaRoots = []string{"record", "auth", "query"}

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
	return map[string]any{
		"record": r.recordMap(s, chains["record"]),
		"auth":   r.recordMap(self, chains["auth"]),
		"query":  query,
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
	for _, m := range formulaPathRegex.FindAllStringSubmatch(getAttr(n, attrFormula), -1) {
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

// formulaPreview runs a formula of the builder on a sample record
// (POST /api/pages/{id}/formula {formula, collection, record, as}).
func formulaPreview(e *core.RequestEvent) error {
	if _, err := e.App.FindRecordById(Collection, e.Request.PathValue("id")); err != nil {
		return e.NotFoundError("", nil)
	}
	var body struct {
		Formula    string `json:"formula"`
		Collection string `json:"collection"`
		Record     string `json:"record"`
		As         string `json:"as"`
	}
	if err := e.BindBody(&body); err != nil || !validAs(body.As) {
		return e.BadRequestError("Invalid formula.", err)
	}
	v := previewVisitor(e.App, body.As)
	s := scope{}
	if body.Collection != "" && body.Record != "" {
		if col, err := listCollection(e.App, body.Collection); err == nil {
			if rec, err := e.App.FindRecordById(col, body.Record); err == nil {
				s = scope{col: col, rec: rec}
			}
		}
	}
	r := &renderer{app: e.App, ctx: e.Request.Context(), query: url.Values{}, preview: true, budget: maxRecords,
		visitor: v, info: v.requestInfo(), path: "/"}
	out, err := r.evalFormula(s, body.Formula)
	if err != nil {
		return e.JSON(200, map[string]string{"error": err.Error()})
	}
	return e.JSON(200, map[string]string{"text": out})
}
