package rbac

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// The roles of a user grant the permissions: only superusers can change them.
// Without this guard, the default rules of the users collection (sign up for
// everyone, self update) would let users give themselves any role.

// roleFields returns the fields of a collection holding roles (relations to
// the roles collection).
func roleFields(app core.App, c *core.Collection) []string {
	roles, err := app.FindCachedCollectionByNameOrId(rolesCollection)
	if err != nil {
		return nil
	}
	var names []string
	for _, f := range c.Fields {
		if rel, ok := f.(*core.RelationField); ok && rel.CollectionId == roles.Id {
			names = append(names, rel.Name)
		}
	}
	return names
}

func protectRoles(app core.App) {
	const denied = "Only superusers can change the roles."
	app.OnRecordCreateRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if !e.HasSuperuserAuth() {
			for _, name := range roleFields(e.App, e.Collection) {
				if len(e.Record.GetStringSlice(name)) > 0 {
					return e.ForbiddenError(denied, nil)
				}
			}
		}
		return e.Next()
	})
	app.OnRecordUpdateRequest().BindFunc(func(e *core.RecordRequestEvent) error {
		if !e.HasSuperuserAuth() {
			orig := e.Record.Original()
			for _, name := range roleFields(e.App, e.Collection) {
				before, after := orig.GetStringSlice(name), e.Record.GetStringSlice(name)
				slices.Sort(before)
				slices.Sort(after)
				if !slices.Equal(before, after) {
					return e.ForbiddenError(denied, nil)
				}
			}
		}
		return e.Next()
	})
	// OAuth2 sign up: the submitted createData
	app.OnRecordAuthWithOAuth2Request().BindFunc(func(e *core.RecordAuthWithOAuth2RequestEvent) error {
		for _, name := range roleFields(e.App, e.Collection) {
			for _, key := range []string{name, name + "+", "+" + name} {
				if _, ok := e.CreateData[key]; ok {
					return e.ForbiddenError(denied, nil)
				}
			}
		}
		return e.Next()
	})
}
