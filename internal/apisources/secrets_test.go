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
	"github.com/Namarusa/PocketbasePlus/internal/secrets"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

const shopKey = "shop-key-a1b2c3d4"

func withSecret(t testing.TB, f *fixture) {
	secrets.Register(f.app)
	testutil.Create(t, f.app, secrets.Collection, map[string]any{"name": "SHOP_KEY", "value": shopKey})
	f.source.Set("auth", "header")
	f.source.Set("auth_name", "X-API-Key")
	f.source.Set("secret", "{{secret.SHOP_KEY}}")
	if err := f.app.Save(f.source); err != nil {
		t.Fatal(err)
	}
}

func TestSecretReferences(t *testing.T) {
	f := setup(t)
	withSecret(t, f)
	f.api.response = `{"echo":"` + shopKey + `"}`
	call := f.call(t, map[string]any{"name": "Ping", "path": "ping", "headers": map[string]any{"X-Other": "k={{secret.SHOP_KEY}}"}})
	input, out, err := apisources.Call(context.Background(), f.app, call.Id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.api.last.Header.Get("X-API-Key") != shopKey || f.api.last.Header.Get("X-Other") != "k="+shopKey {
		t.Errorf("request headers: %v", f.api.last.Header)
	}
	for name, v := range map[string]any{"input": input, "output": out} {
		if b, _ := json.Marshal(v); strings.Contains(string(b), shopKey) {
			t.Errorf("the %s shows the secret: %s", name, b)
		}
	}

	// an unknown secret
	bad := core.NewRecord(call.Collection())
	bad.Load(map[string]any{"name": "Bad", "source": f.source.Id, "path": "x", "headers": map[string]any{"A": "{{secret.NOPE}}"}})
	if err := f.app.Save(bad); err == nil || !strings.Contains(err.Error(), "NOPE doesn't exist") {
		t.Errorf("unknown secret: %v", err)
	}
}

func TestSecretPreviewAndHint(t *testing.T) {
	var f *fixture
	headers := map[string]string{"Content-Type": "application/json"}
	factory := func(t testing.TB) *tests.TestApp {
		f = setup(t)
		withSecret(t, f)
		return f.app
	}
	s := &tests.ApiScenario{Name: "dry run: the secret's name", Method: http.MethodPost, URL: "/api/apisources/test", Headers: headers, TestAppFactory: factory,
		ExpectedStatus: 200, ExpectedContent: []string{`[secret SHOP_KEY]`}, NotExpectedContent: []string{shopKey}}
	s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		headers["Authorization"] = testutil.AuthToken(t, f.admin)
		raw, _ := json.Marshal(map[string]any{"source": f.source.Id, "dryRun": true, "call": map[string]any{"name": "C", "path": "c", "headers": map[string]any{"X": "{{secret.SHOP_KEY}}"}}})
		s.Body = io.NopCloser(strings.NewReader(string(raw)))
	}
	s.Test(t)

	list := &tests.ApiScenario{Name: "the hint names the secret", Method: http.MethodGet, URL: "/api/collections/api_sources/records", Headers: headers,
		TestAppFactory: factory, ExpectedStatus: 200, ExpectedContent: []string{`"secret_hint":"→ SHOP_KEY"`}, NotExpectedContent: []string{shopKey}}
	list.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		headers["Authorization"] = testutil.AuthToken(t, f.admin)
	}
	list.Test(t)
}
