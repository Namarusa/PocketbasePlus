package flows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/secrets"
)

const cronJobPrefix = "flow_"

// isInternal reports whether the collection changes never trigger flows.
func isInternal(c *core.Collection) bool {
	return internalcols.Is(c)
}

var keyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Register binds the flows hooks, schedules and routes.
func Register(app core.App, mailEngine *mail.Engine) *Engine {
	e := newEngine(app, mailEngine)

	app.OnRecordValidate(flowsCollection).BindFunc(func(ev *core.RecordEvent) error {
		if err := validateFlow(ev.App, ev.Record); err != nil {
			return err
		}
		return ev.Next()
	})

	changed := func(ev *core.RecordEvent) error {
		e.invalidate()
		e.SyncSchedules()
		return ev.Next()
	}
	app.OnRecordAfterCreateSuccess(flowsCollection).BindFunc(changed)
	app.OnRecordAfterUpdateSuccess(flowsCollection).BindFunc(changed)
	app.OnRecordAfterDeleteSuccess(flowsCollection).BindFunc(changed)

	// "before" record flows: run inside the save and can modify or reject it
	before := func(event string) func(ev *core.RecordEvent) error {
		return func(ev *core.RecordEvent) error {
			if err := e.runBefore(ev, event); err != nil {
				return err
			}
			return ev.Next()
		}
	}
	app.OnRecordCreate().BindFunc(before("create"))
	app.OnRecordUpdate().BindFunc(before("update"))

	// "after" record flows: run in the background once saved
	app.OnRecordAfterCreateSuccess().BindFunc(func(ev *core.RecordEvent) error {
		e.runAfter(ev, "create", nil)
		return ev.Next()
	})
	app.OnRecordAfterUpdateSuccess().BindFunc(func(ev *core.RecordEvent) error {
		e.runAfter(ev, "update", nil)
		return ev.Next()
	})
	app.OnRecordDelete().BindFunc(func(ev *core.RecordEvent) error {
		if err := e.runBefore(ev, "delete"); err != nil {
			return err
		}
		// filters must be evaluated while the record still exists
		matched := e.matchAfter(ev.App, ev.Record, "delete")
		if err := ev.Next(); err != nil {
			return err
		}
		if len(matched) > 0 {
			e.runAfter(ev, "delete", matched)
		}
		return nil
	})

	// let the running flows write their run logs before the app stops
	app.OnTerminate().BindFunc(func(te *core.TerminateEvent) error {
		if !e.Wait(10 * time.Second) {
			te.App.Logger().Warn("flows: some runs were still in progress at shutdown")
		}
		return te.Next()
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		e.SyncSchedules()

		g := se.Router.Group("/api/flows")
		// public: protected by the flow secret token
		g.Route(http.MethodPost, "/webhook/{id}", e.webhook)
		g.Route(http.MethodGet, "/webhook/{id}", e.webhook)
		// the same with a readable address (webhook.go); one route per method,
		// next to the pages' GET /{path...}
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			se.Router.Route(m, "/hooks/{slug}", e.webhookBySlug)
		}
		// public: the buttons and forms of the pages (access set in the trigger)
		g.POST("/action/{id}", e.action)

		admin := g.Group("")
		admin.Bind(apis.RequireSuperuserAuth())
		admin.POST("/run/{id}", e.manualRun)
		admin.GET("/template-variables/{id}", templateVariables)
		admin.POST("/preview", e.preview)
		admin.GET("/sample/{id}", sampleOf)

		return se.Next()
	})

	return e
}

func triggerConfig(flow *core.Record) map[string]any {
	var cfg map[string]any
	_ = json.Unmarshal([]byte(flow.GetString("trigger")), &cfg)
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg
}

// recordFlows returns the active record flows for the collection, event and timing.
func (e *Engine) recordFlows(c *core.Collection, event, timing string) []*core.Record {
	if isInternal(c) {
		return nil
	}
	var result []*core.Record
	for _, f := range e.activeFlows() {
		if f.GetString("trigger_type") != TriggerRecord {
			continue
		}
		cfg := triggerConfig(f)
		t := asString(cfg, "timing")
		if t == "" {
			t = "after"
		}
		if t != timing || asString(cfg, "collection") != c.Name {
			continue
		}
		events, _ := cfg["events"].([]any)
		if !slices.ContainsFunc(events, func(v any) bool { return toString(v) == event }) {
			continue
		}
		result = append(result, f)
	}
	return result
}

