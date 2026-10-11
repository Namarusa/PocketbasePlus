package flows_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/session"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

const (
	signInFlowID = "flowsignin00001"
	staffFlowID  = "flowstaff000001"
	staffRoleID  = "rolestaff000001"
)

// actionApp has a sign in flow, a staff only flow and two users.
func actionApp(t testing.TB) *tests.TestApp {
	f := setup(t)
	testutil.Create(t, f.app, "roles", map[string]any{"id": staffRoleID, "name": "Staff"})
	testutil.Create(t, f.app, "users", map[string]any{"email": "ada@test.com", "password": "1234567890", "roles": []string{staffRoleID}})
	testutil.Create(t, f.app, "users", map[string]any{"email": "bob@test.com", "password": "1234567890"})
	testutil.Create(t, f.app, "flows", map[string]any{
		"id": signInFlowID, "name": "Sign in", "active": true, "trigger_type": "action", "trigger": map[string]any{},
		"steps": []map[string]any{
			{"key": "login", "type": "sign_in", "config": map[string]any{"identity": "{{.trigger.form.email}}", "password": "{{.trigger.form.password}}"}},
			{"key": "go", "type": "redirect", "config": map[string]any{"url": "", "message": "Welcome {{.steps.login.user.email}}"}},
		},
	})
	testutil.Create(t, f.app, "flows", map[string]any{
		"id": staffFlowID, "name": "Staff", "active": true, "trigger_type": "action",
		"trigger": map[string]any{"access": "roles", "roles": []string{staffRoleID}},
		"steps": []map[string]any{
			{"key": "note", "type": "create_record", "config": map[string]any{"checkRules": false, "collection": "audit", "data": map[string]any{"message": "{{.trigger.user.email}} {{.trigger.form.note}} {{.trigger.record.number}}"}}},
			{"key": "go", "type": "redirect", "config": map[string]any{"url": "/done?n={{.trigger.form.note}}"}},
		},
	})
	return f.app
}

func form(values map[string]string) *strings.Reader {
	v := url.Values{}
	for k, val := range values {
		v.Set(k, val)
	}
	return strings.NewReader(v.Encode())
}

func formHeaders(extra map[string]string) map[string]string {
	h := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://example.com"}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func cookie(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func sessionCookie(t testing.TB, app core.App, email string) string {
	u, err := app.FindAuthRecordByEmail("users", email)
	if err != nil {
		t.Fatal(err)
	}
	return "gp_session=" + testutil.AuthToken(t, u)
}

func TestActionSignIn(t *testing.T) {
	(&tests.ApiScenario{
		Name: "valid password: signed in, sent to next", Method: http.MethodPost, URL: "/api/flows/action/" + signInFlowID,
		Body:           form(map[string]string{"email": "ada@test.com", "password": "1234567890", "_page": "/login?next=/members", "_next": "/members"}),
		Headers:        formHeaders(nil),
		TestAppFactory: actionApp,
		ExpectedStatus: 303,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if loc := res.Header.Get("Location"); loc != "/members" {
				t.Errorf("expected the next page, got %q", loc)
			}
			c := cookie(res, "gp_session")
			if c == nil || !c.HttpOnly || c.Value == "" {
				t.Fatalf("expected the session cookie, got %v", c)
			}
			if u, err := app.FindAuthRecordByToken(c.Value, core.TokenTypeAuth); err != nil || u.Email() != "ada@test.com" {
				t.Errorf("invalid session token %v", err)
			}
			if f := cookie(res, "gp_flash"); f == nil || f.Value == "" {
				t.Error("expected the welcome message")
			}
			runs, _ := app.FindAllRecords("flow_runs")
			for _, r := range runs {
				if strings.Contains(r.GetString("input")+r.GetString("steps"), "1234567890") {
					t.Errorf("the password must not be logged: %s", r.GetString("input"))
				}
			}
		},
	}).Test(t)

	(&tests.ApiScenario{
		Name: "wrong password: back to the page, no session", Method: http.MethodPost, URL: "/api/flows/action/" + signInFlowID,
		Body:           form(map[string]string{"email": "ada@test.com", "password": "wrong", "_page": "/login?next=/members", "_next": "/members"}),
		Headers:        formHeaders(nil),
		TestAppFactory: actionApp,
		ExpectedStatus: 303,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if loc := res.Header.Get("Location"); loc != "/login?next=/members" {
				t.Errorf("expected the login page, got %q", loc)
			}
			if c := cookie(res, "gp_session"); c != nil {
				t.Error("no session expected")
			}
		},
	}).Test(t)

	(&tests.ApiScenario{
		Name: "JSON answer", Method: http.MethodPost, URL: "/api/flows/action/" + signInFlowID,
		Body:            strings.NewReader(`{"email":"nobody@test.com","password":"x"}`),
		Headers:         map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		TestAppFactory:  actionApp,
		ExpectedStatus:  200,
		ExpectedContent: []string{`"status":"rejected"`, `"message":"Invalid email or password."`},
	}).Test(t)

	(&tests.ApiScenario{
		Name: "posted by another site", Method: http.MethodPost, URL: "/api/flows/action/" + signInFlowID,
		Body:            form(map[string]string{"email": "ada@test.com", "password": "1234567890"}),
		Headers:         formHeaders(map[string]string{"Origin": "https://evil.test"}),
		TestAppFactory:  actionApp,
		ExpectedStatus:  403,
		ExpectedContent: []string{`"data":{}`},
	}).Test(t)
}

