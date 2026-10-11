package flows_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// fakeSystemOne answers like the TypeSafe API: yes for nouls, the last option
// of choices, the highest level of scores. It records the requests.
type fakeSystemOne struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (f *fakeSystemOne) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.requests = append(f.requests, body)
	f.mu.Unlock()

	answers := map[string]any{}
	for key, raw := range body["questions"].(map[string]any) {
		q := raw.(map[string]any)
		switch q["type"] {
		case "noul":
			answers[key] = map[string]any{"type": "noul", "noul": 0.9}
		case "choice":
			// the last key in the request order
			dec := json.NewDecoder(strings.NewReader(mustJSON(q["criteria"])))
			last := ""
			_, _ = dec.Token()
			for dec.More() {
				tok, _ := dec.Token()
				last = tok.(string)
				var skip any
				_ = dec.Decode(&skip)
			}
			answers[key] = map[string]any{"type": "choice", "choice": last, "probabilities": map[string]any{last: 0.7}, "confidence": 0.55}
		case "score":
			n := len(q["criteria"].([]any))
			probs := map[string]any{}
			for i := 0; i < n; i++ {
				probs[string(rune('0'+i))] = 0.0
			}
			probs[string(rune('0'+n-1))] = 1.0
			answers[key] = map[string]any{"type": "score", "score": float64(n - 1), "probabilities": probs, "confidence": 1}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]any{"input_tokens": 100, "output_tokens": 10}})
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (f *fixture) aiModel(t *testing.T, url string) *core.Record {
	ai.Register(f.app)
	provider := testutil.Create(t, f.app, "ai_providers", map[string]any{"name": "TypeSafe", "kind": "typesafe", "api_key": "ts-secret-key", "base_url": url})
	return testutil.Create(t, f.app, "ai_models", map[string]any{"name": "Jev", "type": "system_one", "provider": provider.Id, "model": "jev-latest"})
}

