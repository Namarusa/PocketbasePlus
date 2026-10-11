package pages

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
)

// Bindings can follow relations: "brand.name" shows the name of the product's
// brand, "order.customer.email" goes through two relations. A path has at most
// maxHops relations. Every step applies the rules of the record's own fields:
// the relation field must be visible, the related records too (with the
// visitor's API rules: their view rule and the fields the app hides from
// them), and module collections are out of reach. The related records are
// loaded once per page view, for a whole list at a time.

const maxHops = 2

// relatedKey identifies a related record read with or without the rules.
type relatedKey struct {
	col, id string
	rules   bool
}

// splitPath returns the relations of a binding path and its last field
// ("brand.name": ["brand"], "name"), or ok false for an invalid path.
func splitPath(path string) (hops []string, name string, ok bool) {
	parts := strings.Split(path, ".")
	if len(parts) > maxHops+1 {
		return nil, "", false
	}
	for _, p := range parts {
		if p == "" {
			return nil, "", false
		}
	}
	return parts[:len(parts)-1], parts[len(parts)-1], true
}

// follow returns the records reached from the scope through the relations.
func (r *renderer) follow(s scope, hops []string) []scope {
	scopes := []scope{s}
	for _, h := range hops {
		var next []scope
		for _, sc := range scopes {
			next = append(next, r.hop(sc, h)...)
		}
		scopes = next
		if len(scopes) == 0 {
			break
		}
	}
	return scopes
}

// resolve returns the records a binding path refers to and the name of the
// field to show ("brand.name": the brand of the scope record, "name").
func (r *renderer) resolve(s scope, path string) ([]scope, string) {
	hops, name, ok := splitPath(path)
	if !ok || s.rec == nil {
		return nil, ""
	}
	return r.follow(s, hops), name
}

// relationTarget returns the related collection of a visible relation field.
func (r *renderer) relationTarget(s scope, name string) (*core.RelationField, *core.Collection) {
	if s.rec == nil || !canShow(s, name) {
		return nil, nil
	}
	f, _ := s.col.Fields.GetByName(name).(*core.RelationField)
	if f == nil {
		return nil, nil
	}
	col, err := r.app.FindCachedCollectionByNameOrId(f.CollectionId)
	if err != nil || internalcols.Is(col) {
		return nil, nil
	}
	return f, col
}

// hop returns the records of a relation field of the scope record that can be
// shown, in the order of the field.
func (r *renderer) hop(s scope, name string) []scope {
	_, col := r.relationTarget(s, name)
	if col == nil {
		return nil
	}
	ids := s.rec.GetStringSlice(name)
	r.loadRelated(col, ids, s.rules)
	var out []scope
	for _, id := range ids {
		if rec := r.related[relatedKey{col.Id, id, s.rules}]; rec != nil {
			out = append(out, scope{col: col, rec: rec, rules: s.rules})
		}
	}
	return out
}

// loadRelated reads the records that aren't loaded yet (one query). With the
// rules, a record the visitor can't view is kept as nil.
func (r *renderer) loadRelated(col *core.Collection, ids []string, rules bool) {
	if r.related == nil {
		r.related = map[relatedKey]*core.Record{}
	}
	var missing []string
	seen := map[string]bool{}
	for _, id := range ids {
		if _, ok := r.related[relatedKey{col.Id, id, rules}]; !ok && id != "" && !seen[id] {
			seen[id] = true
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return
	}
	for _, id := range missing {
		r.related[relatedKey{col.Id, id, rules}] = nil
	}
	records, err := r.app.FindRecordsByIds(col, missing)
	if err != nil {
		return
	}
	var shown []*core.Record
	for _, rec := range records {
		if rules {
			if col.ViewRule == nil {
				continue // superusers only
			}
			if ok, _ := r.app.CanAccessRecord(rec, r.info, col.ViewRule); !ok {
				continue
			}
		}
		shown = append(shown, rec)
	}
	if rules {
		enrich(r.app, r.info, shown) // the fields the app hides from the visitor
	}
	for _, rec := range shown {
		r.related[relatedKey{col.Id, rec.Id, rules}] = rec
	}
}

// prefetch loads the related records the bindings of a list item refer to,
// for all the records of the list at once (instead of one query per record).
func (r *renderer) prefetch(item *xhtml.Node, records []scope) {
	var paths [][]string
	var visit func(n *xhtml.Node)
	visit = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, p := range bindingPaths(n) {
				if hops, _, ok := splitPath(p); ok && len(hops) > 0 {
					paths = append(paths, hops)
				}
			}
			if n != item && getAttr(n, attrList) != "" {
				return // nested lists bind their own records
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(item)
	for _, hops := range paths {
		scopes := records
		for _, h := range hops {
			// one query per collection for this step
			ids := map[*core.Collection][]string{}
			rules := false
			for _, sc := range scopes {
				if _, col := r.relationTarget(sc, h); col != nil {
					ids[col] = append(ids[col], sc.rec.GetStringSlice(h)...)
					rules = sc.rules
				}
			}
			for col, list := range ids {
				r.loadRelated(col, list, rules)
			}
			var next []scope
			for _, sc := range scopes {
				next = append(next, r.hop(sc, h)...)
			}
			scopes = next
		}
	}
}

// bindingPaths returns the field paths an element binds (its field, its link
// field, the relation of its record page link).
func bindingPaths(n *xhtml.Node) []string {
	var out []string
	if v := getAttr(n, attrField); v != "" {
		out = append(out, v)
	}
	if v := getAttr(n, attrHref); v != "" {
		out = append(out, v)
	}
	if _, via, ok := strings.Cut(getAttr(n, attrLink), ":"); ok && via != "" {
		out = append(out, via+".id")
	}
	return append(out, formulaPaths(n)...)
}

// pathValue returns the value of a path for a filter placeholder (the first
// related record's).
func (r *renderer) pathValue(s scope, path string) any {
	scopes, name := r.resolve(s, path)
	for _, sc := range scopes {
		if canShow(sc, name) || name == "id" {
			return sc.rec.Get(name)
		}
	}
	return ""
}
