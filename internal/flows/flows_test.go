package flows_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app    *tests.TestApp
	engine *flows.Engine
	mail   *mail.Engine
}

func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	m := mail.Register(app)
	m.DisableKick = true
	f := &fixture{app: app, mail: m, engine: flows.Register(app, m)}
	f.engine.Sync = true
	// deferred run log writes must end before the test app is cleaned up
	t.Cleanup(func() { f.engine.Wait(time.Minute) })

	testutil.CreateCollection(t, app, "orders",
		&core.TextField{Name: "number"},
		&core.TextField{Name: "status"},
		&core.NumberField{Name: "total"},
		&core.EmailField{Name: "email"},
	)
	testutil.CreateCollection(t, app, "audit", &core.TextField{Name: "message"})
	return f
}

func (f *fixture) flow(t testing.TB, triggerType string, trigger map[string]any, steps []map[string]any) *core.Record {
	return testutil.Create(t, f.app, "flows", map[string]any{
		"name": "test flow", "active": true, "trigger_type": triggerType, "trigger": trigger, "steps": steps,
	})
}

func (f *fixture) runs(t testing.TB, flowId string) []*core.Record {
	list, err := f.app.FindRecordsByFilter("flow_runs", "flow = {:f}", "-created", 0, 0, map[string]any{"f": flowId})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestAfterRecordFlowWithConditionAndEmail(t *testing.T) {
	f := setup(t)
	tpl := testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Thanks", "slug": "thanks", "subject": "Thanks {{.username}}", "html": "<p>Order {{.order.number}}</p>",
	})
	flow := f.flow(t, "record",
		map[string]any{"collection": "orders", "events": []string{"create"}, "timing": "after", "filter": "total > 10"},
		[]map[string]any{{
			"key": "big", "type": "condition",
			"config": map[string]any{"match": "all", "rules": []map[string]any{{"left": "{{.trigger.record.total}}", "op": "gte", "right": "100"}}},
			"then": []map[string]any{
				{"key": "audit", "type": "create_record", "config": map[string]any{"collection": "audit", "data": map[string]any{"message": "big order {{.trigger.record.number}}"}}},
				{"key": "mail", "type": "send_email", "config": map[string]any{
					"template": tpl.Id, "to": "{{.trigger.record.email}}",
					"variables": map[string]any{"username": "Ada", "order.number": "{{.trigger.record.number}}"},
				}},
			},
			"else": []map[string]any{
				{"key": "small", "type": "log", "config": map[string]any{"message": "small order"}},
			},
		}},
	)

	testutil.Create(t, f.app, "orders", map[string]any{"number": "A1", "total": 5, "email": "a@test.com"})   // filtered out
	testutil.Create(t, f.app, "orders", map[string]any{"number": "A2", "total": 50, "email": "b@test.com"})  // else branch
	testutil.Create(t, f.app, "orders", map[string]any{"number": "A3", "total": 500, "email": "c@test.com"}) // then branch

	runs := f.runs(t, flow.Id)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs (the first order is filtered out), got %d", len(runs))
	}
	for _, r := range runs {
		if r.GetString("status") != flows.StatusSuccess {
			t.Fatalf("run failed: %s %s", r.GetString("error"), r.GetString("steps"))
		}
	}
	audit, _ := f.app.FindAllRecords("audit")
	if len(audit) != 1 || audit[0].GetString("message") != "big order A3" {
		t.Fatalf("unexpected audit records %v", audit)
	}
	msgs, _ := f.app.FindAllRecords("email_messages")
	if len(msgs) != 1 || msgs[0].GetString("subject") != "Thanks Ada" || !strings.Contains(msgs[0].GetString("html"), "Order A3") ||
		msgs[0].GetString("to") != "c@test.com" || !strings.HasPrefix(msgs[0].GetString("source"), "Flow:") {
		t.Fatalf("unexpected email %v", msgs)
	}
}

