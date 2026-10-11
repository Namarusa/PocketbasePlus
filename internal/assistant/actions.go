package assistant

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"

	"github.com/Namarusa/PocketbasePlus/internal/actor"
	"github.com/Namarusa/PocketbasePlus/internal/dashboards"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
)

// Action statuses.
const (
	StatusPending   = "pending"
	StatusApplied   = "applied"
	StatusDiscarded = "discarded"
	StatusFailed    = "failed"
)

// Action types.
const (
	ActionDashboard = "dashboard"
	ActionTemplate  = "email_template"
	ActionCampaign  = "campaign"
	ActionRecord    = "record"
)

// Action is a change proposed by the model and applied by the user.
type Action struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Title   string         `json:"title"`
	Summary string         `json:"summary"`
	Details map[string]any `json:"details,omitempty"` // shown in the card
	Payload map[string]any `json:"payload"`           // what apply uses
	Status  string         `json:"status"`
	Result  map[string]any `json:"result,omitempty"`
	Error   string         `json:"error,omitempty"`
	Created string         `json:"created"`
}

const maxProposedPanels = 30

func (t *toolbox) propose(act *Action) map[string]any {
	act.ID = "act_" + security.RandomStringWithAlphabet(8, "abcdefghijklmnopqrstuvwxyz0123456789")
	act.Status = StatusPending
	act.Created = time.Now().UTC().Format(time.RFC3339)
	t.conv.Actions = append(t.conv.Actions, act)
	return map[string]any{
		"proposed": act.ID,
		"status":   "pending: shown to the user as a card, nothing happens until they click Apply",
		"summary":  act.Summary,
	}
}

// --- dashboards ---------------------------------------------------------------------

var panelHeights = map[string]int{
	dashboards.TypeMetric:     4,
	dashboards.TypeMeter:      5,
	dashboards.TypeTimeSeries: 9,
	dashboards.TypeBar:        9,
	dashboards.TypePie:        9,
	dashboards.TypeList:       9,
	dashboards.TypeLabel:      2,
}

func isNumberPanel(t string) bool {
	return t == dashboards.TypeMetric || t == dashboards.TypeMeter
}

// layout places the panels in full rows of the 24 units grid: up to 4
// numbers side by side, charts and lists by pairs, labels alone.
func layout(panels []map[string]any, startY int) {
	y := startY
	place := func(row []map[string]any) {
		w := dashboards.GridColumns / len(row)
		h := 0
		for _, p := range row {
			h = max(h, panelHeights[p["type"].(string)])
		}
		for i, p := range row {
			p["x"], p["y"], p["w"], p["h"] = i*w, y, w, h
		}
		y += h
	}
	for i := 0; i < len(panels); {
		typ := panels[i]["type"].(string)
		j := i + 1
		switch {
		case typ == dashboards.TypeLabel:
		case isNumberPanel(typ):
			for j < len(panels) && j-i < 4 && isNumberPanel(panels[j]["type"].(string)) {
				j++
			}
		default:
			if j < len(panels) {
				next := panels[j]["type"].(string)
				if next != dashboards.TypeLabel && !isNumberPanel(next) {
					j++
				}
			}
		}
		place(panels[i:j])
		i = j
	}
}

// previewOf summarizes the data of a panel for the model and the card.
func previewOf(data any) string {
	m, _ := data.(map[string]any)
	switch {
	case m == nil:
		return ""
	case m["value"] != nil:
		if f, ok := m["value"].(float64); ok {
			return "value " + strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64)
		}
		return fmt.Sprintf("value %v", m["value"])
	case m["points"] != nil:
		if pts, ok := m["points"].([]dashboards.Point); ok {
			total := 0.0
			for _, p := range pts {
				total += p.Y
			}
			return fmt.Sprintf("%d points, total %v", len(pts), total)
		}
	case m["groups"] != nil:
		if groups, ok := m["groups"].([]dashboards.Group); ok {
			parts := []string{}
			for i, g := range groups {
				if i == 3 {
					parts = append(parts, "…")
					break
				}
				parts = append(parts, fmt.Sprintf("%s: %v", g.Label, g.Value))
			}
			return fmt.Sprintf("%d groups (%s)", len(groups), strings.Join(parts, ", "))
		}
	case m["items"] != nil:
		if items, ok := m["items"].([]map[string]any); ok {
			return fmt.Sprintf("%d rows", len(items))
		}
	case m["series"] != nil:
		return "split series"
	}
	return "ok"
}

