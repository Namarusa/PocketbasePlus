package apisources_test

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/apisources"
	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/httpcall"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// echo is a test API: it records the last request and answers with the
// body and content type set by the test.
type echo struct {
	sync.Mutex
	srv         *httptest.Server
	last        *http.Request
	lastBody    string
	contentType string
	response    string
	status      int
}

func newEcho(t testing.TB) *echo {
	t.Setenv(httpcall.AllowPrivateEnv, "1")
	e := &echo{status: 200, contentType: "application/json", response: `{"ok":true}`}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		e.Lock()
		e.last, e.lastBody = r, string(b)
		ct, res, status := e.contentType, e.response, e.status
		e.Unlock()
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		w.Write([]byte(res))
	}))
	t.Cleanup(e.srv.Close)
	return e
}

type fixture struct {
	app    *tests.TestApp
	engine *flows.Engine
	api    *echo
	source *core.Record
	admin  *core.Record
}

func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	m := mail.Register(app)
	m.DisableKick = true
	f := &fixture{app: app, engine: flows.Register(app, m), api: newEcho(t)}
	f.engine.Sync = true
	t.Cleanup(func() { f.engine.Wait(time.Minute) })
	apisources.Bind(app)
	f.source = testutil.Create(t, app, apisources.SourcesCollection, map[string]any{
		"name": "Shop", "base_url": f.api.srv.URL + "/v1/", "auth": "bearer", "secret": "s3cr3t-token-value",
		"headers": map[string]any{"Accept": "application/json"},
	})
	f.admin = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	return f
}

func (f *fixture) call(t testing.TB, data map[string]any) *core.Record {
	data["source"] = f.source.Id
	return testutil.Create(t, f.app, apisources.CallsCollection, data)
}

func TestCallRequest(t *testing.T) {
	f := setup(t)
	call := f.call(t, map[string]any{
		"name": "Update product", "method": "POST", "path": "/products/{{.vars.id}}",
		"query":     map[string]any{"lang": "{{.vars.lang}}", "fixed": "1"},
		"headers":   map[string]any{"X-Trace": "{{.vars.id}}", "authorization": "replaced"},
		"body_type": "json", "body": `{"name":"{{.vars.name}}","tags":{{.vars.tags}}}`,
		"variables": []map[string]any{{"name": "id", "required": true}, {"name": "lang"}, {"name": "name"}, {"name": "tags"}},
		"outputs":   []map[string]any{{"name": "ok", "path": "ok"}},
	})
	input, out, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{
		"id": "a/../b&c", "name": `x","admin":true`, "tags": []any{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.api.last
	if r.URL.EscapedPath() != "/v1/products/a%2F..%2Fb&c" {
		t.Errorf("path: %s", r.URL.EscapedPath())
	}
	if r.URL.Query().Get("fixed") != "1" || r.URL.Query().Has("lang") {
		t.Errorf("query: %s (an empty optional parameter is left out)", r.URL.RawQuery)
	}
	if r.Header.Get("Authorization") != "Bearer s3cr3t-token-value" || r.Header.Get("Accept") != "application/json" || r.Header.Get("X-Trace") != "a/../b&c" {
		t.Errorf("headers: %v", r.Header)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.api.lastBody), &body); err != nil || body["name"] != `x","admin":true` || body["admin"] != nil || len(body["tags"].([]any)) != 2 {
		t.Errorf("body: %s", f.api.lastBody)
	}
	if out["data"].(map[string]any)["ok"] != true || out["format"] != "json" {
		t.Errorf("output: %v", out)
	}
	logged, _ := json.Marshal(input)
	if strings.Contains(string(logged), "s3cr3t") {
		t.Errorf("the secret is in the logged input: %s", logged)
	}

	// required variables
	if _, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{"id": " "}); err == nil || !strings.Contains(err.Error(), `"id" is required`) {
		t.Errorf("expected a required variable error, got %v", err)
	}
}

