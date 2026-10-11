package pages

import (
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/Namarusa/PocketbasePlus/internal/session"
)

// Members: a page is public, or for the signed-in visitors, or for some roles
// (the roles of the app). Visitors sign in with a page action flow ("Sign in"
// step) and get a session cookie; the elements of a page can be shown to
// guests, signed-in users or roles only (data-pb-visible).
const (
	accessMigrationFile = "1761200000_pages_access.go"
	rulesMigrationFile  = "1761300000_pages_record_rules.go"

	accessPublic = "public"
	accessUsers  = "users"
	accessRoles  = "roles"

	rolesCollection = "roles"
	usersCollection = "users"
)

func init() {
	m.Register(func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		pages.Fields.Add(&core.SelectField{Name: "access", MaxSelect: 1, Values: []string{accessPublic, accessUsers, accessRoles}})
		if roles, err := app.FindCollectionByNameOrId(rolesCollection); err == nil {
			pages.Fields.Add(&core.RelationField{Name: "roles", CollectionId: roles.Id, MaxSelect: 999})
		}
		if err := app.Save(pages); err != nil {
			return err
		}
		settings, err := app.FindCollectionByNameOrId(SettingsCollection)
		if err != nil {
			return err
		}
		settings.Fields.Add(&core.TextField{Name: "login_page", Max: 100}) // slug of the sign in page
		return app.Save(settings)
	}, func(app core.App) error {
		if pages, err := app.FindCollectionByNameOrId(Collection); err == nil {
			pages.Fields.RemoveByName("access")
			pages.Fields.RemoveByName("roles")
			if err := app.Save(pages); err != nil {
				return err
			}
		}
		if settings, err := app.FindCollectionByNameOrId(SettingsCollection); err == nil {
			settings.Fields.RemoveByName("login_page")
			return app.Save(settings)
		}
		return nil
	}, accessMigrationFile)

	// record pages: the view rule of the collection for the visitor
	m.Register(func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		pages.Fields.Add(&core.BoolField{Name: "record_rules"})
		return app.Save(pages)
	}, func(app core.App) error {
		pages, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return nil
		}
		pages.Fields.RemoveByName("record_rules")
		return app.Save(pages)
	}, rulesMigrationFile)
}

// canViewRecord reports whether the visitor can view the record of a record
// page whose "API rules" option is on (its collection's view rule, as the
// REST API would check it).
func (v visitor) canViewRecord(app core.App, page, rec *core.Record) bool {
	if v.all || !page.GetBool("record_rules") {
		return true
	}
	ok, _ := app.CanAccessRecord(rec, v.requestInfo(), rec.Collection().ViewRule)
	return ok
}

// visitor is who views a page.
type visitor struct {
	user  *core.Record // nil: a guest
	roles []string
	all   bool // superuser preview: sees everything
}

func visitorOf(e *core.RequestEvent) visitor {
	u := session.Get(e)
	return visitor{user: u, roles: session.Roles(u)}
}

// requestInfo is the visitor as PocketBase sees a request of theirs: the
// API rules are evaluated with it (@request.auth is the signed-in user).
func (v visitor) requestInfo() *core.RequestInfo {
	info := &core.RequestInfo{Context: core.RequestInfoContextDefault, Method: http.MethodGet, Query: map[string]string{}, Headers: map[string]string{}, Body: map[string]any{}}
	if v.user != nil && v.user.Id != "" {
		info.Auth = v.user
	}
	return info
}

// matches reports whether the visitor is one of the tokens of a
// data-pb-visible value: "guests", "users", "role:<id>" (an empty value: everyone).
func (v visitor) matches(value string) bool {
	tokens := strings.Fields(value)
	if v.all || len(tokens) == 0 {
		return true
	}
	for _, t := range tokens {
		switch {
		case t == "everyone":
			return true
		case t == "guests":
			if v.user == nil {
				return true
			}
		case t == "users":
			if v.user != nil {
				return true
			}
		case strings.HasPrefix(t, "role:"):
			if v.user != nil && slices.Contains(v.roles, t[5:]) {
				return true
			}
		}
	}
	return false
}

// canView reports whether the visitor can open the page.
func (v visitor) canView(page *core.Record) bool {
	if v.all {
		return true
	}
	switch page.GetString("access") {
	case accessUsers:
		return v.user != nil
	case accessRoles:
		if v.user == nil {
			return false
		}
		for _, r := range page.GetStringSlice("roles") {
			if slices.Contains(v.roles, r) {
				return true
			}
		}
		return false
	}
	return true
}