func toolProposeDashboard(t *toolbox, args map[string]any) (any, error) {
	raw, _ := args["panels"].([]any)
	if len(raw) == 0 {
		return nil, errors.New("add at least one panel")
	}
	if len(raw) > maxProposedPanels {
		return nil, fmt.Errorf("at most %d panels", maxProposedPanels)
	}

	var existing *core.Record
	startY := 0
	if id := argString(args, "dashboard_id"); id != "" {
		d, err := t.app.FindRecordById("dashboards", id)
		if err != nil {
			return nil, errors.New("dashboard not found")
		}
		existing = d
		panels, _ := t.app.FindRecordsByFilter("dashboard_panels", "dashboard = {:d}", "", 0, 0, dbx.Params{"d": d.Id})
		for _, p := range panels {
			startY = max(startY, p.GetInt("y")+p.GetInt("h"))
		}
	} else if argString(args, "name") == "" {
		return nil, errors.New("name is required for a new dashboard")
	}

	panels := []map[string]any{}
	previews := []map[string]any{}
	var problems []string
	for i, item := range raw {
		p, _ := item.(map[string]any)
		typ := argString(p, "type")
		name := argString(p, "name")
		options, _ := p["options"].(map[string]any)
		if options == nil {
			options = map[string]any{}
		}
		label := fmt.Sprintf("panel %d (%s)", i+1, name)
		if !dashboards.IsPanelType(typ) {
			problems = append(problems, fmt.Sprintf("%s: unknown type %q", label, typ))
			continue
		}
		preview := ""
		if typ != dashboards.TypeLabel {
			data, err := dashboards.RunPanel(t.ctx, t.app, t.auth, typ, options)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", label, err))
				continue
			}
			preview = previewOf(data)
		}
		panels = append(panels, map[string]any{"type": typ, "name": name, "options": options, "show_header": typ != dashboards.TypeLabel})
		previews = append(previews, map[string]any{"name": name, "type": typ, "preview": preview})
	}
	if len(problems) > 0 {
		return nil, errors.New("fix these panels and propose again: " + strings.Join(problems, "; "))
	}
	layout(panels, startY)

	act := &Action{Type: ActionDashboard, Details: map[string]any{"panels": previews}}
	payload := map[string]any{"panels": panels}
	if existing != nil {
		payload["dashboard_id"] = existing.Id
		act.Title = fmt.Sprintf("Add %d panel(s) to “%s”", len(panels), existing.GetString("name"))
	} else {
		payload["name"] = argString(args, "name")
		payload["note"] = argString(args, "note")
		act.Title = fmt.Sprintf("Dashboard “%s”", argString(args, "name"))
	}
	act.Payload = payload
	names := []string{}
	for _, p := range previews {
		names = append(names, p["name"].(string))
	}
	act.Summary = fmt.Sprintf("%d panels: %s", len(panels), strings.Join(names, ", "))
	res := t.propose(act)
	res["panels"] = previews
	return res, nil
}

// --- email templates --------------------------------------------------------------

type templateDraft struct {
	Name, Subject, HTML, Text, SampleCollection string
	Marketing                                   bool
}

func draftFrom(args map[string]any) (templateDraft, error) {
	d := templateDraft{
		Name:             argString(args, "name"),
		Subject:          argString(args, "subject"),
		HTML:             fmt.Sprint(valueOr(args["html"], "")),
		Text:             fmt.Sprint(valueOr(args["text"], "")),
		SampleCollection: argString(args, "sample_collection"),
	}
	d.Marketing, _ = args["marketing"].(bool)
	if d.Name == "" || d.Subject == "" || strings.TrimSpace(d.HTML) == "" {
		return d, errors.New("name, subject and html are required")
	}
	if err := mail.Validate(d.Subject, d.HTML, d.Text); err != nil {
		return d, fmt.Errorf("template error: %w", err)
	}
	return d, nil
}

