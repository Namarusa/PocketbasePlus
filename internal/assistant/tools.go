package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/dashboards"
	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
)

const (
	maxQueryLimit   = 50
	maxTextInResult = 300
)

// toolbox runs the tools of one chat request.
type toolbox struct {
	a    *Assistant
	app  core.App
	auth *core.Record
	ctx  context.Context
	conv *conversation
}

type toolFunc func(t *toolbox, args map[string]any) (any, error)

var toolFuncs = map[string]toolFunc{
	"describe_collection":    toolDescribeCollection,
	"query_records":          toolQueryRecords,
	"aggregate":              toolAggregate,
	"relative_date":          toolRelativeDate,
	"list_resources":         toolListResources,
	"preview_email":          toolPreviewEmail,
	"propose_dashboard":      toolProposeDashboard,
	"propose_email_template": toolProposeEmailTemplate,
	"propose_campaign":       toolProposeCampaign,
	"propose_record":         toolProposeRecord,
	"describe_functions":     toolDescribeFunctions,
	"get_flow":               toolGetFlow,
	"flow_runs":              toolFlowRuns,
	"propose_flow":           toolProposeFlow,
	"propose_flow_run":       toolProposeFlowRun,
}

// call runs a tool and returns its JSON result (errors included, so that
// the model can fix its arguments).
func (t *toolbox) call(name, rawArgs string) string {
	fn, ok := toolFuncs[name]
	if !ok {
		ext := extensionTool(name)
		if ext == nil {
			return toJSON(map[string]any{"error": "unknown tool " + name})
		}
		fn = func(t *toolbox, args map[string]any) (any, error) {
			return ext.Run(&ToolContext{App: t.app, Auth: t.auth, Ctx: t.ctx, t: t}, args)
		}
	}
	args := map[string]any{}
	if strings.TrimSpace(rawArgs) != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return toJSON(map[string]any{"error": "invalid JSON arguments: " + err.Error()})
		}
	}
	res, err := fn(t, args)
	if err != nil {
		return toJSON(map[string]any{"error": err.Error()})
	}
	out := toJSON(res)
	if len(out) > maxToolResult {
		out = out[:maxToolResult] + `… (truncated: ask for fewer records or fields)`
	}
	return out
}