func TestActionAccess(t *testing.T) {
	var ref string
	cases := []struct {
		name, user, location string
		audit                bool
	}{
		{"guest", "", "/orders", false},
		{"user without the role", "bob@test.com", "/orders", false},
		{"staff", "ada@test.com", "/done?n=hi", true},
	}
	for _, c := range cases {
		headers := formHeaders(nil)
		s := &tests.ApiScenario{
			Name: c.name, Method: http.MethodPost, URL: "/api/flows/action/" + staffFlowID,
			Headers: headers,
			TestAppFactory: func(t testing.TB) *tests.TestApp {
				app := actionApp(t)
				order := testutil.Create(t, app, "orders", map[string]any{"number": "A7"})
				ref = session.SignRecord(app, staffFlowID, "orders", order.Id)
				return app
			},
			ExpectedStatus: 303,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if loc := res.Header.Get("Location"); loc != c.location {
					t.Errorf("expected %q, got %q", c.location, loc)
				}
				list, _ := app.FindAllRecords("audit")
				if c.audit != (len(list) == 1) {
					t.Fatalf("unexpected audit records %d", len(list))
				}
				if c.audit && list[0].GetString("message") != "ada@test.com hi A7" {
					t.Errorf("unexpected trigger data %q", list[0].GetString("message"))
				}
			},
		}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			if c.user != "" {
				headers["Cookie"] = sessionCookie(t, app, c.user)
			}
			s.Body = form(map[string]string{"note": "hi", "_page": "/orders", "_rec": ref})
		}
		s.Test(t)
	}

	(&tests.ApiScenario{
		Name: "forged record reference is ignored", Method: http.MethodPost, URL: "/api/flows/action/" + staffFlowID,
		TestAppFactory: actionApp, ExpectedStatus: 303,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {},
		Headers:        formHeaders(nil),
		Body:           form(map[string]string{"_rec": "orders:x:bad"}),
	}).Test(t)
}

func TestActionStepsValidation(t *testing.T) {
	f := setup(t)
	col, _ := f.app.FindCollectionByNameOrId("flows")
	for _, typ := range []string{"sign_in", "sign_out", "redirect"} {
		r := core.NewRecord(col)
		r.Load(map[string]any{"name": "x", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
			"steps": []map[string]any{{"key": "s", "type": "condition", "config": map[string]any{}, "then": []map[string]any{{"key": "a", "type": typ, "config": map[string]any{}}}}}})
		if err := f.app.Save(r); err == nil || !strings.Contains(err.Error(), "Page action") {
			t.Errorf("%s: expected a validation error, got %v", typ, err)
		}
	}
	// external redirects are refused when the flow runs
	flow := f.flow(t, "action", map[string]any{}, []map[string]any{{"key": "go", "type": "redirect", "config": map[string]any{"url": "https://evil.test"}}})
	if access := flow.GetString("trigger"); !strings.Contains(access, `"public"`) {
		t.Errorf("expected public access by default, got %s", access)
	}
}