func recordTrigger(event string, record *core.Record) map[string]any {
	t := map[string]any{
		"event":      event,
		"collection": record.Collection().Name,
		"record":     recordData(record),
	}
	if event == "update" {
		if orig := record.Original(); orig != nil {
			t["original"] = recordData(orig)
		}
	}
	return t
}

func (e *Engine) runBefore(ev *core.RecordEvent, event string) error {
	for _, f := range e.recordFlows(ev.Record.Collection(), event, "before") {
		res := e.Run(f, recordTrigger(event, ev.Record), RunOptions{
			App:           ev.App,
			Ctx:           ev.Context,
			TriggerRecord: ev.Record,
			Before:        true,
			Timeout:       syncTimeout,
		})
		switch res.Status {
		case StatusRejected:
			return apis.NewBadRequestError(res.Error, nil)
		case StatusFailed:
			return apis.NewBadRequestError(fmt.Sprintf("Flow %q failed: %s", f.GetString("name"), res.Error), nil)
		}
	}
	return nil
}

// matchAfter returns the "after" flows whose trigger filter matches the record.
func (e *Engine) matchAfter(app core.App, record *core.Record, event string) []*core.Record {
	var matched []*core.Record
	for _, f := range e.recordFlows(record.Collection(), event, "after") {
		filter := asString(triggerConfig(f), "filter")
		ok, err := matchesFilter(app, record, filter)
		if err != nil {
			app.Logger().Error("flows: invalid trigger filter", "flow", f.Id, "error", err)
			continue
		}
		if ok {
			matched = append(matched, f)
		}
	}
	return matched
}

func (e *Engine) runAfter(ev *core.RecordEvent, event string, matched []*core.Record) {
	if matched == nil {
		matched = e.matchAfter(ev.App, ev.Record, event)
	}
	if len(matched) == 0 {
		return
	}
	trigger := recordTrigger(event, ev.Record)
	for _, f := range matched {
		e.RunAsync(f, trigger, ev.Context)
	}
}

func matchesFilter(app core.App, record *core.Record, filter string) (bool, error) {
	if strings.TrimSpace(filter) == "" {
		return true, nil
	}
	_, err := app.FindFirstRecordByFilter(record.Collection(), "id = {:__id} && ("+filter+")", dbx.Params{"__id": record.Id})
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "no rows") {
		return false, nil
	}
	return false, err
}

// --- schedules ------------------------------------------------------------------

// SyncSchedules (re)registers a cron job for every active schedule flow.
func (e *Engine) SyncSchedules() {
	c := e.app.Cron()
	for _, job := range c.Jobs() {
		if strings.HasPrefix(job.Id(), cronJobPrefix) {
			c.Remove(job.Id())
		}
	}
	for _, f := range e.activeFlows() {
		if f.GetString("trigger_type") != TriggerSchedule {
			continue
		}
		id := f.Id
		expr := asString(triggerConfig(f), "cron")
		err := c.Add(cronJobPrefix+id, expr, func() {
			flow, err := e.app.FindRecordById(flowsCollection, id)
			if err != nil || !flow.GetBool("active") {
				return
			}
			e.RunAsync(flow, map[string]any{"time": time.Now().UTC().Format(time.RFC3339)}, nil)
		})
		if err != nil {
			e.app.Logger().Error("flows: invalid schedule", "flow", id, "error", err)
		}
	}
}

// --- routes ---------------------------------------------------------------------

type manualRunBody struct {
	Collection string         `json:"collection"`
	RecordIds  []string       `json:"recordIds"`
	Payload    map[string]any `json:"payload"`
	Trigger    map[string]any `json:"trigger"` // test runs of non manual flows
	Wait       bool           `json:"wait"`
	Sample     bool           `json:"sample"` // test run of the editor: keep its test data
}