// --- definitions ------------------------------------------------------------------

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func toolDefinitions() []ai.Tool {
	panel := obj(map[string]any{
		"type":    str("metric | time_series | bar | pie | meter | list | label"),
		"name":    str("Title shown in the panel header"),
		"options": map[string]any{"type": "object", "description": "Options of the panel type (see the panels guide)"},
	}, "type", "name", "options")
	template := obj(map[string]any{
		"name":    str("Template name"),
		"subject": str("Subject (Go template)"),
		"html":    str("HTML body (html/template, inline styles)"),
		"text":    str("Plain text version"),
	}, "name", "subject", "html")

	tools := []ai.Tool{
		{Name: "describe_collection", Description: "Fields of a collection (types, select values, relations), its back-relations, its record count and 3 recent records.",
			Parameters: obj(map[string]any{"collection": str("Collection name")}, "collection")},
		{Name: "query_records", Description: "Records of a collection matching a filter (max 50). Long texts are cut.",
			Parameters: obj(map[string]any{
				"collection": str("Collection name"),
				"filter":     str("PocketBase filter (optional)"),
				"sort":       str("e.g. -created (optional)"),
				"limit":      num("1 to 50 (default 20)"),
				"fields":     strList("Fields to return (default: all readable fields)"),
				"expand":     str("Relation fields to expand, comma separated (optional)"),
			}, "collection")},
		{Name: "aggregate", Description: "Count/sum/avg/min/max of records, optionally grouped by a field (group_by) or over time (date_field + precision). Use it for every number you report.",
			Parameters: obj(map[string]any{
				"collection": str("Collection name"),
				"fn":         str("count | count_distinct | sum | avg | min | max (default count)"),
				"field":      str("Field to aggregate (not for count)"),
				"filter":     str("PocketBase filter (optional)"),
				"group_by":   str("Field to group by (optional)"),
				"date_field": str("Date field for a time series (optional)"),
				"precision":  str("hour | day | week | month | year (time series)"),
				"range":      str("24h | 7d | 30d | 90d | 365d | empty for all (time series)"),
				"limit":      num("Max groups (default 10)"),
			}, "collection")},
		{Name: "relative_date", Description: "A UTC date relative to now, formatted for filters (e.g. 3 days ago at 00:00).",
			Parameters: obj(map[string]any{
				"days":         num("Days to add (negative = in the past), e.g. -3"),
				"months":       num("Months to add (optional)"),
				"start_of_day": boolean("Round to 00:00:00 UTC (default true)"),
			}, "days")},
		{Name: "list_resources", Description: "The dashboards, email templates, flows, AI models, pages, forms or saved API calls of the app (names and ids).",
			Parameters: obj(map[string]any{"kind": str("dashboards | email_templates | flows | ai_models | pages | forms | api_calls")}, "kind")},
		{Name: "describe_functions", Description: "Syntax, description and an example of template functions (by name, or a whole category).",
			Parameters: obj(map[string]any{
				"names":    strList("Function names, e.g. [\"formatDate\", \"parseDate\"]"),
				"category": str("Text | Number | Date | List | Logic | Other (optional)"),
			})},
		{Name: "get_flow", Description: "A flow: its trigger, its steps (full JSON, to change them) and its last runs.",
			Parameters: obj(map[string]any{"flow_id": str("Flow id or exact name")}, "flow_id")},
		{Name: "flow_runs", Description: "The latest runs of a flow (status, error), or with run_id the log of one run: the input, output and error of each step.",
			Parameters: obj(map[string]any{
				"flow_id": str("Flow id or name (for the list)"),
				"status":  str("success | failed | rejected | running (optional filter)"),
				"limit":   num("Runs to list (default 10, max 20)"),
				"run_id":  str("A run id: its step logs"),
			})},
		{Name: "propose_flow", Description: "Proposes a new flow, or changes to one (flow_id: give only what changes; steps replace all the steps). Validated like the flow editor's save. The user applies it.",
			Parameters: obj(map[string]any{
				"flow_id":      str("Existing flow to change (optional)"),
				"change":       str("What the change does, in a few words (changes)"),
				"name":         str("Flow name (required for a new flow)"),
				"description":  str("Short description"),
				"active":       boolean("Enabled (new flows: false unless the user asked)"),
				"trigger_type": str("record | manual | webhook | schedule | flow | action"),
				"trigger":      map[string]any{"type": "object", "description": "Trigger config (see the flows guide)"},
				"steps":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "The complete list of steps (see the flows guide)"},
			})},
		{Name: "propose_flow_run", Description: "Proposes a test run of a saved flow with a sample trigger (its steps really run). The user applies it; the result appears in the proposals.",
			Parameters: obj(map[string]any{
				"flow_id":    str("Flow id or name"),
				"trigger":    map[string]any{"type": "object", "description": "Trigger data, e.g. {\"payload\": {...}} or {\"body\": {...}} for webhooks, {\"form\": {...}} for page actions"},
				"record_id":  str("For record and manual flows: a record of the trigger collection, sent as trigger.record"),
				"collection": str("The record's collection, when not the trigger's"),
			}, "flow_id")},
		{Name: "preview_email", Description: "Renders an email template (an existing one, or a draft subject/html) with a real record to check it.",
			Parameters: obj(map[string]any{
				"template_id": str("Existing template id (or give subject/html)"),
				"subject":     str("Draft subject"),
				"html":        str("Draft HTML"),
				"text":        str("Draft text"),
				"collection":  str("Collection of the sample record (optional)"),
				"record_id":   str("Sample record id (default: the latest record)"),
			})},
		{Name: "propose_dashboard", Description: "Proposes a new dashboard (or panels added to an existing one). The panels are checked against the data first. The user applies it.",
			Parameters: obj(map[string]any{
				"name":         str("Dashboard name (new dashboard)"),
				"note":         str("Short description (optional)"),
				"dashboard_id": str("Existing dashboard to add the panels to (optional)"),
				"panels":       map[string]any{"type": "array", "items": panel, "description": "Panels, in reading order"},
			}, "panels")},
		{Name: "propose_email_template", Description: "Proposes a new email template. The user applies it.",
			Parameters: obj(map[string]any{
				"name":               str("Template name"),
				"subject":            str("Subject (Go template)"),
				"html":               str("HTML body"),
				"text":               str("Plain text version"),
				"marketing":          boolean("Marketing email (unsubscribe link, opt-out list)"),
				"sample_collection":  str("Collection whose records the template is about (preview)"),
				"folder":             str("Folder grouping it in the list (optional; reuse an existing one when it fits)"),
				"account_email":      str("Only to design an email PocketBase sends to accounts: verification | password_reset | email_change | otp | login_alert. Its variables: .record (the account), .link (the confirmation link: put it in a button) and .token, .new_email, .otp, .alert_info depending on the email"),
				"account_collection": str("account_email: the auth collection (empty: all of them but the superusers; \"*\": all of them, superusers included)"),
			}, "name", "subject", "html")},
		{Name: "propose_campaign", Description: "Proposes an email campaign: a marketing email sent once per address to recipients selected in a collection. Counts the recipients first. The user applies (sends) it.",
			Parameters: obj(map[string]any{
				"title":           str("Short campaign name"),
				"template_id":     str("Existing template id, or give template"),
				"template":        template,
				"collection":      str("Collection of the recipients (e.g. customers)"),
				"record_ids":      strList("Recipient record ids (or use filter)"),
				"filter":          str("Filter selecting the recipients (or use record_ids)"),
				"recipient_field": str("Address field: email, or customer.email"),
				"send_at":         str("RFC 3339 date to schedule (optional, default now)"),
			}, "title", "collection", "recipient_field")},
		{Name: "propose_record", Description: "Proposes a new record (e.g. a promo code). The user applies it.",
			Parameters: obj(map[string]any{
				"collection": str("Collection name"),
				"data":       map[string]any{"type": "object", "description": "Field values"},
				"title":      str("Short description of the record"),
			}, "collection", "data")},
	}
	for _, e := range sortedExtensions() {
		for _, tool := range e.Tools {
			tools = append(tools, tool.Def)
		}
	}
	return tools
}