// previewVisitor is the visitor of a preview "as" a role: "public", "users"
// or "role:<id>". It's the first user with that role (so the bindings show
// real data) or a stand-in. An empty value is the superuser (everything).
func previewVisitor(app core.App, as string) visitor {
	switch {
	case as == "public":
		return visitor{}
	case as == accessUsers, strings.HasPrefix(as, "role:"):
		col, err := app.FindCachedCollectionByNameOrId(usersCollection)
		if err != nil {
			return visitor{}
		}
		role := strings.TrimPrefix(as, "role:")
		filter := "id != ''"
		params := dbx.Params{}
		if role != as {
			filter = session.RolesField + ".id ?= {:role}"
			params["role"] = role
		} else if col.Fields.GetByName(session.RolesField) != nil {
			filter = session.RolesField + ":length = 0"
		}
		var u *core.Record
		if list, err := app.FindRecordsByFilter(col, filter, "created", 1, 0, params); err == nil && len(list) > 0 {
			u = list[0]
		} else {
			u = core.NewRecord(col)
			if role != as && col.Fields.GetByName(session.RolesField) != nil {
				u.Set(session.RolesField, []string{role})
			}
		}
		return visitor{user: u, roles: session.Roles(u)}
	}
	return visitor{all: true}
}

// validAs reports whether a preview "as" value is known.
func validAs(as string) bool {
	return as == "" || as == "public" || as == accessUsers || (strings.HasPrefix(as, "role:") && len(as) > 5 && len(as) < 40)
}

// bindLoginPage keeps the sign in page setting in sync with the page: a new
// slug is followed, a deleted page unsets it.
func bindLoginPage(app core.App) {
	follow := func(e *core.RecordEvent, slug string) error {
		old := ""
		if orig := e.Record.Original(); orig != nil {
			old = orig.GetString("slug")
		}
		if old == "" || old == slug {
			return e.Next()
		}
		settings, err := e.App.FindFirstRecordByFilter(SettingsCollection, "login_page = {:slug}", dbx.Params{"slug": old})
		if err == nil {
			settings.Set("login_page", slug)
			if err := e.App.Save(settings); err != nil {
				return err
			}
		}
		return e.Next()
	}
	app.OnRecordAfterUpdateSuccess(Collection).BindFunc(func(e *core.RecordEvent) error {
		return follow(e, e.Record.GetString("slug"))
	})
	app.OnRecordAfterDeleteSuccess(Collection).BindFunc(func(e *core.RecordEvent) error {
		settings, err := e.App.FindFirstRecordByFilter(SettingsCollection, "login_page = {:slug}", dbx.Params{"slug": e.Record.GetString("slug")})
		if err == nil {
			settings.Set("login_page", "")
			if err := e.App.Save(settings); err != nil {
				return err
			}
		}
		return e.Next()
	})
}

// loginPage returns the slug of the sign in page of the site ("" when unset).
func loginPage(app core.App) string {
	rec, err := app.FindFirstRecordByFilter(SettingsCollection, "id != ''")
	if err != nil {
		return ""
	}
	return rec.GetString("login_page")
}

func validateAccess(app core.App, rec *core.Record, errs validation.Errors) {
	switch rec.GetString("access") {
	case accessRoles:
		if len(rec.GetStringSlice("roles")) == 0 {
			errs["roles"] = validation.NewError("validation_required_roles", "Select the roles who can view the page.")
		}
	case accessUsers, accessPublic, "":
		if rec.Collection().Fields.GetByName("roles") != nil {
			rec.Set("roles", []string{})
		}
	}
	if rec.GetString("kind") == kindPart && rec.GetString("access") != "" && rec.GetString("access") != accessPublic {
		errs["access"] = validation.NewError("validation_part_access", "A site part has the access of the pages using it: hide its elements by role instead.")
	}
}

// denied answers a visitor who can't open the page: guests go to the sign in
// page (back to this page once signed in), the others see a refusal.
func denied(e *core.RequestEvent, page *core.Record, v visitor) error {
	path := e.Request.URL.RequestURI()
	login := loginPage(e.App)
	if v.user == nil && login != "" && login != page.GetString("slug") {
		return e.Redirect(http.StatusSeeOther, pageURL(login, "")+"?next="+url.QueryEscape(path))
	}
	title, text, status := "Sign in required", "Sign in to view this page.", http.StatusUnauthorized
	action := ""
	if v.user != nil {
		title, text, status = "No access", "Your account doesn't have access to this page.", http.StatusForbidden
		action = `<form method="post" action="/api/session/logout"><input type="hidden" name="_next" value="` + html.EscapeString(path) + `">` +
			`<button type="submit" style="font:inherit;padding:8px 16px;cursor:pointer">Sign in with another account</button></form>`
	}
	return deniedPage(e, status, title, text, action)
}

func deniedPage(e *core.RequestEvent, status int, title, text, extra string) error {
	h := e.Response.Header()
	h.Set("Content-Security-Policy", pageCSP(false, false))
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Vary", "Cookie")
	return e.HTML(status, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width, initial-scale=1"><title>`+html.EscapeString(title)+`</title></head>`+
		`<body style="font-family:system-ui,sans-serif;text-align:center;padding:15vh 16px;color:#555">`+
		`<h1 style="font-weight:600">`+html.EscapeString(title)+`</h1><p>`+html.EscapeString(text)+`</p>`+extra+`<p><a href="/">Home</a></p></body></html>`)
}
