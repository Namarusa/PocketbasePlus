package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
)

// The flows tools: read a flow and its runs (to explain or fix it), propose
// a new flow or changes to one, and propose a test run. Proposed flows go
// through the same validation as the flow editor before the card is shown.

const (
	ActionFlow    = "flow"
	ActionFlowRun = "flow_run"

	hiddenToken = "(hidden)"
	maxRuns     = 20
)

const flowsGuide = `Flows (get_flow, flow_runs, propose_flow, propose_flow_run):
- A flow has one trigger and a list of steps. Read a flow with get_flow before changing it, and propose the complete new step list (propose_flow with flow_id replaces the steps; omitted fields keep their value).
- trigger_type and trigger:
  - record: {collection, events: ["create","update","delete"], timing: "after"|"before", filter}. "before" runs inside the save and may change the record (set_trigger) or reject it (stop with outcome "reject").
  - manual: {collection} (a button on its records; trigger.record is the record).
  - webhook: {method: "POST"|"GET"|"ANY", wait: bool} (trigger.body, trigger.query, trigger.headers). The token is created on save.
  - schedule: {cron: "0 9 * * *"} (UTC).
  - flow: {} (called by run_flow steps; trigger.payload).
  - action: {access: "public"|"users"|"roles", roles: [role ids]} (page forms and buttons: trigger.form.<name>, trigger.user, trigger.record).
- Step: {key, name, type, config, then, else, steps, continueOnError}. key: letters, digits and _ (unique), used as {{.steps.<key>....}}. then/else: the branches of a condition; steps: the body of a loop.
- Step types and config:
  - condition {match: "all"|"any", rules: [{left, op, right}]}, op: eq neq contains not_contains starts ends gt gte lt lte empty not_empty. Output .result.
  - loop {items: "{{.steps.records.items}}", max} (inside: {{.item}}, {{.index}}). Output .count.
  - delay {seconds} (≤ 600), stop {outcome: "success"|"reject", message}, run_flow {flow: id, payload: {}}.
  - get_records {collection, filter, sort, limit} (output .items, .count; filter values like {{.trigger.record.id}} are bound safely).
  - create_record {collection, data: {field: value}}, update_record {collection, recordId, data}, delete_record {collection, recordId}, set_trigger {data} (before-save flows). Output: the record.
  - send_email {template: id, to: "{{.trigger.record.email}}", variables: {name: value}, delayMinutes}.
  - api_call {call: saved call id, vars: {name: value}, failOnError} (output .ok .status .body .data.<output>).
  - http_request {method, url, query: {}, headers: {}, body, responseFormat: "auto"|"json"|"xml"|"text", retries, failOnError, timeoutSeconds}.
  - notify {to: "all", subject, message, link}, log {message}, set_variables {vars: {name: value}} (output .<name>).
  - script {code} (sandboxed JS: data.trigger, data.steps...; returns the output).
  - ai_generate {model: id, system, prompt, output: "text"|"fields", fields: [{name, type, description}]}.
  - action flows only: sign_in {collection, identity, password}, sign_out {}, redirect {url, message}.
- Values are templates: {{.trigger.record.title}}, {{.steps.<key>.<field>}}, {{.item}}, {{.now}} with the functions (see the Functions section). A value that is one variable (or one function giving a list) keeps its type.
- Date fields only accept "2006-01-02 15:04:05.000Z" or ISO 8601: convert other texts with parseDate (e.g. {{parseDate "DD/MM/YYYY" .trigger.form.date}}).
- New flows are proposed inactive unless the user asked to enable them. To test a flow, propose_flow_run (it really runs the steps: records are written, emails sent) with a sample trigger.
- To explain a failure: flow_runs (the latest runs), then flow_runs with run_id for the step logs (input, output, error of each step).`

// flowRunner runs flows (the flows engine; nil when the module isn't there).
type flowRunner interface {
	Run(flow *core.Record, trigger map[string]any, opts flows.RunOptions) *flows.Result
}

func (t *toolbox) findFlow(id string) (*core.Record, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("flow_id is required")
	}
	if f, err := t.app.FindRecordById("flows", id); err == nil {
		return f, nil
	}
	if list, err := t.app.FindRecordsByFilter("flows", "name = {:n}", "", 1, 0, map[string]any{"n": id}); err == nil && len(list) == 1 {
		return list[0], nil
	}
	return nil, fmt.Errorf("unknown flow %q (use list_resources kind flows)", id)
}