func TestAIDecisionStep(t *testing.T) {
	f := setup(t)
	fake := &fakeSystemOne{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	model := f.aiModel(t, srv.URL)

	testutil.CreateCollection(t, f.app, "teams",
		&core.TextField{Name: "name", Presentable: true},
		&core.TextField{Name: "scope"},
		&core.BoolField{Name: "active"},
	)
	testutil.CreateCollection(t, f.app, "tickets",
		&core.TextField{Name: "subject"},
		&core.TextField{Name: "body"},
		&core.SelectField{Name: "priority", MaxSelect: 1, Values: []string{"low", "normal", "high"}},
		&core.TextField{Name: "team"},
	)
	billing := testutil.Create(t, f.app, "teams", map[string]any{"name": "Billing", "scope": "Payments and refunds", "active": true})
	tech := testutil.Create(t, f.app, "teams", map[string]any{"name": "Tech", "scope": "Bugs", "active": true})
	testutil.Create(t, f.app, "teams", map[string]any{"name": "Old", "active": false})

	flow := f.flow(t, "record",
		map[string]any{"collection": "tickets", "events": []string{"create"}, "timing": "after"},
		[]map[string]any{{
			"key": "triage", "type": "ai_decision",
			"config": map[string]any{
				"model":    model.Id,
				"state":    map[string]any{"mode": "values", "values": map[string]any{"subject": "{{.trigger.record.subject}}", "body": "{{.trigger.record.body}}"}},
				"maxChars": 20,
				"branches": true,
				"questions": []map[string]any{
					{"key": "urgent", "type": "noul", "question": "Is the customer blocked on {{.trigger.record.subject}}?", "yes": "Cannot work", "threshold": 0.5, "minConfidence": 0.5},
					{"key": "team", "type": "choice", "question": "Which team?", "minConfidence": 0.5,
						"options": map[string]any{"source": "records", "collection": "teams", "filter": "active = true", "sort": "name", "describe": []string{"scope"}}},
					{"key": "priority", "type": "score", "question": "How serious is it?",
						"levels":  map[string]any{"source": "select", "collection": "tickets", "field": "priority", "descriptions": map[string]any{"high": "Money is lost"}},
						"context": map[string]any{"customer": map[string]any{"plan": "pro"}}},
					{"key": "lang", "type": "choice", "question": "Language?",
						"options": map[string]any{"source": "static", "items": []map[string]any{{"key": "en", "description": "English"}, {"key": "fr"}}}},
				},
			},
			"then": []map[string]any{
				{"key": "assign", "type": "update_record", "config": map[string]any{
					"collection": "tickets", "recordId": "{{.trigger.record.id}}",
					"data": map[string]any{"team": "{{.steps.triage.team.choice}}", "priority": "{{.steps.triage.priority.label}}"},
				}},
			},
			"else": []map[string]any{
				{"key": "review", "type": "log", "config": map[string]any{"message": "needs a human"}},
			},
		}},
	)

	ticket := testutil.Create(t, f.app, "tickets", map[string]any{"subject": "Payouts failing", "body": "Help! My payouts have been failing for 3 days and nothing works."})

	runs := f.runs(t, flow.Id)
	if len(runs) != 1 || runs[0].GetString("status") != "success" {
		t.Fatalf("expected one successful run, got %d: %v", len(runs), runs)
	}

	// request: state with truncated texts, record options with labels, select levels, context
	if len(fake.requests) != 1 {
		t.Fatalf("expected one API call, got %d", len(fake.requests))
	}
	sent := mustJSON(fake.requests[0])
	for _, want := range []string{
		`"body":"Help! My payouts hav…"`,
		`"instructions":"Is the customer blocked on Payouts failing?"`,
		`"criteria":{"true":"Cannot work"}`,
		`"` + billing.Id + `":{"description":"Payments and refunds","name":"Billing"}`,
		`"` + tech.Id + `":{"description":"Bugs","name":"Tech"}`,
		`"criteria":["low","normal",{"description":"Money is lost","name":"high"}]`,
		`"instructions":{"customer":{"plan":"pro"},"question":"How serious is it?"}`,
		`"criteria":{"en":"English","fr":null}`,
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("request misses %s:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "Old") {
		t.Errorf("inactive teams must be filtered out: %s", sent)
	}

	// output: normalized answers usable by later steps
	var logs []map[string]any
	_ = json.Unmarshal([]byte(runs[0].GetString("steps")), &logs)
	out := logs[0]["output"].(map[string]any)
	team := out["team"].(map[string]any)
	// (the fake answers the last option of its decoded map, so either team)
	chosen, _ := f.app.FindRecordById("teams", team["choice"].(string))
	if chosen == nil || team["label"] != chosen.GetString("name") || team["confident"] != true {
		t.Errorf("team: %v", team)
	}
	urgent := out["urgent"].(map[string]any)
	if urgent["yes"] != true || urgent["value"] != 0.9 || urgent["confidence"] != 0.8 {
		t.Errorf("urgent: %v", urgent)
	}
	priority := out["priority"].(map[string]any)
	if priority["label"] != "high" || priority["level"] != 2.0 {
		t.Errorf("priority: %v", priority)
	}
	if out["confident"] != true || out["calibrated"] != true || out["model"] != "jev-1.13.0" || out["provider"] != "TypeSafe" {
		t.Errorf("step output: %v", out)
	}

	// the "then" branch ran with the answers
	updated, _ := f.app.FindRecordById("tickets", ticket.Id)
	if updated.GetString("team") != chosen.Id || updated.GetString("priority") != "high" {
		t.Errorf("ticket not updated from the answers: team=%q priority=%q", updated.GetString("team"), updated.GetString("priority"))
	}

	// the API key never reaches the run log
	if strings.Contains(runs[0].GetString("steps"), "ts-secret-key") {
		t.Error("the API key leaked into the run log")
	}
}

func TestAIDecisionLowConfidenceGoesToElse(t *testing.T) {
	f := setup(t)
	srv := httptest.NewServer(&fakeSystemOne{})
	defer srv.Close()
	model := f.aiModel(t, srv.URL)

	flow := f.flow(t, "manual", map[string]any{}, []map[string]any{{
		"key": "ai", "type": "ai_decision", "branches": true,
		"config": map[string]any{
			"model":    model.Id,
			"branches": true,
			"state":    map[string]any{"mode": "expression", "expression": "{{.trigger.payload}}"},
			"questions": []map[string]any{{"key": "lang", "type": "choice", "question": "Language?", "minConfidence": 0.9,
				"options": map[string]any{"source": "expression", "expression": "{{.trigger.payload.langs}}", "keyPath": "code", "labelPath": "name"}}},
		},
		"then": []map[string]any{{"key": "yes", "type": "log", "config": map[string]any{"message": "auto"}}},
		"else": []map[string]any{{"key": "no", "type": "log", "config": map[string]any{"message": "review"}}},
	}})

	res := f.engine.Run(flow, map[string]any{"payload": map[string]any{
		"text":  "Bonjour",
		"langs": []any{map[string]any{"code": "en", "name": "English"}, map[string]any{"code": "fr", "name": "French"}},
	}}, flows.RunOptions{})
	if res.Status != "success" {
		t.Fatalf("run failed: %s", res.Error)
	}
	var logs []map[string]any
	_ = json.Unmarshal([]byte(f.runs(t, flow.Id)[0].GetString("steps")), &logs)
	keys := []string{}
	for _, l := range logs {
		keys = append(keys, l["key"].(string))
	}
	if strings.Join(keys, ",") != "ai,no" {
		t.Errorf("expected the else branch (confidence 0.55 < 0.9), got the steps %v", keys)
	}
	if label := logs[0]["output"].(map[string]any)["lang"].(map[string]any)["label"]; label != "French" {
		t.Errorf("expected the label of the expression option, got %v", label)
	}
}

func TestAIDecisionValidation(t *testing.T) {
	f := setup(t)
	srv := httptest.NewServer(&fakeSystemOne{})
	defer srv.Close()
	model := f.aiModel(t, srv.URL)

	save := func(config map[string]any) error {
		col, _ := f.app.FindCollectionByNameOrId("flows")
		r := core.NewRecord(col)
		r.Load(map[string]any{"name": "x", "trigger_type": "manual", "trigger": map[string]any{},
			"steps": []map[string]any{{"key": "ai", "type": "ai_decision", "config": config}}})
		return f.app.Save(r)
	}
	if err := save(map[string]any{"questions": []any{map[string]any{"key": "a", "type": "noul"}}}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Errorf("expected a missing model error, got %v", err)
	}
	if err := save(map[string]any{"model": model.Id, "questions": []any{map[string]any{"key": "confident", "type": "noul"}}}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("expected a reserved key error, got %v", err)
	}
	if err := save(map[string]any{"model": model.Id, "questions": []any{map[string]any{"key": "a", "type": "maybe"}}}); err == nil {
		t.Error("expected an unknown type error")
	}
	if err := save(map[string]any{"model": model.Id, "questions": []any{map[string]any{"key": "a", "type": "noul", "question": "?"}}}); err != nil {
		t.Fatal(err)
	}

	// a model used by a flow can't be deleted, nor its provider
	if err := f.app.Delete(model); err == nil {
		t.Error("expected the deletion of a model used by a flow to fail")
	}
	provider, _ := f.app.FindRecordById("ai_providers", model.GetString("provider"))
	if err := f.app.Delete(provider); err == nil {
		t.Error("expected the deletion of a provider with models to fail")
	}

	// a provider only serves its model type
	col, _ := f.app.FindCollectionByNameOrId("ai_models")
	r := core.NewRecord(col)
	r.Load(map[string]any{"name": "GPT", "type": ai.TypeStandard, "provider": provider.Id, "model": "openai/gpt-5-mini"})
	if err := f.app.Save(r); err == nil || !strings.Contains(err.Error(), "System One") {
		t.Errorf("expected an incompatible provider error, got %v", err)
	}
}

func TestAIGenerateStep(t *testing.T) {
	f := setup(t)
	ai.Register(f.app)
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		msgs := body["messages"].([]any)
		prompts = append(prompts, msgs[len(msgs)-1].(map[string]any)["content"].(string))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "openai/gpt-5-mini",
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"subject": "Thanks for order 42", "urgent": false}`}}},
		})
	}))
	defer srv.Close()
	p := testutil.Create(t, f.app, "ai_providers", map[string]any{"name": "OR", "kind": "openrouter", "api_key": "k", "base_url": srv.URL})
	m := testutil.Create(t, f.app, "ai_models", map[string]any{"name": "GPT", "type": "standard", "provider": p.Id, "model": "openai/gpt-5-mini"})

	flow := f.flow(t, "record", map[string]any{"collection": "orders", "events": []string{"create"}, "timing": "after"}, []map[string]any{
		{"key": "write", "type": "ai_generate", "config": map[string]any{
			"model": m.Id, "system": "You write short emails.", "prompt": "Subject for order {{.trigger.record.number}}",
			"output": "fields", "fields": []map[string]any{{"name": "subject", "type": "text"}, {"name": "urgent", "type": "boolean"}},
		}},
		{"key": "audit", "type": "create_record", "config": map[string]any{"collection": "audit", "data": map[string]any{"message": "{{.steps.write.subject}}"}}},
	})
	testutil.Create(t, f.app, "orders", map[string]any{"number": "42"})

	runs := f.runs(t, flow.Id)
	if len(runs) != 1 || runs[0].GetString("status") != "success" {
		t.Fatalf("run: %v", runs)
	}
	if len(prompts) != 1 || prompts[0] != "Subject for order 42" {
		t.Errorf("prompt: %v", prompts)
	}
	if _, err := f.app.FindFirstRecordByData("audit", "message", "Thanks for order 42"); err != nil {
		t.Error("the generated subject wasn't used by the next step")
	}

	// a System One model can't generate
	jev := f.aiModel(t, srv.URL)
	col, _ := f.app.FindCollectionByNameOrId("flows")
	bad := core.NewRecord(col)
	bad.Load(map[string]any{"name": "x", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{{"key": "w", "type": "ai_generate", "config": map[string]any{"model": jev.Id, "prompt": "hi"}}}})
	if err := f.app.Save(bad); err != nil {
		t.Fatal(err)
	}
	res := f.engine.Run(bad, nil, flows.RunOptions{})
	if res.Status != "failed" || !strings.Contains(res.Error, "System One") {
		t.Errorf("expected a System One error, got %s %s", res.Status, res.Error)
	}
}
