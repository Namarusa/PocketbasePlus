package mail

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	StatusPending   = "pending"
	StatusSent      = "sent"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusSkipped   = "skipped"

	maxAttempts    = 3
	queueBatchSize = 100
)

// Job describes one email to put in the queue.
type Job struct {
	Template   *core.Record   // email_templates record
	Automation *core.Record   // optional email_automations record
	Record     *core.Record   // optional record the email is about
	Records    []*core.Record // optional list of records (digests)
	To         string
	SendAt     time.Time
}

// Enqueue renders the job and stores it as a pending email_messages record.
// Render errors are stored as a failed message so they show up in the log.
func Enqueue(app core.App, job Job) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId(messagesCollection)
	if err != nil {
		return nil, err
	}

	sendAt := job.SendAt
	if sendAt.IsZero() {
		sendAt = time.Now()
	}

	msg := core.NewRecord(col)
	msg.Set("template", job.Template.Id)
	msg.Set("to", job.To)
	msg.Set("send_at", sendAt.UTC())
	msg.Set("status", StatusPending)
	msg.Set("attempts", 0)
	if job.Automation != nil {
		msg.Set("automation", job.Automation.Id)
	}
	if job.Record != nil {
		msg.Set("collection", job.Record.Collection().Name)
		msg.Set("record_id", job.Record.Id)
	}

	if skipUnsubscribed(app, job.Template, msg) {
		return msg, app.Save(msg)
	}

	out, err := renderTemplate(app, job.Template, job.Record, job.Records, job.To)
	if err != nil {
		msg.Set("status", StatusFailed)
		msg.Set("error", "render: "+err.Error())
	} else {
		setContent(msg, finalize(app, job.Template, out, job.To))
	}

	return msg, app.Save(msg)
}

// skipUnsubscribed marks the message of a marketing template as skipped
// when its recipient opted out.
func skipUnsubscribed(app core.App, tpl *core.Record, msg *core.Record) bool {
	if !tpl.GetBool("marketing") || !IsUnsubscribed(app, msg.GetString("to")) {
		return false
	}
	msg.Set("status", StatusSkipped)
	msg.Set("error", "the recipient unsubscribed from marketing emails")
	return true
}

// finalize adds the unsubscribe link to marketing emails.
func finalize(app core.App, tpl *core.Record, out Rendered, to string) Rendered {
	if !tpl.GetBool("marketing") {
		return out
	}
	return withUnsubscribe(out, UnsubscribeURL(app, to))
}

// EnqueueData renders the template with custom data (e.g. from a flow) and
// queues it. The .app and .recipient data are filled automatically.
func EnqueueData(app core.App, tpl *core.Record, to string, data map[string]any, sendAt time.Time, source string) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId(messagesCollection)
	if err != nil {
		return nil, err
	}
	if sendAt.IsZero() {
		sendAt = time.Now()
	}

	full := map[string]any{}
	for k, v := range data {
		full[k] = v
	}
	full["recipient"] = map[string]any{"email": to}
	full["app"] = map[string]any{
		"name": app.Settings().Meta.AppName,
		"url":  app.Settings().Meta.AppURL,
	}
	full["unsubscribe_url"] = UnsubscribeURL(app, to)

	msg := core.NewRecord(col)
	msg.Set("template", tpl.Id)
	msg.Set("to", to)
	msg.Set("send_at", sendAt.UTC())
	msg.Set("status", StatusPending)
	msg.Set("attempts", 0)
	msg.Set("source", source)

	if skipUnsubscribed(app, tpl, msg) {
		return msg, app.Save(msg)
	}

	out, err := Render(tpl.GetString("subject"), tpl.GetString("html"), tpl.GetString("text"), full)
	if err != nil {
		msg.Set("status", StatusFailed)
		msg.Set("error", "render: "+err.Error())
	} else {
		setContent(msg, finalize(app, tpl, out, to))
	}

	return msg, app.Save(msg)
}

func renderTemplate(app core.App, tpl *core.Record, record *core.Record, records []*core.Record, to string) (Rendered, error) {
	return Render(
		tpl.GetString("subject"),
		tpl.GetString("html"),
		tpl.GetString("text"),
		TemplateData(app, record, records, to),
	)
}

func setContent(msg *core.Record, out Rendered) {
	msg.Set("subject", out.Subject)
	msg.Set("html", out.HTML)
	msg.Set("text", out.Text)
}

// ProcessQueue sends all due pending messages. Concurrent calls are no-ops.
func (e *Engine) ProcessQueue() {
	if !e.processing.TryLock() {
		return
	}
	defer e.processing.Unlock()

	// messages whose status couldn't be saved stay due: don't pick them up
	// again in this pass (the next cron run retries them)
	seen := map[string]bool{}
	for {
		due, err := e.app.FindRecordsByFilter(
			messagesCollection,
			"status = {:status} && send_at <= {:now}",
			"send_at",
			queueBatchSize,
			0,
			dbx.Params{"status": StatusPending, "now": types.NowDateTime().String()},
		)
		if err != nil {
			e.app.Logger().Error("mail: failed to load the queue", "error", err)
			return
		}

		fresh := 0
		for _, msg := range due {
			if seen[msg.Id] {
				continue
			}
			seen[msg.Id] = true
			fresh++
			if err := e.deliver(msg); err != nil {
				e.app.Logger().Error("mail: failed to update queued message", "id", msg.Id, "error", err)
			}
		}

		if len(due) < queueBatchSize || fresh == 0 {
			return
		}
	}
}

