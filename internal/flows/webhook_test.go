package flows_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// webhookApp has the users of actionApp and a webhook flow per case.
func webhookApp(flows ...map[string]any) func(t testing.TB) *tests.TestApp {
	return func(t testing.TB) *tests.TestApp {
		app := actionApp(t)
		for _, f := range flows {
			data := map[string]any{"active": true, "trigger_type": "webhook"}
			for k, v := range f {
				data[k] = v
			}
			testutil.Create(t, app, "flows", data)
		}
		return app
	}
}

func userToken(t testing.TB, app core.App, email string) string {
	u, err := app.FindAuthRecordByEmail("users", email)
	if err != nil {
		t.Fatal(err)
	}
	return testutil.AuthToken(t, u)
}

var respondKeys int

func respondStep(config map[string]any) map[string]any {
	respondKeys++
	return map[string]any{"key": "reply" + strconv.Itoa(respondKeys), "type": "respond", "config": config}
}

func TestWebhookAccess(t *testing.T) {
	echo := respondStep(map[string]any{"status": "201", "headers": map[string]any{"X-Flow": "yes"}, "body": `{"who":"{{.trigger.user.email}}","got":{{json .trigger.body}}}`})
	factory := webhookApp(
		map[string]any{"id": "hookpublic00001", "name": "Public", "trigger": map[string]any{"access": "public", "wait": true, "slug": "new-order"}, "steps": []map[string]any{echo}},
		map[string]any{"id": "hookusers000001", "name": "Users", "trigger": map[string]any{"access": "users", "wait": true}, "steps": []map[string]any{echo}},
		map[string]any{"id": "hookroles000001", "name": "Staff", "trigger": map[string]any{"access": "roles", "roles": []string{staffRoleID}, "wait": true}, "steps": []map[string]any{echo}},
		map[string]any{"id": "hookrules000001", "name": "Rules", "trigger": map[string]any{"access": "public", "wait": true}, "steps": []map[string]any{
			{"key": "add", "type": "create_record", "config": map[string]any{"collection": "audit", "data": map[string]any{"message": "x"}}},
		}},
	)
	type c struct {
		name, url string
		headers   func(app core.App, t testing.TB) map[string]string
		status    int
		want      []string
	}
	with := func(extra map[string]string) func(core.App, testing.TB) map[string]string {
		return func(core.App, testing.TB) map[string]string {
			h := map[string]string{"Content-Type": "application/json"}
			for k, v := range extra {
				h[k] = v
			}
			return h
		}
	}
	bearer := func(email string) func(core.App, testing.TB) map[string]string {
		return func(app core.App, t testing.TB) map[string]string {
			return map[string]string{"Content-Type": "application/json", "Authorization": userToken(t, app, email)}
		}
	}
	session := func(email, site string) func(core.App, testing.TB) map[string]string {
		return func(app core.App, t testing.TB) map[string]string {
			return map[string]string{"Content-Type": "application/json", "Cookie": sessionCookie(t, app, email), "Sec-Fetch-Site": site}
		}
	}
	for _, tc := range []c{
		{"public", "/api/flows/webhook/hookpublic00001", with(nil), 201, []string{`"who":""`, `"got":{"n":1}`}},
		{"public by its address", "/hooks/new-order", with(nil), 201, []string{`"got":{"n":1}`}},
		{"users: a guest", "/api/flows/webhook/hookusers000001", with(nil), 401, []string{"Sign in"}},
		{"users: a token", "/api/flows/webhook/hookusers000001", bearer("bob@test.com"), 201, []string{`"who":"bob@test.com"`}},
		{"users: the session of the site", "/api/flows/webhook/hookusers000001", session("bob@test.com", "same-origin"), 201, []string{`"who":"bob@test.com"`}},
		{"users: the session from another site", "/api/flows/webhook/hookusers000001", session("bob@test.com", "cross-site"), 401, nil},
		{"roles: without the role", "/api/flows/webhook/hookroles000001", bearer("bob@test.com"), 403, nil},
		{"roles: with the role", "/api/flows/webhook/hookroles000001", bearer("ada@test.com"), 201, []string{`"who":"ada@test.com"`}},
		{"the caller's API rules", "/api/flows/webhook/hookrules000001", with(nil), 400, []string{"access"}},
		{"an unknown address", "/hooks/nope", with(nil), 404, nil},
	} {
		s := &tests.ApiScenario{Name: tc.name, Method: http.MethodPost, URL: tc.url, Body: strings.NewReader(`{"n":1}`),
			TestAppFactory: factory, ExpectedStatus: tc.status, ExpectedContent: tc.want}
		if tc.want == nil {
			s.ExpectedContent = []string{"{"}
		}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			s.Headers = tc.headers(app, t)
		}
		if tc.status == 201 {
			s.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) {
				if res.Header.Get("X-Flow") != "yes" {
					t.Errorf("header: %v", res.Header)
				}
			}
		}
		s.Test(t)
	}
}

