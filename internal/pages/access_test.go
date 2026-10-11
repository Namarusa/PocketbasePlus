package pages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/types"
	xhtml "golang.org/x/net/html"

	"github.com/Namarusa/PocketbasePlus/internal/session"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

const staffRole = "rolestaff000001"

// membersApp has a public login page, a page for the signed-in users, one
// for the staff role, and two users (ada is staff).
func membersApp(t testing.TB) *tests.TestApp {
	f := setup(t)
	session.Register(f.app)
	testutil.Create(t, f.app, "roles", map[string]any{"id": staffRole, "name": "Staff"})
	testutil.Create(t, f.app, "users", map[string]any{"email": "ada@test.com", "password": "1234567890", "name": "Ada", "roles": []string{staffRole}})
	testutil.Create(t, f.app, "users", map[string]any{"email": "bob@test.com", "password": "1234567890", "name": "Bob"})
	f.page(t, map[string]any{"slug": "login", "html": `<form data-pb-action="flowsignin00001"><input name="email"></form>`})
	f.page(t, map[string]any{"slug": "members", "access": accessUsers, "html": `<p>Hello <b data-pb-user="name"></b> <i data-pb-user="email"></i></p>`})
	f.page(t, map[string]any{"slug": "staff", "access": accessRoles, "roles": []string{staffRole}, "html": `<p>Staff only</p>`})
	settings, err := f.app.FindFirstRecordByFilter(SettingsCollection, "id != ''")
	if err != nil {
		t.Fatal(err)
	}
	settings.Set("login_page", "login")
	if err := f.app.Save(settings); err != nil {
		t.Fatal(err)
	}
	return f.app
}

func sessionOf(t testing.TB, app core.App, email string) string {
	u, err := app.FindAuthRecordByEmail("users", email)
	if err != nil {
		t.Fatal(err)
	}
	return "gp_session=" + testutil.AuthToken(t, u)
}

func TestPageAccess(t *testing.T) {
	cases := []struct {
		name, url, user string
		status          int
		content         []string
		location        string
	}{
		{name: "public page", url: "/login", status: 200, content: []string{`action="/api/flows/action/flowsignin00001"`, `name="_page" value="/login"`}},
		{name: "login keeps next", url: "/login?next=%2Fstaff", status: 200, content: []string{`name="_next" value="/staff"`}},
		{name: "guest sent to the login page", url: "/members", status: 303, location: "/login?next=%2Fmembers"},
		{name: "signed-in user", url: "/members", user: "bob@test.com", status: 200, content: []string{"Hello <b>Bob</b> <i>bob@test.com</i>"}},
		{name: "user without the role", url: "/staff", user: "bob@test.com", status: 403, content: []string{"No access", `action="/api/session/logout"`}},
		{name: "staff", url: "/staff", user: "ada@test.com", status: 200, content: []string{"Staff only"}},
		{name: "expired or forged session is a guest", url: "/staff", user: "forged", status: 303, location: "/login?next=%2Fstaff"},
	}
	for _, c := range cases {
		headers := map[string]string{}
		s := &tests.ApiScenario{
			Name: c.name, Method: http.MethodGet, URL: c.url, Headers: headers,
			TestAppFactory: membersApp, ExpectedStatus: c.status, ExpectedContent: c.content,
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				switch c.user {
				case "":
				case "forged":
					headers["Cookie"] = "gp_session=abc.def.ghi"
				default:
					headers["Cookie"] = sessionOf(t, app, c.user)
				}
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if loc := res.Header.Get("Location"); loc != c.location {
					t.Errorf("expected the location %q, got %q", c.location, loc)
				}
				if c.status == 200 && res.Header.Get("Vary") != "Cookie" {
					t.Error("expected Vary: Cookie")
				}
			},
		}
		if c.content == nil {
			s.ExpectedContent = nil
			s.NotExpectedContent = []string{"Staff only"}
		}
		s.Test(t)
	}

	// signing out
	(&tests.ApiScenario{
		Name: "sign out", Method: http.MethodPost, URL: "/api/session/logout",
		Body: strings.NewReader("_next=%2Flogin"), Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		TestAppFactory: membersApp, ExpectedStatus: 303,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.Header.Get("Location") != "/login" {
				t.Errorf("unexpected location %q", res.Header.Get("Location"))
			}
			found := false
			for _, ck := range res.Cookies() {
				if ck.Name == "gp_session" && ck.MaxAge < 0 {
					found = true
				}
			}
			if !found {
				t.Error("expected the session cookie to be cleared")
			}
		},
	}).Test(t)
}

