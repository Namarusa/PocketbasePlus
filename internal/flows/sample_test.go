package flows_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// routes calls the app's routes as a superuser (or anonymously).
func routes(t *testing.T, f *fixture) func(method, path string, body any, auth bool) (int, map[string]any) {
	r, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatal(err)
	}
	var mux http.Handler
	if err := f.app.OnServe().Trigger(&core.ServeEvent{App: f.app, Router: r}, func(e *core.ServeEvent) error {
		mux, err = e.Router.BuildMux()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	su := testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	return func(method, path string, body any, auth bool) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.Header.Set("Authorization", testutil.AuthToken(t, su))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
}

func TestTestDataAndPreview(t *testing.T) {
	f := setup(t)
	call := routes(t, f)
	cards := []any{}
	for i := range 25 {
		cards = append(cards, map[string]any{"name": fmt.Sprintf("card %d", i), "hp": i * 10})
	}
	flow := f.flow(t, "webhook", map[string]any{"token": "x"}, []map[string]any{
		{"key": "vars", "type": "set_variables", "config": map[string]any{"vars": map[string]any{
			"cards": "{{trigger.body.cards}}", "api_key": "{{trigger.body.api_key}}", "long": "{{repeat 1000 \"a\"}}",
		}}},
		{"key": "each", "type": "loop", "config": map[string]any{"items": "{{steps.vars.cards}}"}, "steps": []map[string]any{
			{"key": "msg", "type": "log", "config": map[string]any{"message": "{{item.name}} has {{item.hp}}"}},
		}},
	})

	// a run without the flag keeps no test data
	code, _ := call(http.MethodPost, "/api/flows/run/"+flow.Id, map[string]any{"wait": true, "trigger": map[string]any{"body": map[string]any{"cards": cards[:2]}}}, true)
	if code != 200 {
		t.Fatal(code)
	}
	if _, s := call(http.MethodGet, "/api/flows/sample/"+flow.Id, nil, true); len(s) != 0 {
		t.Fatalf("no test data expected yet: %v", s)
	}

	code, res := call(http.MethodPost, "/api/flows/run/"+flow.Id, map[string]any{"wait": true, "sample": true,
		"trigger": map[string]any{"body": map[string]any{"cards": cards, "api_key": "sk-secret-123"}}}, true)
	if code != 200 || fmt.Sprint(res["runs"].([]any)[0].(map[string]any)["status"]) != "success" {
		t.Fatalf("run: %d %v", code, res)
	}
	_, s := call(http.MethodGet, "/api/flows/sample/"+flow.Id, nil, true)
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "sk-secret-123") {
		t.Errorf("secrets must not be kept: %s", raw)
	}
	data := s["data"].(map[string]any)
	stepCards := data["steps"].(map[string]any)["vars"].(map[string]any)["cards"].([]any)
	if len(stepCards) != 25 || len(s["counts"].(map[string]any)) != 0 {
		t.Errorf("lists up to 50 items are kept whole: %d %v", len(stepCards), s["counts"])
	}
	if long := data["steps"].(map[string]any)["vars"].(map[string]any)["long"].(string); len(long) > 310 {
		t.Errorf("long texts are cut: %d", len(long))
	}
	if loop := data["loops"].(map[string]any)["each"].([]any); len(loop) != 25 {
		t.Errorf("the loop list is kept: %v", loop)
	}
	if msg := data["steps"].(map[string]any)["msg"].(map[string]any)["message"]; msg != "card 0 has 0" {
		t.Errorf("a step of a loop keeps its first output: %v", msg)
	}

	preview := func(body map[string]any) map[string]any {
		body["flow"] = flow.Id
		code, out := call(http.MethodPost, "/api/flows/preview", body, true)
		if code != 200 {
			t.Fatalf("preview: %d %v", code, out)
		}
		return out
	}
	if out := preview(map[string]any{"expr": "{{steps.vars.cards}}"}); out["type"] != "list" || len(out["value"].([]any)) != 20 {
		t.Errorf("list: %v", out)
	}
	if out := preview(map[string]any{"expr": "Hi {{upper item.name}}", "loops": []string{"each"}}); out["text"] != "Hi CARD 0" {
		t.Errorf("loop item: %v", out)
	}
	if out := preview(map[string]any{"expr": "{{uper item.name}}", "loops": []string{"each"}}); !strings.Contains(fmt.Sprint(out["error"]), `"uper" not defined`) {
		t.Errorf("unknown function: %v", out)
	}
	if out := preview(map[string]any{"expr": "{{steps.nope.x}}"}); out["missing"] != "steps.nope.x" || out["type"] != "empty" {
		t.Errorf("missing path: %v", out)
	}
	if out := preview(map[string]any{"expr": "{{trigger.body.api_key}}"}); strings.Contains(fmt.Sprint(out), "sk-secret") {
		t.Errorf("secrets hidden in previews: %v", out)
	}
	// saved API samples fill the steps the run didn't reach
	if out := preview(map[string]any{"expr": "{{steps.api.body.title}}", "steps": map[string]any{"api": map[string]any{"body": map[string]any{"title": "From the call"}}}}); out["text"] != "From the call" {
		t.Errorf("extra steps: %v", out)
	}
	if out := preview(map[string]any{"expr": "{{divide 1 0}}"}); !strings.Contains(fmt.Sprint(out["error"]), "division by zero") || strings.Contains(fmt.Sprint(out["error"]), "template:") {
		t.Errorf("clean error: %v", out)
	}

	if code, _ := call(http.MethodPost, "/api/flows/preview", map[string]any{"expr": "{{1}}"}, false); code != 401 {
		t.Errorf("preview needs a superuser, got %d", code)
	}
	if code, _ := call(http.MethodGet, "/api/flows/sample/"+flow.Id, nil, false); code != 401 {
		t.Errorf("sample needs a superuser, got %d", code)
	}
}