func TestSignInMethods(t *testing.T) {
	f := setup(t)
	call := f.call(t, map[string]any{"name": "Ping", "path": "ping"})
	cases := []struct {
		auth, name, user string
		check            func(r *http.Request) bool
	}{
		{"none", "", "", func(r *http.Request) bool { return r.Header.Get("Authorization") == "" }},
		{"basic", "", "bob", func(r *http.Request) bool {
			u, p, ok := r.BasicAuth()
			return ok && u == "bob" && p == "s3cr3t-token-value"
		}},
		{"header", "X-API-Key", "", func(r *http.Request) bool { return r.Header.Get("X-API-Key") == "s3cr3t-token-value" }},
		{"query", "api_key", "", func(r *http.Request) bool { return r.URL.Query().Get("api_key") == "s3cr3t-token-value" }},
	}
	for _, c := range cases {
		f.source.Set("auth", c.auth)
		f.source.Set("auth_name", c.name)
		f.source.Set("username", c.user)
		if err := f.app.Save(f.source); err != nil {
			t.Fatal(err)
		}
		input, _, err := apisources.Call(context.Background(), f.app, call.Id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !c.check(f.api.last) {
			t.Errorf("%s: unexpected request %s %v", c.auth, f.api.last.URL, f.api.last.Header)
		}
		if logged, _ := json.Marshal(input); strings.Contains(string(logged), "s3cr3t") {
			t.Errorf("%s: the secret is in the logged input: %s", c.auth, logged)
		}
	}

	// a header or parameter name is required
	f.source.Set("auth", "header")
	f.source.Set("auth_name", "")
	if err := f.app.Save(f.source); err == nil {
		t.Error("expected an error without the header name")
	}
}

func TestBodies(t *testing.T) {
	f := setup(t)
	vars := []map[string]any{{"name": "v"}}
	xmlCall := f.call(t, map[string]any{"name": "X", "method": "POST", "body_type": "xml", "body": `<a b="{{.vars.v}}">{{.vars.v}}</a>`, "variables": vars})
	formCall := f.call(t, map[string]any{"name": "F", "method": "POST", "body_type": "form", "form": map[string]any{"q": "{{.vars.v}}"}, "variables": vars})
	textCall := f.call(t, map[string]any{"name": "T", "method": "POST", "body_type": "text", "body": "hello {{.vars.v}}", "variables": vars})

	value := `x"<y>&z`
	if _, _, err := apisources.Call(context.Background(), f.app, xmlCall.Id, map[string]any{"v": value}); err != nil {
		t.Fatal(err)
	}
	if want := `<a b="x&#34;&lt;y&gt;&amp;z">x&#34;&lt;y&gt;&amp;z</a>`; f.api.lastBody != want || f.api.last.Header.Get("Content-Type") != "application/xml" {
		t.Errorf("xml body: %s", f.api.lastBody)
	}
	if html.UnescapeString(f.api.lastBody) != `<a b="`+value+`">`+value+`</a>` {
		t.Errorf("xml value changed: %s", f.api.lastBody)
	}
	apisources.Call(context.Background(), f.app, formCall.Id, map[string]any{"v": "a&b=c"})
	if f.api.lastBody != "q=a%26b%3Dc" || f.api.last.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("form body: %s", f.api.lastBody)
	}
	apisources.Call(context.Background(), f.app, textCall.Id, map[string]any{"v": "you"})
	if f.api.lastBody != "hello you" {
		t.Errorf("text body: %s", f.api.lastBody)
	}
}

func TestValidation(t *testing.T) {
	f := setup(t)
	col, _ := f.app.FindCollectionByNameOrId(apisources.CallsCollection)
	bad := []map[string]any{
		{"name": "a", "path": "/x/{{.vars.missing}}"},
		{"name": "b", "path": "/x/{{.vars.id"},
		{"name": "c", "variables": []map[string]any{{"name": "a b"}}},
		{"name": "d", "outputs": []map[string]any{{"name": "x", "path": ""}}},
		{"name": "e", "headers": map[string]any{"Bad Header": "x"}},
	}
	for _, data := range bad {
		r := core.NewRecord(col)
		data["source"] = f.source.Id
		r.Load(data)
		if err := f.app.Save(r); err == nil {
			t.Errorf("expected %v to be refused", data)
		}
	}
	src, _ := f.app.FindCollectionByNameOrId(apisources.SourcesCollection)
	r := core.NewRecord(src)
	r.Load(map[string]any{"name": "Bad", "base_url": "ftp://x"})
	if err := f.app.Save(r); err == nil {
		t.Error("expected a non http base url to be refused")
	}
}

