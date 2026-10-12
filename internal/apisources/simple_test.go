package apisources_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/apisources"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// {{name}} works like {{.vars.name}}, with the same escaping for its place
func TestShortVariables(t *testing.T) {
	f := setup(t)
	call := f.call(t, map[string]any{
		"name": "Update product", "method": "POST", "path": "/products/{{id}}",
		"query":     map[string]any{"lang": "{{ lang }}", "q": "{{upper name}}"},
		"headers":   map[string]any{"X-Trace": "{{id}}"},
		"body_type": "json", "body": `{"name":"{{name}}","tags":{{tags}},"count":{{len tags}},"title":"{{.vars.title}}"}`,
		// "title" is also a function: it keeps .vars
		"variables": []map[string]any{{"name": "id", "required": true}, {"name": "lang", "default": "fr"}, {"name": "name"}, {"name": "tags"}, {"name": "title"}},
	})
	_, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{
		"id": "a/../b&c", "name": `x","admin":true`, "tags": []any{"a", "b"}, "title": "T",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.api.last
	if r.URL.EscapedPath() != "/v1/products/a%2F..%2Fb&c" {
		t.Errorf("path: %s", r.URL.EscapedPath())
	}
	if r.URL.Query().Get("lang") != "fr" || r.URL.Query().Get("q") != `X","ADMIN":TRUE` {
		t.Errorf("query (the default of lang): %s", r.URL.RawQuery)
	}
	if r.Header.Get("X-Trace") != "a/../b&c" {
		t.Errorf("header: %v", r.Header)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.api.lastBody), &body); err != nil || body["name"] != `x","admin":true` || body["admin"] != nil || len(body["tags"].([]any)) != 2 || body["count"] != 2.0 || body["title"] != "T" {
		t.Errorf("body: %s (%v)", f.api.lastBody, err)
	}

	// a default doesn't replace a given value; a required variable without
	// value nor default still fails
	if _, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{"id": "1", "lang": "en", "tags": []any{}}); err != nil || f.api.last.URL.Query().Get("lang") != "en" {
		t.Errorf("given value: %v %s", err, f.api.last.URL.RawQuery)
	}
	if _, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{}); err == nil || !strings.Contains(err.Error(), `"id" is required`) {
		t.Errorf("required: %v", err)
	}

	// an undeclared {{name}} is reported as a missing variable
	_, err = f.app.FindRecordById(apisources.CallsCollection, call.Id)
	if err != nil {
		t.Fatal(err)
	}
	bad := core.NewRecord(call.Collection())
	bad.Load(map[string]any{"source": f.source.Id, "name": "Bad", "path": "items/{{missing}}"})
	if err := f.app.Save(bad); err == nil || !strings.Contains(err.Error(), `Add the variable "missing"`) {
		t.Errorf("undeclared: %v", err)
	}
}

func TestSignInWithShortVariables(t *testing.T) {
	f := setup(t)
	f.source.Set("auth", "header")
	f.source.Set("auth_name", "X-Key")
	f.source.Set("secret", "{{token}}")
	f.source.Set("variables", []map[string]any{{"name": "token"}})
	if err := f.app.Save(f.source); err != nil {
		t.Fatal(err)
	}
	call := f.call(t, map[string]any{"name": "Ping", "path": "ping"})
	input, _, err := apisources.Call(context.Background(), f.app, call.Id, map[string]any{"token": "tok-123456"})
	if err != nil || f.api.last.Header.Get("X-Key") != "tok-123456" {
		t.Fatalf("%v %v", err, f.api.last.Header)
	}
	if logged, _ := json.Marshal(input); strings.Contains(string(logged), "tok-123456") {
		t.Errorf("the sign-in variable must be hidden: %s", logged)
	}
}

func TestDryRunAndHints(t *testing.T) {
	var f *fixture
	factory := func(t testing.TB) *tests.TestApp {
		f = setup(t)
		f.source.Set("auth", "query")
		f.source.Set("auth_name", "key")
		if err := f.app.Save(f.source); err != nil {
			t.Fatal(err)
		}
		return f.app
	}
	headers := map[string]string{"Content-Type": "application/json"}
	post := func(name string, call map[string]any, extra map[string]any, expected []string, notExpected []string) {
		s := &tests.ApiScenario{Name: name, Method: http.MethodPost, URL: "/api/apisources/test", Headers: headers, TestAppFactory: factory,
			ExpectedStatus: 200, ExpectedContent: expected, NotExpectedContent: notExpected}
		s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			headers["Authorization"] = testutil.AuthToken(t, f.admin)
			body := map[string]any{"source": f.source.Id, "call": call}
			for k, v := range extra {
				body[k] = v
			}
			raw, _ := json.Marshal(body)
			s.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		s.Test(t)
	}
	call := map[string]any{"name": "Cards", "path": "cards", "query": map[string]any{"name": "{{name}}"}, "variables": []map[string]any{{"name": "name", "default": "pika chu"}}}
	post("dry run: the final address with the defaults, the key hidden", call, map[string]any{"dryRun": true},
		[]string{`"url":"`, `/v1/cards?key=%5Bhidden%5D&name=pika+chu`}, []string{"s3cr3t", `"status"`})
	post("dry run of an invalid call: its error", map[string]any{"name": "Bad", "path": "{{nope}}"}, map[string]any{"dryRun": true},
		[]string{`Add the variable \"nope\"`}, nil)
	post("a 401 explains the sign-in", map[string]any{"name": "Cards", "path": "unauthorized"}, nil,
		[]string{`"hint":"The API refuses the sign-in`}, nil)
}
