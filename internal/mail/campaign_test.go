package mail_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

// campaign: two customers, one with two orders, the other unsubscribed
func campaignSetup(t testing.TB) (*fixture, *core.Record, *core.Record) {
	f := setup(t)
	f.app.Settings().Meta.AppURL = "https://shop.test"
	f.app.Settings().Meta.AppName = "Shop"
	tpl := testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Promo", "slug": "promo", "subject": "15% off", "marketing": true,
		"html": "<html><body><p>Use PROMO15</p></body></html>", "text": "Use PROMO15",
	})
	other := testutil.Create(t, f.app, "users", map[string]any{"email": "Other@Test.com", "password": "1234567890"})
	for _, c := range []*core.Record{f.customer, f.customer, other} {
		testutil.Create(t, f.app, "orders", map[string]any{"number": "1", "customer": c.Id})
	}
	if err := mail.Unsubscribe(f.app, "other@test.com", "manual"); err != nil {
		t.Fatal(err)
	}
	return f, tpl, other
}

func TestBulkSendDedupeOptOutAndLink(t *testing.T) {
	f, tpl, _ := campaignSetup(t)
	req := mail.BulkRequest{TemplateId: tpl.Id, Collection: "orders", RecipientField: "customer.email", Dedupe: true, DryRun: true}

	res, err := f.engine.SendBulk(f.app, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Records != 3 || res.Queued != 1 || res.Duplicates != 1 || res.Unsubscribed != 1 || !res.Marketing || len(res.Sample) != 1 {
		t.Fatalf("unexpected dry run: %+v", res)
	}
	if n := len(f.messages(t, mail.StatusPending)); n != 0 {
		t.Fatalf("a dry run must not queue, got %d", n)
	}

	req.DryRun = false
	if res, err = f.engine.SendBulk(f.app, req); err != nil || res.Queued != 1 {
		t.Fatalf("send: %+v %v", res, err)
	}
	msgs := f.messages(t, mail.StatusPending)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 queued message, got %d", len(msgs))
	}
	link := mail.UnsubscribeURL(f.app, "customer@test.com")
	html := msgs[0].GetString("html")
	if !strings.HasPrefix(link, "https://shop.test/email/unsubscribe?e=customer%40test.com&t=") ||
		!strings.Contains(html, `Unsubscribe</a></p></body>`) || !strings.Contains(html, strings.ReplaceAll(link, "&", "&amp;")) {
		t.Errorf("missing unsubscribe link in %s", html)
	}
	if !strings.Contains(msgs[0].GetString("text"), "Unsubscribe: "+link) {
		t.Errorf("missing unsubscribe link in the text: %s", msgs[0].GetString("text"))
	}

	// sent with the one-click headers
	f.engine.ProcessQueue()
	sent := f.app.TestMailer.LastMessage()
	if sent.Headers["List-Unsubscribe"] != "<"+link+">" || sent.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Errorf("unexpected headers: %v", sent.Headers)
	}

	// without dedupe: one email per record
	req.Dedupe, req.DryRun = false, true
	if res, _ = f.engine.SendBulk(f.app, req); res.Queued != 2 {
		t.Errorf("expected 2 emails without dedupe, got %+v", res)
	}
}

func TestOptOutAfterQueueingAndTransactional(t *testing.T) {
	f, tpl, _ := campaignSetup(t)
	if _, err := f.engine.SendBulk(f.app, mail.BulkRequest{TemplateId: tpl.Id, StaticRecipients: "customer@test.com"}); err != nil {
		t.Fatal(err)
	}
	// unsubscribes before the queue runs: skipped
	if err := mail.Unsubscribe(f.app, "CUSTOMER@test.com", "link"); err != nil {
		t.Fatal(err)
	}
	f.engine.ProcessQueue()
	if n := len(f.messages(t, mail.StatusSkipped)); n != 1 || f.app.TestMailer.TotalSend() != 0 {
		t.Errorf("expected the message to be skipped, skipped=%d sent=%d", n, f.app.TestMailer.TotalSend())
	}

	// transactional templates ignore the opt-out list and have no link
	if _, err := f.engine.SendBulk(f.app, mail.BulkRequest{TemplateId: f.template.Id, StaticRecipients: "customer@test.com"}); err != nil {
		t.Fatal(err)
	}
	f.engine.ProcessQueue()
	if f.app.TestMailer.TotalSend() != 1 || strings.Contains(f.app.TestMailer.LastMessage().HTML, "unsubscribe") {
		t.Errorf("transactional email: sent=%d html=%s", f.app.TestMailer.TotalSend(), f.app.TestMailer.LastMessage().HTML)
	}
}

func TestUnsubscribePage(t *testing.T) {
	var link string
	factory := func(t testing.TB) *tests.TestApp {
		f, _, _ := campaignSetup(t)
		link = mail.UnsubscribeURL(f.app, "customer@test.com")
		return f.app
	}
	path := func() string { return strings.TrimPrefix(link, "https://shop.test") }

	confirm := &tests.ApiScenario{Name: "confirmation page", Method: http.MethodGet, TestAppFactory: factory,
		ExpectedStatus: 200, ExpectedContent: []string{"Stop sending marketing emails to customer@test.com?", `<form method="post"`}}
	confirm.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { confirm.URL = path() }
	confirm.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) {
		if mail.IsUnsubscribed(app, "customer@test.com") {
			t.Error("GET must not unsubscribe (link scanners)")
		}
	}
	confirm.Test(t)

	oneClick := &tests.ApiScenario{Name: "one-click POST", Method: http.MethodPost, TestAppFactory: factory,
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:    strings.NewReader("List-Unsubscribe=One-Click"), ExpectedStatus: 200, ExpectedContent: []string{"unsubscribed"}}
	oneClick.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { oneClick.URL = path() }
	oneClick.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) {
		if !mail.IsUnsubscribed(app, "customer@test.com") {
			t.Error("expected the address to be unsubscribed")
		}
	}
	oneClick.Test(t)

	(&tests.ApiScenario{Name: "forged token", Method: http.MethodGet, TestAppFactory: factory,
		URL:            "/email/unsubscribe?e=" + url.QueryEscape("customer@test.com") + "&t=forged",
		ExpectedStatus: 400, ExpectedContent: []string{"Invalid link"}}).Test(t)
}
