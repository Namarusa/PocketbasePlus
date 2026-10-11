package mail

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"

	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
)

const (
	templatesCollection   = "email_templates"
	automationsCollection = "email_automations"
	messagesCollection    = "email_messages"

	TriggerRecordEvent = "record_event"
	TriggerSchedule    = "schedule"
	TriggerDelayed     = "delayed"

	cronJobPrefix    = "email_auto_"
	queueCronJobId   = "email_queue"
	maxDigestRecords = 5000
)

var errNoRows = sql.ErrNoRows

// Engine holds the mail module state.
type Engine struct {
	app core.App

	// DisableKick prevents background queue processing right after
	// enqueueing (the cron worker still runs). Used by tests.
	DisableKick bool

	processing sync.Mutex

	cacheMu     sync.RWMutex
	cacheLoaded bool
	eventAutos  []*core.Record // active record_event and delayed automations
}

// Register binds the mail hooks, cron jobs and routes to the app.
func Register(app core.App) *Engine {
	e := &Engine{app: app}

	app.OnRecordValidate(templatesCollection).BindFunc(func(ev *core.RecordEvent) error {
		if err := validateTemplate(ev.Record); err != nil {
			return err
		}
		return ev.Next()
	})
	app.OnRecordValidate(unsubscribesCollection).BindFunc(func(ev *core.RecordEvent) error {
		ev.Record.Set("email", normalizeEmail(ev.Record.GetString("email")))
		return ev.Next()
	})
	app.OnRecordValidate(automationsCollection).BindFunc(func(ev *core.RecordEvent) error {
		if err := validateAutomation(ev.App, ev.Record); err != nil {
			return err
		}
		return ev.Next()
	})

	automationChanged := func(ev *core.RecordEvent) error {
		e.invalidate()
		e.SyncSchedules()
		return ev.Next()
	}
	app.OnRecordAfterCreateSuccess(automationsCollection).BindFunc(automationChanged)
	app.OnRecordAfterUpdateSuccess(automationsCollection).BindFunc(automationChanged)
	app.OnRecordAfterDeleteSuccess(automationsCollection).BindFunc(automationChanged)

	// record event triggers (all collections)
	app.OnRecordAfterCreateSuccess().BindFunc(func(ev *core.RecordEvent) error {
		e.handleEvent(ev.App, ev.Record, "create", nil)
		return ev.Next()
	})
	app.OnRecordAfterUpdateSuccess().BindFunc(func(ev *core.RecordEvent) error {
		e.handleEvent(ev.App, ev.Record, "update", nil)
		return ev.Next()
	})
	// delete filters must be evaluated while the record still exists
	app.OnRecordDelete().BindFunc(func(ev *core.RecordEvent) error {
		matched := e.match(ev.App, ev.Record, "delete")
		if err := ev.Next(); err != nil {
			return err
		}
		if len(matched) > 0 {
			e.handleEvent(ev.App, ev.Record, "delete", matched)
		}
		return nil
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.App.Cron().MustAdd(queueCronJobId, "* * * * *", e.ProcessQueue)
		e.SyncSchedules()
		registerRoutes(e, se)
		return se.Next()
	})

	return e
}

// isInternal reports whether the collection records never trigger automations.
func isInternal(c *core.Collection) bool {
	return internalcols.Is(c)
}

// --- record event automations -------------------------------------------

func (e *Engine) invalidate() {
	e.cacheMu.Lock()
	e.cacheLoaded = false
	e.eventAutos = nil
	e.cacheMu.Unlock()
}

func (e *Engine) eventAutomations() []*core.Record {
	e.cacheMu.RLock()
	if e.cacheLoaded {
		defer e.cacheMu.RUnlock()
		return e.eventAutos
	}
	e.cacheMu.RUnlock()

	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if !e.cacheLoaded {
		list, err := e.app.FindRecordsByFilter(
			automationsCollection,
			"active = true && (trigger = 'record_event' || trigger = 'delayed')",
			"", 0, 0,
		)
		if err != nil {
			e.app.Logger().Error("mail: failed to load automations", "error", err)
			return nil
		}
		e.eventAutos = list
		e.cacheLoaded = true
	}
	return e.eventAutos
}

