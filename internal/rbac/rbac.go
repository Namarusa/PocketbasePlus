package rbac

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
)

const (
	rolesCollection       = "roles"
	managedCollection     = "rbac_collections"
	permissionsCollection = "permissions"
)

// Register binds the RBAC hooks and routes to the app.
func Register(app core.App) {
	recompileFromRecord := func(e *core.RecordEvent) error {
		invalidateGrants(e.Record.GetString("collection"))
		if orig := e.Record.Original(); orig != nil {
			invalidateGrants(orig.GetString("collection"))
		}
		recompileLogged(e.App, e.Record.GetString("collection"))
		if orig := e.Record.Original(); orig != nil && orig.GetString("collection") != e.Record.GetString("collection") {
			recompileLogged(e.App, orig.GetString("collection"))
		}
		return e.Next()
	}
	app.OnRecordAfterCreateSuccess(permissionsCollection, managedCollection).BindFunc(recompileFromRecord)
	app.OnRecordAfterUpdateSuccess(permissionsCollection, managedCollection).BindFunc(recompileFromRecord)
	app.OnRecordAfterDeleteSuccess(permissionsCollection, managedCollection).BindFunc(recompileFromRecord)

	registerFieldRules(app)
	protectRoles(app)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		if err := RecompileAll(se.App); err != nil {
			se.App.Logger().Error("rbac: failed to recompile rules", "error", err)
		}

		g := se.Router.Group("/api/rbac")
		g.Bind(apis.RequireSuperuserAuth())
		g.GET("/collections", listCollections)
		g.GET("/matrix/{collection}", getMatrix)
		g.PUT("/matrix/{collection}", saveMatrix)

		return se.Next()
	})
}

// IsManageable reports whether a collection can have RBAC generated rules.
func IsManageable(c *core.Collection) bool {
	// internal collections must never get generated rules (they would become exposed)
	return !c.System && !c.IsView() && !internalcols.Is(c)
}