func (d templateDraft) payload() map[string]any {
	return map[string]any{"name": d.Name, "subject": d.Subject, "html": d.HTML, "text": d.Text, "marketing": d.Marketing, "sample_collection": d.SampleCollection}
}

func valueOr(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}

func toolProposeEmailTemplate(t *toolbox, args map[string]any) (any, error) {
	d, err := draftFrom(args)
	if err != nil {
		return nil, err
	}
	if d.SampleCollection != "" {
		if _, err := t.collection(map[string]any{"collection": d.SampleCollection}); err != nil {
			return nil, err
		}
	}
	record, _ := t.sampleRecord(d.SampleCollection, "")
	out, err := mail.Render(d.Subject, d.HTML, d.Text, mail.TemplateData(t.app, record, nil, t.auth.Email()))
	if err != nil {
		return nil, fmt.Errorf("template error: %w", err)
	}
	kind := "Transactional"
	if d.Marketing {
		kind = "Marketing"
	}
	return t.propose(&Action{
		Type:    ActionTemplate,
		Title:   fmt.Sprintf("Email template “%s”", d.Name),
		Summary: fmt.Sprintf("%s email, subject: %s", kind, out.Subject),
		Details: map[string]any{"subject": out.Subject, "html": out.HTML},
		Payload: d.payload(),
	}), nil
}

var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

