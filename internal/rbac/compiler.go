// Package rbac turns a simple roles × actions permission matrix into
// native PocketBase API rules, so that enforcement stays in PocketBase
// (including list filtering and realtime subscriptions).
package rbac

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ScopeOwn    = "own"
	ScopeAll    = "all"
	ScopeCustom = "custom" // records matching a filter
)

// Actions are the PocketBase API rule types in display order.
var Actions = []string{"list", "view", "create", "update", "delete"}

// Grant gives a role access to one action on a collection.
type Grant struct {
	RoleId string `json:"roleId"`
	Action string `json:"action"`
	Scope  string `json:"scope"`

	// Filter is the PocketBase filter of the custom scope.
	Filter string `json:"filter,omitempty"`
	// Fields limits the readable (list/view) or writable (create/update)
	// fields. Empty means all fields.
	Fields []string `json:"fields,omitempty"`
	// Presets are default values of created records (create only).
	Presets map[string]any `json:"presets,omitempty"`
}

// Rules holds the compiled rule per action. A nil rule means "superusers only".
type Rules map[string]*string

// Compile builds the API rules of a single collection from its grants.
//
// ownerField is the field compared with the authenticated user id for the
// "own" scope (e.g. "owner", or "id" for the users collection itself).
func Compile(ownerField string, grants []Grant) (Rules, error) {
	byAction := map[string][]Grant{}
	for _, g := range grants {
		if !isValidAction(g.Action) {
			return nil, fmt.Errorf("invalid action %q", g.Action)
		}
		if !isSafeId(g.RoleId) {
			return nil, fmt.Errorf("invalid role id %q", g.RoleId)
		}
		switch g.Scope {
		case ScopeAll:
		case ScopeCustom:
			if strings.TrimSpace(g.Filter) == "" {
				return nil, fmt.Errorf("the %q scope requires a filter", ScopeCustom)
			}
		case ScopeOwn:
			if ownerField == "" {
				return nil, fmt.Errorf("the %q scope requires an owner field", ScopeOwn)
			}
			if !isSafeId(ownerField) {
				return nil, fmt.Errorf("invalid owner field %q", ownerField)
			}
			if g.Action == "create" && ownerField == "id" {
				return nil, fmt.Errorf("the %q scope can't be used for create when the owner field is \"id\"", ScopeOwn)
			}
		default:
			return nil, fmt.Errorf("invalid scope %q", g.Scope)
		}
		byAction[g.Action] = append(byAction[g.Action], g)
	}

	rules := Rules{}
	for _, action := range Actions {
		list := byAction[action]
		if len(list) == 0 {
			rules[action] = nil
			continue
		}

		// deterministic output so unchanged matrices don't trigger collection saves
		sort.Slice(list, func(i, j int) bool { return list[i].RoleId < list[j].RoleId })

		parts := make([]string, 0, len(list))
		for _, g := range list {
			hasRole := fmt.Sprintf(`@request.auth.roles.id ?= "%s"`, g.RoleId)
			if g.Scope == ScopeAll {
				parts = append(parts, hasRole)
				continue
			}
			if g.Scope == ScopeCustom {
				// the filter is validated on its own (see ValidateFilter) so its
				// parentheses can't escape the role condition
				parts = append(parts, fmt.Sprintf("(%s && (%s))", hasRole, strings.TrimSpace(g.Filter)))
				continue
			}
			switch {
			case action == "create":
				parts = append(parts, fmt.Sprintf("(%s && @request.body.%s = @request.auth.id)", hasRole, ownerField))
			case action == "update" && ownerField != "id":
				// owners can't hand their records over to someone else
				parts = append(parts, fmt.Sprintf(
					"(%s && %s = @request.auth.id && (@request.body.%s:isset = false || @request.body.%s = @request.auth.id))",
					hasRole, ownerField, ownerField, ownerField,
				))
			default:
				parts = append(parts, fmt.Sprintf("(%s && %s = @request.auth.id)", hasRole, ownerField))
			}
		}

		rule := strings.Join(parts, " || ")
		rules[action] = &rule
	}

	return rules, nil
}

func isValidAction(action string) bool {
	for _, a := range Actions {
		if a == action {
			return true
		}
	}
	return false
}

// record ids and field names are alphanumeric; guarding against anything else keeps the
// generated filter expressions injection free.
func isSafeId(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