func TestActionLimitsAndRedirects(t *testing.T) {
	flows.ResetSignInLimiter()
	t.Cleanup(flows.ResetSignInLimiter)
	const externalID = "flowexternal001"
	factory := func(t testing.TB) *tests.TestApp {
		app := actionApp(t)
		testutil.Create(t, app, "flows", map[string]any{"id": externalID, "name": "x", "active": true, "trigger_type": "action", "trigger": map[string]any{},
			"steps": []map[string]any{{"key": "go", "type": "redirect", "config": map[string]any{"url": "https://evil.test"}}}})
		return app
	}
	post := func(flowID, body string) string {
		t.Helper()
		var out string
		(&tests.ApiScenario{
			Name: "post", Method: http.MethodPost, URL: "/api/flows/action/" + flowID,
			Body:            strings.NewReader(body),
			Headers:         map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
			TestAppFactory:  factory,
			ExpectedStatus:  200,
			ExpectedContent: []string{`"status":`},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				b := new(strings.Builder)
				_, _ = io.Copy(b, res.Body)
				out = b.String()
			},
		}).Test(t)
		return out
	}
	for range 10 {
		post(signInFlowID, `{"email":"ada@test.com","password":"wrong"}`)
	}
	if out := post(signInFlowID, `{"email":"ada@test.com","password":"1234567890"}`); !strings.Contains(out, "Too many failed attempts") {
		t.Errorf("expected the limit, got %s", out)
	}
	if out := post(externalID, `{}`); !strings.Contains(out, `"status":"failed"`) || strings.Contains(out, "evil") {
		t.Errorf("an external redirect must fail, got %s", out)
	}
}

// notesApp: notes listed by the staff only, created by any signed-in user,
// changed by their owner only (the API rules of the collection).
const (
	notesFlowID  = "flownotes000001"
	createFlowID = "flowcreate00001"
	readFlowID   = "flowread0000001"
	adaNoteID    = "noteada00000001"
)

func notesApp(t testing.TB) *tests.TestApp {
	app := actionApp(t)
	notes := core.NewBaseCollection("notes")
	notes.Fields.Add(&core.TextField{Name: "text"}, &core.TextField{Name: "owner"})
	notes.ListRule = types.Pointer(`@request.auth.roles.id ?= "` + staffRoleID + `"`)
	notes.ViewRule = notes.ListRule
	notes.CreateRule = types.Pointer(`@request.auth.id != "" && owner = @request.auth.id`)
	notes.UpdateRule = types.Pointer(`owner = @request.auth.id`)
	if err := app.Save(notes); err != nil {
		t.Fatal(err)
	}
	ada, _ := app.FindAuthRecordByEmail("users", "ada@test.com")
	testutil.Create(t, app, "notes", map[string]any{"id": adaNoteID, "text": "first", "owner": ada.Id})
	steps := func(step map[string]any) []map[string]any { return []map[string]any{step} }
	testutil.Create(t, app, "flows", map[string]any{"id": notesFlowID, "name": "Edit", "active": true, "trigger_type": "action", "trigger": map[string]any{},
		"steps": steps(map[string]any{"key": "upd", "type": "update_record", "config": map[string]any{"collection": "notes", "recordId": "{{.trigger.form.id}}", "data": map[string]any{"text": "{{.trigger.form.text}}"}}})})
	testutil.Create(t, app, "flows", map[string]any{"id": createFlowID, "name": "Create", "active": true, "trigger_type": "action", "trigger": map[string]any{},
		"steps": steps(map[string]any{"key": "new", "type": "create_record", "config": map[string]any{"collection": "notes", "data": map[string]any{"text": "{{.trigger.form.text}}", "owner": "{{.trigger.form.owner}}"}}})})
	testutil.Create(t, app, "flows", map[string]any{"id": readFlowID, "name": "Read", "active": true, "trigger_type": "action", "trigger": map[string]any{},
		"steps": []map[string]any{
			{"key": "list", "type": "get_records", "config": map[string]any{"collection": "notes", "limit": 10}},
			{"key": "go", "type": "redirect", "config": map[string]any{"message": "{{.steps.list.count}} notes"}},
		}})
	return app
}