func TestVisitorRendering(t *testing.T) {
	f := setup(t)
	testutil.Create(t, f.app, "roles", map[string]any{"id": staffRole, "name": "Staff"})
	ada := testutil.Create(t, f.app, "users", map[string]any{"email": "ada@test.com", "password": "1234567890", "name": "Ada", "roles": []string{staffRole}})
	src := `<body><p data-pb-visible="guests">Sign in</p><p data-pb-visible="users">Hi <span data-pb-user="name"></span></p>` +
		`<p data-pb-visible="role:` + staffRole + `">Staff</p><p data-pb-visible="guests role:other">Other</p>` +
		`<div data-pb-message class="x"></div>` +
		`<ul data-pb-list="products" data-pb-filter="name = {{auth.name}}"><li><b data-pb-field="name"></b><button data-pb-action="flow1">Buy</button></li></ul>` +
		`<a data-pb-logout="/bye" class="btn">Sign out</a></body>`
	run := func(v visitor, flash *session.Flash) string {
		t.Helper()
		out, _, err := render(t.Context(), f.app, pageView{HTML: src, Visitor: v, Path: "/shop", Flash: flash}, url.Values{"next": {"//evil.test"}}, false)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// a product named like the visitor
	testutil.Create(t, f.app, "products", map[string]any{"id": "adaproduct00001", "name": "Ada"})

	guest := run(visitor{}, nil)
	for _, want := range []string{"Sign in", "Other", `<div class="x" hidden="">`, `<form style="display:contents" method="post" action="/api/session/logout"><button class="btn" type="submit">Sign out</button><input type="hidden" name="_next" value="/bye"/></form>`} {
		if !strings.Contains(guest, want) {
			t.Errorf("guest: expected %s\n%s", want, guest)
		}
	}
	for _, unwanted := range []string{"Staff", "Hi ", "<li>", "data-pb-", "_next\" value=\"//evil"} {
		if strings.Contains(guest, unwanted) {
			t.Errorf("guest: unexpected %s\n%s", unwanted, guest)
		}
	}

	member := run(visitor{user: ada, roles: []string{staffRole}}, &session.Flash{Type: "success", Text: "Saved <ok>"})
	for _, want := range []string{"Hi <span>Ada</span>", ">Staff<", `<div class="x gpp-flash gpp-flash-success" role="status">Saved &lt;ok&gt;</div>`,
		`<li><b>Ada</b><form style="display:contents" method="post" action="/api/flows/action/flow1"><button>Buy</button><input type="hidden" name="_page" value="/shop"/><input type="hidden" name="_rec" value="products:adaproduct00001:`} {
		if !strings.Contains(member, want) {
			t.Errorf("member: expected %s\n%s", want, member)
		}
	}
	if strings.Contains(member, "Sign in") || strings.Contains(member, "Other") || strings.Count(member, "<li>") != 1 {
		t.Errorf("member: unexpected content\n%s", member)
	}

	// without a message area, the message is shown at the top
	out, _, _ := render(t.Context(), f.app, pageView{HTML: "<body><main>x</main></body>", Flash: &session.Flash{Type: "error", Text: "Nope"}}, nil, false)
	if !strings.Contains(out, `<body><div class="gpp-flash gpp-flash-error" role="alert" style="margin:12px auto;max-width:960px">Nope</div><main>`) {
		t.Errorf("expected the message at the top\n%s", out)
	}
}

func TestPreviewAs(t *testing.T) {
	f := setup(t)
	testutil.Create(t, f.app, "roles", map[string]any{"id": staffRole, "name": "Staff"})
	testutil.Create(t, f.app, "users", map[string]any{"email": "ada@test.com", "password": "1234567890", "name": "Ada", "roles": []string{staffRole}})
	page := f.page(t, map[string]any{"slug": "staff", "access": accessRoles, "roles": []string{staffRole}})

	future := "9999999999"
	token := future + "." + previewSig(f.app, page.Id, previewScope("", "role:"+staffRole), future)
	if !validPreview(f.app, page, token, previewScope("", "role:"+staffRole)) || validPreview(f.app, page, token, previewScope("", "public")) || validPreview(f.app, page, token, "") {
		t.Error("a preview token is bound to its visitor")
	}
	if v := previewVisitor(f.app, "role:"+staffRole); !v.canView(page) || v.user.GetString("name") != "Ada" {
		t.Error("expected the first staff user")
	}
	if v := previewVisitor(f.app, "public"); v.canView(page) || v.user != nil {
		t.Error("the public can't view the page")
	}
	if v := previewVisitor(f.app, "users"); v.canView(page) || v.user == nil {
		t.Error("a user without the role can't view the page")
	}
	if !previewVisitor(f.app, "").canView(page) {
		t.Error("the superuser preview sees everything")
	}

	// validation
	col, _ := f.app.FindCollectionByNameOrId(Collection)
	r := core.NewRecord(col)
	r.Load(map[string]any{"title": "x", "slug": "x", "access": accessRoles})
	if err := f.app.Save(r); err == nil || !strings.Contains(err.Error(), "roles") {
		t.Errorf("expected the roles to be required, got %v", err)
	}
}

func TestLoginPageSetting(t *testing.T) {
	app := membersApp(t)
	login, _ := app.FindFirstRecordByData(Collection, "slug", "login")
	login.Set("slug", "sign-in")
	if err := app.Save(login); err != nil {
		t.Fatal(err)
	}
	if got := loginPage(app); got != "sign-in" {
		t.Errorf("expected the new slug, got %q", got)
	}
	if err := app.Delete(login); err != nil {
		t.Fatal(err)
	}
	if got := loginPage(app); got != "" {
		t.Errorf("expected no sign in page, got %q", got)
	}
}

func TestListsWithAPIRules(t *testing.T) {
	f := setup(t)
	testutil.Create(t, f.app, "roles", map[string]any{"id": staffRole, "name": "Staff"})
	ada := testutil.Create(t, f.app, "users", map[string]any{"email": "ada@test.com", "password": "1234567890", "name": "Ada", "roles": []string{staffRole}})
	bob := testutil.Create(t, f.app, "users", map[string]any{"email": "bob@test.com", "password": "1234567890", "name": "Bob"})
	// products: listed by the staff only (the fixture collection has no rules: superusers only)
	f.products.ListRule = types.Pointer(`@request.auth.roles.id ?= "` + staffRole + `"`)
	f.products.ViewRule = f.products.ListRule
	if err := f.app.Save(f.products); err != nil {
		t.Fatal(err)
	}
	list := func(access string) string {
		return `<body><ul data-pb-list="products" data-pb-sort="name" ` + access + `><li><b data-pb-field="name"></b></li></ul></body>`
	}
	run := func(src string, v visitor) string {
		t.Helper()
		out, _, err := render(t.Context(), f.app, pageView{HTML: src, Visitor: v}, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	rules := list(`data-pb-access="rules"`)
	if out := run(rules, visitor{user: ada, roles: []string{staffRole}}); strings.Count(out, "<li>") != 2 {
		t.Errorf("staff: expected both products\n%s", out)
	}
	for _, v := range []visitor{{}, {user: bob}} {
		if out := run(rules, v); strings.Count(out, "<li>") != 0 {
			t.Errorf("expected no products for %v\n%s", v.user, out)
		}
	}
	if out := run(list(""), visitor{}); strings.Count(out, "<li>") != 2 {
		t.Errorf("page is the gate: expected both products\n%s", out)
	}
	if out := run(rules, visitor{all: true}); strings.Count(out, "<li>") != 2 {
		t.Errorf("the superuser preview sees everything\n%s", out)
	}

	// a collection without list rule (superusers only): nothing with rules
	f.products.ListRule = nil
	if err := f.app.Save(f.products); err != nil {
		t.Fatal(err)
	}
	if out := run(rules, visitor{user: ada, roles: []string{staffRole}}); strings.Count(out, "<li>") != 0 {
		t.Errorf("superusers only: expected no products\n%s", out)
	}
}

func TestRecordPageWithAPIRules(t *testing.T) {
	cases := []struct {
		name, user string
		status     int
	}{
		{"staff views", "ada@test.com", 200},
		{"others don't", "bob@test.com", 404},
		{"guest is asked to sign in", "", 303},
	}
	for _, c := range cases {
		headers := map[string]string{}
		(&tests.ApiScenario{
			Name: c.name, Method: http.MethodGet, URL: "/product/" + lampID, Headers: headers,
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				app := membersApp(t)
				products, _ := app.FindCollectionByNameOrId("products")
				products.ViewRule = types.Pointer(`@request.auth.roles.id ?= "` + staffRole + `"`)
				if err := app.Save(products); err != nil {
					t.Fatal(err)
				}
				testutil.Create(t, app, Collection, map[string]any{"title": "Product", "slug": "product", "published": true,
					"record_collection": products.Id, "record_rules": true, "html": `<h1 data-pb-field="name"></h1>`})
				return app
			},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				if c.user != "" {
					headers["Cookie"] = sessionOf(t, app, c.user)
				}
			},
			ExpectedStatus:  c.status,
			ExpectedContent: map[int][]string{200: {"<h1>Lamp &lt;b&gt;</h1>"}, 404: {"Page not found"}, 303: nil}[c.status],
		}).Test(t)
	}
}

func TestRichTextAllowList(t *testing.T) {
	src := `<p style="color:red;position:fixed;background-color:url(x)">Hi <b>there</b><style>body{display:none}</style></p>` +
		`<form action="/api/flows/action/x" method="post"><input name="id" value="1"><button>Click</button></form>` +
		`<a href="https://example.com" target="_blank" onclick="x()">link</a><a href="javascript:alert(1)">bad</a>` +
		`<iframe src="/_/"></iframe><svg><a href="/x"><text>s</text></a></svg><font color="red">old</font>` +
		`<img src="/api/files/a/b/c.png" onerror="x()"><video src="/v.mp4"></video><!-- c -->`
	var b strings.Builder
	for _, n := range parseHTMLFragment(src) {
		_ = xhtml.Render(&b, n)
	}
	got := b.String()
	want := `<p style="color:red">Hi <b>there</b></p><a href="https://example.com" target="_blank" rel="nofollow ugc noopener noreferrer">link</a><a rel="nofollow ugc">bad</a>old<img src="/api/files/a/b/c.png" loading="lazy"/>`
	if got != want {
		t.Errorf("unexpected rich text\n got: %s\nwant: %s", got, want)
	}
}

func TestDraftAssets(t *testing.T) {
	const pageID = "pagedraft000001"
	var name, token string
	factory := func(t testing.TB) *tests.TestApp {
		f := setup(t)
		img, _ := filesystem.NewFileFromBytes([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), "photo.gif")
		p := f.page(t, map[string]any{"id": pageID, "slug": "draft", "published": false, "assets": []*filesystem.File{img}})
		name = p.GetStringSlice("assets")[0]
		token = "9999999999." + previewSig(f.app, pageID, "", "9999999999")
		return f.app
	}
	for _, c := range []struct {
		name, referer string
		status        int
	}{
		{"no referrer", "", 404},
		{"other page", "http://example.com/other", 404},
		{"preview", "http://example.com/draft?preview=TOKEN", 200},
		{"forged preview", "http://example.com/draft?preview=9999999999.abc", 404},
		{"builder", "http://example.com/_/", 200},
	} {
		headers := map[string]string{}
		s := &tests.ApiScenario{Name: c.name, Method: http.MethodGet, Headers: headers, TestAppFactory: factory, ExpectedStatus: c.status, ExpectedContent: []string{""}}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			s.URL = "/p/_assets/" + pageID + "/" + name
			if c.referer != "" {
				headers["Referer"] = strings.Replace(c.referer, "TOKEN", token, 1)
			}
		}
		s.Test(t)
	}
}
