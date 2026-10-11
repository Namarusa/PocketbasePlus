package forms_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/activity"
	"github.com/Namarusa/PocketbasePlus/internal/forms"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app     *tests.TestApp
	form    *core.Record
	acme    *core.Record
	user    *core.Record
	leadsId string
}

// setup creates a closed "leads" collection (no API rules) and its form.
func setup(t testing.TB, change func(f *fixture, form map[string]any)) *fixture {
	app := testutil.NewApp(t)
	activity.Register(app)
	forms.Register(app)
	f := &fixture{app: app}

	companies := testutil.CreateCollection(t, app, "companies", &core.TextField{Name: "name", Presentable: true})
	f.acme = testutil.Create(t, app, "companies", map[string]any{"name": "Acme"})
	leads := testutil.CreateCollection(t, app, "leads",
		&core.TextField{Name: "name", Required: true},
		&core.EmailField{Name: "email", Required: true},
		&core.EditorField{Name: "message"},
		&core.NumberField{Name: "budget"},
		&core.BoolField{Name: "consent"},
		&core.SelectField{Name: "topic", MaxSelect: 1, Values: []string{"sales", "support"}},
		&core.SelectField{Name: "tags", MaxSelect: 3, Values: []string{"a", "b", "c"}},
		&core.TextField{Name: "source"},
		&core.TextField{Name: "status"},
		&core.TextField{Name: "owner"},
		&core.FileField{Name: "attachment", MaxSelect: 1, MaxSize: 1 << 20},
		&core.RelationField{Name: "company", CollectionId: companies.Id, MaxSelect: 1},
	)
	f.leadsId = leads.Id
	f.user = testutil.Create(t, app, "users", map[string]any{"email": "user@test.com", "password": "1234567890"})

	data := map[string]any{
		"name": "Contact", "slug": "contact", "collection": "leads", "active": true,
		"fields": []map[string]any{
			{"field": "name", "label": "Your name"}, {"field": "email"}, {"field": "message"}, {"field": "budget"},
			{"field": "consent"}, {"field": "topic"}, {"field": "tags"}, {"field": "attachment"},
			{"field": "company", "listRecords": true},
		},
		"fixed":           map[string]any{"status": "new", "source": "@query.src"},
		"allowed_domains": []string{"https://partner.example.com/page"},
		"rate_limit":      3,
		"success_message": "Thanks!",
	}
	if change != nil {
		change(f, data)
	}
	f.form = testutil.Create(t, app, "forms", data)
	return f
}

type req struct {
	name     string
	method   string
	url      string
	body     func(f *fixture) (io.Reader, string)
	headers  map[string]string
	auth     bool
	status   int
	expected []string
	not      []string
	change   func(f *fixture, form map[string]any)
	before   func(t testing.TB, f *fixture)
	after    func(t testing.TB, f *fixture, res *http.Response)
}

func (r req) run(t *testing.T) {
	var f *fixture
	headers := map[string]string{}
	for k, v := range r.headers {
		headers[k] = v
	}
	s := &tests.ApiScenario{
		Name: r.name, Method: r.method, URL: r.url, Headers: headers,
		ExpectedStatus: r.status, ExpectedContent: r.expected, NotExpectedContent: r.not,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t, r.change)
			return f.app
		},
	}
	s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		if r.before != nil {
			r.before(t, f)
		}
		if r.body != nil {
			body, ct := r.body(f)
			s.Body = body
			headers["Content-Type"] = ct
		}
		if r.auth {
			headers["Authorization"] = testutil.AuthToken(t, f.user)
		}
	}
	if r.after != nil {
		s.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) { r.after(t, f, res) }
	}
	s.Test(t)
}

func jsonBody(fn func(f *fixture) map[string]any) func(f *fixture) (io.Reader, string) {
	return func(f *fixture) (io.Reader, string) {
		raw, _ := json.Marshal(fn(f))
		return bytes.NewReader(raw), "application/json"
	}
}

func valid(f *fixture) map[string]any {
	return map[string]any{
		"_token": forms.IssueToken(f.form), "_query": map[string]any{"src": "newsletter"},
		"name": "Ada", "email": "ada@example.com", "budget": 1500, "consent": true, "topic": "sales",
		"tags": []string{"a", "c"}, "company": f.acme.Id,
		"status": "hacked", "owner": "intruder", // not in the form: ignored
	}
}

func onlyLead(t testing.TB, f *fixture) *core.Record {
	t.Helper()
	list, _ := f.app.FindAllRecords("leads")
	if len(list) != 1 {
		t.Fatalf("expected 1 lead, got %d", len(list))
	}
	return list[0]
}