// --- read tools ------------------------------------------------------------------

func (t *toolbox) collection(args map[string]any) (*core.Collection, error) {
	name := argString(args, "collection")
	if name == "" {
		return nil, errors.New("collection is required")
	}
	c, err := t.app.FindCachedCollectionByNameOrId(name)
	if err != nil || internalcols.Is(c) {
		return nil, fmt.Errorf("unknown collection %q", name)
	}
	return c, nil
}

func toolDescribeCollection(t *toolbox, args map[string]any) (any, error) {
	c, err := t.collection(args)
	if err != nil {
		return nil, err
	}
	fields := []map[string]any{}
	for _, f := range readableFields(c) {
		info := map[string]any{"name": f.GetName(), "type": f.Type(), "summary": fieldSummary(t.app, f)}
		if r, ok := f.(interface{ GetRequired() bool }); ok && r.GetRequired() {
			info["required"] = true
		}
		if p, ok := f.(interface{ GetPresentable() bool }); ok && p.GetPresentable() {
			info["presentable"] = true
		}
		fields = append(fields, info)
	}

	// relations pointing to this collection, usable as back-relations
	back := []string{}
	for _, other := range userCollections(t.app) {
		for _, f := range other.Fields {
			if rf, ok := f.(*core.RelationField); ok && rf.CollectionId == c.Id {
				back = append(back, fmt.Sprintf("%s_via_%s (%s records whose %s points here)", other.Name, rf.Name, other.Name, rf.Name))
			}
		}
	}

	out := map[string]any{"name": c.Name, "type": c.Type, "fields": fields, "back_relations": back}
	if !c.IsView() {
		n, _ := t.app.CountRecords(c.Name)
		out["records"] = n
	}
	sortBy := ""
	if c.Fields.GetByName("created") != nil {
		sortBy = "-created"
	}
	if recs, err := t.app.FindRecordsByFilter(c, "", sortBy, 3, 0); err == nil {
		out["samples"] = exportRecords(recs, nil)
	}
	return out, nil
}

func toolQueryRecords(t *toolbox, args map[string]any) (any, error) {
	c, err := t.collection(args)
	if err != nil {
		return nil, err
	}
	limit := argInt(args, "limit", 20)
	if limit <= 0 || limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	records, err := t.app.FindRecordsByFilter(c, argString(args, "filter"), argString(args, "sort"), limit+1, 0)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	if expand := argString(args, "expand"); expand != "" {
		if errs := t.app.ExpandRecords(records, strings.Split(expand, ","), nil); len(errs) > 0 {
			for k, e := range errs {
				return nil, fmt.Errorf("expand %s: %w", k, e)
			}
		}
	}
	return map[string]any{
		"items":    exportRecords(records, argStrings(args, "fields")),
		"returned": len(records),
		"has_more": hasMore,
	}, nil
}

