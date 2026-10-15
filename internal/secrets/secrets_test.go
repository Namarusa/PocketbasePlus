package secrets_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/secrets"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// the encryption key variable of the test apps
const keyEnv = "pb_test_env"

const testKey = "0123456789abcdef0123456789abcdef"

func newApp(t testing.TB) *tests.TestApp {
	app := testutil.NewApp(t)
	secrets.Register(app)
	return app
}

func TestRefs(t *testing.T) {
	src := map[string]any{
		"a": "Bearer {{secret.B_KEY}}",
		"b": []any{`{{printf "%s:%s" secret.A_KEY .secret.B_KEY}}`, "{{ $.secret.C }}"},
		"c": "{{.vars.secret.X}} secret.NOT {{upper secretary.Y}}",
	}
	if got := secrets.Refs(src); !slices.Equal(got, []string{"A_KEY", "B_KEY", "C"}) {
		t.Errorf("refs: %v", got)
	}
}

func TestMask(t *testing.T) {
	v := "a b/c"
	got := secrets.Mask("x a b/c y a+b%2Fc z a%20b%2Fc", []string{v, "ab"})
	if strings.Contains(got, "b/c") || strings.Contains(got, "b%2F") {
		t.Errorf("mask: %s", got)
	}
	err := secrets.MaskError(errors.New("GET https://x.test/?k=a+b%2Fc failed"), []string{v})
	if strings.Contains(err.Error(), "%2F") {
		t.Errorf("error: %v", err)
	}
	out := secrets.MaskValue(map[string]any{"n": 3, "l": []any{"key a b/c"}, "h": http.Header{"X": {"a b/c"}}}, []string{v}).(map[string]any)
	if out["n"] != 3 || out["l"].([]any)[0] != "key [hidden]" || out["h"].(http.Header).Get("X") != "[hidden]" {
		t.Errorf("value: %v", out)
	}
}

func TestStorage(t *testing.T) {
	t.Setenv(keyEnv, testKey)
	app := newApp(t)
	r := testutil.Create(t, app, secrets.Collection, map[string]any{"name": " openai_key ", "value": "sk-abcdefghijklmnop1234"})
	if r.GetString("name") != "OPENAI_KEY" || !strings.HasPrefix(r.GetString("value"), "enc:v1:") || r.GetString("hint") != "sk-…1234" {
		t.Fatalf("stored: %s %s %s", r.GetString("name"), r.GetString("value"), r.GetString("hint"))
	}
	if v, err := secrets.Value(app, "OPENAI_KEY"); err != nil || v != "sk-abcdefghijklmnop1234" {
		t.Errorf("value: %q %v", v, err)
	}

	// saved without a new value: kept
	r.Set("description", "the key")
	r.Set("value", "")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	if v, _ := secrets.Value(app, "OPENAI_KEY"); v != "sk-abcdefghijklmnop1234" {
		t.Errorf("value after an edit: %q", v)
	}
	// replaced
	r.Set("value", "new-value-123456")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	if v, _ := secrets.Value(app, "OPENAI_KEY"); v != "new-value-123456" || r.GetString("hint") != "…3456" {
		t.Errorf("replaced: %q %s", v, r.GetString("hint"))
	}

	// another key
	t.Setenv(keyEnv, strings.Repeat("z", 32))
	if _, err := secrets.Value(app, "OPENAI_KEY"); err == nil || !strings.Contains(err.Error(), "can't be decrypted") {
		t.Errorf("wrong key: %v", err)
	}
	t.Setenv(keyEnv, "")
	if _, err := secrets.Value(app, "OPENAI_KEY"); err == nil || !strings.Contains(err.Error(), "isn't set") {
		t.Errorf("no key: %v", err)
	}
	if _, err := secrets.Value(app, "MISSING"); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("missing: %v", err)
	}

	// invalid names
	col, _ := app.FindCollectionByNameOrId(secrets.Collection)
	bad := core.NewRecord(col)
	bad.Load(map[string]any{"name": "1abc", "value": "x"})
	if err := app.Save(bad); err == nil {
		t.Error("expected an invalid name error")
	}
}

func TestRoutes(t *testing.T) {
	headers := map[string]string{}
	var app *tests.TestApp
	factory := func(t testing.TB) *tests.TestApp {
		app = newApp(t)
		// stored as text: no key yet
		testutil.Create(t, app, secrets.Collection, map[string]any{"name": "SHOP_TOKEN", "value": "plain-token-123456"})
		admin := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
		headers["Authorization"] = testutil.AuthToken(t, admin)
		return app
	}

	// the key is set: the server encrypts the stored values when it starts
	(&tests.ApiScenario{Name: "list", Method: http.MethodGet, URL: "/api/collections/secrets/records", Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp { t.Setenv(keyEnv, testKey); return factory(t) },
		ExpectedStatus: 200, ExpectedContent: []string{`"name":"SHOP_TOKEN"`, `"hint":"…3456"`, `"encrypted":true`},
		NotExpectedContent: []string{"plain-token", `"value"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			r, _ := app.FindFirstRecordByData(secrets.Collection, "name", "SHOP_TOKEN")
			if !strings.HasPrefix(r.GetString("value"), "enc:v1:") {
				t.Errorf("not encrypted at start: %s", r.GetString("value"))
			}
		}}).Test(t)

	(&tests.ApiScenario{Name: "search by value", Method: http.MethodGet, URL: "/api/collections/secrets/records?filter=" + "value~'plain'", Headers: headers,
		TestAppFactory: factory, ExpectedStatus: 400, ExpectedContent: []string{"can't be searched"}}).Test(t)

	(&tests.ApiScenario{Name: "guests", Method: http.MethodGet, URL: "/api/collections/secrets/records",
		TestAppFactory: factory, ExpectedStatus: 403, ExpectedContent: []string{`"data":{}`}}).Test(t)

	(&tests.ApiScenario{Name: "status", Method: http.MethodGet, URL: "/api/secrets/status", Headers: headers,
		TestAppFactory: factory, ExpectedStatus: 200, ExpectedContent: []string{`"encrypted":false`, `"env":"pb_test_env"`}}).Test(t)

	(&tests.ApiScenario{Name: "status: superusers only", Method: http.MethodGet, URL: "/api/secrets/status",
		TestAppFactory: factory, ExpectedStatus: 401, ExpectedContent: []string{`"data":{}`}}).Test(t)
}

func TestInUse(t *testing.T) {
	app := newApp(t)
	s := testutil.Create(t, app, secrets.Collection, map[string]any{"name": "SHOP_TOKEN", "value": "plain-token-123456"})
	testutil.Create(t, app, "flows", map[string]any{"name": "Sync", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{{"key": "c", "type": "http_request", "config": map[string]any{"url": "https://x.test", "headers": map[string]any{"A": "Bearer {{secret.SHOP_TOKEN}}"}}}}})
	if uses := secrets.Uses(app, "SHOP_TOKEN"); !slices.Equal(uses, []string{"the flow Sync"}) {
		t.Errorf("uses: %v", uses)
	}
	if err := app.Delete(s); err == nil || !strings.Contains(err.Error(), "the flow Sync") {
		t.Errorf("delete: %v", err)
	}
}