func TestAPICallStep(t *testing.T) {
	f := setup(t)
	f.api.contentType = "application/rss+xml"
	f.api.response = `<rss version="2.0"><channel><title>News</title><item><title>One</title></item><item><title>Two</title></item></channel></rss>`
	call := f.call(t, map[string]any{
		"name": "Feed", "path": "feed/{{.vars.topic}}", "variables": []map[string]any{{"name": "topic", "required": true}},
		"outputs": []map[string]any{{"name": "title", "path": "rss.channel.title"}, {"name": "items", "path": "rss.channel.item"}},
	})
	flow := testutil.Create(t, f.app, "flows", map[string]any{
		"name": "feed", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{
			{"key": "feed", "type": "api_call", "config": map[string]any{"call": call.Id, "vars": map[string]any{"topic": "{{.trigger.payload.topic}}"}}},
			{"key": "check", "type": "condition", "config": map[string]any{"match": "all", "rules": []map[string]any{{"left": "{{.steps.feed.data.title}}", "op": "eq", "right": "News"}}},
				"then": []map[string]any{{"key": "each", "type": "loop", "config": map[string]any{"items": "{{.steps.feed.data.items}}"},
					"steps": []map[string]any{{"key": "line", "type": "log", "config": map[string]any{"message": "{{.item.title}}"}}}}}},
			// XML attributes and list positions, as the variable picker inserts them
			{"key": "attr", "type": "log", "config": map[string]any{"message": `v{{index .steps.feed.body.rss "@version"}} {{index .steps.feed.body.rss "channel" "item" 1 "title"}}`}},
		},
	})
	res := f.engine.Run(flow, map[string]any{"payload": map[string]any{"topic": "go lang"}}, flows.RunOptions{})
	if res.Status != "success" {
		t.Fatalf("run: %s %s", res.Status, res.Error)
	}
	if f.api.last.URL.EscapedPath() != "/v1/feed/go%20lang" {
		t.Errorf("path: %s", f.api.last.URL.EscapedPath())
	}
	runs, _ := f.app.FindRecordsByFilter("flow_runs", "flow = {:f}", "", 0, 0, map[string]any{"f": flow.Id})
	if len(runs) != 1 {
		t.Fatalf("expected one run, got %d", len(runs))
	}
	logs := runs[0].GetString("steps")
	if !strings.Contains(logs, `"message":"Two"`) {
		t.Errorf("expected the loop over the items: %s", logs)
	}
	if !strings.Contains(logs, `"message":"v2.0 Two"`) {
		t.Errorf("expected the attribute and the second item: %s", logs)
	}
	if strings.Contains(logs, "s3cr3t") {
		t.Error("the secret is in the run log")
	}

	// an error status fails the step, unless asked not to
	f.api.status = 500
	if res := f.engine.Run(flow, map[string]any{"payload": map[string]any{"topic": "x"}}, flows.RunOptions{}); res.Status != "failed" || !strings.Contains(res.Error, "status 500") {
		t.Errorf("expected a failure, got %s %s", res.Status, res.Error)
	}

	// unknown variables and calls are refused when the flow is saved
	flow.Set("steps", []map[string]any{{"key": "feed", "type": "api_call", "config": map[string]any{"call": call.Id, "vars": map[string]any{"nope": "1"}}}})
	if err := f.app.Save(flow); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("expected the unknown variable to be refused, got %v", err)
	}
	flow.Set("steps", []map[string]any{{"key": "feed", "type": "api_call", "config": map[string]any{"call": "missing"}}})
	if err := f.app.Save(flow); err == nil {
		t.Error("expected the unknown call to be refused")
	}

	// a call used by a flow can't be deleted
	if err := f.app.Delete(call); err == nil || !strings.Contains(err.Error(), "feed") {
		t.Errorf("expected the deletion to be refused, got %v", err)
	}
	// nor an API with calls
	if err := f.app.Delete(f.source); err == nil {
		t.Error("expected the API deletion to be refused")
	}
}

func TestRoutes(t *testing.T) {
	var f *fixture
	var callID string
	factory := func(t testing.TB) *tests.TestApp {
		f = setup(t)
		f.api.response = `{"items":[{"id":"p1","name":"Pen"}],"total":1}`
		callID = f.call(t, map[string]any{"name": "List", "path": "items"}).Id
		return f.app
	}
	headers := map[string]string{}
	before := func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		headers["Authorization"] = testutil.AuthToken(t, f.admin)
	}

	body := func() io.Reader {
		raw, _ := json.Marshal(map[string]any{
			"id": callID, "source": f.source.Id,
			"call": map[string]any{"name": "List", "path": "items/{{.vars.q}}", "variables": []map[string]any{{"name": "q", "test": "a b"}},
				"outputs": []map[string]any{{"name": "first", "path": "items.0.name"}}},
		})
		return strings.NewReader(string(raw))
	}
	s := &tests.ApiScenario{Name: "test a draft", Method: http.MethodPost, URL: "/api/apisources/test", Headers: headers, TestAppFactory: factory,
		ExpectedStatus: 200, ExpectedContent: []string{`"status":200`, `"first":"Pen"`, `"format":"json"`, `items/a%20b`}, NotExpectedContent: []string{"s3cr3t"},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			saved, _ := app.FindRecordById(apisources.CallsCollection, callID)
			if !strings.Contains(saved.GetString("sample"), "Pen") {
				t.Errorf("the sample wasn't saved: %s", saved.GetString("sample"))
			}
			if saved.GetString("path") != "items" {
				t.Error("the draft was saved")
			}
		}}
	s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		before(t, app, e)
		s.Body = body()
		headers["Content-Type"] = "application/json"
	}
	s.Test(t)

	(&tests.ApiScenario{Name: "superusers only", Method: http.MethodPost, URL: "/api/apisources/test", Body: strings.NewReader(`{}`),
		TestAppFactory: factory, ExpectedStatus: 401, ExpectedContent: []string{`"data":{}`}}).Test(t)

	(&tests.ApiScenario{Name: "the secret isn't returned", Method: http.MethodGet, URL: "/api/collections/api_sources/records", Headers: headers,
		TestAppFactory: factory, BeforeTestFunc: before, ExpectedStatus: 200,
		ExpectedContent: []string{`"secret_hint":"…alue"`}, NotExpectedContent: []string{"s3cr3t", `"secret":`}}).Test(t)

	(&tests.ApiScenario{Name: "calls for the flow editor", Method: http.MethodGet, URL: "/api/apisources/calls", Headers: headers,
		TestAppFactory: factory, BeforeTestFunc: before, ExpectedStatus: 200,
		ExpectedContent: []string{`"sourceName":"Shop"`, `"name":"List"`}}).Test(t)
}