// flowTrigger returns the trigger config without its secret token.
func flowTrigger(f *core.Record) map[string]any {
	cfg := map[string]any{}
	_ = f.UnmarshalJSONField("trigger", &cfg)
	if _, ok := cfg["token"]; ok {
		cfg["token"] = hiddenToken
	}
	return cfg
}

func flowSteps(f *core.Record) []any {
	var steps []any
	_ = f.UnmarshalJSONField("steps", &steps)
	if steps == nil {
		steps = []any{}
	}
	return steps
}

func toolGetFlow(t *toolbox, args map[string]any) (any, error) {
	f, err := t.findFlow(argString(args, "flow_id"))
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id": f.Id, "name": f.GetString("name"), "description": f.GetString("description"), "active": f.GetBool("active"),
		"trigger_type": f.GetString("trigger_type"), "trigger": flowTrigger(f), "steps": flowSteps(f),
		"link": "#/flows/" + f.Id,
	}
	if f.GetString("trigger_type") == flows.TriggerWebhook {
		out["webhook_url"] = strings.TrimRight(t.app.Settings().Meta.AppURL, "/") + "/api/flows/webhook/" + f.Id + "?token=" + hiddenToken
	}
	if runs, err := t.app.FindRecordsByFilter("flow_runs", "flow = {:f}", "-created", 5, 0, map[string]any{"f": f.Id}); err == nil {
		recent := []map[string]any{}
		for _, r := range runs {
			recent = append(recent, runSummary(r))
		}
		out["recent_runs"] = recent
	}
	return out, nil
}

func runSummary(r *core.Record) map[string]any {
	m := map[string]any{"run_id": r.Id, "status": r.GetString("status"), "created": r.GetString("created"), "duration_ms": r.GetInt("duration_ms")}
	if e := r.GetString("error"); e != "" {
		m["error"] = limitText(e, 400)
	}
	return m
}

func toolFlowRuns(t *toolbox, args map[string]any) (any, error) {
	if id := argString(args, "run_id"); id != "" {
		r, err := t.app.FindRecordById("flow_runs", id)
		if err != nil {
			return nil, fmt.Errorf("unknown run %q", id)
		}
		var input, output, steps any
		_ = r.UnmarshalJSONField("input", &input)
		_ = r.UnmarshalJSONField("output", &output)
		_ = r.UnmarshalJSONField("steps", &steps)
		out := runSummary(r)
		out["flow"] = r.GetString("flow")
		out["trigger"] = cutValues(input, 0)
		out["steps"] = cutValues(steps, 0)
		out["output"] = cutValues(output, 0)
		return out, nil
	}
	f, err := t.findFlow(argString(args, "flow_id"))
	if err != nil {
		return nil, err
	}
	filter := "flow = {:f}"
	params := map[string]any{"f": f.Id}
	if s := argString(args, "status"); s != "" {
		filter += " && status = {:s}"
		params["s"] = s
	}
	runs, err := t.app.FindRecordsByFilter("flow_runs", filter, "-created", min(max(argInt(args, "limit", 10), 1), maxRuns), 0, params)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, r := range runs {
		out = append(out, runSummary(r))
	}
	return map[string]any{"flow": f.GetString("name"), "runs": out}, nil
}

// cutValues shortens the long texts and lists of logged data for the model.
func cutValues(v any, depth int) any {
	switch val := v.(type) {
	case string:
		return limitText(val, maxTextInResult)
	case map[string]any:
		if depth > 6 {
			return "…"
		}
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = cutValues(item, depth+1)
		}
		return out
	case []any:
		if depth > 6 {
			return "…"
		}
		n := min(len(val), 20)
		out := make([]any, 0, n+1)
		for _, item := range val[:n] {
			out = append(out, cutValues(item, depth+1))
		}
		if len(val) > n {
			out = append(out, fmt.Sprintf("… %d more", len(val)-n))
		}
		return out
	}
	return v
}

// --- proposals ---------------------------------------------------------------------