func TestBeforeFlowCanModifyAndReject(t *testing.T) {
	f := setup(t)
	f.flow(t, "record",
		map[string]any{"collection": "orders", "events": []string{"create", "update"}, "timing": "before"},
		[]map[string]any{
			{
				"key": "check", "type": "condition",
				"config": map[string]any{"rules": []map[string]any{{"left": "{{.trigger.record.number}}", "op": "empty"}}},
				"then":   []map[string]any{{"key": "reject", "type": "stop", "config": map[string]any{"outcome": "reject", "message": "An order number is required."}}},
			},
			{"key": "normalize", "type": "set_trigger", "config": map[string]any{"data": map[string]any{"number": "{{upper .trigger.record.number}}", "status": "new"}}},
		},
	)

	col, _ := f.app.FindCollectionByNameOrId("orders")
	bad := core.NewRecord(col)
	if err := f.app.Save(bad); err == nil || !strings.Contains(err.Error(), "An order number is required.") {
		t.Fatalf("expected the save to be rejected, got %v", err)
	}

	ok := testutil.Create(t, f.app, "orders", map[string]any{"number": "a7"})
	saved, _ := f.app.FindRecordById("orders", ok.Id)
	if saved.GetString("number") != "A7" || saved.GetString("status") != "new" {
		t.Fatalf("expected the record to be modified before save, got %v", saved.FieldsData())
	}
}

// A before flow running another flow used to deadlock: the child run log was
// written with the main app while the save transaction held the only writer.
func TestBeforeFlowRunningAnotherFlow(t *testing.T) {
	f := setup(t)
	child := f.flow(t, "flow", map[string]any{}, []map[string]any{
		{"key": "audit", "type": "create_record", "config": map[string]any{"collection": "audit", "data": map[string]any{"message": "child ran"}}},
	})
	f.flow(t, "record",
		map[string]any{"collection": "orders", "events": []string{"create"}, "timing": "before"},
		[]map[string]any{{"key": "child", "type": "run_flow", "config": map[string]any{"flow": child.Id}}},
	)

	col, _ := f.app.FindCollectionByNameOrId("orders")
	done := make(chan error, 1)
	go func() {
		done <- f.app.RunInTransaction(func(txApp core.App) error {
			r := core.NewRecord(col)
			r.Set("number", "X1")
			return txApp.Save(r)
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the save deadlocked")
	}

	if !f.engine.Wait(10 * time.Second) {
		t.Fatal("the run logs were not written")
	}
	if runs := f.runs(t, child.Id); len(runs) != 1 || runs[0].GetString("status") != "success" {
		t.Fatalf("expected one successful child run, got %d", len(runs))
	}
	if audits, _ := f.app.FindAllRecords("audit"); len(audits) != 1 {
		t.Fatalf("expected the child flow to write in the save transaction, got %d audit records", len(audits))
	}
}

func TestLoopUpdateAndScript(t *testing.T) {
	f := setup(t)
	for i, total := range []int{10, 20, 30} {
		testutil.Create(t, f.app, "orders", map[string]any{"number": fmt.Sprint("N", i), "total": total, "status": "new"})
	}
	flow := f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "orders", "type": "get_records", "config": map[string]any{"collection": "orders", "filter": "status = {{.trigger.payload.status}}", "sort": "total"}},
		{"key": "each", "type": "loop", "config": map[string]any{"items": "{{.steps.orders.items}}"}, "steps": []map[string]any{
			{"key": "mark", "type": "update_record", "config": map[string]any{"collection": "orders", "recordId": "{{.item.id}}", "data": map[string]any{"status": "seen-{{.index}}"}}},
		}},
		{"key": "sum", "type": "script", "config": map[string]any{"code": `
			const total = data.steps.orders.items.reduce((s, o) => s + o.total, 0);
			console.log("total is", total);
			return { total, count: data.steps.each.count };`}},
	})

	res := f.engine.Run(flow, map[string]any{"payload": map[string]any{"status": "new"}}, flows.RunOptions{})
	if res.Status != flows.StatusSuccess {
		t.Fatalf("run failed: %s", res.Error)
	}
	out := res.Output.(map[string]any)
	if out["total"] != 60.0 || out["count"] != 3.0 {
		t.Fatalf("unexpected script output %#v", out)
	}
	seen, _ := f.app.FindRecordsByFilter("orders", "status ~ 'seen-'", "", 0, 0)
	if len(seen) != 3 {
		t.Fatalf("expected 3 updated orders, got %d", len(seen))
	}
}