func TestSchema(t *testing.T) {
	req{
		name: "only the form fields", method: http.MethodGet, url: "/api/forms/contact", status: 200,
		expected: []string{`"label":"Your name"`, `"name":"company"`, `"label":"Acme"`, `"token":"`, `"required":true`},
		not:      []string{`"name":"status"`, `"name":"source"`, `"name":"owner"`, `"secret"`},
	}.run(t)
	req{
		name: "inactive forms are not found", method: http.MethodGet, url: "/api/forms/contact", status: 404,
		change: func(f *fixture, form map[string]any) { form["active"] = false }, expected: []string{`"data":{}`},
	}.run(t)
}

func TestSubmit(t *testing.T) {
	req{
		name: "valid submission", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid),
		status: 200, expected: []string{`"ok":true`, `"message":"Thanks!"`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			lead := onlyLead(t, f)
			if lead.GetString("status") != "new" || lead.GetString("source") != "newsletter" || lead.GetString("owner") != "" {
				t.Fatalf("fixed values must apply and other fields be ignored: %v", lead.FieldsData())
			}
			if lead.GetFloat("budget") != 1500 || !lead.GetBool("consent") || len(lead.GetStringSlice("tags")) != 2 || lead.GetString("company") != f.acme.Id {
				t.Fatalf("unexpected values %v", lead.FieldsData())
			}
			revs, _ := f.app.FindAllRecords("revisions")
			if len(revs) == 0 || revs[len(revs)-1].GetString("actor_type") != "form" || revs[len(revs)-1].GetString("actor_label") != "Form: Contact" {
				t.Fatalf("expected the form as the author of the revision")
			}
		},
	}.run(t)

	req{
		name: "validation errors per field", method: http.MethodPost, url: "/api/forms/contact/submit",
		body: jsonBody(func(f *fixture) map[string]any {
			v := valid(f)
			delete(v, "name")
			v["email"] = "not-an-email"
			return v
		}),
		status: 400, expected: []string{`"name":{"code":"validation_required"`, `"email":{`},
	}.run(t)

	req{
		name: "expired token", method: http.MethodPost, url: "/api/forms/contact/submit",
		body: jsonBody(func(f *fixture) map[string]any {
			v := valid(f)
			v["_token"] = forms.OldToken(f.form)
			return v
		}),
		status: 400, expected: []string{"expired"},
	}.run(t)

	req{
		name: "honeypot pretends success", method: http.MethodPost, url: "/api/forms/contact/submit",
		body: jsonBody(func(f *fixture) map[string]any {
			v := valid(f)
			v["website"] = "http://spam"
			return v
		}),
		status: 200, expected: []string{`"ok":true`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			if list, _ := f.app.FindAllRecords("leads"); len(list) != 0 {
				t.Fatal("the honeypot submission must not be saved")
			}
		},
	}.run(t)

	req{
		name: "other sites can't submit", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid),
		headers: map[string]string{"Origin": "https://evil.example.com"}, status: 403, expected: []string{"can't be submitted from this site"},
	}.run(t)
	req{
		name: "allowed domains can", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid),
		headers: map[string]string{"Origin": "https://partner.example.com"}, status: 200, expected: []string{`"ok":true`},
	}.run(t)

	login := func(f *fixture, form map[string]any) {
		form["require_auth"] = true
		form["fixed"] = map[string]any{"owner": "@request.auth.id"}
	}
	req{name: "login required", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid), change: login, status: 401, expected: []string{"Sign in"}}.run(t)
	req{
		name: "signed-in user as fixed value", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid), change: login, auth: true,
		status: 200, expected: []string{`"ok":true`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			if lead := onlyLead(t, f); lead.GetString("owner") != f.user.Id {
				t.Fatalf("expected the user as owner, got %q", lead.GetString("owner"))
			}
		},
	}.run(t)

	req{
		name: "multipart with a file", method: http.MethodPost, url: "/api/forms/contact/submit",
		body: func(f *fixture) (io.Reader, string) {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			w.WriteField("_token", forms.IssueToken(f.form))
			w.WriteField("name", "Bob")
			w.WriteField("email", "bob@example.com")
			w.WriteField("consent", "on")
			w.WriteField("tags", "a")
			w.WriteField("tags", "b")
			fw, _ := w.CreateFormFile("attachment", "brief.txt")
			fw.Write([]byte("hello"))
			w.Close()
			return &buf, w.FormDataContentType()
		},
		status: 200, expected: []string{`"ok":true`},
		after: func(t testing.TB, f *fixture, _ *http.Response) {
			lead := onlyLead(t, f)
			if !lead.GetBool("consent") || len(lead.GetStringSlice("tags")) != 2 || !strings.HasPrefix(lead.GetString("attachment"), "brief_") {
				t.Fatalf("unexpected multipart values %v", lead.FieldsData())
			}
		},
	}.run(t)
}