// draftFlow builds the flow record of a proposal (a new one, or a copy of the
// flow with the changes) and validates it as the flow editor's save does.
func (t *toolbox) draftFlow(args map[string]any) (draft *core.Record, existing *core.Record, err error) {
	col, err := t.app.FindCollectionByNameOrId("flows")
	if err != nil {
		return nil, nil, errors.New("the flows module isn't installed")
	}
	if id := argString(args, "flow_id"); id != "" {
		if existing, err = t.findFlow(id); err != nil {
			return nil, nil, err
		}
		draft = existing.Fresh()
	} else {
		draft = core.NewRecord(col)
		draft.Set("active", false)
		draft.Set("trigger", map[string]any{})
		draft.Set("steps", []any{})
		if argString(args, "name") == "" || argString(args, "trigger_type") == "" {
			return nil, nil, errors.New("a new flow needs a name and a trigger_type")
		}
	}
	for _, k := range []string{"name", "description", "trigger_type"} {
		if v := argString(args, k); v != "" {
			draft.Set(k, v)
		}
	}
	if v, ok := args["active"].(bool); ok {
		draft.Set("active", v)
	}
	if trig, ok := args["trigger"].(map[string]any); ok {
		// the secret token stays (the model never sees it)
		if existing != nil {
			old := map[string]any{}
			_ = existing.UnmarshalJSONField("trigger", &old)
			if tok, ok := trig["token"]; !ok || tok == hiddenToken || tok == "" {
				if old["token"] != nil {
					trig["token"] = old["token"]
				} else {
					delete(trig, "token")
				}
			}
		} else if trig["token"] == hiddenToken {
			delete(trig, "token")
		}
		draft.Set("trigger", trig)
	}
	if steps, ok := args["steps"].([]any); ok {
		draft.Set("steps", steps)
	}
	if err := t.app.Validate(draft); err != nil {
		return nil, nil, fmt.Errorf("invalid flow: %w", err)
	}
	return draft, existing, nil
}

func toolProposeFlow(t *toolbox, args map[string]any) (any, error) {
	draft, existing, err := t.draftFlow(args)
	if err != nil {
		return nil, err
	}
	trigger := map[string]any{}
	_ = draft.UnmarshalJSONField("trigger", &trigger)
	steps := flowSteps(draft)
	payload := map[string]any{
		"name": draft.GetString("name"), "description": draft.GetString("description"), "active": draft.GetBool("active"),
		"trigger_type": draft.GetString("trigger_type"), "trigger": trigger, "steps": steps,
	}
	title := "New flow “" + draft.GetString("name") + "”"
	summary := fmt.Sprintf("%s, %d steps, %s", describeTrigger(draft.GetString("trigger_type"), trigger), countSteps(steps), activeLabel(draft.GetBool("active")))
	if existing != nil {
		payload["flow_id"] = existing.Id
		payload["updated"] = existing.GetString("updated")
		title = "Change the flow “" + existing.GetString("name") + "”"
		if c := argString(args, "change"); c != "" {
			summary = c + " · " + summary
		}
	}
	shownTrigger := flowTrigger(draft)
	return t.propose(&Action{
		Type:    ActionFlow,
		Title:   title,
		Summary: summary,
		Details: map[string]any{"trigger": describeTrigger(draft.GetString("trigger_type"), shownTrigger), "outline": outline(steps, 0), "active": draft.GetBool("active")},
		Payload: payload,
	}), nil
}

func activeLabel(active bool) string {
	if active {
		return "active"
	}
	return "inactive (enable it in the flow)"
}

func describeTrigger(typ string, cfg map[string]any) string {
	switch typ {
	case flows.TriggerRecord:
		events := []string{}
		if list, ok := cfg["events"].([]any); ok {
			for _, e := range list {
				events = append(events, fmt.Sprint(e))
			}
		}
		timing := "after"
		if cfg["timing"] == "before" {
			timing = "before"
		}
		s := fmt.Sprintf("%s %s in %v", timing, strings.Join(events, "/"), cfg["collection"])
		if f, _ := cfg["filter"].(string); f != "" {
			s += " (" + f + ")"
		}
		return s
	case flows.TriggerManual:
		if c, _ := cfg["collection"].(string); c != "" {
			return "button on " + c + " records"
		}
		return "run manually"
	case flows.TriggerWebhook:
		return "webhook"
	case flows.TriggerSchedule:
		return fmt.Sprintf("schedule %v (UTC)", cfg["cron"])
	case flows.TriggerAction:
		return fmt.Sprintf("page action (%v)", cfg["access"])
	}
	return "called by other flows"
}

