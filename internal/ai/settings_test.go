package ai_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestProviderKeyIsNeverReturned(t *testing.T) {
	headers := map[string]string{}
	s := &tests.ApiScenario{
		Name: "providers list", Method: http.MethodGet, URL: "/api/collections/ai_providers/records", Headers: headers,
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"name":"OpenRouter"`, `"key_hint":"…7890"`},
		NotExpectedContent: []string{"sk-or-secret", `"api_key"`},
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			app := testutil.NewApp(t)
			ai.Register(app)
			testutil.Create(t, app, "ai_providers", map[string]any{"name": "OpenRouter", "kind": "openrouter", "api_key": "sk-or-secret-1234567890"})
			su := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
			headers["Authorization"] = testutil.AuthToken(t, su)
			return app
		},
	}
	s.Test(t)
}

func TestModelTestRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model": "jev-1.13.0", "answers": {
			"urgent": {"type": "noul", "noul": 0.97},
			"team": {"type": "choice", "choice": "billing", "probabilities": {"billing": 0.9, "technical": 0.1, "sales": 0}, "confidence": 0.85},
			"mood": {"type": "score", "score": 1.8, "probabilities": {"0": 0, "1": 0.2, "2": 0.8}, "confidence": 0.8}
		}}`))
	}))
	defer srv.Close()

	headers := map[string]string{}
	var model *core.Record
	s := &tests.ApiScenario{
		Name: "test a model", Method: http.MethodPost, Headers: headers,
		ExpectedStatus:  200,
		ExpectedContent: []string{`"model":"jev-1.13.0"`, `"choice":"billing"`, `"calibrated":true`, `"durationMs"`},
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			app := testutil.NewApp(t)
			ai.Register(app)
			p := testutil.Create(t, app, "ai_providers", map[string]any{"name": "TypeSafe", "kind": "typesafe", "api_key": "k", "base_url": srv.URL})
			model = testutil.Create(t, app, "ai_models", map[string]any{"name": "Jev", "type": "system_one", "provider": p.Id, "model": "jev-latest"})
			su := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
			headers["Authorization"] = testutil.AuthToken(t, su)
			return app
		},
	}
	s.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		s.URL = "/api/ai/models/" + model.Id + "/test"
	}
	s.Test(t)
}