// Kick processes the queue in the background (e.g. right after enqueueing).
func (e *Engine) Kick() {
	if e.DisableKick {
		return
	}
	go e.ProcessQueue()
}

func (e *Engine) deliver(msg *core.Record) error {
	app := e.app

	if skip, reason, err := e.recheck(msg); err != nil {
		return markFailedAttempt(app, msg, err)
	} else if skip {
		msg.Set("status", StatusSkipped)
		msg.Set("error", reason)
		return app.Save(msg)
	}

	// marketing emails: the recipient may have unsubscribed since queued
	var headers map[string]string
	if tpl, err := app.FindRecordById(templatesCollection, msg.GetString("template")); err == nil && tpl.GetBool("marketing") {
		if skipUnsubscribed(app, tpl, msg) {
			return app.Save(msg)
		}
		if link := UnsubscribeURL(app, msg.GetString("to")); link != "" {
			headers = map[string]string{
				"List-Unsubscribe":      "<" + link + ">",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			}
		}
	}

	meta := app.Settings().Meta
	err := app.NewMailClient().Send(&mailer.Message{
		From:    mail.Address{Name: meta.SenderName, Address: meta.SenderAddress},
		To:      []mail.Address{{Address: msg.GetString("to")}},
		Subject: msg.GetString("subject"),
		HTML:    msg.GetString("html"),
		Text:    msg.GetString("text"),
		Headers: headers,
	})
	if err != nil {
		return markFailedAttempt(app, msg, err)
	}

	msg.Set("status", StatusSent)
	msg.Set("error", "")
	msg.Set("attempts", msg.GetInt("attempts")+1)
	msg.Set("sent_at", types.NowDateTime())
	return app.Save(msg)
}

func markFailedAttempt(app core.App, msg *core.Record, cause error) error {
	attempts := msg.GetInt("attempts") + 1
	msg.Set("attempts", attempts)
	msg.Set("error", cause.Error())
	if attempts >= maxAttempts {
		msg.Set("status", StatusFailed)
	} else {
		// simple linear backoff: 2min, 4min, ...
		msg.Set("send_at", time.Now().UTC().Add(time.Duration(attempts)*2*time.Minute))
	}
	return app.Save(msg)
}

// recheck re-evaluates delayed automations configured with recheck_filter
// and re-renders the message with fresh record data.
func (e *Engine) recheck(msg *core.Record) (skip bool, reason string, err error) {
	automationId := msg.GetString("automation")
	if automationId == "" {
		return false, "", nil
	}
	automation, err := e.app.FindRecordById(automationsCollection, automationId)
	if err != nil || !automation.GetBool("recheck_filter") {
		return false, "", nil // automation deleted or no recheck: send as rendered
	}

	record, err := e.app.FindRecordById(msg.GetString("collection"), msg.GetString("record_id"))
	if err != nil {
		return true, "the record no longer exists", nil
	}

	ok, err := matchesFilter(e.app, record, automation.GetString("filter"))
	if err != nil {
		return false, "", err
	}
	if !ok {
		return true, "the record no longer matches the automation filter", nil
	}

	tpl, err := e.app.FindRecordById(templatesCollection, msg.GetString("template"))
	if err != nil {
		return false, "", fmt.Errorf("template not found: %w", err)
	}
	out, err := renderTemplate(e.app, tpl, record, nil, msg.GetString("to"))
	if err != nil {
		return false, "", fmt.Errorf("render: %w", err)
	}
	setContent(msg, finalize(e.app, tpl, out, msg.GetString("to")))
	return false, "", nil
}

// matchesFilter reports whether the record satisfies a PocketBase filter expression.
func matchesFilter(app core.App, record *core.Record, filter string) (bool, error) {
	if strings.TrimSpace(filter) == "" {
		return true, nil
	}
	_, err := app.FindFirstRecordByFilter(
		record.Collection(),
		"id = {:__id} && ("+filter+")",
		dbx.Params{"__id": record.Id},
	)
	if err == nil {
		return true, nil
	}
	if isNoRows(err) {
		return false, nil
	}
	return false, err
}

// ResolveRecipients returns the valid, unique email addresses found at the
// recipient path of a record ("email", or "relation.email" one level deep).
func ResolveRecipients(app core.App, record *core.Record, path string) []string {
	path = strings.TrimSpace(path)
	if record == nil || path == "" {
		return nil
	}

	var raw []string
	field, sub, hasSub := strings.Cut(path, ".")
	if !hasSub {
		raw = record.GetStringSlice(field)
	} else {
		if errs := app.ExpandRecord(record, []string{field}, nil); len(errs) > 0 {
			app.Logger().Warn("mail: failed to expand recipient relation", "path", path, "errors", errs)
		}
		for _, rel := range record.ExpandedAll(field) {
			raw = append(raw, rel.GetString(sub))
		}
	}

	return ParseAddresses(strings.Join(raw, ","))
}

// ParseAddresses splits a comma, semicolon, space or newline separated list
// into valid and unique email addresses.
func ParseAddresses(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	seen := map[string]bool{}
	result := []string{}
	for _, p := range parts {
		addr, err := mail.ParseAddress(p)
		if err != nil {
			continue
		}
		email := strings.ToLower(addr.Address)
		if !seen[email] {
			seen[email] = true
			result = append(result, addr.Address)
		}
	}
	return result
}

func isNoRows(err error) bool {
	return errors.Is(err, errNoRows)
}
