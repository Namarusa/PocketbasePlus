package rbac

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ganigeorgiev/fexpr"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Field level permissions (managed collections, never for superusers):
//
//   - read: the fields not readable by any of the user's roles granting
//     list/view are hidden from the responses (lists, views, expands, realtime),
//     and can't be used in filters, sorts and expands;
//   - write: the fields not writable by the roles granting create/update can't
//     be set (unchanged values sent back by clients are accepted);
//   - presets: default values of the fields not sent on create.

// grantsCache holds the grants of the managed collections (nil for unmanaged ones).
var grantsCache sync.Map // collection id -> []Grant

func invalidateGrants(collectionId string) {
	grantsCache.Delete(collectionId)
}

// managedGrants returns the grants of a managed collection, or nil.
func managedGrants(app core.App, c *core.Collection) []Grant {
	if v, ok := grantsCache.Load(c.Id); ok {
		return v.([]Grant)
	}
	var grants []Grant
	if settings, err := findSettings(app, c.Id); err == nil && settings != nil && settings.GetBool("managed") {
		grants, _ = findGrants(app, c.Id)
		if grants == nil {
			grants = []Grant{}
		}
	}
	grantsCache.Store(c.Id, grants)
	return grants
}

func userRoles(auth *core.Record) []string {
	if auth == nil || auth.Collection().Fields.GetByName("roles") == nil {
		return nil
	}
	return auth.GetStringSlice("roles")
}

// allowed returns the fields allowed by the user's grants for the actions:
// all=true when a matching grant has no field limit, matched=false when no
// grant matches (the access is then decided by the API rules only).
func allowed(grants []Grant, roles []string, actions ...string) (all bool, fields map[string]bool, matched bool) {
	fields = map[string]bool{}
	for _, g := range grants {
		if !slices.Contains(roles, g.RoleId) || !slices.Contains(actions, g.Action) {
			continue
		}
		matched = true
		if len(g.Fields) == 0 {
			all = true
		}
		for _, f := range g.Fields {
			fields[f] = true
		}
	}
	return all, fields, matched
}

// ReadableFields returns the fields of a collection the user's roles can read,
// or nil when they aren't limited (superusers, unmanaged collections...).
func ReadableFields(app core.App, c *core.Collection, auth *core.Record) map[string]bool {
	return readableFields(app, c, auth)
}

// restricted returns the readable fields of a request, or nil when not limited.
func readableFields(app core.App, c *core.Collection, auth *core.Record) map[string]bool {
	if auth != nil && auth.IsSuperuser() {
		return nil
	}
	grants := managedGrants(app, c)
	if len(grants) == 0 {
		return nil
	}
	all, fields, matched := allowed(grants, userRoles(auth), "list", "view")
	if all || !matched {
		return nil
	}
	fields[core.FieldNameId] = true
	return fields
}

func registerFieldRules(app core.App) {
	// hide the unreadable fields
	app.OnRecordEnrich().BindFunc(func(e *core.RecordEnrichEvent) error {
		if e.RequestInfo != nil {
			if readable := readableFields(e.App, e.Record.Collection(), e.RequestInfo.Auth); readable != nil {
				hidden := []string{}
				for _, f := range e.Record.Collection().Fields {
					if !readable[f.GetName()] {
						hidden = append(hidden, f.GetName())
					}
				}
				e.Record.Hide(hidden...)
			}
		}
		return e.Next()
	})

	// no filtering, sorting or expanding by unreadable fields
	checkQuery := func(e *core.RequestEvent, c *core.Collection) error {
		info, err := e.RequestInfo()
		if err != nil {
			return err
		}
		readable := readableFields(e.App, c, info.Auth)
		if readable == nil {
			return nil
		}
		used, err := queryFields(info.Query["filter"], info.Query["sort"], info.Query["expand"])
		if err != nil {
			return e.BadRequestError("Invalid filter.", err)
		}
		var denied []string
		for _, f := range used {
			if !readable[f] {
				denied = append(denied, f)
			}
		}
		if len(denied) > 0 {
			return e.ForbiddenError("You can't filter, sort or expand by: "+strings.Join(denied, ", ")+".", nil)
		}
		return nil
	}
	app.OnRecordsListRequest().BindFunc(func(e *core.RecordsListRequestEvent) error {
		if err := checkQuery(e.RequestEvent, e.Collection); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordViewRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if err := checkQuery(e.RequestEvent, e.Collection); err != nil {
			return err
		}
		return e.Next()
	})

	// writable fields and presets
	app.OnRecordCreateRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if err := checkWrite(e, "create"); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordUpdateRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if err := checkWrite(e, "update"); err != nil {
			return err
		}
		return e.Next()
	})
}

