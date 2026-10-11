// Package labels returns readable names of records (for relation values):
// their presentable fields, or a usual name field.
package labels

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Fields returns the fields used to label the records of a collection:
// the presentable ones, or the first of name/title/label/username/email.
// Hidden fields are skipped unless includeHidden.
func Fields(c *core.Collection, includeHidden bool) []string {
	var names []string
	for _, f := range c.Fields {
		if f.GetHidden() && !includeHidden {
			continue
		}
		if p, ok := f.(interface{ GetPresentable() bool }); ok && p.GetPresentable() {
			names = append(names, f.GetName())
		}
	}
	if len(names) > 0 {
		return names
	}
	for _, candidate := range []string{"name", "title", "label", "username", "email"} {
		if f := c.Fields.GetByName(candidate); f != nil && (!f.GetHidden() || includeHidden) {
			return []string{candidate}
		}
	}
	return nil
}

// Of returns the label of a record from the label fields (its id when empty).
func Of(r *core.Record, fields []string) string {
	parts := []string{}
	for _, n := range fields {
		if v := r.GetString(n); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return r.Id
	}
	return strings.Join(parts, " ")
}