func TestPreviewBatch(t *testing.T) {
	f := setup(t)
	call := routes(t, f)
	flow := f.flow(t, "manual", map[string]any{}, []map[string]any{{"key": "x", "type": "set_variables", "config": map[string]any{"vars": map[string]any{"a": "1"}}}})
	code, out := call(http.MethodPost, "/api/flows/preview", map[string]any{"flow": flow.Id, "items": []map[string]any{
		{"id": "one", "expr": "{{add 1 2}}"},
		{"id": "two", "expr": "{{steps.x.a}}"},
	}}, true)
	if code != 200 || out["hasSample"] != false {
		t.Fatalf("%d %v", code, out)
	}
	r := out["results"].(map[string]any)
	if r["one"].(map[string]any)["text"] != "3" || r["two"].(map[string]any)["missing"] != "steps.x.a" {
		t.Errorf("batch: %v", r)
	}
}

func TestLongListsAreShortened(t *testing.T) {
	f := setup(t)
	call := routes(t, f)
	list := []any{}
	for i := range 120 {
		list = append(list, i)
	}
	flow := f.flow(t, "webhook", map[string]any{"token": "x"}, []map[string]any{{"key": "v", "type": "set_variables", "config": map[string]any{"vars": map[string]any{"n": "{{trigger.body.list}}"}}}})
	call(http.MethodPost, "/api/flows/run/"+flow.Id, map[string]any{"wait": true, "sample": true, "trigger": map[string]any{"body": map[string]any{"list": list}}}, true)
	_, s := call(http.MethodGet, "/api/flows/sample/"+flow.Id, nil, true)
	if s["counts"].(map[string]any)["steps.v.n"] != 120.0 {
		t.Fatalf("counts: %v", s["counts"])
	}
	_, out := call(http.MethodPost, "/api/flows/preview", map[string]any{"flow": flow.Id, "expr": "{{len steps.v.n}}"}, true)
	if out["text"] != "50" || out["shortened"] != true {
		t.Errorf("a count on a shortened list is flagged: %v", out)
	}
	_, out = call(http.MethodPost, "/api/flows/preview", map[string]any{"flow": flow.Id, "expr": "{{trigger.body}}"}, true)
	if out["shortened"] == true {
		t.Errorf("not flagged when not reading the list: %v", out)
	}
}