// bodyFields returns the fields sent in a create/update request (without the
// +/- modifiers of multiple values).
func bodyFields(e *core.RecordRequestEvent) ([]string, error) {
	info, err := e.RequestInfo()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	add := func(key string) {
		key = strings.Trim(key, "+-")
		if key != "" {
			seen[key] = true
		}
	}
	for k := range info.Body {
		add(k)
	}
	if e.Request.MultipartForm != nil {
		for k := range e.Request.MultipartForm.File {
			add(k)
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func checkWrite(e *core.RecordRequestEvent, action string) error {
	keys, err := bodyFields(e)
	if err != nil {
		return err
	}
	if err := CheckWrite(e.App, e.Record, e.Auth, action, keys); err != nil {
		return e.ForbiddenError(err.Error(), nil)
	}
	return nil
}

// CheckWrite applies the field permissions of the user's roles to a record
// being created or updated (action "create" or "update") with the sent fields:
// an error names the fields they can't change, and on create the presets of
// their roles fill the fields not sent. Superusers and unmanaged collections
// aren't limited.
func CheckWrite(app core.App, rec *core.Record, auth *core.Record, action string, keys []string) error {
	if auth != nil && auth.IsSuperuser() {
		return nil
	}
	col := rec.Collection()
	grants := managedGrants(app, col)
	if len(grants) == 0 {
		return nil
	}
	roles := userRoles(auth)
	all, writable, matched := allowed(grants, roles, action)
	if !matched {
		return nil
	}

	if !all {
		var orig *core.Record
		if action == "update" {
			orig = rec.Original()
		}
		var denied []string
		for _, k := range keys {
			if writable[k] || col.Fields.GetByName(k) == nil {
				continue // allowed, or not a field (e.g. passwordConfirm)
			}
			if orig != nil && sameValue(orig.Get(k), rec.Get(k)) {
				continue // unchanged value sent back
			}
			denied = append(denied, k)
		}
		if len(denied) > 0 {
			return fmt.Errorf("You can't change the field(s): %s.", strings.Join(denied, ", "))
		}
	}

	if action == "create" {
		sent := map[string]bool{}
		for _, k := range keys {
			sent[k] = true
		}
		for _, g := range grants {
			if g.Action != "create" || !slices.Contains(roles, g.RoleId) {
				continue
			}
			for k, v := range g.Presets {
				if !sent[k] {
					rec.Set(k, resolvePreset(v, auth))
				}
			}
		}
	}
	return nil
}

func sameValue(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// resolvePreset resolves the "@request.auth.id" and "@now" macros.
func resolvePreset(v any, auth *core.Record) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	switch strings.TrimSpace(s) {
	case "@request.auth.id":
		if auth != nil {
			return auth.Id
		}
		return ""
	case "@now":
		return types.NowDateTime()
	}
	return v
}

// queryFields returns the top level fields used by a filter, sort and expand.
func queryFields(filter, sortParam, expand string) ([]string, error) {
	used := map[string]bool{}
	add := func(identifier string) {
		if identifier == "" || strings.HasPrefix(identifier, "@") {
			return
		}
		name := identifier
		if i := strings.IndexAny(name, ".:"); i >= 0 {
			name = name[:i]
		}
		switch strings.ToLower(name) {
		case "", "null", "true", "false":
			return
		}
		used[name] = true
	}
	if strings.TrimSpace(filter) != "" {
		groups, err := fexpr.Parse(filter)
		if err != nil {
			return nil, err
		}
		walkGroups(groups, add)
	}
	for _, s := range strings.Split(sortParam, ",") {
		add(strings.TrimLeft(strings.TrimSpace(s), "+-"))
	}
	for _, x := range strings.Split(expand, ",") {
		add(strings.TrimSpace(x))
	}
	names := make([]string, 0, len(used))
	for n := range used {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func walkGroups(groups []fexpr.ExprGroup, add func(string)) {
	for _, g := range groups {
		switch item := g.Item.(type) {
		case fexpr.Expr:
			walkToken(item.Left, add)
			walkToken(item.Right, add)
		case []fexpr.ExprGroup:
			walkGroups(item, add)
		}
	}
}

func walkToken(t fexpr.Token, add func(string)) {
	switch t.Type {
	case fexpr.TokenIdentifier:
		add(t.Literal)
	case fexpr.TokenFunction:
		if args, ok := t.Meta.([]fexpr.Token); ok {
			for _, a := range args {
				walkToken(a, add)
			}
		}
	}
}

// --- validation of the matrix ----------------------------------------------------

// validateGrant checks the custom filter, the fields and the presets of a grant.
func validateGrant(app core.App, c *core.Collection, g *Grant) error {
	label := fmt.Sprintf("%s (%s)", g.Action, g.RoleId)
	if g.Scope == ScopeCustom {
		if err := ValidateFilter(app, c, g.Filter); err != nil {
			return fmt.Errorf("%s: invalid filter: %w", label, err)
		}
	} else {
		g.Filter = ""
	}

	if g.Action == "delete" {
		g.Fields = nil
	}
	seen := map[string]bool{}
	fields := []string{}
	for _, f := range g.Fields {
		if c.Fields.GetByName(f) == nil {
			return fmt.Errorf("%s: unknown field %q", label, f)
		}
		if !seen[f] {
			seen[f] = true
			fields = append(fields, f)
		}
	}
	g.Fields = fields

	if g.Action != "create" {
		g.Presets = nil
	}
	for k := range g.Presets {
		f := c.Fields.GetByName(k)
		if f == nil || k == core.FieldNameId || f.Type() == core.FieldTypeAutodate || f.Type() == core.FieldTypePassword {
			return fmt.Errorf("%s: invalid preset field %q", label, k)
		}
	}
	return nil
}

// ValidateFilter checks that a filter is valid on its own for the collection.
func ValidateFilter(app core.App, c *core.Collection, filter string) error {
	if strings.TrimSpace(filter) == "" {
		return fmt.Errorf("the filter is empty")
	}
	resolver := core.NewRecordFieldResolver(app, c, &core.RequestInfo{}, true)
	_, err := search.FilterData(filter).BuildExpr(resolver)
	return err
}
