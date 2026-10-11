package rbac_test

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/rbac"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app                    *tests.TestApp
	author, editor, nobody *core.Record
	superuser              *core.Record
}

// setup creates a "posts" collection managed by RBAC where authors can
// list/create/update their own posts and editors can list/update all posts.
func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	rbac.Register(app)

	users, _ := app.FindCollectionByNameOrId("users")
	posts := testutil.CreateCollection(t, app, "posts",
		&core.TextField{Name: "title"},
		&core.RelationField{Name: "owner", CollectionId: users.Id, MaxSelect: 1},
	)

	authorRole := testutil.Create(t, app, "roles", map[string]any{"name": "author"})
	editorRole := testutil.Create(t, app, "roles", map[string]any{"name": "editor"})

	f := &fixture{app: app}
	f.author = testutil.Create(t, app, "users", map[string]any{"email": "author@test.com", "password": "1234567890", "roles": []string{authorRole.Id}})
	f.editor = testutil.Create(t, app, "users", map[string]any{"email": "editor@test.com", "password": "1234567890", "roles": []string{editorRole.Id}})
	f.nobody = testutil.Create(t, app, "users", map[string]any{"email": "nobody@test.com", "password": "1234567890"})
	f.superuser = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})

	testutil.Create(t, app, "posts", map[string]any{"title": "author post", "owner": f.author.Id})
	testutil.Create(t, app, "posts", map[string]any{"title": "editor post", "owner": f.editor.Id})

	testutil.Create(t, app, "rbac_collections", map[string]any{"collection": posts.Id, "owner_field": "owner", "managed": true})
	for _, p := range []map[string]any{
		{"role": authorRole.Id, "action": "list", "scope": "own"},
		{"role": authorRole.Id, "action": "create", "scope": "own"},
		{"role": authorRole.Id, "action": "update", "scope": "own"},
		{"role": editorRole.Id, "action": "list", "scope": "all"},
		{"role": editorRole.Id, "action": "update", "scope": "all"},
	} {
		p["collection"] = posts.Id
		testutil.Create(t, app, "permissions", p)
	}
	return f
}

func TestRulesAreCompiledFromPermissions(t *testing.T) {
	f := setup(t)
	posts, err := f.app.FindCollectionByNameOrId("posts")
	if err != nil {
		t.Fatal(err)
	}
	if posts.ListRule == nil || !strings.Contains(*posts.ListRule, "owner = @request.auth.id") {
		t.Fatalf("unexpected list rule %v", posts.ListRule)
	}
	if posts.ViewRule != nil || posts.DeleteRule != nil {
		t.Fatalf("expected nil view/delete rules, got %v / %v", posts.ViewRule, posts.DeleteRule)
	}
}

func TestEnforcement(t *testing.T) {
	author := func(f *fixture) *core.Record { return f.author }
	editor := func(f *fixture) *core.Record { return f.editor }
	nobody := func(f *fixture) *core.Record { return f.nobody }

	scenarios := []struct {
		name     string
		user     func(f *fixture) *core.Record
		method   string
		owner    func(f *fixture) *core.Record // body owner for create requests
		status   int
		expected []string
	}{
		{"author lists own posts only", author, http.MethodGet, nil, 200, []string{`"totalItems":1`, `"title":"author post"`}},
		{"editor lists all posts", editor, http.MethodGet, nil, 200, []string{`"totalItems":2`}},
		{"user without role sees nothing", nobody, http.MethodGet, nil, 200, []string{`"totalItems":0`}},
		{"author creates own post", author, http.MethodPost, author, 200, []string{`"title":"new"`}},
		{"author can't create a post for someone else", author, http.MethodPost, editor, 400, []string{`"data":{}`}},
		{"editor can't create", editor, http.MethodPost, editor, 400, []string{`"data":{}`}},
	}

	for _, s := range scenarios {
		var f *fixture
		headers := map[string]string{}
		body := &bytes.Buffer{}
		(&tests.ApiScenario{
			Name:    s.name,
			Method:  s.method,
			URL:     "/api/collections/posts/records",
			Body:    body,
			Headers: headers,
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				f = setup(t)
				return f.app
			},
			// headers and body are read after this hook, so they can depend on the fixture
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				headers["Authorization"] = testutil.AuthToken(t, s.user(f))
				if s.owner != nil {
					fmt.Fprintf(body, `{"title":"new","owner":%q}`, s.owner(f).Id)
				}
			},
			ExpectedStatus:  s.status,
			ExpectedContent: s.expected,
		}).Test(t)
	}
}

func TestMatrixRoutes(t *testing.T) {
	var f *fixture
	headers := map[string]string{}
	body := &bytes.Buffer{}
	(&tests.ApiScenario{
		Name:    "save matrix replaces the permissions and recompiles",
		Method:  http.MethodPut,
		URL:     "/api/rbac/matrix/posts",
		Body:    body,
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			return f.app
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			headers["Authorization"] = testutil.AuthToken(t, f.superuser)
			role := f.editor.GetStringSlice("roles")[0]
			fmt.Fprintf(body, `{"managed":true,"ownerField":"owner","grants":[{"roleId":%q,"action":"view","scope":"all"}]}`, role)
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"action":"view"`, `"managed":true`, `"list":null`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			posts, _ := app.FindCollectionByNameOrId("posts")
			if posts.ListRule != nil || posts.ViewRule == nil {
				t.Fatalf("expected only a view rule, got list=%v view=%v", posts.ListRule, posts.ViewRule)
			}
			perms, _ := app.FindAllRecords("permissions")
			if len(perms) != 1 {
				t.Fatalf("expected 1 permission, got %d", len(perms))
			}
		},
	}).Test(t)

	headers2 := map[string]string{}
	var f2 *fixture
	(&tests.ApiScenario{
		Name:    "regular users can't access the matrix",
		Method:  http.MethodGet,
		URL:     "/api/rbac/matrix/posts",
		Headers: headers2,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f2 = setup(t)
			return f2.app
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			headers2["Authorization"] = testutil.AuthToken(t, f2.editor)
		},
		ExpectedStatus:  403,
		ExpectedContent: []string{`"data":{}`},
	}).Test(t)
}

func TestOwnUpdateKeepsTheOwner(t *testing.T) {
	scenarios := []struct {
		name   string
		body   func(f *fixture) string
		status int
	}{
		{"author updates own post", func(f *fixture) string { return `{"title":"edited"}` }, 200},
		{"author keeps self as owner", func(f *fixture) string { return fmt.Sprintf(`{"owner":%q}`, f.author.Id) }, 200},
		{"author can't give the post away", func(f *fixture) string { return fmt.Sprintf(`{"owner":%q}`, f.editor.Id) }, 404},
	}
	for _, s := range scenarios {
		var f *fixture
		headers := map[string]string{}
		body := &bytes.Buffer{}
		scenario := &tests.ApiScenario{
			Name:    s.name,
			Method:  http.MethodPatch,
			Body:    body,
			Headers: headers,
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				f = setup(t)
				return f.app
			},
			ExpectedStatus:  s.status,
			ExpectedContent: []string{`"`},
		}
		scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			post, err := app.FindFirstRecordByData("posts", "title", "author post")
			if err != nil {
				t.Fatal(err)
			}
			scenario.URL = "/api/collections/posts/records/" + post.Id
			headers["Authorization"] = testutil.AuthToken(t, f.author)
			body.WriteString(s.body(f))
		}
		scenario.Test(t)
	}
}
