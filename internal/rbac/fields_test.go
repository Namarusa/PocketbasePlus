package rbac_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/rbac"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type docsFixture struct {
	app                                    *tests.TestApp
	reader, editor, creator, viewer, admin *core.Record
	doc                                    *core.Record
}

// setupDocs creates a managed "docs" collection with field limited grants:
//   - reader: list/view all docs, reads title, status and owner only
//   - editor: list/view/update all docs, writes the title only
//   - creator: creates docs, with status and owner presets
//   - viewer: lists the published docs only (custom scope)
func setupDocs(t testing.TB) *docsFixture {
	app := testutil.NewApp(t)
	rbac.Register(app)
	f := &docsFixture{app: app}

	users, _ := app.FindCollectionByNameOrId("users")
	docs := testutil.CreateCollection(t, app, "docs",
		&core.TextField{Name: "title"},
		&core.TextField{Name: "status"},
		&core.TextField{Name: "secret_note"},
		&core.NumberField{Name: "budget"},
		&core.RelationField{Name: "owner", CollectionId: users.Id, MaxSelect: 1},
	)

	role := func(name string) string { return testutil.Create(t, app, "roles", map[string]any{"name": name}).Id }
	readerRole, editorRole, creatorRole, viewerRole := role("reader"), role("editor"), role("creator"), role("viewer")
	user := func(email, roleId string) *core.Record {
		return testutil.Create(t, app, "users", map[string]any{"email": email, "password": "1234567890", "roles": []string{roleId}})
	}
	f.reader = user("reader@test.com", readerRole)
	f.editor = user("editor@test.com", editorRole)
	f.creator = user("creator@test.com", creatorRole)
	f.viewer = user("viewer@test.com", viewerRole)
	f.admin = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})

	f.doc = testutil.Create(t, app, "docs", map[string]any{"title": "Plan", "status": "published", "secret_note": "top secret", "budget": 1000, "owner": f.editor.Id})
	testutil.Create(t, app, "docs", map[string]any{"title": "Draft", "status": "draft", "budget": 5})

	testutil.Create(t, app, "rbac_collections", map[string]any{"collection": docs.Id, "managed": true, "owner_field": "owner"})
	for _, p := range []map[string]any{
		{"role": readerRole, "action": "list", "scope": "all", "fields": []string{"title", "status", "owner"}},
		{"role": readerRole, "action": "view", "scope": "all", "fields": []string{"title", "status", "owner"}},
		{"role": editorRole, "action": "list", "scope": "all"},
		{"role": editorRole, "action": "view", "scope": "all"},
		{"role": editorRole, "action": "update", "scope": "all", "fields": []string{"title"}},
		{"role": creatorRole, "action": "create", "scope": "all", "presets": map[string]any{"status": "draft", "owner": "@request.auth.id"}},
		{"role": viewerRole, "action": "list", "scope": "custom", "filter": `status = "published"`},
	} {
		p["collection"] = docs.Id
		testutil.Create(t, app, "permissions", p)
	}
	return f
}

func docsRequest(t *testing.T, name, method string, who func(f *docsFixture) *core.Record, url func(f *docsFixture) string, body string, status int, expected []string, notExpected []string, after func(t testing.TB, f *docsFixture)) {
	var f *docsFixture
	headers := map[string]string{}
	scenario := &tests.ApiScenario{
		Name:               name,
		Method:             method,
		Body:               strings.NewReader(body),
		Headers:            headers,
		ExpectedStatus:     status,
		ExpectedContent:    expected,
		NotExpectedContent: notExpected,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setupDocs(t)
			return f.app
		},
	}
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		scenario.URL = url(f)
		headers["Authorization"] = testutil.AuthToken(t, who(f))
	}
	if after != nil {
		scenario.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) { after(t, f) }
	}
	scenario.Test(t)
}

