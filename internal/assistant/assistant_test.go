package assistant_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/activity"
	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/assistant"
	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// scripted is a fake model: each call returns the next scripted answer,
// built from the request (to read the previous tool results).
type scripted struct {
	mu       sync.Mutex
	steps    []func(req ai.ChatRequest) ai.Message
	requests []ai.ChatRequest
}

func (s *scripted) Chat(ctx context.Context, model string, req ai.ChatRequest) (*ai.ChatResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if len(s.steps) == 0 {
		return &ai.ChatResult{Message: ai.Message{Role: "assistant", Content: "done"}}, nil
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return &ai.ChatResult{Model: "fake", Message: step(req)}, nil
}

func call(id, name string, args any) ai.ToolCall {
	tc := ai.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	b, _ := json.Marshal(args)
	tc.Function.Arguments = string(b)
	return tc
}

func calls(c ...ai.ToolCall) func(ai.ChatRequest) ai.Message {
	return func(ai.ChatRequest) ai.Message { return ai.Message{Role: "assistant", ToolCalls: c} }
}

func say(text string) func(ai.ChatRequest) ai.Message {
	return func(ai.ChatRequest) ai.Message { return ai.Message{Role: "assistant", Content: text} }
}

// toolResult returns the result of a tool call in the request messages.
func toolResult(req ai.ChatRequest, id string) map[string]any {
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == id {
			var out map[string]any
			_ = json.Unmarshal([]byte(m.Content), &out)
			return out
		}
	}
	return nil
}

// toolRaw returns the raw result of a tool call.
func toolRaw(req ai.ChatRequest, id string) string {
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == id {
			return m.Content
		}
	}
	return ""
}

type fixture struct {
	app       *tests.TestApp
	mail      *mail.Engine
	model     *scripted
	su, other *core.Record
	customers map[string]*core.Record
	desk      *core.Record
}

func setup(t testing.TB, model *scripted) *fixture {
	app := testutil.NewApp(t)
	app.Settings().Meta.AppURL = "https://shop.test"
	f := &fixture{app: app, model: model, customers: map[string]*core.Record{}}
	activity.Register(app)
	f.mail = mail.Register(app)
	f.mail.DisableKick = true
	a := assistant.Register(app, f.mail, flows.Register(app, f.mail))
	a.SetModel(func(core.App) (*ai.Model, ai.Chatter, error) {
		return &ai.Model{Name: "Fake", Model: "fake"}, model, nil
	})

	customers := testutil.CreateCollection(t, app, "customers",
		&core.TextField{Name: "name", Presentable: true}, &core.EmailField{Name: "email"})
	products := testutil.CreateCollection(t, app, "products", &core.TextField{Name: "name", Presentable: true})
	testutil.CreateCollection(t, app, "orders",
		&core.RelationField{Name: "customer", CollectionId: customers.Id, MaxSelect: 1},
		&core.RelationField{Name: "product", CollectionId: products.Id, MaxSelect: 1},
		&core.NumberField{Name: "total"},
		&core.SelectField{Name: "status", MaxSelect: 1, Values: []string{"pending", "paid"}},
	)
	testutil.CreateCollection(t, app, "promo_codes", &core.TextField{Name: "code", Required: true}, &core.NumberField{Name: "percent"})

	f.desk = testutil.Create(t, app, "products", map[string]any{"name": "Desk"})
	chair := testutil.Create(t, app, "products", map[string]any{"name": "Chair"})
	for _, n := range []string{"ada", "bob", "cy", "dan"} {
		f.customers[n] = testutil.Create(t, app, "customers", map[string]any{"name": n, "email": n + "@test.com"})
	}
	order := func(c string, p *core.Record, total float64, daysAgo int) {
		r := testutil.Create(t, app, "orders", map[string]any{"customer": f.customers[c].Id, "product": p.Id, "total": total, "status": "paid"})
		// backdate
		created, _ := types.ParseDateTime(time.Now().UTC().AddDate(0, 0, -daysAgo))
		if _, err := app.DB().NewQuery("UPDATE orders SET created = {:c} WHERE id = {:id}").Bind(map[string]any{"c": created.String(), "id": r.Id}).Execute(); err != nil {
			t.Fatal(err)
		}
	}
	order("ada", f.desk, 300, 1)
	order("ada", f.desk, 300, 2) // twice: one email
	order("bob", f.desk, 250, 2)
	order("cy", f.desk, 280, 10) // too old
	order("dan", chair, 90, 1)   // other product
	if err := mail.Unsubscribe(app, "bob@test.com", "manual"); err != nil {
		t.Fatal(err)
	}

	f.su = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	f.other = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "other@test.com", "password": "1234567890"})
	return f
}