// match returns the active event automations triggered by the record event.
func (e *Engine) match(app core.App, record *core.Record, event string) []*core.Record {
	if isInternal(record.Collection()) {
		return nil
	}
	var matched []*core.Record
	for _, a := range e.eventAutomations() {
		if a.GetString("collection") != record.Collection().Name || !slices.Contains(a.GetStringSlice("events"), event) {
			continue
		}
		ok, err := matchesFilter(app, record, a.GetString("filter"))
		if err != nil {
			app.Logger().Error("mail: invalid automation filter", "automation", a.Id, "error", err)
			continue
		}
		if ok {
			matched = append(matched, a)
		}
	}
	return matched
}

func (e *Engine) handleEvent(app core.App, record *core.Record, event string, matched []*core.Record) {
	if matched == nil {
		matched = e.match(app, record, event)
	}
	total := 0
	for _, a := range matched {
		n, err := e.EnqueueForAutomation(app, a, record, nil)
		if err != nil {
			app.Logger().Error("mail: failed to enqueue automation", "automation", a.Id, "error", err)
		}
		total += n
	}
	if total > 0 {
		e.Kick()
	}
}

// EnqueueForAutomation queues the automation template for the record (or
// list of records for digests) to the resolved recipients.
func (e *Engine) EnqueueForAutomation(app core.App, a *core.Record, record *core.Record, records []*core.Record) (int, error) {
	tpl, err := app.FindRecordById(templatesCollection, a.GetString("template"))
	if err != nil {
		return 0, fmt.Errorf("template not found: %w", err)
	}

	recipients := ParseAddresses(a.GetString("static_recipients"))
	if record != nil {
		recipients = mergeAddresses(ResolveRecipients(app, record, a.GetString("recipient_field")), recipients)
	}

	sendAt := time.Now()
	if a.GetString("trigger") == TriggerDelayed {
		sendAt = sendAt.Add(time.Duration(a.GetInt("delay_minutes")) * time.Minute)
	}

	for _, to := range recipients {
		if _, err := Enqueue(app, Job{Template: tpl, Automation: a, Record: record, Records: records, To: to, SendAt: sendAt}); err != nil {
			return 0, err
		}
	}
	return len(recipients), nil
}

func mergeAddresses(lists ...[]string) []string {
	return ParseAddresses(strings.Join(slices.Concat(lists...), ","))
}

// --- schedules ----------------------------------------------------------

// SyncSchedules (re)registers a cron job for every active schedule automation.
func (e *Engine) SyncSchedules() {
	c := e.app.Cron()
	for _, job := range c.Jobs() {
		if strings.HasPrefix(job.Id(), cronJobPrefix) {
			c.Remove(job.Id())
		}
	}

	list, err := e.app.FindRecordsByFilter(automationsCollection, "active = true && trigger = 'schedule'", "", 0, 0)
	if err != nil {
		e.app.Logger().Error("mail: failed to load schedules", "error", err)
		return
	}
	for _, a := range list {
		id := a.Id
		if err := c.Add(cronJobPrefix+id, a.GetString("cron"), func() { e.RunSchedule(id) }); err != nil {
			e.app.Logger().Error("mail: invalid schedule", "automation", id, "error", err)
		}
	}
}

// RunSchedule executes a schedule automation now.
//
// With a collection and a recipient field, one email is queued per matching
// record. With a collection and no recipient field, a single digest (with
// all matching records as .records) is queued per static recipient.
// Without a collection, the template is sent to the static recipients.
func (e *Engine) RunSchedule(automationId string) (int, error) {
	a, err := e.app.FindRecordById(automationsCollection, automationId)
	if err != nil {
		return 0, err
	}

	collection := a.GetString("collection")
	if collection == "" {
		return e.enqueueAndKick(a, nil, nil)
	}

	// one more than the limit to detect when records are left out
	records, err := e.app.FindRecordsByFilter(collection, a.GetString("filter"), "-created", maxDigestRecords+1, 0)
	if err != nil {
		// collections without a "created" field
		records, err = e.app.FindRecordsByFilter(collection, a.GetString("filter"), "", maxDigestRecords+1, 0)
		if err != nil {
			return 0, err
		}
	}
	if len(records) > maxDigestRecords {
		records = records[:maxDigestRecords]
		e.app.Logger().Warn(
			fmt.Sprintf("mail: schedule automation matched more than %d records, only the %d most recent are used", maxDigestRecords, maxDigestRecords),
			"automation", a.Id,
		)
	}

	if a.GetString("recipient_field") == "" {
		if len(records) == 0 {
			return 0, nil
		}
		return e.enqueueAndKick(a, nil, records)
	}

	total := 0
	for _, r := range records {
		n, err := e.EnqueueForAutomation(e.app, a, r, nil)
		total += n
		if err != nil {
			return total, err
		}
	}
	if total > 0 {
		e.Kick()
	}
	return total, nil
}

