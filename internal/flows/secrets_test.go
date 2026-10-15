package flows_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/secrets"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

const secretValue = "tok-9f8e7d6c5b4a"

func TestHTTPRequestSecrets(t *testing.T) {
	f := setup(t)
	secrets.Register(f.app)
	t.Setenv(flows.AllowPrivateHTTPEnv, "1")
	testutil.Create(t, f.app, secrets.Collection, map[string]any{"name": "SHOP_TOKEN", "value": secretValue})

	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		// an API echoing the key
		w.Write([]byte(`{"seen":"` + r.Header.Get("Authorization") + `","key":"` + r.URL.Query().Get("key") + `"}`))
	}))
	defer srv.Close()

	flow := f.flow(t, "manual", map[string]any{}, []map[string]any{{"key": "call", "type": "http_request", "config": map[string]any{
		"method": "GET", "url": srv.URL + "/items", "responseFormat": "auto",
		"headers": map[string]any{"Authorization": "Bearer {{secret.SHOP_TOKEN}}"},
		"query":   map[string]any{"key": `{{printf "%s" secret.SHOP_TOKEN}}`},
	}}})
	res := f.engine.Run(flow, map[string]any{}, flows.RunOptions{})
	if res.Status != flows.StatusSuccess {
		t.Fatalf("run: %s %s", res.Status, res.Error)
	}
	if got.Header.Get("Authorization") != "Bearer "+secretValue || got.URL.Query().Get("key") != secretValue {
		t.Errorf("the request didn't get the secret: %v %s", got.Header, got.URL.RawQuery)
	}
	out, _ := json.Marshal(res.Output)
	if strings.Contains(string(out), secretValue) || !strings.Contains(string(out), "[hidden]") {
		t.Errorf("the output shows the secret: %s", out)
	}
	runs := f.runs(t, flow.Id)
	for _, field := range []string{"steps", "output", "input"} {
		if strings.Contains(runs[0].GetString(field), secretValue) {
			t.Errorf("the run log %s shows the secret: %s", field, runs[0].GetString(field))
		}
	}
	// the secret data doesn't stay in the run data
	flow = f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "call", "type": "http_request", "config": map[string]any{"method": "GET", "url": srv.URL, "headers": map[string]any{"X-Key": "{{secret.SHOP_TOKEN}}"}}},
		{"key": "after", "type": "set_variables", "config": map[string]any{"variables": map[string]any{"v": "{{.secret}}"}}},
	})
	res = f.engine.Run(flow, map[string]any{}, flows.RunOptions{})
	if b, _ := json.Marshal(res.Output); strings.Contains(string(b), secretValue) {
		t.Errorf("the next step reads the secret: %s", b)
	}
}

func TestSecretsValidation(t *testing.T) {
	f := setup(t)
	secrets.Register(f.app)
	testutil.Create(t, f.app, secrets.Collection, map[string]any{"name": "SHOP_TOKEN", "value": secretValue})
	save := func(steps []map[string]any) error {
		t.Helper()
		col, _ := f.app.FindCollectionByNameOrId("flows")
		r := core.NewRecord(col)
		r.Load(map[string]any{"name": "x", "active": true, "trigger_type": "manual", "trigger": map[string]any{}, "steps": steps})
		return f.app.Save(r)
	}
	if err := save([]map[string]any{{"key": "log", "type": "log", "config": map[string]any{"message": "{{secret.SHOP_TOKEN}}"}}}); err == nil || !strings.Contains(err.Error(), "only be used in HTTP requests") {
		t.Errorf("a secret in a log step: %v", err)
	}
	if err := save([]map[string]any{{"key": "c", "type": "http_request", "config": map[string]any{"url": "https://x.test", "headers": map[string]any{"A": "{{secret.MISSING}}"}}}}); err == nil || !strings.Contains(err.Error(), "MISSING doesn't exist") {
		t.Errorf("an unknown secret: %v", err)
	}
	if err := save([]map[string]any{{"key": "c", "type": "http_request", "config": map[string]any{"url": "https://x.test", "headers": map[string]any{"A": "{{secret.SHOP_TOKEN}}"}}}}); err != nil {
		t.Errorf("a secret in an HTTP request: %v", err)
	}
}