func TestActionAPIRules(t *testing.T) {
	ownerOf := func(app core.App, email string) string {
		u, _ := app.FindAuthRecordByEmail("users", email)
		return u.Id
	}
	cases := []struct {
		name, flow, user, body string
		expect, notExpect      []string
		check                  func(t testing.TB, app core.App)
	}{
		{name: "owner updates", flow: notesFlowID, user: "ada@test.com", body: `{"id":"` + adaNoteID + `","text":"changed"}`, expect: []string{`"status":"success"`},
			check: func(t testing.TB, app core.App) {
				if n, _ := app.FindRecordById("notes", adaNoteID); n.GetString("text") != "changed" {
					t.Error("expected the change")
				}
			}},
		{name: "another user can't update", flow: notesFlowID, user: "bob@test.com", body: `{"id":"` + adaNoteID + `","text":"hacked"}`, expect: []string{`"status":"rejected"`, "don't have access"},
			check: func(t testing.TB, app core.App) {
				if n, _ := app.FindRecordById("notes", adaNoteID); n.GetString("text") != "first" {
					t.Error("the note must not change")
				}
			}},
		{name: "guest can't create", flow: createFlowID, body: `{"text":"x","owner":""}`, expect: []string{`"status":"rejected"`}},
		{name: "create for someone else is undone", flow: createFlowID, user: "bob@test.com", body: `{"text":"x","owner":"OTHER"}`, expect: []string{`"status":"rejected"`},
			check: func(t testing.TB, app core.App) {
				if n, _ := app.CountRecords("notes"); n != 1 {
					t.Errorf("expected the create to be undone, got %d notes", n)
				}
			}},
		{name: "create own note", flow: createFlowID, user: "bob@test.com", body: `{"text":"mine","owner":"BOB"}`, expect: []string{`"status":"success"`},
			check: func(t testing.TB, app core.App) {
				if n, _ := app.CountRecords("notes"); n != 2 {
					t.Errorf("expected the new note, got %d", n)
				}
			}},
		{name: "staff lists", flow: readFlowID, user: "ada@test.com", body: `{}`, expect: []string{`"message":"1 notes"`}},
		{name: "others list nothing", flow: readFlowID, user: "bob@test.com", body: `{}`, expect: []string{`"message":"0 notes"`, `"status":"success"`}, notExpect: []string{"output"}},
	}
	for _, c := range cases {
		headers := map[string]string{"Content-Type": "application/json", "Accept": "application/json"}
		s := &tests.ApiScenario{
			Name: c.name, Method: http.MethodPost, URL: "/api/flows/action/" + c.flow, Headers: headers,
			TestAppFactory: notesApp, ExpectedStatus: 200, ExpectedContent: c.expect, NotExpectedContent: c.notExpect,
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if c.check != nil {
					c.check(t, app)
				}
			},
		}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			if c.user != "" {
				headers["Cookie"] = sessionCookie(t, app, c.user)
			}
			s.Body = strings.NewReader(strings.ReplaceAll(c.body, `"BOB"`, `"`+ownerOf(app, "bob@test.com")+`"`))
		}
		s.Test(t)
	}
}

func TestActionHTTPLimit(t *testing.T) {
	t.Cleanup(flows.SetActionHTTPLimit(2))
	t.Setenv(flows.AllowPrivateHTTPEnv, "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	const id = "flowhttp0000001"
	factory := func(t testing.TB) *tests.TestApp {
		f := setup(t)
		testutil.Create(t, f.app, "flows", map[string]any{"id": id, "name": "x", "active": true, "trigger_type": "action", "trigger": map[string]any{},
			"steps": []map[string]any{{"key": "call", "type": "http_request", "config": map[string]any{"method": "GET", "url": srv.URL}}}})
		return f.app
	}
	for i, want := range []string{`"status":"success"`, `"status":"success"`, "Too many requests"} {
		(&tests.ApiScenario{
			Name: fmt.Sprint("request ", i), Method: http.MethodPost, URL: "/api/flows/action/" + id, Body: strings.NewReader(`{}`),
			Headers:         map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
			TestAppFactory:  factory,
			ExpectedStatus:  200,
			ExpectedContent: []string{want},
		}).Test(t)
	}
}
