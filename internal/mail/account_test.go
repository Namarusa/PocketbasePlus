package mail_test

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/mails"

	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestAccountEmails(t *testing.T) {
	f := setup(t)
	f.app.Settings().Meta.AppURL = "https://app.test"
	member := core.NewAuthCollection("members")
	member.Fields.Add(&core.TextField{Name: "name"})
	if err := f.app.Save(member); err != nil {
		t.Fatal(err)
	}
	m := testutil.Create(t, f.app, member.Name, map[string]any{"email": "m@test.com", "password": "1234567890", "name": "Ada"})

	last := func() (string, string) {
		t.Helper()
		msg := f.app.TestMailer.LastMessage()
		return msg.Subject, msg.HTML
	}

	// no template: PocketBase's email
	if err := mails.SendRecordPasswordReset(f.app, f.customer); err != nil {
		t.Fatal(err)
	}
	if s, _ := last(); !strings.HasPrefix(s, "Reset your") {
		t.Errorf("default subject: %q", s)
	}

	// a template for all the collections
	testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Reset", "slug": "reset", "account_email": "password_reset", "folder": "ignored",
		"subject": "New password for {{.record.email}}",
		"html":    `<a href="{{.link}}">Choose it</a> {{.token}}`,
	})
	if err := mails.SendRecordPasswordReset(f.app, f.customer); err != nil {
		t.Fatal(err)
	}
	s, h := last()
	if s != "New password for customer@test.com" || !strings.Contains(h, `href="https://app.test/_/#/auth/confirm-password-reset/`) {
		t.Errorf("replaced: %q %s", s, h)
	}

	// a collection's own template wins; the superusers keep PocketBase's
	testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Members reset", "slug": "members-reset", "account_email": "password_reset", "account_collection": member.Name,
		"subject": "Hi {{.record.name}}", "html": "<p>x</p>",
	})
	if err := mails.SendRecordPasswordReset(f.app, m); err != nil {
		t.Fatal(err)
	}
	if s, _ := last(); s != "Hi Ada" {
		t.Errorf("collection template: %q", s)
	}
	su := testutil.Create(t, f.app, core.CollectionNameSuperusers, map[string]any{"email": "su@test.com", "password": "1234567890"})
	if err := mails.SendRecordPasswordReset(f.app, su); err != nil {
		t.Fatal(err)
	}
	if s, _ := last(); !strings.HasPrefix(s, "Reset your") {
		t.Errorf("superusers keep the default: %q", s)
	}

	// "*": all of them, the superusers included (a single "all" template per email)
	all, _ := f.app.FindFirstRecordByData("email_templates", "slug", "reset")
	all.Set("account_collection", "*")
	if err := f.app.Save(all); err != nil {
		t.Fatal(err)
	}
	if err := mails.SendRecordPasswordReset(f.app, su); err != nil {
		t.Fatal(err)
	}
	if s, _ := last(); s != "New password for su@test.com" {
		t.Errorf("superusers included: %q", s)
	}
	if err := mails.SendRecordPasswordReset(f.app, m); err != nil {
		t.Fatal(err)
	}
	if s, _ := last(); s != "Hi Ada" {
		t.Errorf("the collection's own still wins: %q", s)
	}
	tplCol, _ := f.app.FindCollectionByNameOrId("email_templates")
	both := core.NewRecord(tplCol)
	both.Load(map[string]any{"name": "Both", "slug": "both", "account_email": "password_reset", "account_collection": "", "subject": "x", "html": "x"})
	if err := f.app.Save(both); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf(`"" and "*" together: %v`, err)
	}

	// one-time code and sign-in alert values (escaped once)
	testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Code", "slug": "code", "account_email": "otp", "subject": "Code {{.otp}}", "html": "<b>{{.otp}}</b>",
	})
	testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Alert", "slug": "alert", "account_email": "login_alert", "subject": "Alert", "html": "<p>{{.alert_info}}</p>",
	})
	if err := mails.SendRecordOTP(f.app, f.customer, "otpid", "654321"); err != nil {
		t.Fatal(err)
	}
	if s, h := last(); s != "Code 654321" || h != "<b>654321</b>" {
		t.Errorf("otp: %q %q", s, h)
	}
	if err := mails.SendRecordAuthAlert(f.app, f.customer, "Firefox <x> & co"); err != nil {
		t.Fatal(err)
	}
	if _, h := last(); h != "<p>Firefox &lt;x&gt; &amp; co</p>" {
		t.Errorf("alert info: %q", h)
	}

	// a template that fails: PocketBase's email is sent
	testutil.Create(t, f.app, "email_templates", map[string]any{
		"name": "Broken", "slug": "broken", "account_email": "verification", "subject": "V", "html": "{{index .record 5}}",
	})
	if err := mails.SendRecordVerification(f.app, f.customer); err != nil {
		t.Fatal(err)
	}
	if s, _ := last(); !strings.HasPrefix(s, "Verify your") {
		t.Errorf("fallback: %q", s)
	}

	// one template per email and collection; never marketing; no folder
	tpl, err := f.app.FindFirstRecordByData("email_templates", "slug", "reset")
	if err != nil || tpl.GetString("folder") != "" || tpl.GetBool("marketing") {
		t.Errorf("account template fields: %v", tpl)
	}
	col, _ := f.app.FindCollectionByNameOrId("email_templates")
	dup := core.NewRecord(col)
	dup.Load(map[string]any{"name": "Dup", "slug": "dup", "account_email": "password_reset", "subject": "x", "html": "x"})
	if err := f.app.Save(dup); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("duplicate: %v", err)
	}
	dup.Load(map[string]any{"account_email": "otp", "account_collection": "orders"})
	if err := f.app.Save(dup); err == nil || !strings.Contains(err.Error(), "auth collection") {
		t.Errorf("not an auth collection: %v", err)
	}

	// folders are trimmed
	f.template.Set("folder", "  Orders   and  shipping ")
	if err := f.app.Save(f.template); err != nil || f.template.GetString("folder") != "Orders and shipping" {
		t.Errorf("folder: %v %q", err, f.template.GetString("folder"))
	}

	// account templates can't be sent otherwise
	autos, _ := f.app.FindCollectionByNameOrId("email_automations")
	if _, err := f.engine.SendBulk(f.app, mail.BulkRequest{TemplateId: tpl.Id, StaticRecipients: "a@test.com"}); err == nil || !strings.Contains(err.Error(), "account email") {
		t.Errorf("bulk send: %v", err)
	}
	a := core.NewRecord(autos)
	a.Load(map[string]any{"name": "x", "template": tpl.Id, "trigger": "schedule", "cron": "0 * * * *", "static_recipients": "a@test.com"})
	if err := f.app.Save(a); err == nil || !strings.Contains(err.Error(), "account email") {
		t.Errorf("automation: %v", err)
	}
}