func TestScriptSandboxAndTimeout(t *testing.T) {
	f := setup(t)
	flow := f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "s", "type": "script", "config": map[string]any{"code": `return typeof require + "," + typeof $app + "," + typeof process`}},
	})
	res := f.engine.Run(flow, nil, flows.RunOptions{})
	if res.Output != "undefined,undefined,undefined" {
		t.Fatalf("the script must not have access to the app/node APIs, got %#v (%s)", res.Output, res.Error)
	}

	loop := f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "s", "type": "script", "config": map[string]any{"code": `while (true) {}`}},
	})
	res = f.engine.Run(loop, nil, flows.RunOptions{})
	if res.Status != flows.StatusFailed || !strings.Contains(res.Error, "timeout") {
		t.Fatalf("expected a timeout error, got %s %s", res.Status, res.Error)
	}

	// a lower limit so that the test doesn't depend on the machine speed
	defer flows.SetScriptMaxHeapGrowth(32 << 20)()
	hog := f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "s", "type": "script", "config": map[string]any{"code": `const a = []; while (true) { a.push("x".repeat(1 << 20) + a.length) }`}},
	})
	res = f.engine.Run(hog, nil, flows.RunOptions{})
	if res.Status != flows.StatusFailed || !strings.Contains(res.Error, "memory") {
		t.Fatalf("expected a memory limit error, got %s %s", res.Status, res.Error)
	}

	recursion := f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "s", "type": "script", "config": map[string]any{"code": `function f() { return f() } return f()`}},
	})
	res = f.engine.Run(recursion, nil, flows.RunOptions{})
	if res.Status != flows.StatusFailed {
		t.Fatalf("expected a call stack error, got %s %s", res.Status, res.Error)
	}
}

func TestRecursionGuard(t *testing.T) {
	f := setup(t)
	// every update triggers another update of the same record
	flow := f.flow(t, "record",
		map[string]any{"collection": "orders", "events": []string{"update"}, "timing": "after"},
		[]map[string]any{{"key": "bump", "type": "update_record", "config": map[string]any{
			"collection": "orders", "recordId": "{{.trigger.record.id}}", "data": map[string]any{"total": "{{.trigger.record.total}}"},
		}}},
	)
	order := testutil.Create(t, f.app, "orders", map[string]any{"number": "R1", "total": 1})
	order.Set("status", "x")
	if err := f.app.Save(order); err != nil {
		t.Fatal(err)
	}
	if n := len(f.runs(t, flow.Id)); n == 0 || n > 6 {
		t.Fatalf("expected the chain to stop at the max depth, got %d runs", n)
	}
}

func TestValidation(t *testing.T) {
	f := setup(t)
	col, _ := f.app.FindCollectionByNameOrId("flows")
	for name, data := range map[string]map[string]any{
		"duplicate keys":       {"trigger_type": "manual", "steps": []map[string]any{{"key": "a", "type": "log"}, {"key": "a", "type": "log"}}},
		"unknown step":         {"trigger_type": "manual", "steps": []map[string]any{{"key": "a", "type": "nope"}}},
		"bad template":         {"trigger_type": "manual", "steps": []map[string]any{{"key": "a", "type": "log", "config": map[string]any{"message": "{{.x"}}}},
		"bad cron":             {"trigger_type": "schedule", "trigger": map[string]any{"cron": "every day"}},
		"internal collection":  {"trigger_type": "record", "trigger": map[string]any{"collection": "flow_runs", "events": []string{"create"}}},
		"set_trigger in after": {"trigger_type": "record", "trigger": map[string]any{"collection": "orders", "events": []string{"create"}}, "steps": []map[string]any{{"key": "a", "type": "set_trigger"}}},
	} {
		r := core.NewRecord(col)
		data["name"] = name
		r.Load(data)
		if err := f.app.Save(r); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestWebhook(t *testing.T) {
	var flow *core.Record
	factory := func(t testing.TB) *tests.TestApp {
		f := setup(t)
		flow = f.flow(t, "webhook", map[string]any{"wait": true}, []map[string]any{
			{"key": "reply", "type": "set_variables", "config": map[string]any{"vars": map[string]any{"hello": "{{.trigger.body.name}}", "q": "{{.trigger.query.x}}"}}},
		})
		return f.app
	}
	token := func() string {
		var cfg map[string]any
		_ = flow.UnmarshalJSONField("trigger", &cfg)
		return fmt.Sprint(cfg["token"])
	}

	// the URL depends on the created flow: ApiScenario reads it after BeforeTestFunc
	valid := &tests.ApiScenario{
		Name:            "valid token",
		Method:          http.MethodPost,
		Body:            bytes.NewBufferString(`{"name":"Ada"}`),
		TestAppFactory:  factory,
		ExpectedStatus:  200,
		ExpectedContent: []string{`"hello":"Ada"`, `"q":"1"`},
	}
	valid.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		valid.URL = fmt.Sprintf("/api/flows/webhook/%s?token=%s&x=1", flow.Id, token())
	}
	valid.Test(t)

	invalid := &tests.ApiScenario{
		Name:            "invalid token",
		Method:          http.MethodPost,
		TestAppFactory:  factory,
		ExpectedStatus:  401,
		ExpectedContent: []string{`"data":{}`},
	}
	invalid.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		invalid.URL = fmt.Sprintf("/api/flows/webhook/%s?token=wrong", flow.Id)
	}
	invalid.Test(t)
}

