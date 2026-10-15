package pages

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestErrorPages(t *testing.T) {
	// the members app without a sign in page, with a custom 404 (published)
	// and a draft 403
	factory := func(t testing.TB) *tests.TestApp {
		app := membersApp(t)
		settings, _ := app.FindFirstRecordByFilter(SettingsCollection, "id != ''")
		settings.Set("login_page", "")
		if err := app.Save(settings); err != nil {
			t.Fatal(err)
		}
		testutil.Create(t, app, Collection, map[string]any{"title": "Lost", "slug": "error-404", "kind": kindError, "error_code": "404", "published": true, "kit": kitBasecoat,
			"html": `<h1>Lost at <span data-pb-formula="{{error.path}}">x</span> (<span data-pb-formula="{{error.code}}">x</span>)</h1>`})
		testutil.Create(t, app, Collection, map[string]any{"title": "Nope", "slug": "error-403", "kind": kindError, "error_code": "403", "published": false,
			"html": `<h1>Custom 403</h1>`})
		return app
	}
	cases := []struct {
		name, url, user string
		status          int
		content, not    []string
	}{
		{name: "the site's 404 page", url: "/nothing-here?x=1", status: 404, content: []string{"Lost at <span>/nothing-here?x=1</span> (<span>404</span>)", "/p/_theme.css"}},
		{name: "an error page isn't served at its address", url: "/error-404", status: 404, content: []string{"Lost at <span>/error-404</span>"}},
		{name: "the default 401 page, with the theme", url: "/members", status: 401, content: []string{"<h1", "Sign in required", "/p/_theme.css"}, not: []string{"Sign in with another account"}},
		{name: "the default 403 (the custom one is a draft)", url: "/staff", user: "bob@test.com", status: 403,
			content: []string{"No access", "Sign in with another account", `name="_next" value="/staff"`}, not: []string{"Custom 403"}},
	}
	for _, c := range cases {
		headers := map[string]string{}
		s := &tests.ApiScenario{
			Name: c.name, Method: http.MethodGet, URL: c.url, Headers: headers,
			TestAppFactory: factory, ExpectedStatus: c.status, ExpectedContent: c.content, NotExpectedContent: c.not,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if c.user != "" {
					headers["Cookie"] = sessionOf(t, app, c.user)
				}
			},
		}
		s.Test(t)
	}
}

func TestErrorPageValidation(t *testing.T) {
	f := setup(t)
	col, _ := f.app.FindCollectionByNameOrId(Collection)
	save := func(data map[string]any) (*core.Record, error) {
		r := core.NewRecord(col)
		r.Load(map[string]any{"title": "E", "html": "<p>x</p>", "kind": kindError})
		r.Load(data)
		return r, f.app.Save(r)
	}
	r, err := save(map[string]any{"slug": "e1", "error_code": "404", "home": true, "access": accessUsers})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetBool("home") || r.GetString("access") != accessPublic {
		t.Errorf("an error page is public and never the home page: %v %s", r.GetBool("home"), r.GetString("access"))
	}
	if _, err := save(map[string]any{"slug": "e2", "error_code": "404"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("a second 404 page: %v", err)
	}
	if _, err := save(map[string]any{"slug": "e3", "error_code": "418"}); err == nil {
		t.Error("an unknown code")
	}
	if _, err := save(map[string]any{"slug": "e4", "error_code": "500"}); err != nil {
		t.Errorf("a 500 page: %v", err)
	}
	// other pages have no code
	r, err = save(map[string]any{"slug": "s1", "kind": "", "error_code": "401"})
	if err != nil || r.GetString("error_code") != "" {
		t.Errorf("a single page: %v %q", err, r.GetString("error_code"))
	}
}