func toolAggregate(t *toolbox, args map[string]any) (any, error) {
	c, err := t.collection(args)
	if err != nil {
		return nil, err
	}
	fn := argString(args, "fn")
	if fn == "" {
		fn = "count"
	}
	options := map[string]any{
		"collection": c.Name,
		"fn":         fn,
		"field":      argString(args, "field"),
		"filter":     argString(args, "filter"),
		"limit":      argInt(args, "limit", 10),
	}
	panelType := dashboards.TypeMetric
	switch {
	case argString(args, "date_field") != "":
		panelType = dashboards.TypeTimeSeries
		options["dateField"] = argString(args, "date_field")
		options["precision"] = argString(args, "precision")
		if options["precision"] == "" {
			options["precision"] = "day"
		}
		options["range"] = argString(args, "range")
		if g := argString(args, "group_by"); g != "" {
			options["splitField"] = g
		}
	case argString(args, "group_by") != "":
		panelType = dashboards.TypeBar
		options["groupField"] = argString(args, "group_by")
	}
	data, err := dashboards.RunPanel(t.ctx, t.app, t.auth, panelType, options)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func toolRelativeDate(t *toolbox, args map[string]any) (any, error) {
	d := time.Now().UTC().AddDate(0, argInt(args, "months", 0), argInt(args, "days", 0))
	if v, ok := args["start_of_day"].(bool); !ok || v {
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	}
	return map[string]any{"date": d.Format("2006-01-02 15:04:05.000Z"), "weekday": d.Weekday().String()}, nil
}

func toolListResources(t *toolbox, args map[string]any) (any, error) {
	switch argString(args, "kind") {
	case "dashboards":
		list, err := t.app.FindRecordsByFilter("dashboards", "", "name", 100, 0)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, d := range list {
			n, _ := t.app.CountRecords("dashboard_panels", dbx.HashExp{"dashboard": d.Id})
			out = append(out, map[string]any{"id": d.Id, "name": d.GetString("name"), "panels": n, "link": "#/dashboards/" + d.Id})
		}
		return out, nil
	case "email_templates":
		list, err := t.app.FindRecordsByFilter("email_templates", "", "name", 100, 0)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, r := range list {
			vars, _ := mail.TemplateVariables(r.GetString("subject"), r.GetString("html"), r.GetString("text"))
			out = append(out, map[string]any{
				"id": r.Id, "name": r.GetString("name"), "subject": r.GetString("subject"), "folder": r.GetString("folder"),
				"account_email": r.GetString("account_email"), "account_collection": r.GetString("account_collection"),
				"marketing": r.GetBool("marketing"), "variables": vars, "sample_collection": r.GetString("sample_collection"),
			})
		}
		return out, nil
	case "flows":
		list, err := t.app.FindRecordsByFilter("flows", "", "name", 100, 0)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, r := range list {
			out = append(out, map[string]any{"id": r.Id, "name": r.GetString("name"), "active": r.GetBool("active"), "trigger": r.GetString("trigger_type")})
		}
		return out, nil
	case "ai_models":
		list, err := t.app.FindRecordsByFilter("ai_models", "", "name", 100, 0)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, r := range list {
			out = append(out, map[string]any{"id": r.Id, "name": r.GetString("name"), "type": r.GetString("type")})
		}
		return out, nil
	case "pages":
		list, err := t.app.FindRecordsByFilter("pages", "", "title", 200, 0)
		if err != nil {
			return nil, errors.New("the Pages module isn't installed")
		}
		out := []map[string]any{}
		for _, r := range list {
			out = append(out, map[string]any{"id": r.Id, "title": r.GetString("title"), "slug": r.GetString("slug"), "kind": r.GetString("kind"),
				"published": r.GetBool("published"), "record_collection": r.GetString("record_collection"), "link": "#/pages/" + r.Id})
		}
		return out, nil
	case "forms":
		list, err := t.app.FindRecordsByFilter("forms", "", "name", 100, 0)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, r := range list {
			out = append(out, map[string]any{"id": r.Id, "name": r.GetString("name"), "slug": r.GetString("slug"), "collection": r.GetString("collection"), "active": r.GetBool("active")})
		}
		return out, nil
	case "api_calls":
		calls, err := t.app.FindRecordsByFilter("api_calls", "", "name", 200, 0)
		if err != nil {
			return nil, errors.New("the APIs module isn't installed")
		}
		out := []map[string]any{}
		for _, c := range calls {
			api := ""
			if src, err := t.app.FindRecordById("api_sources", c.GetString("source")); err == nil {
				api = src.GetString("name")
			}
			var vars, outputs any
			_ = c.UnmarshalJSONField("variables", &vars)
			_ = c.UnmarshalJSONField("outputs", &outputs)
			out = append(out, map[string]any{"id": c.Id, "api": api, "name": c.GetString("name"), "method": c.GetString("method"), "path": c.GetString("path"),
				"description": c.GetString("description"), "variables": vars, "outputs": outputs})
		}
		return out, nil
	}
	return nil, errors.New(`kind must be "dashboards", "email_templates", "flows", "ai_models", "pages", "forms" or "api_calls"`)
}

func toolDescribeFunctions(t *toolbox, args map[string]any) (any, error) {
	names := map[string]bool{}
	for _, n := range argStrings(args, "names") {
		names[n] = true
	}
	cat := argString(args, "category")
	out := []map[string]any{}
	for _, f := range formula.Catalog() {
		if names[f.Name] || (cat != "" && strings.EqualFold(f.Category, cat)) {
			out = append(out, map[string]any{"name": f.Name, "syntax": f.Syntax, "description": f.Description, "example": f.Example, "result": f.Result})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("give names or a category (Text, Number, Date, List, Logic, Other)")
	}
	return out, nil
}

var tagRegex = regexp.MustCompile(`(?s)<style.*?</style>|<[^>]+>`)

// htmlText is a rough text version of an HTML email (for the model).
func htmlText(html string) string {
	return limitText(strings.Join(strings.Fields(tagRegex.ReplaceAllString(html, " ")), " "), 2000)
}

func toolPreviewEmail(t *toolbox, args map[string]any) (any, error) {
	subject, html, text := argString(args, "subject"), argString(args, "html"), argString(args, "text")
	if id := argString(args, "template_id"); id != "" {
		tpl, err := t.app.FindRecordById("email_templates", id)
		if err != nil {
			return nil, errors.New("template not found")
		}
		subject, html, text = tpl.GetString("subject"), tpl.GetString("html"), tpl.GetString("text")
		if args["collection"] == nil {
			args["collection"] = tpl.GetString("sample_collection")
		}
	}
	record, err := t.sampleRecord(argString(args, "collection"), argString(args, "record_id"))
	if err != nil {
		return nil, err
	}
	out, err := mail.Render(subject, html, text, mail.TemplateData(t.app, record, nil, t.auth.Email()))
	if err != nil {
		return map[string]any{"error": "template error: " + err.Error()}, nil
	}
	res := map[string]any{"subject": out.Subject, "html_as_text": htmlText(out.HTML), "text": limitText(out.Text, 2000)}
	if record != nil {
		res["sample_record"] = record.Id
	}
	return res, nil
}

// sampleRecord returns a record to render templates with (nil without collection).
func (t *toolbox) sampleRecord(collection, id string) (*core.Record, error) {
	if collection == "" {
		return nil, nil
	}
	c, err := t.collection(map[string]any{"collection": collection})
	if err != nil {
		return nil, err
	}
	if id != "" {
		r, err := t.app.FindRecordById(c, id)
		if err != nil {
			return nil, fmt.Errorf("record %q not found in %s", id, c.Name)
		}
		return r, nil
	}
	sortBy := ""
	if c.Fields.GetByName("created") != nil {
		sortBy = "-created"
	}
	list, err := t.app.FindRecordsByFilter(c, "", sortBy, 1, 0)
	if err != nil || len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// --- helpers --------------------------------------------------------------------

// exportRecords exports records as plain data without hidden fields, with
// the long texts cut.
func exportRecords(records []*core.Record, fields []string) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, r := range records {
		data := r.PublicExport()
		delete(data, "collectionId")
		delete(data, "collectionName")
		if len(fields) > 0 {
			keep := map[string]any{"id": data["id"]}
			for _, f := range fields {
				if v, ok := data[f]; ok {
					keep[f] = v
				}
			}
			if e, ok := data["expand"]; ok {
				keep["expand"] = e
			}
			data = keep
		}
		out = append(out, shorten(data).(map[string]any))
	}
	return out
}

// shorten cuts the long texts of plain data (and expanded records).
func shorten(v any) any {
	switch val := v.(type) {
	case string:
		return limitText(val, maxTextInResult)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = shorten(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = shorten(item)
		}
		return out
	case *core.Record:
		data := val.PublicExport()
		delete(data, "collectionId")
		delete(data, "collectionName")
		return shorten(data)
	case []*core.Record:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = shorten(item)
		}
		return out
	}
	// other values (dates, numbers...) as plain JSON
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var plain any
	_ = json.Unmarshal(b, &plain)
	if s, ok := plain.(string); ok {
		return limitText(s, maxTextInResult)
	}
	return plain
}

func argString(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprint(v)
	case bool:
		return fmt.Sprint(v)
	}
	return ""
}

func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case string:
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}

func argStrings(args map[string]any, key string) []string {
	var out []string
	switch v := args[key].(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