// manualRun runs a flow from the dashboard: once per selected record for
// manual flows, or once with a custom trigger payload (test runs).
func (e *Engine) manualRun(re *core.RequestEvent) error {
	flow, err := re.App.FindRecordById(flowsCollection, re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError("Flow not found.", err)
	}
	var body manualRunBody
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}

	triggers := []map[string]any{}
	if body.Trigger != nil {
		triggers = append(triggers, body.Trigger)
	} else {
		base := map[string]any{"user": re.Auth.Email(), "payload": body.Payload}
		collection := body.Collection
		if collection == "" {
			collection = asString(triggerConfig(flow), "collection")
		}
		if len(body.RecordIds) > 0 {
			if len(body.RecordIds) > 1000 {
				return re.BadRequestError("Too many records (max 1000).", nil)
			}
			records, err := re.App.FindRecordsByIds(collection, body.RecordIds)
			if err != nil {
				return re.BadRequestError("Failed to load the records.", err)
			}
			for _, rec := range records {
				t := map[string]any{"collection": rec.Collection().Name, "record": recordData(rec)}
				for k, v := range base {
					t[k] = v
				}
				triggers = append(triggers, t)
			}
		} else {
			base["collection"] = collection
			triggers = append(triggers, base)
		}
	}

	if !body.Wait {
		for _, t := range triggers {
			e.RunAsync(flow, t, re.Request.Context())
		}
		return re.JSON(http.StatusAccepted, map[string]any{"started": len(triggers)})
	}

	results := []map[string]any{}
	for i, t := range triggers {
		opts := RunOptions{Ctx: context.WithoutCancel(re.Request.Context()), Timeout: 2 * time.Minute, Sample: body.Sample && i == 0}
		if flow.GetString("trigger_type") == TriggerAction {
			// test run: the sign in is checked, nobody is signed in
			opts.action = &actionState{ip: re.RealIP()}
		}
		res := e.Run(flow, t, opts)
		result := map[string]any{"runId": res.RunId, "status": res.Status, "output": res.Output, "error": res.Error}
		if a := opts.action; a != nil {
			result["action"] = map[string]any{"redirect": a.redirect, "message": a.message, "signIn": a.signIn != nil, "signOut": a.signOut}
		}
		results = append(results, result)
	}
	return re.JSON(http.StatusOK, map[string]any{"runs": results})
}

func templateVariables(re *core.RequestEvent) error {
	tpl, err := re.App.FindRecordById("email_templates", re.Request.PathValue("id"))
	if err != nil {
		return re.NotFoundError("Template not found.", err)
	}
	vars, err := mail.TemplateVariables(tpl.GetString("subject"), tpl.GetString("html"), tpl.GetString("text"))
	if err != nil {
		return re.BadRequestError("Invalid template: "+err.Error(), nil)
	}
	return re.JSON(http.StatusOK, map[string]any{"variables": vars})
}