func uniqueSlug(app core.App, name string) string {
	base := strings.Trim(slugRegex.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "template"
	}
	base = limitText(base, 80)
	slug := base
	for i := 2; ; i++ {
		if _, err := app.FindFirstRecordByData("email_templates", "slug", slug); err != nil {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func createTemplate(app core.App, ctxApp core.App, p map[string]any, forceMarketing bool, save func(*core.Record) error) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId("email_templates")
	if err != nil {
		return nil, err
	}
	rec := core.NewRecord(col)
	rec.Set("name", p["name"])
	rec.Set("slug", uniqueSlug(ctxApp, fmt.Sprint(p["name"])))
	rec.Set("subject", p["subject"])
	rec.Set("html", p["html"])
	rec.Set("text", p["text"])
	rec.Set("sample_collection", p["sample_collection"])
	marketing, _ := p["marketing"].(bool)
	rec.Set("marketing", marketing || forceMarketing)
	return rec, save(rec)
}

// --- campaigns --------------------------------------------------------------------

var errRollback = errors.New("rollback")

func toolProposeCampaign(t *toolbox, args map[string]any) (any, error) {
	title := argString(args, "title")
	c, err := t.collection(args)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"collection":      c.Name,
		"record_ids":      argStrings(args, "record_ids"),
		"filter":          argString(args, "filter"),
		"recipient_field": argString(args, "recipient_field"),
		"send_at":         argString(args, "send_at"),
	}
	var sendAt time.Time
	if s := argString(args, "send_at"); s != "" {
		if sendAt, err = time.Parse(time.RFC3339, s); err != nil {
			return nil, errors.New("send_at must be an RFC 3339 date, e.g. 2026-10-06T09:00:00Z")
		}
		if sendAt.Before(time.Now().Add(-time.Minute)) {
			return nil, errors.New("send_at is in the past")
		}
	}

	var existing *core.Record
	var draft templateDraft
	warning := ""
	if id := argString(args, "template_id"); id != "" {
		if existing, err = t.app.FindRecordById("email_templates", id); err != nil {
			return nil, errors.New("template not found")
		}
		payload["template_id"] = existing.Id
		if !existing.GetBool("marketing") {
			warning = "the template isn't marked as a marketing email: it has no unsubscribe link and the opt-out list is ignored. Prefer a new marketing template."
		}
	} else {
		tplArgs, _ := args["template"].(map[string]any)
		if tplArgs == nil {
			return nil, errors.New("give template_id or template")
		}
		tplArgs["marketing"] = true
		if tplArgs["name"] == nil {
			tplArgs["name"] = title
		}
		if draft, err = draftFrom(tplArgs); err != nil {
			return nil, err
		}
		draft.SampleCollection = c.Name
		payload["template"] = draft.payload()
	}

	// count the recipients (and render a preview) with a temporary template
	var res *mail.BulkResult
	var preview mail.Rendered
	err = t.app.RunInTransaction(func(tx core.App) error {
		tpl := existing
		if tpl == nil {
			created, err := createTemplate(tx, tx, draft.payload(), true, func(r *core.Record) error { return tx.Save(r) })
			if err != nil {
				return fmt.Errorf("template: %w", err)
			}
			tpl = created
		}
		r, err := t.a.mail.SendBulk(tx, mail.BulkRequest{
			TemplateId: tpl.Id, Collection: c.Name, RecordIds: argStrings(args, "record_ids"),
			Filter: argString(args, "filter"), RecipientField: argString(args, "recipient_field"),
			Dedupe: true, DryRun: true,
		})
		if err != nil {
			return err
		}
		res = r
		// preview with the first actual recipient
		var record *core.Record
		if len(r.SampleRecords) > 0 {
			record, _ = tx.FindRecordById(c, r.SampleRecords[0])
		}
		to := t.auth.Email()
		if len(r.Sample) > 0 {
			to = r.Sample[0]
		}
		preview, err = mail.Render(tpl.GetString("subject"), tpl.GetString("html"), tpl.GetString("text"), mail.TemplateData(tx, record, nil, to))
		if err != nil {
			return fmt.Errorf("template error: %w", err)
		}
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		return nil, err
	}
	if res.Queued == 0 {
		return nil, fmt.Errorf("no recipient: %d record(s) matched, %d without address, %d unsubscribed. Check the selection and the recipient field", res.Records, res.NoAddress, res.Unsubscribed)
	}

	when := "now"
	if !sendAt.IsZero() {
		when = sendAt.UTC().Format("Mon Jan 2 2006 15:04 UTC")
	}
	skipped := []string{}
	if res.Duplicates > 0 {
		skipped = append(skipped, fmt.Sprintf("%d duplicate(s)", res.Duplicates))
	}
	if res.Unsubscribed > 0 {
		skipped = append(skipped, fmt.Sprintf("%d unsubscribed", res.Unsubscribed))
	}
	if res.NoAddress > 0 {
		skipped = append(skipped, fmt.Sprintf("%d without address", res.NoAddress))
	}
	summary := fmt.Sprintf("Send “%s” to %d recipient(s), %s", preview.Subject, res.Queued, when)
	if len(skipped) > 0 {
		summary += " (skipped: " + strings.Join(skipped, ", ") + ")"
	}
	if title == "" {
		title = preview.Subject
	}
	details := map[string]any{"subject": preview.Subject, "html": preview.HTML, "recipients": res.Queued, "sample": res.Sample, "send_at": when}
	if warning != "" {
		details["warning"] = warning
	}
	out := t.propose(&Action{Type: ActionCampaign, Title: "Campaign “" + title + "”", Summary: summary, Details: details, Payload: payload})
	out["recipients"] = res.Queued
	out["sample_recipients"] = res.Sample
	if warning != "" {
		out["warning"] = warning
	}
	return out, nil
}

// --- records ------------------------------------------------------------------------

func toolProposeRecord(t *toolbox, args map[string]any) (any, error) {
	c, err := t.collection(args)
	if err != nil {
		return nil, err
	}
	if c.IsView() || c.IsAuth() {
		return nil, errors.New("only base collections (not views or auth collections)")
	}
	data, _ := args["data"].(map[string]any)
	if len(data) == 0 {
		return nil, errors.New("data is required")
	}
	rec := core.NewRecord(c)
	for k, v := range data {
		f := c.Fields.GetByName(k)
		if f == nil || f.GetHidden() || f.Type() == core.FieldTypeAutodate || k == "id" {
			return nil, fmt.Errorf("unknown or read-only field %q", k)
		}
		rec.Set(k, v)
	}
	if err := t.app.Validate(rec); err != nil {
		return nil, fmt.Errorf("invalid record: %w", err)
	}
	title := argString(args, "title")
	if title == "" {
		title = "New " + c.Name + " record"
	}
	return t.propose(&Action{
		Type:    ActionRecord,
		Title:   title,
		Summary: fmt.Sprintf("Create a record in %s", c.Name),
		Details: map[string]any{"data": data},
		Payload: map[string]any{"collection": c.Name, "data": data},
	}), nil
}

// --- apply ------------------------------------------------------------------------

// apply executes an action as the superuser (changes are attributed to
// them, "via AI assistant").
func (a *Assistant) apply(re *core.RequestEvent, act *Action) (map[string]any, error) {
	ctx := actor.With(re.Request.Context(), actor.Actor{
		Type: actor.TypeSuperuser, Id: re.Auth.Id, Label: re.Auth.Email() + " (AI assistant)",
	})
	app := re.App
	p := act.Payload
	switch act.Type {
	case ActionDashboard:
		var dashboardId string
		err := app.RunInTransaction(func(tx core.App) error {
			if id, _ := p["dashboard_id"].(string); id != "" {
				if _, err := tx.FindRecordById("dashboards", id); err != nil {
					return errors.New("the dashboard doesn't exist anymore")
				}
				dashboardId = id
			} else {
				col, err := tx.FindCollectionByNameOrId("dashboards")
				if err != nil {
					return err
				}
				d := core.NewRecord(col)
				d.Set("name", p["name"])
				d.Set("note", p["note"])
				d.Set("icon", "ri-sparkling-2-line")
				if err := tx.SaveWithContext(ctx, d); err != nil {
					return err
				}
				dashboardId = d.Id
			}
			col, err := tx.FindCollectionByNameOrId("dashboard_panels")
			if err != nil {
				return err
			}
			panels, _ := p["panels"].([]any)
			for _, raw := range panels {
				pm, _ := raw.(map[string]any)
				r := core.NewRecord(col)
				r.Set("dashboard", dashboardId)
				for _, k := range []string{"type", "name", "options", "show_header", "x", "y", "w", "h"} {
					r.Set(k, pm[k])
				}
				if err := tx.SaveWithContext(ctx, r); err != nil {
					return fmt.Errorf("panel %v: %w", pm["name"], err)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"link": "#/dashboards/" + dashboardId, "label": "Open the dashboard"}, nil

	case ActionTemplate:
		rec, err := createTemplate(app, app, p, false, func(r *core.Record) error { return app.SaveWithContext(ctx, r) })
		if err != nil {
			return nil, err
		}
		return map[string]any{"link": "#/emails/templates/" + rec.Id, "label": "Open the template"}, nil

	case ActionCampaign:
		templateId, _ := p["template_id"].(string)
		if templateId == "" {
			draft, _ := p["template"].(map[string]any)
			rec, err := createTemplate(app, app, draft, true, func(r *core.Record) error { return app.SaveWithContext(ctx, r) })
			if err != nil {
				return nil, fmt.Errorf("template: %w", err)
			}
			templateId = rec.Id
			p["template_id"] = rec.Id // a retry reuses it
		}
		req := mail.BulkRequest{
			TemplateId:     templateId,
			Collection:     fmt.Sprint(p["collection"]),
			RecordIds:      argStrings(p, "record_ids"),
			Filter:         argString(p, "filter"),
			RecipientField: argString(p, "recipient_field"),
			Dedupe:         true,
		}
		if s := argString(p, "send_at"); s != "" {
			req.SendAt, _ = time.Parse(time.RFC3339, s)
		}
		res, err := a.mail.SendBulk(app, req)
		if err != nil {
			return nil, err
		}
		return map[string]any{"link": "#/emails/log", "label": "Open the email log", "queued": res.Queued, "unsubscribed": res.Unsubscribed, "duplicates": res.Duplicates}, nil

	case ActionRecord:
		c, err := app.FindCollectionByNameOrId(fmt.Sprint(p["collection"]))
		if err != nil {
			return nil, errors.New("the collection doesn't exist anymore")
		}
		rec := core.NewRecord(c)
		data, _ := p["data"].(map[string]any)
		for k, v := range data {
			rec.Set(k, v)
		}
		if err := app.SaveWithContext(ctx, rec); err != nil {
			return nil, err
		}
		return map[string]any{"link": "#/collections?collection=" + c.Name + "&record=" + rec.Id, "label": "Open the record", "id": rec.Id}, nil
	case ActionFlow:
		return applyFlow(ctx, app, p)

	case ActionFlowRun:
		return a.applyFlowRun(ctx, app, p)
	}
	if fn := extensionApply(act.Type); fn != nil {
		return fn(ctx, app, re.Auth, p)
	}
	return nil, fmt.Errorf("unknown action type %q", act.Type)
}