func TestRateLimit(t *testing.T) {
	req{
		name: "the IP used its submissions", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid),
		change: func(f *fixture, form map[string]any) { form["rate_limit"] = 2 },
		// httptest requests come from 192.0.2.1
		before:   func(t testing.TB, f *fixture) { forms.UseLimit(f.form.Id, "192.0.2.1", 2) },
		status:   429,
		expected: []string{"Too many submissions"},
	}.run(t)
	req{
		name: "unlimited when 0", method: http.MethodPost, url: "/api/forms/contact/submit", body: jsonBody(valid),
		change: func(f *fixture, form map[string]any) { form["rate_limit"] = 0 },
		before: func(t testing.TB, f *fixture) { forms.UseLimit(f.form.Id, "192.0.2.1", 50) },
		status: 200, expected: []string{`"ok":true`},
	}.run(t)
}

func TestPagesFrameAncestors(t *testing.T) {
	check := func(name, url, wantCSP string, wantXFO bool) {
		req{
			name: name, method: http.MethodGet, url: url, status: 200, expected: []string{`data-slug="contact"`, "/_/extensions/pocketbaseplus/form.js"},
			after: func(t testing.TB, f *fixture, res *http.Response) {
				if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors "+wantCSP) {
					t.Fatalf("unexpected CSP %q", csp)
				}
				if got := res.Header.Get("X-Frame-Options") == "DENY"; got != wantXFO {
					t.Fatalf("unexpected X-Frame-Options %q", res.Header.Get("X-Frame-Options"))
				}
			},
		}.run(t)
	}
	check("full page can't be framed", "/f/contact", "'none'", true)
	check("embed: the site and the allowed domains", "/f/contact/embed", "'self' https://partner.example.com", false)
}

func TestFormValidation(t *testing.T) {
	app := testutil.NewApp(t)
	forms.Register(app)
	companies := testutil.CreateCollection(t, app, "companies", &core.TextField{Name: "name"})
	testutil.CreateCollection(t, app, "leads", &core.TextField{Name: "name"}, &core.RelationField{Name: "company", CollectionId: companies.Id, MaxSelect: 1})
	col, _ := app.FindCollectionByNameOrId("forms")
	for name, data := range map[string]map[string]any{
		"relation without public listing": {"fields": []map[string]any{{"field": "company"}}},
		"invalid domain":                  {"allowed_domains": []string{"javascript:alert(1)"}},
		"unknown field":                   {"fields": []map[string]any{{"field": "nope"}}},
		"auth collection":                 {"collection": "users"},
		"internal collection":             {"collection": "flows"},
	} {
		r := core.NewRecord(col)
		r.Set("name", "Test")
		r.Set("slug", "test")
		r.Set("collection", "leads")
		for k, v := range data {
			r.Set(k, v)
		}
		if err := app.Save(r); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDraftPreview(t *testing.T) {
	asSuperuser := func(t testing.TB, f *fixture) map[string]string {
		su := testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
		return map[string]string{"Authorization": testutil.AuthToken(t, su)}
	}
	draft := func(body map[string]any, check func(t testing.TB, f *fixture)) func(f *fixture) (io.Reader, string) {
		return jsonBody(func(f *fixture) map[string]any { return body })
	}
	var f *fixture
	headers := map[string]string{}
	s := &tests.ApiScenario{
		Name: "unsaved settings are previewed", Method: http.MethodPost, URL: "/api/forms/contact/draft", Headers: headers,
		ExpectedStatus: 200, ExpectedContent: []string{`"title":"New title"`, `"draft":true`, `"token":""`, `"label":"Full name"`},
		NotExpectedContent: []string{`"name":"email"`},
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t, nil)
			return f.app
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			saved, _ := app.FindRecordById("forms", f.form.Id)
			if saved.GetString("title") == "New title" {
				t.Fatal("a draft preview must not save the form")
			}
		},
	}
	s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		for k, v := range asSuperuser(t, f) {
			headers[k] = v
		}
		body, ct := draft(map[string]any{"title": "New title", "fields": []map[string]any{{"field": "name", "label": "Full name"}}}, nil)(f)
		s.Body = body
		headers["Content-Type"] = ct
	}
	s.Test(t)

	req{
		name: "guests can't preview drafts", method: http.MethodPost, url: "/api/forms/contact/draft",
		body:   jsonBody(func(*fixture) map[string]any { return map[string]any{"fields": []map[string]any{{"field": "nope"}}} }),
		before: func(t testing.TB, f *fixture) {}, status: 401, expected: []string{`"data":{}`},
	}.run(t)

	req{
		name: "users can't preview drafts", method: http.MethodPost, url: "/api/forms/contact/draft", auth: true,
		body: jsonBody(func(*fixture) map[string]any { return map[string]any{"title": "x"} }), status: 403, expected: []string{`"data":{}`},
	}.run(t)
}
