package mail_test

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app      *tests.TestApp
	engine   *mail.Engine
	template *core.Record
	customer *core.Record
}

func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	f := &fixture{app: app, engine: mail.Register(app)}
	f.engine.DisableKick = true

	users, _ := app.FindCollectionByNameOrId("users")
	testutil.CreateCollection(t, app, "orders",
		&core.TextField{Name: "number"},
		&core.TextField{Name: "status"},
		&core.EmailField{Name: "email"},
		&core.RelationField{Name: "customer", CollectionId: users.Id, MaxSelect: 1},
	)
	f.customer = testutil.Create(t, app, "users", map[string]any{"email": "customer@test.com", "password": "1234567890"})
	f.template = testutil.Create(t, app, "email_templates", map[string]any{
		"name":    "Order update",
		"slug":    "order-update",
		"subject": "Order {{.record.number}}",
		"html":    "<p>Order {{.record.number}} is {{.record.status}}</p>{{range .records}}<li>{{.number}}</li>{{end}}",
	})
	return f
}

func (f *fixture) automation(t testing.TB, data map[string]any) *core.Record {
	data["name"] = "test"
	data["active"] = true
	data["template"] = f.template.Id
	return testutil.Create(t, f.app, "email_automations", data)
}

func (f *fixture) messages(t testing.TB, status string) []*core.Record {
	list, err := f.app.FindRecordsByFilter("email_messages", "status = {:s}", "", 0, 0, map[string]any{"s": status})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestRecordEventAutomation(t *testing.T) {
	f := setup(t)
	f.automation(t, map[string]any{
		"trigger":           "record_event",
		"collection":        "orders",
		"events":            []string{"create", "update"},
		"filter":            "status = 'paid'",
		"recipient_field":   "email",
		"static_recipients": "shop@test.com",
	})

	order := testutil.Create(t, f.app, "orders", map[string]any{"number": "A1", "status": "new", "email": "buyer@test.com"})
	if n := len(f.messages(t, mail.StatusPending)); n != 0 {
		t.Fatalf("expected no messages for an unpaid order, got %d", n)
	}

	order.Set("status", "paid")
	if err := f.app.Save(order); err != nil {
		t.Fatal(err)
	}
	if n := len(f.messages(t, mail.StatusPending)); n != 2 {
		t.Fatalf("expected 2 pending messages (buyer + shop), got %d", n)
	}

	f.engine.ProcessQueue()
	if f.app.TestMailer.TotalSend() != 2 {
		t.Fatalf("expected 2 sent emails, got %d", f.app.TestMailer.TotalSend())
	}
	sent := f.messages(t, mail.StatusSent)
	if len(sent) != 2 || sent[0].GetString("subject") != "Order A1" {
		t.Fatalf("unexpected sent messages %v", sent)
	}
	if !strings.Contains(f.app.TestMailer.LastMessage().HTML, "Order A1 is paid") {
		t.Fatalf("unexpected html %q", f.app.TestMailer.LastMessage().HTML)
	}
}

func TestRelationRecipientAndDeleteEvent(t *testing.T) {
	f := setup(t)
	f.automation(t, map[string]any{
		"trigger":         "record_event",
		"collection":      "orders",
		"events":          []string{"delete"},
		"filter":          "status = 'paid'",
		"recipient_field": "customer.email",
	})

	paid := testutil.Create(t, f.app, "orders", map[string]any{"number": "A1", "status": "paid", "customer": f.customer.Id})
	unpaid := testutil.Create(t, f.app, "orders", map[string]any{"number": "A2", "status": "new", "customer": f.customer.Id})
	for _, r := range []*core.Record{paid, unpaid} {
		if err := f.app.Delete(r); err != nil {
			t.Fatal(err)
		}
	}

	pending := f.messages(t, mail.StatusPending)
	if len(pending) != 1 || pending[0].GetString("to") != "customer@test.com" || pending[0].GetString("subject") != "Order A1" {
		t.Fatalf("expected one message for the deleted paid order, got %v", pending)
	}
}

func TestDelayedAutomationWithRecheck(t *testing.T) {
	f := setup(t)
	f.automation(t, map[string]any{
		"trigger":         "delayed",
		"collection":      "orders",
		"events":          []string{"create"},
		"filter":          "status = 'new'",
		"delay_minutes":   60,
		"recheck_filter":  true,
		"recipient_field": "email",
	})

	stillNew := testutil.Create(t, f.app, "orders", map[string]any{"number": "A1", "status": "new", "email": "a@test.com"})
	paidLater := testutil.Create(t, f.app, "orders", map[string]any{"number": "A2", "status": "new", "email": "b@test.com"})

	pending := f.messages(t, mail.StatusPending)
	if len(pending) != 2 || pending[0].GetDateTime("send_at").Time().Before(time.Now().Add(59*time.Minute)) {
		t.Fatalf("expected 2 messages delayed by 1h, got %v", pending)
	}

	f.engine.ProcessQueue()
	if f.app.TestMailer.TotalSend() != 0 {
		t.Fatal("delayed messages must not be sent before their time")
	}

	// fast forward and change one record so that it no longer matches
	stillNew.Set("number", "A1-updated")
	paidLater.Set("status", "paid")
	for _, r := range []*core.Record{stillNew, paidLater} {
		if err := f.app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range pending {
		m.Set("send_at", time.Now().Add(-time.Minute).UTC())
		if err := f.app.Save(m); err != nil {
			t.Fatal(err)
		}
	}

	f.engine.ProcessQueue()
	if f.app.TestMailer.TotalSend() != 1 {
		t.Fatalf("expected 1 sent email, got %d", f.app.TestMailer.TotalSend())
	}
	if got := f.app.TestMailer.LastMessage().Subject; got != "Order A1-updated" {
		t.Fatalf("expected the message to be re-rendered with fresh data, got %q", got)
	}
	if len(f.messages(t, mail.StatusSkipped)) != 1 {
		t.Fatal("expected the paid order message to be skipped")
	}
}

func TestScheduleDigest(t *testing.T) {
	f := setup(t)
	a := f.automation(t, map[string]any{
		"trigger":           "schedule",
		"cron":              "0 9 * * 1",
		"collection":        "orders",
		"filter":            "status = 'new'",
		"static_recipients": "shop@test.com",
	})
	testutil.Create(t, f.app, "orders", map[string]any{"number": "A1", "status": "new"})
	testutil.Create(t, f.app, "orders", map[string]any{"number": "A2", "status": "new"})
	testutil.Create(t, f.app, "orders", map[string]any{"number": "A3", "status": "paid"})

	found := false
	for _, j := range f.app.Cron().Jobs() {
		if j.Id() == "email_auto_"+a.Id {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the schedule cron job to be registered")
	}

	n, err := f.engine.RunSchedule(a.Id)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 digest, got %d (%v)", n, err)
	}
	html := f.messages(t, mail.StatusPending)[0].GetString("html")
	if !strings.Contains(html, "<li>A1</li>") || !strings.Contains(html, "<li>A2</li>") || strings.Contains(html, "A3") {
		t.Fatalf("unexpected digest html %q", html)
	}
}

func TestValidation(t *testing.T) {
	f := setup(t)

	tplCol, _ := f.app.FindCollectionByNameOrId("email_templates")
	bad := core.NewRecord(tplCol)
	bad.Load(map[string]any{"name": "x", "slug": "x", "subject": "x", "html": "{{.record.name"})
	if err := f.app.Save(bad); err == nil || !strings.Contains(err.Error(), "html") {
		t.Fatalf("expected html template error, got %v", err)
	}

	autoCol, _ := f.app.FindCollectionByNameOrId("email_automations")
	for name, data := range map[string]map[string]any{
		"bad cron":          {"trigger": "schedule", "cron": "nope", "static_recipients": "a@test.com"},
		"bad filter":        {"trigger": "record_event", "collection": "orders", "events": []string{"create"}, "filter": "unknown_field = 1", "recipient_field": "email"},
		"bad recipient":     {"trigger": "record_event", "collection": "orders", "events": []string{"create"}, "recipient_field": "number.email"},
		"internal coll":     {"trigger": "record_event", "collection": "email_messages", "events": []string{"create"}, "static_recipients": "a@test.com"},
		"missing recipient": {"trigger": "record_event", "collection": "orders", "events": []string{"create"}},
	} {
		r := core.NewRecord(autoCol)
		data["name"] = name
		data["template"] = f.template.Id
		r.Load(data)
		if err := f.app.Save(r); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestManualSendRoute(t *testing.T) {
	var f *fixture
	headers := map[string]string{}
	body := &bytes.Buffer{}
	(&tests.ApiScenario{
		Name:    "manual send to filtered records",
		Method:  http.MethodPost,
		URL:     "/api/emails/send",
		Body:    body,
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			testutil.Create(t, f.app, "orders", map[string]any{"number": "A1", "status": "new", "email": "a@test.com"})
			testutil.Create(t, f.app, "orders", map[string]any{"number": "A2", "status": "paid", "email": "b@test.com"})
			return f.app
		},
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			admin := testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
			headers["Authorization"] = testutil.AuthToken(t, admin)
			fmt.Fprintf(body, `{"templateId":%q,"collection":"orders","filter":"status = 'new'","recipientField":"email"}`, f.template.Id)
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"queued":1`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			pending := f.messages(t, mail.StatusPending)
			if len(pending) != 1 || pending[0].GetString("to") != "a@test.com" {
				t.Fatalf("unexpected queue %v", pending)
			}
		},
	}).Test(t)
}