func (e *Engine) enqueueAndKick(a *core.Record, record *core.Record, records []*core.Record) (int, error) {
	n, err := e.EnqueueForAutomation(e.app, a, record, records)
	if n > 0 {
		e.Kick()
	}
	return n, err
}

// --- validation ---------------------------------------------------------

func validateTemplate(r *core.Record) error {
	errs := validation.Errors{}
	if _, err := Render(r.GetString("subject"), "", "", nil, true); err != nil {
		errs["subject"] = validation.NewError("invalid_template", err.Error())
	}
	if _, err := Render("", r.GetString("html"), "", nil, true); err != nil {
		errs["html"] = validation.NewError("invalid_template", err.Error())
	}
	if _, err := Render("", "", r.GetString("text"), nil, true); err != nil {
		errs["text"] = validation.NewError("invalid_template", err.Error())
	}
	if strings.TrimSpace(r.GetString("html")) == "" && strings.TrimSpace(r.GetString("text")) == "" {
		errs["html"] = validation.NewError("required_body", "Either the html or the text body is required.")
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateAutomation(app core.App, r *core.Record) error {
	errs := validation.Errors{}
	trigger := r.GetString("trigger")
	collectionName := r.GetString("collection")
	recipientField := strings.TrimSpace(r.GetString("recipient_field"))
	static := ParseAddresses(r.GetString("static_recipients"))

	var collection *core.Collection
	if collectionName != "" {
		c, err := app.FindCollectionByNameOrId(collectionName)
		if err != nil || isInternal(c) {
			errs["collection"] = validation.NewError("invalid_collection", "Unknown or internal collection.")
		} else {
			collection = c
			r.Set("collection", c.Name) // normalize ids to names
		}
	}

	switch trigger {
	case TriggerRecordEvent, TriggerDelayed:
		if collectionName == "" {
			errs["collection"] = validation.NewError("required", "A collection is required for record events.")
		}
		if len(r.GetStringSlice("events")) == 0 {
			errs["events"] = validation.NewError("required", "Select at least one event.")
		}
		if trigger == TriggerDelayed && r.GetInt("delay_minutes") <= 0 {
			errs["delay_minutes"] = validation.NewError("required", "The delay must be greater than 0.")
		}
		if recipientField == "" && len(static) == 0 {
			errs["recipient_field"] = validation.NewError("required", "Set a recipient field or static recipients.")
		}
	case TriggerSchedule:
		if _, err := cron.NewSchedule(r.GetString("cron")); err != nil {
			errs["cron"] = validation.NewError("invalid_cron", err.Error())
		}
		if recipientField == "" && len(static) == 0 {
			errs["static_recipients"] = validation.NewError("required", "Set a recipient field or static recipients.")
		}
	}

	if collection != nil {
		if filter := r.GetString("filter"); filter != "" {
			if _, err := app.FindRecordsByFilter(collection, filter, "", 1, 0); err != nil {
				errs["filter"] = validation.NewError("invalid_filter", err.Error())
			}
		}
		if recipientField != "" {
			if err := validateRecipientPath(app, collection, recipientField); err != nil {
				errs["recipient_field"] = validation.NewError("invalid_recipient_field", err.Error())
			}
		}
	} else if recipientField != "" {
		errs["recipient_field"] = validation.NewError("invalid_recipient_field", "A recipient field requires a collection.")
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateRecipientPath(app core.App, c *core.Collection, path string) error {
	fieldName, sub, hasSub := strings.Cut(path, ".")
	f := c.Fields.GetByName(fieldName)
	if f == nil {
		return fmt.Errorf("unknown field %q", fieldName)
	}
	if !hasSub {
		return nil
	}
	rel, ok := f.(*core.RelationField)
	if !ok {
		return fmt.Errorf("%q is not a relation field", fieldName)
	}
	relCol, err := app.FindCollectionByNameOrId(rel.CollectionId)
	if err != nil {
		return err
	}
	if strings.Contains(sub, ".") || relCol.Fields.GetByName(sub) == nil {
		return fmt.Errorf("unknown field %q in %s", sub, relCol.Name)
	}
	return nil
}