func TestWebhookResponses(t *testing.T) {
	factory := webhookApp(
		map[string]any{"id": "hookhtml0000001", "name": "Html", "trigger": map[string]any{"access": "public", "wait": true}, "steps": []map[string]any{
			respondStep(map[string]any{"format": "json", "body": `{"first":true}`}),
			respondStep(map[string]any{"format": "html", "body": `<h1>Hello {{.trigger.query.name}}</h1>`}), // the last one wins
		}},
		map[string]any{"id": "hookredir000001", "name": "Redirect", "trigger": map[string]any{"access": "public", "wait": true}, "steps": []map[string]any{
			respondStep(map[string]any{"redirect": "/thanks?id={{.trigger.query.id}}"}),
		}},
		map[string]any{"id": "hookcookie00001", "name": "Cookie", "trigger": map[string]any{"access": "public", "wait": true}, "steps": []map[string]any{
			respondStep(map[string]any{"headers": map[string]any{"Set-Cookie": "a=b"}}),
		}},
		map[string]any{"id": "hooktoken000001", "name": "Token", "trigger": map[string]any{"token": "tok123", "wait": true}, "steps": []map[string]any{
			respondStep(map[string]any{"format": "text", "status": "202", "body": "ok"}),
		}},
	)
	(&tests.ApiScenario{Name: "html", Method: http.MethodGet, URL: "/api/flows/webhook/hookhtml0000001?name=<b>Ada</b>", TestAppFactory: factory,
		ExpectedStatus: 200, ExpectedContent: []string{"<h1>Hello &lt;b&gt;Ada&lt;/b&gt;</h1>"},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.Header.Get("Content-Security-Policy") != "sandbox" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
				t.Errorf("headers: %v", res.Header)
			}
		}}).Test(t)
	(&tests.ApiScenario{Name: "redirect", Method: http.MethodGet, URL: "/api/flows/webhook/hookredir000001?id=7", TestAppFactory: factory,
		ExpectedStatus: 303,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.Header.Get("Location") != "/thanks?id=7" {
				t.Errorf("location: %q", res.Header.Get("Location"))
			}
		}}).Test(t)
	(&tests.ApiScenario{Name: "no cookies", Method: http.MethodGet, URL: "/api/flows/webhook/hookcookie00001", TestAppFactory: factory,
		ExpectedStatus: 500, ExpectedContent: []string{"The flow failed"},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if res.Header.Get("Set-Cookie") != "" {
				t.Error("a cookie was set")
			}
		}}).Test(t)
	(&tests.ApiScenario{Name: "token still needed by default", Method: http.MethodGet, URL: "/api/flows/webhook/hooktoken000001", TestAppFactory: factory,
		ExpectedStatus: 401, ExpectedContent: []string{"Invalid flow token"}}).Test(t)
	(&tests.ApiScenario{Name: "text with the token", Method: http.MethodGet, URL: "/api/flows/webhook/hooktoken000001?token=tok123", TestAppFactory: factory,
		ExpectedStatus: 202, ExpectedContent: []string{"ok"}}).Test(t)
}

func TestWebhookValidation(t *testing.T) {
	f := setup(t)
	save := func(id, trigger string, cfg map[string]any, steps []map[string]any) error {
		col, _ := f.app.FindCollectionByNameOrId("flows")
		r := core.NewRecord(col)
		r.Load(map[string]any{"id": id, "name": id, "active": true, "trigger_type": trigger, "trigger": cfg, "steps": steps})
		return f.app.Save(r)
	}
	if err := save("hookaddress0001", "webhook", map[string]any{"slug": "New-Order"}, nil); err != nil {
		t.Fatal(err)
	}
	saved, _ := f.app.FindRecordById("flows", "hookaddress0001")
	if !strings.Contains(saved.GetString("trigger"), `"slug":"new-order"`) || !strings.Contains(saved.GetString("trigger"), `"access":"token"`) {
		t.Errorf("trigger: %s", saved.GetString("trigger"))
	}
	if err := save("hookaddress0002", "webhook", map[string]any{"slug": "new-order"}, nil); err == nil || !strings.Contains(err.Error(), "used by the flow") {
		t.Errorf("a taken address: %v", err)
	}
	if err := save("hookaddress0003", "webhook", map[string]any{"slug": "bad slug!"}, nil); err == nil {
		t.Error("expected an invalid address error")
	}
	if err := save("hookaddress0004", "webhook", map[string]any{"access": "roles"}, nil); err == nil {
		t.Error("expected a roles error")
	}
	if err := save("hookaddress0005", "manual", map[string]any{}, []map[string]any{respondStep(map[string]any{})}); err == nil || !strings.Contains(err.Error(), "Webhook") {
		t.Errorf("respond outside a webhook: %v", err)
	}
}