func TestAPIVariables(t *testing.T) {
	f := setup(t)
	host := strings.TrimPrefix(f.api.srv.URL, "http://")
	src := testutil.Create(t, f.app, apisources.SourcesCollection, map[string]any{
		"name": "Devices", "base_url": "http://{{.vars.host}}/{{.vars.version}}",
		"auth": "header", "auth_name": "X-{{.vars.tenant}}-Key", "secret": "{{.vars.token}}",
		"headers":   map[string]any{"X-Tenant": "{{.vars.tenant}}"},
		"variables": []map[string]any{{"name": "host", "required": true}, {"name": "version", "test": "v2"}, {"name": "tenant"}, {"name": "token"}},
	})
	call := testutil.Create(t, f.app, apisources.CallsCollection, map[string]any{
		"name": "Posts", "source": src.Id, "path": "posts/{{.vars.id}}", "variables": []map[string]any{{"name": "id"}},
	})
	input, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{
		"host": host, "version": "v2", "tenant": "acme", "token": "tok-987654321", "id": "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.api.last
	if r.URL.Path != "/v2/posts/7" || r.Header.Get("X-acme-Key") != "tok-987654321" || r.Header.Get("X-Tenant") != "acme" {
		t.Errorf("request: %s %v", r.URL, r.Header)
	}
	logged, _ := json.Marshal(input)
	if strings.Contains(string(logged), "tok-987654321") || !strings.Contains(string(logged), `"token":"[hidden]"`) {
		t.Errorf("the token variable must be hidden: %s", logged)
	}
	// a value of the address can't change its path
	f.api.last = nil
	apisources.Call(context.Background(), f.app, call.Id, map[string]any{"host": host, "version": "a/../b", "id": "7"})
	if f.api.last == nil || f.api.last.URL.EscapedPath() != "/a%2F..%2Fb/posts/7" {
		t.Errorf("escaped path: %v", f.api.last)
	}
	// the API's required variables
	if _, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{"id": "7"}); err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("expected the host to be required, got %v", err)
	}

	// a flow step sets the API's variables too
	flow := testutil.Create(t, f.app, "flows", map[string]any{
		"name": "devices", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{{"key": "posts", "type": "api_call", "config": map[string]any{"call": call.Id,
			"vars": map[string]any{"host": host, "version": "v1", "tenant": "t", "token": "secret-from-flow", "id": "1"}}}},
	})
	if res := f.engine.Run(flow, map[string]any{}, flows.RunOptions{}); res.Status != "success" {
		t.Fatalf("run: %s %s", res.Status, res.Error)
	}
	runs, _ := f.app.FindRecordsByFilter("flow_runs", "flow = {:f}", "", 0, 0, map[string]any{"f": flow.Id})
	if logs := runs[0].GetString("steps"); strings.Contains(logs, "secret-from-flow") {
		t.Errorf("the token is in the run log: %s", logs)
	}

	// names: unknown variables, and a call variable named like an API one
	bad := testutil.Create(t, f.app, apisources.SourcesCollection, map[string]any{"name": "Bad"})
	bad.Set("base_url", "https://{{.vars.nope}}")
	if err := f.app.Save(bad); err == nil {
		t.Error("expected an undeclared variable to be refused")
	}
	col, _ := f.app.FindCollectionByNameOrId(apisources.CallsCollection)
	dup := core.NewRecord(col)
	dup.Load(map[string]any{"name": "Dup", "source": src.Id, "variables": []map[string]any{{"name": "host"}}})
	if err := f.app.Save(dup); err == nil {
		t.Error("expected a call variable named like an API one to be refused")
	}
	src.Set("variables", []map[string]any{{"name": "host"}, {"name": "version"}, {"name": "tenant"}, {"name": "token"}, {"name": "id"}})
	if err := f.app.Save(src); err == nil {
		t.Error("expected an API variable named like a call one to be refused")
	}
}