func TestFieldLevelPermissions(t *testing.T) {
	list := func(*docsFixture) string { return "/api/collections/docs/records" }
	one := func(f *docsFixture) string { return "/api/collections/docs/records/" + f.doc.Id }
	reader := func(f *docsFixture) *core.Record { return f.reader }
	editor := func(f *docsFixture) *core.Record { return f.editor }

	docsRequest(t, "reader doesn't see the unreadable fields", http.MethodGet, reader, list, "", 200,
		[]string{`"title":"Plan"`, `"status":"published"`}, []string{`secret_note`, `budget`}, nil)
	docsRequest(t, "nor in a single record", http.MethodGet, reader, one, "", 200,
		[]string{`"title":"Plan"`}, []string{`top secret`}, nil)
	docsRequest(t, "superusers see everything", http.MethodGet, func(f *docsFixture) *core.Record { return f.admin }, one, "", 200,
		[]string{`"secret_note":"top secret"`, `"budget":1000`}, nil, nil)
	docsRequest(t, "reader can't filter by an unreadable field", http.MethodGet, reader,
		func(*docsFixture) string { return "/api/collections/docs/records?filter=" + urlEncode(`budget > 100`) }, "", 403,
		[]string{`budget`}, nil, nil)
	docsRequest(t, "reader can't sort by an unreadable field", http.MethodGet, reader,
		func(*docsFixture) string { return "/api/collections/docs/records?sort=-secret_note" }, "", 403, []string{`secret_note`}, nil, nil)
	docsRequest(t, "reader can filter by a readable field", http.MethodGet, reader,
		func(*docsFixture) string {
			return "/api/collections/docs/records?filter=" + urlEncode(`title = "Plan" && (status != "x")`)
		}, "", 200,
		[]string{`"totalItems":1`}, nil, nil)

	docsRequest(t, "editor updates a writable field", http.MethodPatch, editor, one, `{"title":"New plan"}`, 200, []string{`"title":"New plan"`}, nil, nil)
	docsRequest(t, "editor can't change another field", http.MethodPatch, editor, one, `{"budget":5000}`, 403, []string{`budget`}, nil, nil)
	docsRequest(t, "editor can send unchanged values back", http.MethodPatch, editor, one, `{"title":"Edited","budget":1000,"status":"published"}`, 200, []string{`"title":"Edited"`}, nil, nil)
}

func TestCustomScopeAndPresets(t *testing.T) {
	docsRequest(t, "custom scope lists the published docs only", http.MethodGet, func(f *docsFixture) *core.Record { return f.viewer },
		func(*docsFixture) string { return "/api/collections/docs/records" }, "", 200,
		[]string{`"totalItems":1`, `"title":"Plan"`}, []string{`"Draft"`}, nil)

	docsRequest(t, "presets fill the fields not sent", http.MethodPost, func(f *docsFixture) *core.Record { return f.creator },
		func(*docsFixture) string { return "/api/collections/docs/records" }, `{"title":"Mine"}`, 200,
		[]string{`"title":"Mine"`}, nil, func(t testing.TB, f *docsFixture) {
			rec, err := f.app.FindFirstRecordByData("docs", "title", "Mine")
			if err != nil || rec.GetString("status") != "draft" || rec.GetString("owner") != f.creator.Id {
				t.Fatalf("expected the presets to be applied, got %v / %v", rec, err)
			}
		})
}

func TestMatrixValidatesTheNewOptions(t *testing.T) {
	var f *docsFixture
	for _, c := range []struct{ name, grant, expected string }{
		{"invalid custom filter", `{"roleId":"%s","action":"list","scope":"custom","filter":"nope_field = 1"}`, "invalid filter"},
		{"escaping parentheses", `{"roleId":"%s","action":"list","scope":"custom","filter":"1=1) || (1=1"}`, "invalid filter"},
		{"unknown field", `{"roleId":"%s","action":"view","scope":"all","fields":["nope"]}`, "unknown field"},
		{"invalid preset", `{"roleId":"%s","action":"create","scope":"all","presets":{"id":"x"}}`, "invalid preset"},
	} {
		headers := map[string]string{}
		scenario := &tests.ApiScenario{
			Name:            c.name,
			Method:          http.MethodPut,
			URL:             "/api/rbac/matrix/docs",
			Headers:         headers,
			ExpectedStatus:  400,
			ExpectedContent: []string{c.expected},
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				f = setupDocs(t)
				return f.app
			},
		}
		grant := c.grant
		scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			headers["Authorization"] = testutil.AuthToken(t, f.admin)
			roleId := f.reader.GetStringSlice("roles")[0]
			scenario.Body = strings.NewReader(`{"managed":true,"ownerField":"owner","grants":[` + strings.Replace(grant, "%s", roleId, 1) + `]}`)
		}
		scenario.Test(t)
	}
}

func urlEncode(s string) string {
	r := strings.NewReplacer(" ", "%20", `"`, "%22", "&", "%26", ">", "%3E", "=", "%3D", "(", "%28", ")", "%29", "!", "%21")
	return r.Replace(s)
}