// RecompileAll regenerates the rules of every managed collection.
func RecompileAll(app core.App) error {
	records, err := app.FindRecordsByFilter(managedCollection, "managed = true", "", 0, 0)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range records {
		if err := Recompile(app, r.GetString("collection")); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Recompile regenerates the API rules of a single collection (by id) from
// its permissions. Unmanaged collections are left untouched.
func Recompile(app core.App, collectionId string) error {
	if collectionId == "" {
		return nil
	}

	settings, err := findSettings(app, collectionId)
	if err != nil || settings == nil || !settings.GetBool("managed") {
		return err
	}

	collection, err := app.FindCollectionByNameOrId(collectionId)
	if err != nil {
		return nil // stale mapping of a deleted collection
	}
	if !IsManageable(collection) {
		return nil
	}

	grants, err := findGrants(app, collectionId)
	if err != nil {
		return err
	}

	rules, err := Compile(settings.GetString("owner_field"), grants)
	if err != nil {
		return err
	}

	if !applyRules(collection, rules) {
		return nil // nothing changed
	}
	return app.Save(collection)
}

func recompileLogged(app core.App, collectionId string) {
	if err := Recompile(app, collectionId); err != nil {
		app.Logger().Error("rbac: failed to recompile rules", "collection", collectionId, "error", err)
	}
}

// applyRules sets the compiled rules on the collection and reports whether any changed.
func applyRules(c *core.Collection, rules Rules) bool {
	changed := false
	set := func(dst **string, rule *string) {
		if !sameRule(*dst, rule) {
			*dst = rule
			changed = true
		}
	}
	set(&c.ListRule, rules["list"])
	set(&c.ViewRule, rules["view"])
	set(&c.CreateRule, rules["create"])
	set(&c.UpdateRule, rules["update"])
	set(&c.DeleteRule, rules["delete"])
	return changed
}

func sameRule(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func findSettings(app core.App, collectionId string) (*core.Record, error) {
	r, err := app.FindFirstRecordByFilter(managedCollection, "collection = {:c}", dbx.Params{"c": collectionId})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func findGrants(app core.App, collectionId string) ([]Grant, error) {
	records, err := app.FindRecordsByFilter(permissionsCollection, "collection = {:c}", "", 0, 0, dbx.Params{"c": collectionId})
	if err != nil {
		return nil, err
	}
	grants := make([]Grant, 0, len(records))
	for _, r := range records {
		g := Grant{
			RoleId: r.GetString("role"),
			Action: r.GetString("action"),
			Scope:  r.GetString("scope"),
			Filter: r.GetString("filter"),
		}
		_ = r.UnmarshalJSONField("fields", &g.Fields)
		_ = r.UnmarshalJSONField("presets", &g.Presets)
		grants = append(grants, g)
	}
	return grants, nil
}

// --- routes -----------------------------------------------------------------

type fieldInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type collectionInfo struct {
	Id         string      `json:"id"`
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Managed    bool        `json:"managed"`
	OwnerField string      `json:"ownerField"`
	Fields     []fieldInfo `json:"fields"`
}

type roleInfo struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type matrix struct {
	Collection collectionInfo     `json:"collection"`
	Roles      []roleInfo         `json:"roles"`
	Grants     []Grant            `json:"grants"`
	Rules      map[string]*string `json:"rules"`
}

func describe(app core.App, c *core.Collection) (collectionInfo, error) {
	info := collectionInfo{Id: c.Id, Name: c.Name, Type: c.Type, Fields: []fieldInfo{}}
	for _, f := range c.Fields {
		info.Fields = append(info.Fields, fieldInfo{Name: f.GetName(), Type: f.Type()})
	}
	settings, err := findSettings(app, c.Id)
	if err != nil {
		return info, err
	}
	if settings != nil {
		info.Managed = settings.GetBool("managed")
		info.OwnerField = settings.GetString("owner_field")
	}
	return info, nil
}

func listCollections(e *core.RequestEvent) error {
	collections, err := e.App.FindAllCollections()
	if err != nil {
		return err
	}
	result := []collectionInfo{}
	for _, c := range collections {
		if !IsManageable(c) {
			continue
		}
		info, err := describe(e.App, c)
		if err != nil {
			return err
		}
		result = append(result, info)
	}
	return e.JSON(http.StatusOK, result)
}

func findManageable(e *core.RequestEvent) (*core.Collection, error) {
	c, err := e.App.FindCollectionByNameOrId(e.Request.PathValue("collection"))
	if err != nil || !IsManageable(c) {
		return nil, e.NotFoundError("Collection not found or not manageable.", err)
	}
	return c, nil
}

func buildMatrix(app core.App, c *core.Collection) (*matrix, error) {
	info, err := describe(app, c)
	if err != nil {
		return nil, err
	}
	roles, err := app.FindRecordsByFilter(rolesCollection, "", "name", 0, 0)
	if err != nil {
		return nil, err
	}
	grants, err := findGrants(app, c.Id)
	if err != nil {
		return nil, err
	}
	m := &matrix{
		Collection: info,
		Roles:      make([]roleInfo, 0, len(roles)),
		Grants:     grants,
		Rules: map[string]*string{
			"list": c.ListRule, "view": c.ViewRule, "create": c.CreateRule,
			"update": c.UpdateRule, "delete": c.DeleteRule,
		},
	}
	for _, r := range roles {
		m.Roles = append(m.Roles, roleInfo{Id: r.Id, Name: r.GetString("name")})
	}
	return m, nil
}

func getMatrix(e *core.RequestEvent) error {
	c, err := findManageable(e)
	if err != nil {
		return err
	}
	m, err := buildMatrix(e.App, c)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, m)
}

type saveMatrixBody struct {
	Managed    bool    `json:"managed"`
	OwnerField string  `json:"ownerField"`
	Grants     []Grant `json:"grants"`
}

func saveMatrix(e *core.RequestEvent) error {
	c, err := findManageable(e)
	if err != nil {
		return err
	}

	var body saveMatrixBody
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}

	if body.OwnerField != "" && body.OwnerField != "id" && c.Fields.GetByName(body.OwnerField) == nil {
		return e.BadRequestError("Unknown owner field "+body.OwnerField+".", nil)
	}

	seen := map[string]bool{}
	for _, g := range body.Grants {
		key := g.RoleId + "/" + g.Action
		if seen[key] {
			return e.BadRequestError("Duplicated grant for role "+g.RoleId+" and action "+g.Action+".", nil)
		}
		seen[key] = true
		if _, err := e.App.FindRecordById(rolesCollection, g.RoleId); err != nil {
			return e.BadRequestError("Unknown role "+g.RoleId+".", nil)
		}
	}

	// validate before persisting anything
	if _, err := Compile(body.OwnerField, body.Grants); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	for i := range body.Grants {
		if err := validateGrant(e.App, c, &body.Grants[i]); err != nil {
			return e.BadRequestError(err.Error(), nil)
		}
	}

	err = e.App.RunInTransaction(func(txApp core.App) error {
		settings, err := findSettings(txApp, c.Id)
		if err != nil {
			return err
		}
		if settings == nil {
			col, err := txApp.FindCollectionByNameOrId(managedCollection)
			if err != nil {
				return err
			}
			settings = core.NewRecord(col)
			settings.Set("collection", c.Id)
		}
		settings.Set("managed", body.Managed)
		settings.Set("owner_field", body.OwnerField)
		if err := txApp.Save(settings); err != nil {
			return err
		}

		existing, err := txApp.FindRecordsByFilter(permissionsCollection, "collection = {:c}", "", 0, 0, dbx.Params{"c": c.Id})
		if err != nil {
			return err
		}
		for _, r := range existing {
			if err := txApp.Delete(r); err != nil {
				return err
			}
		}

		permCol, err := txApp.FindCollectionByNameOrId(permissionsCollection)
		if err != nil {
			return err
		}
		for _, g := range body.Grants {
			r := core.NewRecord(permCol)
			r.Set("role", g.RoleId)
			r.Set("collection", c.Id)
			r.Set("action", g.Action)
			r.Set("scope", g.Scope)
			r.Set("filter", g.Filter)
			r.Set("fields", g.Fields)
			r.Set("presets", g.Presets)
			if err := txApp.Save(r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return e.BadRequestError("Failed to save the permissions.", err)
	}

	// the record hooks already recompiled, but do it once more to surface errors
	if err := Recompile(e.App, c.Id); err != nil {
		return e.BadRequestError("Failed to compile the rules: "+err.Error(), nil)
	}

	c, err = e.App.FindCollectionByNameOrId(c.Id)
	if err != nil {
		return err
	}
	m, err := buildMatrix(e.App, c)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, m)
}