func countSteps(steps []any) int {
	n := 0
	for _, raw := range steps {
		s, _ := raw.(map[string]any)
		n++
		for _, k := range []string{"then", "else", "steps"} {
			if nested, ok := s[k].([]any); ok {
				n += countSteps(nested)
			}
		}
	}
	return n
}

// outline lists the steps for the card: "condition (big): then: …".
func outline(steps []any, depth int) []string {
	var out []string
	for _, raw := range steps {
		s, _ := raw.(map[string]any)
		label := fmt.Sprint(s["type"])
		if name, _ := s["name"].(string); name != "" {
			label = name + " · " + label
		}
		out = append(out, strings.Repeat("    ", depth)+label+" ("+fmt.Sprint(s["key"])+")")
		for _, k := range []string{"then", "else", "steps"} {
			if nested, ok := s[k].([]any); ok && len(nested) > 0 {
				name := map[string]string{"then": "if yes", "else": "if no", "steps": "for each item"}[k]
				out = append(out, strings.Repeat("    ", depth+1)+name+":")
				out = append(out, outline(nested, depth+2)...)
			}
		}
	}
	return out
}

func toolProposeFlowRun(t *toolbox, args map[string]any) (any, error) {
	if t.a.flows == nil {
		return nil, errors.New("the flows module isn't installed")
	}
	f, err := t.findFlow(argString(args, "flow_id"))
	if err != nil {
		return nil, err
	}
	trigger, _ := args["trigger"].(map[string]any)
	if trigger == nil {
		trigger = map[string]any{}
	}
	// a record of a record or manual flow, by its id
	if id := argString(args, "record_id"); id != "" {
		cfg := flowTrigger(f)
		name, _ := cfg["collection"].(string)
		if c := argString(args, "collection"); c != "" {
			name = c
		}
		rec, err := t.app.FindRecordById(name, id)
		if err != nil {
			return nil, fmt.Errorf("record %q not found in %q", id, name)
		}
		trigger["collection"] = rec.Collection().Name
		trigger["record"] = rec.PublicExport()
	}
	raw, _ := json.Marshal(trigger)
	if len(raw) > 64<<10 {
		return nil, errors.New("the trigger is too large")
	}
	return t.propose(&Action{
		Type:    ActionFlowRun,
		Title:   "Test run of “" + f.GetString("name") + "”",
		Summary: "Runs the flow now with this trigger: its steps really run (records written, emails sent).",
		Details: map[string]any{"trigger": string(raw)},
		Payload: map[string]any{"flow_id": f.Id, "trigger": trigger},
	}), nil
}

// --- apply ---------------------------------------------------------------------

func applyFlow(ctx context.Context, app core.App, p map[string]any) (map[string]any, error) {
	col, err := app.FindCollectionByNameOrId("flows")
	if err != nil {
		return nil, err
	}
	var rec *core.Record
	if id, _ := p["flow_id"].(string); id != "" {
		if rec, err = app.FindRecordById(col, id); err != nil {
			return nil, errors.New("the flow doesn't exist anymore")
		}
		if u, _ := p["updated"].(string); u != "" && rec.GetString("updated") != u {
			return nil, errors.New("the flow was changed since this proposal: ask for a new one")
		}
	} else {
		rec = core.NewRecord(col)
	}
	for _, k := range []string{"name", "description", "active", "trigger_type", "trigger", "steps"} {
		rec.Set(k, p[k])
	}
	if err := app.SaveWithContext(ctx, rec); err != nil {
		return nil, err
	}
	return map[string]any{"link": "#/flows/" + rec.Id, "label": "Open the flow", "id": rec.Id}, nil
}

func (a *Assistant) applyFlowRun(ctx context.Context, app core.App, p map[string]any) (map[string]any, error) {
	if a.flows == nil {
		return nil, errors.New("the flows module isn't installed")
	}
	f, err := app.FindRecordById("flows", fmt.Sprint(p["flow_id"]))
	if err != nil {
		return nil, errors.New("the flow doesn't exist anymore")
	}
	trigger, _ := p["trigger"].(map[string]any)
	res := a.flows.Run(f, trigger, flows.RunOptions{Ctx: context.WithoutCancel(ctx), Timeout: 2 * time.Minute})
	out := map[string]any{"link": "#/flows/" + f.Id + "/runs", "label": "Open the runs", "run_id": res.RunId, "status": res.Status}
	if res.Error != "" {
		out["error"] = res.Error
	}
	return out, nil
}