// --- validation -----------------------------------------------------------------

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validateFlow(app core.App, f *core.Record) error {
	errs := validation.Errors{}
	cfg := triggerConfig(f)

	switch f.GetString("trigger_type") {
	case TriggerRecord:
		c, err := app.FindCollectionByNameOrId(asString(cfg, "collection"))
		if err != nil || isInternal(c) {
			errs["trigger"] = validation.NewError("invalid_collection", "Select a valid collection.")
			break
		}
		cfg["collection"] = c.Name
		if events, _ := cfg["events"].([]any); len(events) == 0 {
			errs["trigger"] = validation.NewError("required_events", "Select at least one event.")
		}
		timing := asString(cfg, "timing")
		if timing != "before" {
			cfg["timing"] = "after"
		}
		if filter := asString(cfg, "filter"); filter != "" && timing != "before" {
			if _, err := app.FindRecordsByFilter(c, filter, "", 1, 0); err != nil {
				errs["trigger"] = validation.NewError("invalid_filter", "Invalid filter: "+err.Error())
			}
		}
	case TriggerSchedule:
		if _, err := cron.NewSchedule(asString(cfg, "cron")); err != nil {
			errs["trigger"] = validation.NewError("invalid_cron", "Invalid cron expression: "+err.Error())
		}
	case TriggerWebhook:
		if asString(cfg, "token") == "" {
			cfg["token"] = randomToken()
		}
		if err := validateWebhook(app, f, cfg); err != nil {
			errs["trigger"] = err
		}
	case TriggerAction:
		switch asString(cfg, "access") {
		case "users":
		case "roles":
			roles, _ := cfg["roles"].([]any)
			if len(roles) == 0 {
				errs["trigger"] = validation.NewError("required_roles", "Select at least one role.")
			}
			for _, id := range roles {
				if _, err := app.FindRecordById("roles", toString(id)); err != nil {
					errs["trigger"] = validation.NewError("invalid_role", "Unknown role.")
				}
			}
		default:
			cfg["access"] = "public"
			delete(cfg, "roles")
		}
	case TriggerManual:
		if name := asString(cfg, "collection"); name != "" {
			if _, err := app.FindCollectionByNameOrId(name); err != nil {
				errs["trigger"] = validation.NewError("invalid_collection", "Unknown collection.")
			}
		}
	}
	f.Set("trigger", cfg)

	var steps []Step
	if raw := f.GetString("steps"); raw != "" && raw != "null" {
		if err := json.Unmarshal([]byte(raw), &steps); err != nil {
			errs["steps"] = validation.NewError("invalid_steps", "Invalid steps: "+err.Error())
		}
	}
	keys := map[string]bool{}
	if err := validateSteps(steps, keys, f.GetString("trigger_type") == TriggerRecord && asString(cfg, "timing") == "before"); err != nil {
		errs["steps"] = validation.NewError("invalid_steps", err.Error())
	} else if err := validateActionSteps(steps, f.GetString("trigger_type") == TriggerAction); err != nil {
		errs["steps"] = validation.NewError("invalid_steps", err.Error())
	} else if err := validateAPICallSteps(app, steps); err != nil {
		errs["steps"] = validation.NewError("invalid_steps", err.Error())
	} else if err := validateSecrets(app, steps); err != nil {
		errs["steps"] = validation.NewError("invalid_steps", err.Error())
	} else if err := validateRespondSteps(steps, f.GetString("trigger_type") == TriggerWebhook); err != nil {
		errs["steps"] = validation.NewError("invalid_steps", err.Error())
	}
	if steps == nil {
		steps = []Step{}
	}
	f.Set("steps", steps)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateSecrets checks that the secrets are only used by the HTTP requests
// (their values never become flow data) and exist.
func validateSecrets(app core.App, steps []Step) error {
	for _, s := range steps {
		if len(secrets.Refs(s.Config)) > 0 {
			label := s.Name
			if label == "" {
				label = s.Type
			}
			if s.Type != "http_request" {
				return fmt.Errorf("step %q: %w", label, secrets.ErrNotHere)
			}
			if err := secrets.Check(app, s.Config); err != nil {
				return fmt.Errorf("step %q: %w", label, err)
			}
		}
		for _, nested := range [][]Step{s.Then, s.Else, s.Steps} {
			if err := validateSecrets(app, nested); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSteps(steps []Step, keys map[string]bool, before bool) error {
	for i := range steps {
		s := &steps[i]
		label := s.Name
		if label == "" {
			label = s.Type
		}
		if _, ok := stepHandlers[s.Type]; !ok {
			return fmt.Errorf("step %q: unknown type %q", label, s.Type)
		}
		if s.ID == "" {
			s.ID = randomToken()[:12]
		}
		if s.Key == "" {
			return fmt.Errorf("step %q: a key is required", label)
		}
		if !keyRegex.MatchString(s.Key) {
			return fmt.Errorf("step %q: the key %q must contain only letters, digits and _", label, s.Key)
		}
		if keys[s.Key] {
			return fmt.Errorf("step %q: the key %q is used more than once", label, s.Key)
		}
		keys[s.Key] = true
		if err := validateTemplates(s.Config); err != nil {
			return fmt.Errorf("step %q: invalid template: %w", label, err)
		}
		if s.Type == "set_trigger" && !before {
			return fmt.Errorf("step %q: only available in \"before save\" record flows", label)
		}
		if s.Type == "ai_decision" {
			if err := validateAIStep(s); err != nil {
				return fmt.Errorf("step %q: %w", label, err)
			}
		}
		if s.Type == "ai_generate" {
			if err := validateAIGenerateStep(s); err != nil {
				return fmt.Errorf("step %q: %w", label, err)
			}
		}
		if s.Type == "delay" && before {
			return fmt.Errorf("step %q: delays are not allowed in \"before save\" flows", label)
		}
		for _, nested := range [][]Step{s.Then, s.Else, s.Steps} {
			if err := validateSteps(nested, keys, before); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAPICallSteps(app core.App, steps []Step) error {
	for i := range steps {
		s := &steps[i]
		if s.Type == "api_call" {
			if err := validateAPICallStep(app, s); err != nil {
				label := s.Name
				if label == "" {
					label = s.Type
				}
				return fmt.Errorf("step %q: %w", label, err)
			}
		}
		for _, nested := range [][]Step{s.Then, s.Else, s.Steps} {
			if err := validateAPICallSteps(app, nested); err != nil {
				return err
			}
		}
	}
	return nil
}

// actionSteps only work in page action flows (they decide the response).
var actionSteps = map[string]bool{"sign_in": true, "sign_out": true, "redirect": true}

func validateActionSteps(steps []Step, action bool) error {
	for _, s := range steps {
		if actionSteps[s.Type] && !action {
			label := s.Name
			if label == "" {
				label = s.Type
			}
			return fmt.Errorf("step %q: only available in \"Page action\" flows", label)
		}
		for _, nested := range [][]Step{s.Then, s.Else, s.Steps} {
			if err := validateActionSteps(nested, action); err != nil {
				return err
			}
		}
	}
	return nil
}