type response struct {
	ID       string           `json:"id"`
	Messages []ai.Message     `json:"messages"`
	Actions  []map[string]any `json:"actions"`
	Error    string           `json:"error"`
}

// handler builds the app router once (the conversation spans several requests).
func (f *fixture) handler(t *testing.T) http.Handler {
	t.Helper()
	r, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatal(err)
	}
	var mux http.Handler
	ev := &core.ServeEvent{App: f.app, Router: r}
	err = f.app.OnServe().Trigger(ev, func(e *core.ServeEvent) error {
		m, err := e.Router.BuildMux()
		mux = m
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

// post sends a JSON request as a superuser and decodes the response.
func post(t *testing.T, h http.Handler, as *core.Record, path string, body any) (int, response) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	if as != nil {
		req.Header.Set("Authorization", testutil.AuthToken(t, as))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out response
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestDashboardConversation(t *testing.T) {
	model := &scripted{}
	f := setup(t, model)
	h := f.handler(t)

	invalid := []any{map[string]any{"type": "metric", "name": "Orders", "options": map[string]any{"collection": "nope", "fn": "count"}}}
	valid := []any{
		map[string]any{"type": "metric", "name": "Orders (30 days)", "options": map[string]any{"collection": "orders", "fn": "count", "filter": "created >= @monthStart || created < @monthStart"}},
		map[string]any{"type": "metric", "name": "Revenue", "options": map[string]any{"collection": "orders", "fn": "sum", "field": "total", "prefix": "$"}},
		map[string]any{"type": "time_series", "name": "Revenue per day", "options": map[string]any{"collection": "orders", "dateField": "created", "precision": "day", "range": "30d", "fn": "sum", "field": "total"}},
		map[string]any{"type": "bar", "name": "By product", "options": map[string]any{"collection": "orders", "groupField": "product", "fn": "sum", "field": "total"}},
	}
	model.steps = []func(ai.ChatRequest) ai.Message{
		calls(call("c1", "describe_collection", map[string]any{"collection": "orders"}), call("c2", "aggregate", map[string]any{"collection": "orders", "fn": "sum", "field": "total", "group_by": "product"})),
		calls(call("c3", "propose_dashboard", map[string]any{"name": "Orders", "panels": invalid})),
		func(req ai.ChatRequest) ai.Message {
			// the previous results reached the model
			if d := toolResult(req, "c1"); d["records"] != 5.0 || len(d["back_relations"].([]any)) != 0 {
				t.Errorf("describe_collection: %v", d)
			}
			if r := toolResult(req, "c3"); !strings.Contains(fmt.Sprint(r["error"]), `unknown collection "nope"`) {
				t.Errorf("expected the invalid panel error, got %v", r)
			}
			return calls(call("c4", "propose_dashboard", map[string]any{"name": "Orders, last 30 days", "panels": valid}))(req)
		},
		say("I prepared the dashboard: click **Apply** to create it."),
	}

	code, res := post(t, h, f.su, "/api/assistant/chat", map[string]any{"message": "Create a dashboard showing the last 30 days orders", "page": map[string]any{"path": "#/dashboards"}})
	if code != 200 || res.Error != "" {
		t.Fatalf("chat: %d %+v", code, res)
	}
	// system prompt: schema, date, guide
	system := model.requests[0].Messages[0].Content
	for _, want := range []string{"- orders (base, 5): customer → customers, product → products, total number, status select[pending|paid]", "Now: ", "propose_dashboard", "The user is on this page: #/dashboards"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt misses %q", want)
		}
	}
	if len(model.requests[0].Tools) < 8 {
		t.Errorf("expected the tools, got %d", len(model.requests[0].Tools))
	}
	if last := res.Messages[len(res.Messages)-1]; last.Role != "assistant" || !strings.Contains(last.Content, "Apply") {
		t.Errorf("unexpected last message: %+v", last)
	}
	if len(res.Actions) != 1 || res.Actions[0]["status"] != "pending" || res.Actions[0]["type"] != "dashboard" {
		t.Fatalf("actions: %v", res.Actions)
	}
	if n, _ := f.app.CountRecords("dashboards"); n != 0 {
		t.Fatal("nothing must be created before Apply")
	}
	panels := res.Actions[0]["details"].(map[string]any)["panels"].([]any)
	if p := panels[1].(map[string]any); p["preview"] != "value 1220" {
		t.Errorf("revenue preview: %v", p)
	}

	// another superuser can't apply it
	actionId := res.Actions[0]["id"].(string)
	if code, _ := post(t, h, f.other, "/api/assistant/conversations/"+res.ID+"/actions/"+actionId+"/apply", nil); code != 404 {
		t.Errorf("expected 404 for another superuser, got %d", code)
	}

	code, applied := post(t, h, f.su, "/api/assistant/conversations/"+res.ID+"/actions/"+actionId+"/apply", nil)
	if code != 200 || applied.Actions[0]["status"] != "applied" {
		t.Fatalf("apply: %d %+v", code, applied)
	}
	d, err := f.app.FindFirstRecordByData("dashboards", "name", "Orders, last 30 days")
	if err != nil {
		t.Fatal(err)
	}
	if link := applied.Actions[0]["result"].(map[string]any)["link"]; link != "#/dashboards/"+d.Id {
		t.Errorf("link: %v", link)
	}
	list, _ := f.app.FindRecordsByFilter("dashboard_panels", "dashboard = {:d}", "y,x", 0, 0, map[string]any{"d": d.Id})
	// two numbers side by side, then the two charts side by side
	if len(list) != 4 || list[0].GetInt("w") != 12 || list[1].GetInt("x") != 12 ||
		list[2].GetString("type") != "time_series" || list[2].GetInt("y") != 4 || list[3].GetInt("x") != 12 || list[3].GetInt("w") != 12 {
		t.Errorf("panels layout: %d", len(list))
		for _, p := range list {
			t.Logf("%s x=%d y=%d w=%d h=%d", p.GetString("name"), p.GetInt("x"), p.GetInt("y"), p.GetInt("w"), p.GetInt("h"))
		}
	}

	// applied once only
	if code, _ := post(t, h, f.su, "/api/assistant/conversations/"+res.ID+"/actions/"+actionId+"/apply", nil); code != 400 {
		t.Errorf("expected a second apply to fail, got %d", code)
	}
}

func TestCampaignConversation(t *testing.T) {
	model := &scripted{}
	f := setup(t, model)
	h := f.handler(t)

	var since string
	model.steps = []func(ai.ChatRequest) ai.Message{
		calls(call("d1", "relative_date", map[string]any{"days": -3})),
		func(req ai.ChatRequest) ai.Message {
			since = toolResult(req, "d1")["date"].(string)
			return calls(call("q1", "query_records", map[string]any{
				"collection": "orders", "filter": fmt.Sprintf(`product.name = "Desk" && created >= "%s"`, since), "fields": []string{"customer"}, "limit": 50,
			}))(req)
		},
		func(req ai.ChatRequest) ai.Message {
			items := toolResult(req, "q1")["items"].([]any)
			ids := []string{}
			for _, it := range items {
				ids = append(ids, it.(map[string]any)["customer"].(string))
			}
			return calls(
				call("p1", "propose_record", map[string]any{"collection": "promo_codes", "title": "Promo code DESK15", "data": map[string]any{"code": "DESK15", "percent": 15}}),
				call("p2", "propose_campaign", map[string]any{
					"title": "Desk promo", "collection": "customers", "record_ids": ids, "recipient_field": "email",
					"template": map[string]any{"subject": "15% off for {{.record.name}}", "html": "<p>Hi {{.record.name}}, use <b>DESK15</b>.</p>", "text": "Use DESK15"},
				}),
			)(req)
		},
		func(req ai.ChatRequest) ai.Message {
			r := toolResult(req, "p2")
			if r["recipients"] != 1.0 {
				t.Errorf("expected 1 recipient (ada once, bob unsubscribed), got %v", r)
			}
			return say("Ready: 1 recipient.")(req)
		},
	}
	code, res := post(t, h, f.su, "/api/assistant/chat", map[string]any{"message": "Email the customers who ordered the Desk in the past 3 days a DESK15 promo code"})
	if code != 200 || len(res.Actions) != 2 {
		t.Fatalf("chat: %d %+v", code, res)
	}
	if n, _ := f.app.CountRecords("email_templates"); n != 0 {
		t.Fatal("the dry run must not leave a template")
	}
	campaign := res.Actions[1]
	// ada ordered twice: her id is listed twice but she's one record
	if !strings.Contains(campaign["summary"].(string), "1 recipient(s), now (skipped: 1 unsubscribed)") {
		t.Errorf("summary: %v", campaign["summary"])
	}
	if html := campaign["details"].(map[string]any)["html"].(string); !strings.Contains(html, "Hi ada") {
		t.Errorf("preview: %s", html)
	}

	// apply both
	for _, act := range res.Actions {
		code, out := post(t, h, f.su, "/api/assistant/conversations/"+res.ID+"/actions/"+act["id"].(string)+"/apply", nil)
		if code != 200 || out.Error != "" {
			t.Fatalf("apply %s: %d %+v", act["type"], code, out)
		}
	}
	if _, err := f.app.FindFirstRecordByData("promo_codes", "code", "DESK15"); err != nil {
		t.Error("promo code not created")
	}
	tpl, err := f.app.FindFirstRecordByData("email_templates", "name", "Desk promo")
	if err != nil || !tpl.GetBool("marketing") {
		t.Fatalf("campaign template: %v", err)
	}
	msgs, _ := f.app.FindRecordsByFilter("email_messages", "", "", 0, 0)
	if len(msgs) != 1 || msgs[0].GetString("to") != "ada@test.com" || !strings.Contains(msgs[0].GetString("html"), "/email/unsubscribe?e=ada%40test.com") {
		t.Fatalf("messages: %d", len(msgs))
	}

	// the promo code revision is attributed to the superuser via the assistant
	rev, err := f.app.FindFirstRecordByData("revisions", "collection_name", "promo_codes")
	if err != nil || rev.GetString("actor_label") != "admin@test.com (AI assistant)" || rev.GetString("actor_type") != "superuser" {
		t.Errorf("revision actor: %v %v", err, rev)
	}
}

func TestChatAccess(t *testing.T) {
	model := &scripted{steps: []func(ai.ChatRequest) ai.Message{say("hello")}}
	f := setup(t, model)
	h := f.handler(t)

	if code, _ := post(t, h, nil, "/api/assistant/chat", map[string]any{"message": "hi"}); code != 401 {
		t.Errorf("anonymous: expected 401, got %d", code)
	}
	code, res := post(t, h, f.su, "/api/assistant/chat", map[string]any{"message": "hi"})
	if code != 200 {
		t.Fatal(code)
	}
	if code, _ := post(t, h, f.other, "/api/assistant/chat", map[string]any{"message": "hi", "conversation": res.ID}); code != 404 {
		t.Errorf("another superuser's conversation: expected 404, got %d", code)
	}
	// tool results can't reach internal collections
	model.steps = []func(ai.ChatRequest) ai.Message{
		calls(call("x", "query_records", map[string]any{"collection": "_superusers"})),
		func(req ai.ChatRequest) ai.Message {
			if r := toolResult(req, "x"); !strings.Contains(fmt.Sprint(r["error"]), "unknown collection") {
				t.Errorf("expected internal collections to be refused, got %v", r)
			}
			return say("no")(req)
		},
	}
	post(t, h, f.su, "/api/assistant/chat", map[string]any{"message": "list the superusers", "conversation": res.ID})
}

func TestFlowConversation(t *testing.T) {
	model := &scripted{}
	f := setup(t, model)
	h := f.handler(t)
	webhook := testutil.Create(t, f.app, "flows", map[string]any{"name": "Hook", "active": true, "trigger_type": "webhook",
		"trigger": map[string]any{"method": "POST", "token": "s3cr3t-token"},
		"steps":   []any{map[string]any{"key": "boom", "type": "script", "config": map[string]any{"code": "throw new Error('nope')"}}}})

	steps := []any{
		map[string]any{"key": "check", "type": "condition", "config": map[string]any{"match": "all", "rules": []any{map[string]any{"left": "{{.trigger.record.total}}", "op": "gte", "right": "200"}}},
			"then": []any{map[string]any{"key": "note", "type": "create_record", "config": map[string]any{"collection": "promo_codes",
				"data": map[string]any{"code": `{{upper (substr 0 4 .trigger.record.id)}}`, "percent": 10}}}}},
	}
	model.steps = []func(ai.ChatRequest) ai.Message{
		calls(call("c1", "get_flow", map[string]any{"flow_id": "Hook"}), call("c2", "describe_functions", map[string]any{"names": []string{"parseDate"}}),
			call("c3", "propose_flow", map[string]any{"name": "Big orders", "trigger_type": "record", "trigger": map[string]any{"collection": "orders", "events": []string{"create"}}, "steps": []any{map[string]any{"type": "log"}}})),
		func(req ai.ChatRequest) ai.Message {
			flow := toolResult(req, "c1")
			if flow["id"] != webhook.Id || strings.Contains(fmt.Sprint(flow), "s3cr3t") || len(flow["steps"].([]any)) != 1 {
				t.Errorf("get_flow: %v", flow)
			}
			if fn := toolRaw(req, "c2"); !strings.Contains(fn, "DD/MM/YYYY") {
				t.Errorf("describe_functions: %v", fn)
			}
			if r := toolResult(req, "c3"); !strings.Contains(fmt.Sprint(r["error"]), "a key is required") {
				t.Errorf("expected the validation error, got %v", r)
			}
			return calls(
				call("c4", "propose_flow", map[string]any{"name": "Big orders", "trigger_type": "record", "trigger": map[string]any{"collection": "orders", "events": []string{"create"}}, "steps": steps}),
				call("c5", "propose_flow", map[string]any{"flow_id": webhook.Id, "change": "disable", "active": false, "trigger": map[string]any{"method": "GET", "token": "(hidden)"}}),
			)(req)
		},
		say("Proposed."),
	}
	code, res := post(t, h, f.su, "/api/assistant/chat", map[string]any{"message": "Add a flow for big orders", "page": map[string]any{"path": "#/flows/" + webhook.Id, "flow": webhook.Id}})
	if code != 200 || res.Error != "" {
		t.Fatalf("chat: %d %+v", code, res)
	}
	system := model.requests[0].Messages[0].Content
	for _, want := range []string{`flow "Hook"`, "parseDate", "propose_flow_run", "- Date: now, today, formatDate"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt misses %q", want)
		}
	}
	if len(res.Actions) != 2 || res.Actions[0]["type"] != "flow" {
		t.Fatalf("actions: %v", res.Actions)
	}
	if n, _ := f.app.CountRecords("flows"); n != 1 {
		t.Fatal("nothing must be created before Apply")
	}
	apply := func(id string) response {
		code, res := post(t, h, f.su, "/api/assistant/conversations/"+res.ID+"/actions/"+id+"/apply", nil)
		if code != 200 || res.Error != "" {
			t.Fatalf("apply %s: %d %+v", id, code, res)
		}
		return res
	}
	apply(res.Actions[0]["id"].(string))
	apply(res.Actions[1]["id"].(string))
	created, err := f.app.FindFirstRecordByData("flows", "name", "Big orders")
	if err != nil || created.GetBool("active") || !strings.Contains(created.GetString("steps"), `"then"`) {
		t.Fatalf("the new flow: %v %v", err, created)
	}
	webhook, _ = f.app.FindRecordById("flows", webhook.Id)
	if webhook.GetBool("active") || !strings.Contains(webhook.GetString("trigger"), "s3cr3t-token") || !strings.Contains(webhook.GetString("trigger"), "GET") {
		t.Errorf("the webhook flow keeps its token and changes: %s %v", webhook.GetString("trigger"), webhook.GetBool("active"))
	}

	// a test run, then the failure explained from the run log
	created.Set("active", true)
	if err := f.app.Save(created); err != nil {
		t.Fatal(err)
	}
	order, _ := f.app.FindFirstRecordByFilter("orders", "total = 300")
	model.steps = []func(ai.ChatRequest) ai.Message{
		calls(call("r1", "propose_flow_run", map[string]any{"flow_id": created.Id, "record_id": order.Id}), call("r2", "propose_flow_run", map[string]any{"flow_id": "Hook", "trigger": map[string]any{"body": map[string]any{"x": 1}}})),
		say("Click Apply to run them."),
	}
	_, res = post(t, h, f.su, "/api/assistant/chat", map[string]any{"conversation": res.ID, "message": "Test them"})
	if len(res.Actions) != 4 || res.Actions[2]["type"] != "flow_run" {
		t.Fatalf("actions: %v", res.Actions)
	}
	ran := apply(res.Actions[2]["id"].(string))
	if r := ran.Actions[2]["result"].(map[string]any); r["status"] != "success" {
		t.Errorf("test run: %v", r)
	}
	if n, _ := f.app.CountRecords("promo_codes"); n != 1 {
		t.Errorf("the test run must create the promo code, got %d", n)
	}
	ran = apply(res.Actions[3]["id"].(string))
	result := ran.Actions[3]["result"].(map[string]any)
	if result["status"] != "failed" || !strings.Contains(fmt.Sprint(result["error"]), "nope") {
		t.Fatalf("failed run: %v", result)
	}
	model.steps = []func(ai.ChatRequest) ai.Message{
		func(req ai.ChatRequest) ai.Message {
			if !strings.Contains(req.Messages[0].Content, "failed, error:") {
				t.Errorf("the run result is in the proposals")
			}
			return calls(call("l1", "flow_runs", map[string]any{"flow_id": "Hook", "status": "failed"}), call("l2", "flow_runs", map[string]any{"run_id": result["run_id"]}))(req)
		},
		func(req ai.ChatRequest) ai.Message {
			if runs := toolResult(req, "l1")["runs"].([]any); len(runs) != 1 {
				t.Errorf("failed runs: %v", runs)
			}
			log := toolResult(req, "l2")
			if steps := log["steps"].([]any); len(steps) != 1 || !strings.Contains(fmt.Sprint(steps[0]), "nope") {
				t.Errorf("run log: %v", log)
			}
			return say("The script throws.")(req)
		},
	}
	post(t, h, f.su, "/api/assistant/chat", map[string]any{"conversation": res.ID, "message": "Why did Hook fail?"})
}

func TestListResourcesKinds(t *testing.T) {
	model := &scripted{}
	f := setup(t, model)
	h := f.handler(t)
	model.steps = []func(ai.ChatRequest) ai.Message{
		calls(call("k1", "list_resources", map[string]any{"kind": "ai_models"}), call("k2", "list_resources", map[string]any{"kind": "forms"}), call("k3", "list_resources", map[string]any{"kind": "pages"})),
		func(req ai.ChatRequest) ai.Message {
			for _, id := range []string{"k1", "k2"} {
				if r := toolResult(req, id); r != nil && r["error"] != nil {
					t.Errorf("%s: %v", id, r)
				}
			}
			if r := toolResult(req, "k3"); !strings.Contains(fmt.Sprint(r["error"]), "Pages module isn't installed") {
				t.Errorf("pages without the module: %v", r)
			}
			return say("ok")(req)
		},
	}
	post(t, h, f.su, "/api/assistant/chat", map[string]any{"message": "list"})
}
