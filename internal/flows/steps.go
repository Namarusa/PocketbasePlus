package flows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/activity"
	"github.com/Namarusa/PocketbasePlus/internal/httpcall"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/rbac"
)

type stepHandler func(r *run, s Step) (input any, output any, err error)

// stepHandlers maps the step types to their implementation.
var stepHandlers map[string]stepHandler

func init() {
	stepHandlers = map[string]stepHandler{
		"condition":     stepCondition,
		"loop":          stepLoop,
		"get_records":   stepGetRecords,
		"create_record": stepCreateRecord,
		"update_record": stepUpdateRecord,
		"delete_record": stepDeleteRecord,
		"set_trigger":   stepSetTrigger,
		"send_email":    stepSendEmail,
		"http_request":  stepHTTPRequest,
		"api_call":      stepAPICall,
		"delay":         stepDelay,
		"set_variables": stepSetVariables,
		"script":        stepScript,
		"log":           stepLog,
		"run_flow":      stepRunFlow,
		"stop":          stepStop,
		"notify":        stepNotify,
		"ai_decision":   stepAIDecision,
		"ai_generate":   stepAIGenerate,
		"sign_in":       stepSignIn,
		"sign_out":      stepSignOut,
		"redirect":      stepRedirect,
	}
}

// resolved returns the step config with every value resolved.
func (r *run) resolved(s Step, keys ...string) (map[string]any, error) {
	out := map[string]any{}
	for _, k := range keys {
		v, err := resolveValue(s.Config[k], r.data)
		if err != nil {
			return out, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// --- control flow -------------------------------------------------------------

var conditionOps = map[string]bool{
	"eq": true, "neq": true, "contains": true, "not_contains": true, "starts": true, "ends": true,
	"gt": true, "gte": true, "lt": true, "lte": true, "empty": true, "not_empty": true,
}

func stepCondition(r *run, s Step) (any, any, error) {
	match := asString(s.Config, "match")
	rules, _ := s.Config["rules"].([]any)

	evaluated := []any{}
	result := match != "any" // all: true until a rule fails; any: false until one passes
	if len(rules) == 0 {
		result = true
	}
	for _, raw := range rules {
		rule := asMap(raw)
		left, err := resolveValue(rule["left"], r.data)
		if err != nil {
			return evaluated, nil, err
		}
		right, err := resolveValue(rule["right"], r.data)
		if err != nil {
			return evaluated, nil, err
		}
		op := asString(rule, "op")
		ok, err := compare(left, op, right)
		if err != nil {
			return evaluated, nil, err
		}
		evaluated = append(evaluated, map[string]any{"left": left, "op": op, "right": right, "result": ok})
		if match == "any" && ok {
			result = true
		}
		if match != "any" && !ok {
			result = false
		}
	}

	branch := s.Else
	if result {
		branch = s.Then
	}
	output := map[string]any{"result": result}
	if err := r.runNested(branch); err != nil {
		return evaluated, output, err
	}
	return evaluated, output, nil
}

func compare(left any, op string, right any) (bool, error) {
	if !conditionOps[op] {
		return false, fmt.Errorf("unknown operator %q", op)
	}
	ls, rs := toString(left), toString(right)
	switch op {
	case "empty":
		return isEmpty(left), nil
	case "not_empty":
		return !isEmpty(left), nil
	case "contains":
		if list, ok := left.([]any); ok {
			for _, item := range list {
				if toString(item) == rs {
					return true, nil
				}
			}
			return false, nil
		}
		return strings.Contains(ls, rs), nil
	case "not_contains":
		ok, err := compare(left, "contains", right)
		return !ok, err
	case "starts":
		return strings.HasPrefix(ls, rs), nil
	case "ends":
		return strings.HasSuffix(ls, rs), nil
	}

	lf, lerr := strconv.ParseFloat(strings.TrimSpace(ls), 64)
	rf, rerr := strconv.ParseFloat(strings.TrimSpace(rs), 64)
	numeric := lerr == nil && rerr == nil
	switch op {
	case "eq":
		if numeric {
			return lf == rf, nil
		}
		return ls == rs, nil
	case "neq":
		if numeric {
			return lf != rf, nil
		}
		return ls != rs, nil
	}
	if !numeric {
		// dates and other strings compare lexicographically (ISO dates sort correctly)
		c := strings.Compare(ls, rs)
		switch op {
		case "gt":
			return c > 0, nil
		case "gte":
			return c >= 0, nil
		case "lt":
			return c < 0, nil
		default:
			return c <= 0, nil
		}
	}
	switch op {
	case "gt":
		return lf > rf, nil
	case "gte":
		return lf >= rf, nil
	case "lt":
		return lf < rf, nil
	default:
		return lf <= rf, nil
	}
}

func isEmpty(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	case bool:
		return !val
	case float64:
		return val == 0
	}
	return false
}

func stepLoop(r *run, s Step) (any, any, error) {
	items, err := resolveValue(s.Config["items"], r.data)
	if err != nil {
		return nil, nil, err
	}
	list, ok := items.([]any)
	if !ok {
		switch items.(type) {
		case nil:
			list = []any{}
		case string:
			if items != "" {
				return items, nil, fmt.Errorf("items must be a list, got a text")
			}
			list = []any{}
		case map[string]any:
			list = []any{items} // one object (e.g. an XML element that isn't repeated)
		default:
			return items, nil, fmt.Errorf("items must be a list, got %T", items)
		}
	}
	max := asInt(s.Config, "max", 1000)
	if max <= 0 || max > 10000 {
		max = 10000
	}
	if len(list) > max {
		list = list[:max]
	}

	r.sample.loop(s.Key, list)

	prevItem, prevIndex := r.data["item"], r.data["index"]
	defer func() {
		r.data["item"], r.data["index"] = prevItem, prevIndex
	}()

	for i, item := range list {
		r.data["item"] = item
		r.data["index"] = i
		if err := r.runNested(s.Steps); err != nil {
			return map[string]any{"count": len(list)}, map[string]any{"count": len(list), "failedAt": i}, err
		}
	}
	return map[string]any{"count": len(list)}, map[string]any{"count": len(list)}, nil
}

func stepStop(r *run, s Step) (any, any, error) {
	cfg, err := r.resolved(s, "outcome", "message")
	if err != nil {
		return nil, nil, err
	}
	if toString(cfg["outcome"]) == "reject" {
		msg := toString(cfg["message"])
		if msg == "" {
			msg = "Rejected by flow " + r.flow.GetString("name") + "."
		}
		return cfg, nil, &rejectError{message: msg}
	}
	return cfg, nil, errStop
}

func stepRunFlow(r *run, s Step) (any, any, error) {
	cfg, err := r.resolved(s, "flow", "payload")
	if err != nil {
		return nil, nil, err
	}
	target, err := r.app.FindRecordById(flowsCollection, toString(cfg["flow"]))
	if err != nil {
		return cfg, nil, errors.New("flow not found")
	}
	if target.Id == r.flow.Id {
		return cfg, nil, errors.New("a flow can't run itself")
	}
	res := r.engine.Run(target, map[string]any{"payload": cfg["payload"]}, RunOptions{App: r.app, Ctx: r.ctx, Timeout: syncTimeout * 4})
	out := map[string]any{"status": res.Status, "output": res.Output, "runId": res.RunId}
	if res.Status != StatusSuccess {
		return cfg, out, fmt.Errorf("flow %q %s: %s", target.GetString("name"), res.Status, res.Error)
	}
	return cfg, out, nil
}

// --- data -----------------------------------------------------------------------

func (r *run) collection(name string) (*core.Collection, error) {
	c, err := r.app.FindCollectionByNameOrId(name)
	if err != nil {
		return nil, fmt.Errorf("collection %q not found", name)
	}
	return c, nil
}

// writableCollection is like collection but rejects read-only view collections.
func (r *run) writableCollection(name string) (*core.Collection, error) {
	c, err := r.collection(name)
	if err == nil && c.IsView() {
		return nil, fmt.Errorf("collection %q is a read-only view", name)
	}
	return c, err
}

func stepGetRecords(r *run, s Step) (any, any, error) {
	col, err := r.collection(asString(s.Config, "collection"))
	if err != nil {
		return nil, nil, err
	}
	filter, params, err := bindFilter(asString(s.Config, "filter"), r.data)
	if err != nil {
		return nil, nil, err
	}
	limit := asInt(s.Config, "limit", 50)
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	sort := asString(s.Config, "sort")
	input := map[string]any{"collection": col.Name, "filter": filter, "params": params, "sort": sort, "limit": limit}

	if r.asVisitor(s) {
		input["asVisitor"] = true
		list, err := r.findAsVisitor(col, filter, sort, limit, params)
		if err != nil {
			return input, nil, err
		}
		items := make([]any, 0, len(list))
		for _, it := range list {
			items = append(items, it)
		}
		var first any
		if len(items) > 0 {
			first = items[0]
		}
		return input, map[string]any{"items": items, "count": len(items), "first": first}, nil
	}

	records, err := r.app.FindRecordsByFilter(col, filter, sort, limit, 0, params)
	if err != nil {
		return input, nil, err
	}
	items := make([]any, 0, len(records))
	for _, rec := range records {
		items = append(items, recordData(rec))
	}
	var first any
	if len(items) > 0 {
		first = items[0]
	}
	return input, map[string]any{"items": items, "count": len(items), "first": first}, nil
}

func (r *run) recordFields(s Step) (map[string]any, error) {
	v, err := resolveValue(s.Config["data"], r.data)
	if err != nil {
		return nil, err
	}
	fields := asMap(v)
	if fields == nil {
		fields = map[string]any{}
	}
	return fields, nil
}

// checkDates refuses the values of date fields that PocketBase can't read:
// it would store them empty, silently. It reads its own format
// (2026-10-01 09:30:00.000Z) and ISO 8601; other texts need parseDate.
func checkDates(col *core.Collection, fields map[string]any) error {
	for name, v := range fields {
		switch col.Fields.GetByName(name).(type) {
		case *core.DateField, *core.AutodateField:
		default:
			continue
		}
		if text, isText := v.(string); v == nil || isText && strings.TrimSpace(text) == "" {
			continue // clears the field
		}
		if d, err := types.ParseDateTime(v); err == nil && !d.IsZero() {
			continue
		}
		return fmt.Errorf(`%s: %q isn't a date the field can store (2026-10-01 09:30:00.000Z or ISO 8601): convert it with parseDate, e.g. {{parseDate "DD/MM/YYYY" .trigger.form.date}}`, name, toString(v))
	}
	return nil
}

func stepCreateRecord(r *run, s Step) (any, any, error) {
	col, err := r.writableCollection(asString(s.Config, "collection"))
	if err != nil {
		return nil, nil, err
	}
	fields, err := r.recordFields(s)
	if err != nil {
		return nil, nil, err
	}
	if err := checkDates(col, fields); err != nil {
		return fields, nil, err
	}
	rec := core.NewRecord(col)
	rec.Load(fields)
	if pass, ok := fields["password"].(string); ok && col.IsAuth() {
		rec.SetPassword(pass)
	}
	if r.asVisitor(s) {
		// as the REST API: the field permissions of the roles, then the create
		// rule checked on the new record (undone when it doesn't pass)
		info := r.visitorInfo(fields)
		if err := rbac.CheckWrite(r.app, rec, info.Auth, "create", fieldKeys(fields)); err != nil {
			return fields, nil, &rejectError{message: err.Error()}
		}
		err := r.app.RunInTransaction(func(tx core.App) error {
			if err := tx.SaveWithContext(r.ctx, rec); err != nil {
				return err
			}
			if col.CreateRule == nil {
				return errNoAccess
			}
			if ok, err := tx.CanAccessRecord(rec, info, col.CreateRule); err != nil || !ok {
				return errNoAccess
			}
			return nil
		})
		if err != nil {
			return fields, nil, err
		}
		return fields, r.visibleData(rec, info), nil
	}
	if err := r.app.SaveWithContext(r.ctx, rec); err != nil {
		return fields, nil, err
	}
	return fields, recordData(rec), nil
}

func stepUpdateRecord(r *run, s Step) (any, any, error) {
	col, err := r.writableCollection(asString(s.Config, "collection"))
	if err != nil {
		return nil, nil, err
	}
	id, err := resolveString(s.Config["recordId"], r.data)
	if err != nil {
		return nil, nil, err
	}
	fields, err := r.recordFields(s)
	if err != nil {
		return nil, nil, err
	}
	input := map[string]any{"collection": col.Name, "recordId": id, "data": fields}
	if err := checkDates(col, fields); err != nil {
		return input, nil, err
	}
	rec, err := r.app.FindRecordById(col, id)
	if err != nil {
		return input, nil, fmt.Errorf("record %q not found", id)
	}
	if r.asVisitor(s) {
		info := r.visitorInfo(fields)
		if !r.allowedBy(rec, info, col.UpdateRule) {
			return input, nil, errNoAccess
		}
		rec.Load(fields)
		if err := rbac.CheckWrite(r.app, rec, info.Auth, "update", fieldKeys(fields)); err != nil {
			return input, nil, &rejectError{message: err.Error()}
		}
		if err := r.app.SaveWithContext(r.ctx, rec); err != nil {
			return input, nil, err
		}
		return input, r.visibleData(rec, info), nil
	}
	rec.Load(fields)
	if err := r.app.SaveWithContext(r.ctx, rec); err != nil {
		return input, nil, err
	}
	return input, recordData(rec), nil
}

func stepDeleteRecord(r *run, s Step) (any, any, error) {
	col, err := r.writableCollection(asString(s.Config, "collection"))
	if err != nil {
		return nil, nil, err
	}
	id, err := resolveString(s.Config["recordId"], r.data)
	if err != nil {
		return nil, nil, err
	}
	input := map[string]any{"collection": col.Name, "recordId": id}
	rec, err := r.app.FindRecordById(col, id)
	if err != nil {
		return input, nil, fmt.Errorf("record %q not found", id)
	}
	if r.asVisitor(s) && !r.allowedBy(rec, r.visitorInfo(nil), col.DeleteRule) {
		return input, nil, errNoAccess
	}
	data := recordData(rec)
	if err := r.app.DeleteWithContext(r.ctx, rec); err != nil {
		return input, nil, err
	}
	return input, data, nil
}

// stepSetTrigger changes fields of the record being saved (before record flows).
func stepSetTrigger(r *run, s Step) (any, any, error) {
	if !r.before || r.triggerRecord == nil {
		return nil, nil, errors.New(`"Update triggering record" is only available in "before save" record flows`)
	}
	fields, err := r.recordFields(s)
	if err != nil {
		return nil, nil, err
	}
	if err := checkDates(r.triggerRecord.Collection(), fields); err != nil {
		return fields, nil, err
	}
	r.triggerRecord.Load(fields)
	data := recordData(r.triggerRecord)
	if trig := asMap(r.data["trigger"]); trig != nil {
		trig["record"] = data
	}
	return fields, data, nil
}

// --- integrations -------------------------------------------------------------

func stepSendEmail(r *run, s Step) (any, any, error) {
	tplId := asString(s.Config, "template")
	tpl, err := r.app.FindRecordById("email_templates", tplId)
	if err != nil {
		return nil, nil, fmt.Errorf("email template %q not found", tplId)
	}
	to, err := resolveString(s.Config["to"], r.data)
	if err != nil {
		return nil, nil, err
	}
	recipients := mail.ParseAddresses(to)
	if len(recipients) == 0 {
		return map[string]any{"to": to}, nil, errors.New("no valid recipient address")
	}
	if r.action != nil {
		// page actions are started by visitors: the site can't be used to send many emails
		if len(recipients) > maxActionRecipients {
			return map[string]any{"to": to}, nil, &rejectError{message: "Too many recipients."}
		}
		if !allowActionEmails(r.action.ip, len(recipients)) {
			return map[string]any{"to": to}, nil, &rejectError{message: "Too many emails sent, try again later."}
		}
	}

	data := map[string]any{}
	for path, expr := range asMap(s.Config["variables"]) {
		v, err := resolveValue(expr, r.data)
		if err != nil {
			return nil, nil, fmt.Errorf("variable %s: %w", path, err)
		}
		setPath(data, path, v)
	}

	var sendAt time.Time
	if delay := asInt(s.Config, "delayMinutes", 0); delay > 0 {
		sendAt = time.Now().Add(time.Duration(delay) * time.Minute)
	}

	source := "Flow: " + r.flow.GetString("name")
	ids := []string{}
	for _, addr := range recipients {
		msg, err := mail.EnqueueData(r.app, tpl, addr, data, sendAt, source)
		if err != nil {
			return data, nil, err
		}
		if msg.GetString("status") == mail.StatusFailed {
			return data, nil, errors.New(msg.GetString("error"))
		}
		ids = append(ids, msg.Id)
	}
	if r.engine.mail != nil {
		r.engine.mail.Kick()
	}
	return map[string]any{"template": tpl.GetString("name"), "to": recipients, "variables": data},
		map[string]any{"queued": len(ids), "messageIds": ids, "to": recipients}, nil
}

// AllowPrivateHTTPEnv set to "1" or "true" lets the HTTP requests reach
// loopback, private and link-local addresses (e.g. services on the same network).
const AllowPrivateHTTPEnv = httpcall.AllowPrivateEnv

// httpLimit refuses the requests of a page action when the visitor made too many.
func (r *run) httpLimit() error {
	if r.action != nil && !allowActionHTTP(r.action.ip) {
		return &rejectError{message: "Too many requests, try again later."}
	}
	return nil
}

func stepHTTPRequest(r *run, s Step) (any, any, error) {
	cfg, err := r.resolved(s, "method", "headers", "query")
	if err != nil {
		return nil, nil, err
	}
	// trigger values are escaped for their place in the URL and a JSON body
	if cfg["url"], err = renderURL(s.Config["url"], r.data); err != nil {
		return cfg, nil, fmt.Errorf("url: %w", err)
	}
	if cfg["body"], err = renderJSONBody(s.Config["body"], r.data); err != nil {
		return cfg, nil, fmt.Errorf("body: %w", err)
	}
	if err := r.httpLimit(); err != nil {
		return cfg, nil, err
	}

	req := httpcall.Request{
		Method:  toString(cfg["method"]),
		URL:     toString(cfg["url"]),
		Headers: map[string]string{},
		Timeout: time.Duration(asInt(s.Config, "timeoutSeconds", 15)) * time.Second,
		Retries: asInt(s.Config, "retries", 0),
		// flows saved before the formats have none: JSON or text, as then
		Format: asString(s.Config, "responseFormat"),
	}
	for k, v := range asMap(cfg["headers"]) {
		req.Headers[k] = toString(v)
	}
	if q := asMap(cfg["query"]); len(q) > 0 {
		req.Query = url.Values{}
		for k, v := range q {
			req.Query.Set(k, toString(v))
		}
	}
	if body := toString(cfg["body"]); body != "" {
		req.Body = []byte(body)
		req.ContentType = "text/plain; charset=utf-8"
		if json.Valid(req.Body) {
			req.ContentType = "application/json"
		}
	}

	res, err := httpcall.Do(r.ctx, req)
	if res == nil {
		return cfg, nil, err
	}
	output := res.Output()
	if err != nil {
		return cfg, output, err
	}
	failOnError := true
	if v, ok := s.Config["failOnError"].(bool); ok {
		failOnError = v
	}
	if failOnError && !res.OK() {
		return cfg, output, fmt.Errorf("request failed with status %d", res.Status)
	}
	return cfg, output, nil
}

// --- saved API calls (optional module internal/apisources) -------------------

// APICaller runs a saved API call with its variables. It returns the logged
// input and the output ({status, ok, format, headers, body, raw, data}).
type APICaller struct {
	Call func(ctx context.Context, app core.App, callID string, vars map[string]any) (input any, output map[string]any, err error)
	// Variables returns the variables of a call (an error when it doesn't exist).
	Variables func(app core.App, callID string) ([]string, error)
}

var apiCaller *APICaller

// SetAPICaller installs the saved API calls (the "API call" step).
func SetAPICaller(c *APICaller) { apiCaller = c }

func stepAPICall(r *run, s Step) (any, any, error) {
	if apiCaller == nil {
		return nil, nil, errors.New("the APIs module isn't installed")
	}
	vars, err := resolveValue(asMap(s.Config["vars"]), r.data)
	if err != nil {
		return nil, nil, fmt.Errorf("variables: %w", err)
	}
	if err := r.httpLimit(); err != nil {
		return nil, nil, err // (the values may hold a token: the call hides them)
	}
	input, output, err := apiCaller.Call(r.ctx, r.app, asString(s.Config, "call"), asMap(vars))
	if err != nil {
		return input, output, err
	}
	failOnError := true
	if v, ok := s.Config["failOnError"].(bool); ok {
		failOnError = v
	}
	if ok, _ := output["ok"].(bool); failOnError && !ok {
		return input, output, fmt.Errorf("the API answered with status %v", output["status"])
	}
	return input, output, nil
}

// validateAPICallStep checks the call and the variables of an "API call" step.
func validateAPICallStep(app core.App, s *Step) error {
	if apiCaller == nil {
		return errors.New("the APIs module isn't installed")
	}
	names, err := apiCaller.Variables(app, asString(s.Config, "call"))
	if err != nil {
		return err
	}
	for k := range asMap(s.Config["vars"]) {
		if !slices.Contains(names, k) {
			return fmt.Errorf("the call has no variable %q", k)
		}
	}
	return nil
}

func stepDelay(r *run, s Step) (any, any, error) {
	seconds := asInt(s.Config, "seconds", 0)
	if r.before && seconds > 0 {
		return nil, nil, errors.New("delays are not allowed in \"before save\" flows")
	}
	if seconds < 0 || seconds > 600 {
		return nil, nil, errors.New("the delay must be between 0 and 600 seconds (use an email delay or a schedule for longer waits)")
	}
	select {
	case <-time.After(time.Duration(seconds) * time.Second):
	case <-r.ctx.Done():
		return nil, nil, r.ctx.Err()
	}
	return map[string]any{"seconds": seconds}, map[string]any{"waited": seconds}, nil
}

func stepSetVariables(r *run, s Step) (any, any, error) {
	v, err := resolveValue(s.Config["vars"], r.data)
	if err != nil {
		return nil, nil, err
	}
	vars := asMap(v)
	if vars == nil {
		vars = map[string]any{}
	}
	return nil, vars, nil
}

// stepNotify sends an in-app notification to superusers ("all" or emails).
func stepNotify(r *run, s Step) (any, any, error) {
	cfg, err := r.resolved(s, "to", "subject", "message", "link")
	if err != nil {
		return nil, nil, err
	}
	to := strings.FieldsFunc(toString(cfg["to"]), func(c rune) bool { return c == ',' || c == ';' || c == '\n' || c == ' ' })
	if len(to) == 0 {
		to = []string{"all"}
	}
	subject := toString(cfg["subject"])
	if strings.TrimSpace(subject) == "" {
		return cfg, nil, errors.New("the subject is required")
	}
	n, err := activity.NotifySuperusers(r.app, to, "flow", subject, toString(cfg["message"]), toString(cfg["link"]))
	if err != nil {
		return cfg, nil, err
	}
	return cfg, map[string]any{"notified": n}, nil
}

func stepLog(r *run, s Step) (any, any, error) {
	msg, err := resolveString(s.Config["message"], r.data)
	if err != nil {
		return nil, nil, err
	}
	r.app.Logger().Info("flow log: "+msg, "flow", r.flow.GetString("name"))
	return nil, map[string]any{"message": msg}, nil
}