func TestHTTPRequestBlocksPrivateAddresses(t *testing.T) {
	f := setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	flow := f.flow(t, "manual", map[string]any{}, []map[string]any{
		{"key": "call", "type": "http_request", "config": map[string]any{"url": "{{.trigger.payload.url}}"}},
	})
	trigger := map[string]any{"payload": map[string]any{"url": srv.URL}}

	res := f.engine.Run(flow, trigger, flows.RunOptions{})
	if res.Status != "failed" || !strings.Contains(res.Error, flows.AllowPrivateHTTPEnv) {
		t.Fatalf("expected the loopback request to be blocked, got %s %q", res.Status, res.Error)
	}

	t.Setenv(flows.AllowPrivateHTTPEnv, "1")
	res = f.engine.Run(flow, trigger, flows.RunOptions{})
	if res.Status != "success" {
		t.Fatalf("expected the request to be allowed, got %s %q", res.Status, res.Error)
	}
}

func TestHTTPRequestFormats(t *testing.T) {
	f := setup(t)
	t.Setenv(flows.AllowPrivateHTTPEnv, "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<order id="7"><status>paid</status></order>` + r.URL.Query().Get("x")))
	}))
	defer srv.Close()

	run := func(config map[string]any) map[string]any {
		t.Helper()
		config["url"] = srv.URL
		config["method"] = "GET"
		flow := f.flow(t, "manual", map[string]any{}, []map[string]any{{"key": "call", "type": "http_request", "config": config}})
		res := f.engine.Run(flow, map[string]any{}, flows.RunOptions{})
		if res.Status != "success" {
			t.Fatalf("run: %s %s", res.Status, res.Error)
		}
		return res.Output.(map[string]any)
	}
	// flows saved before the formats: the XML stays text
	out := run(map[string]any{})
	if out["format"] != "text" || !strings.HasPrefix(out["body"].(string), "<order") {
		t.Errorf("legacy output: %v", out)
	}
	out = run(map[string]any{"responseFormat": "auto", "query": map[string]any{"x": ""}})
	order := out["body"].(map[string]any)["order"].(map[string]any)
	if out["format"] != "xml" || order["@id"] != "7" || order["status"] != "paid" {
		t.Errorf("xml output: %v", out)
	}
	out = run(map[string]any{"responseFormat": "text"})
	if out["format"] != "text" || out["raw"] != out["body"] {
		t.Errorf("text output: %v", out)
	}
}

// Dates written in another format are refused instead of being stored empty;
// parseDate converts them.
func TestDateFieldValues(t *testing.T) {
	f := setup(t)
	testutil.CreateCollection(t, f.app, "events", &core.TextField{Name: "name"}, &core.DateField{Name: "starts"})
	run := func(value string) *flows.Result {
		flow := f.flow(t, "webhook", map[string]any{}, []map[string]any{
			{"key": "add", "type": "create_record", "config": map[string]any{"collection": "events", "data": map[string]any{"name": value, "starts": value}}},
		})
		return f.engine.Run(flow, map[string]any{"payload": map[string]any{"date": "25/12/2026 14:30"}}, flows.RunOptions{})
	}
	res := run("{{.trigger.payload.date}}")
	if res.Status != flows.StatusFailed || !strings.Contains(res.Error, "isn't a date the field can store") || !strings.Contains(res.Error, "parseDate") {
		t.Fatalf("want a date error, got %s %q", res.Status, res.Error)
	}
	for value, want := range map[string]string{
		`{{parseDate "DD/MM/YYYY HH:mm" .trigger.payload.date "Europe/Paris"}}`: "2026-12-25 13:30:00.000Z",
		`{{addDays 1 "2026-10-01T09:30:00+02:00"}}`:                             "2026-10-02 07:30:00.000Z",
		`{{tz "Asia/Tokyo" "2026-10-01 00:00:00.000Z"}}`:                        "2026-10-01 00:00:00.000Z",
		``: "",
	} {
		if res := run(value); res.Status != flows.StatusSuccess {
			t.Errorf("%s: %s %s", value, res.Status, res.Error)
			continue
		}
		recs, _ := f.app.FindRecordsByFilter("events", "", "-@rowid", 1, 0)
		if got := recs[0].GetDateTime("starts").String(); got != want {
			t.Errorf("%s: stored %q, want %q", value, got, want)
		}
	}
}
