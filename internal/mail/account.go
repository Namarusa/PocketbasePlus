package mail

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Account emails: the emails PocketBase sends to the accounts of the auth
// collections (verification, password reset, email change, one-time code,
// sign-in alert). A template with "account_email" replaces one of them, for
// one auth collection (account_collection) or all of them: "" every auth
// collection but the superusers, "*" every one including them (a single
// "all" template per email; a collection's own template wins). The data
// of these templates: .record (the account), .app, .recipient.email, and
// .link / .token, .otp, .alert_info, .new_email depending on the email.
// Without a template, or when it fails, PocketBase's own email is sent.

// AccountEmail describes an account email.
type AccountEmail struct {
	Kind        string   `json:"kind"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Variables   []string `json:"variables"` // besides .record, .app and .recipient
	linkPath    string   // the page of the dashboard the link opens (with the token)
	template    func(*core.Collection) core.EmailTemplate
}

// AccountEmails lists the account emails, in the order of the dashboard.
var AccountEmails = []AccountEmail{
	{Kind: "verification", Label: "Verification", Description: "Confirms the address of a new account.",
		Variables: []string{".link", ".token"}, linkPath: "confirm-verification",
		template: func(c *core.Collection) core.EmailTemplate { return c.VerificationTemplate }},
	{Kind: "password_reset", Label: "Password reset", Description: "The link to choose a new password.",
		Variables: []string{".link", ".token"}, linkPath: "confirm-password-reset",
		template: func(c *core.Collection) core.EmailTemplate { return c.ResetPasswordTemplate }},
	{Kind: "email_change", Label: "Email change", Description: "Confirms the new address of an account (sent to it).",
		Variables: []string{".link", ".token", ".new_email"}, linkPath: "confirm-email-change",
		template: func(c *core.Collection) core.EmailTemplate { return c.ConfirmEmailChangeTemplate }},
	{Kind: "otp", Label: "One-time code", Description: "The code to sign in without a password (OTP).",
		Variables: []string{".otp", ".otp_id"},
		template:  func(c *core.Collection) core.EmailTemplate { return c.OTP.EmailTemplate }},
	{Kind: "login_alert", Label: "Sign-in alert", Description: "A sign-in from a new device or location.",
		Variables: []string{".alert_info"},
		template:  func(c *core.Collection) core.EmailTemplate { return c.AuthAlert.EmailTemplate }},
}

// ErrAccountTemplate refuses to send an account email template otherwise.
var ErrAccountTemplate = errors.New("this template replaces an account email (PocketBase sends it, with its link or code): choose another one")

// IsAccountTemplate reports whether the template replaces an account email.
func IsAccountTemplate(tpl *core.Record) bool {
	return tpl.GetString("account_email") != ""
}

func accountEmail(kind string) (AccountEmail, bool) {
	for _, a := range AccountEmails {
		if a.Kind == kind {
			return a, true
		}
	}
	return AccountEmail{}, false
}

func bindAccountEmails(app core.App) {
	bind := func(h *hook.TaggedHook[*core.MailerRecordEvent], kind string) {
		h.BindFunc(func(e *core.MailerRecordEvent) error {
			replaceAccountEmail(e, kind)
			return e.Next()
		})
	}
	bind(app.OnMailerRecordVerificationSend(), "verification")
	bind(app.OnMailerRecordPasswordResetSend(), "password_reset")
	bind(app.OnMailerRecordEmailChangeSend(), "email_change")
	bind(app.OnMailerRecordOTPSend(), "otp")
	bind(app.OnMailerRecordAuthAlertSend(), "login_alert")
}

// AllAuthCollections is the account_collection of a template used for every
// auth collection, the superusers included ("" leaves them out).
const AllAuthCollections = "*"

// accountTemplate is the template replacing an account email of the
// collection: the one of that collection, else the one of all of them.
func accountTemplate(app core.App, kind string, col *core.Collection) *core.Record {
	list, err := app.FindRecordsByFilter(templatesCollection, "account_email = {:kind}", "", 0, 0, map[string]any{"kind": kind})
	if err != nil {
		return nil
	}
	var all *core.Record
	for _, r := range list {
		switch r.GetString("account_collection") {
		case col.Name:
			return r
		case "", AllAuthCollections:
			all = r
		}
	}
	if all != nil && col.Name == core.CollectionNameSuperusers && all.GetString("account_collection") != AllAuthCollections {
		return nil
	}
	return all
}

// replaceAccountEmail renders the template of the account email, if any, in
// place of PocketBase's.
func replaceAccountEmail(e *core.MailerRecordEvent, kind string) {
	tpl := accountTemplate(e.App, kind, e.Record.Collection())
	if tpl == nil || e.Message == nil {
		return
	}
	to := ""
	if len(e.Message.To) > 0 {
		to = e.Message.To[0].Address
	}
	data := TemplateData(e.App, e.Record, nil, to)
	delete(data, "unsubscribe_url") // account emails are never marketing
	for k, v := range accountValues(e.App, kind, e.Meta) {
		data[k] = v
	}
	out, err := Render(tpl.GetString("subject"), tpl.GetString("html"), tpl.GetString("text"), data)
	if err != nil {
		e.App.Logger().Error("mail: the account email template failed, PocketBase's email is sent instead", "template", tpl.Id, "kind", kind, "error", err)
		return
	}
	e.Message.Subject = out.Subject
	e.Message.HTML = out.HTML
	e.Message.Text = out.Text
}

// accountValues are the values of an account email from PocketBase's meta
// (token, otpId, password, info, newEmail).
func accountValues(app core.App, kind string, meta map[string]any) map[string]any {
	str := func(k string) string { s, _ := meta[k].(string); return s }
	out := map[string]any{}
	a, _ := accountEmail(kind)
	if token := str("token"); token != "" {
		out["token"] = token
		if a.linkPath != "" {
			out["link"] = strings.TrimRight(app.Settings().Meta.AppURL, "/") + "/_/#/auth/" + a.linkPath + "/" + token
		}
	}
	if kind == "otp" {
		out["otp"] = str("password")
		out["otp_id"] = str("otpId")
	}
	if kind == "login_alert" {
		// PocketBase escapes it for its own HTML: the templates escape it again
		out["alert_info"] = html.UnescapeString(str("info"))
	}
	if kind == "email_change" {
		out["new_email"] = str("newEmail")
	}
	return out
}

// SampleAccountValues are the values of the previews and the test emails.
func SampleAccountValues(app core.App, kind string) map[string]any {
	return accountValues(app, kind, map[string]any{
		"token": "sample-token", "otpId": "sample-otp-id", "password": "123456",
		"info": "Chrome on Windows, from 203.0.113.7", "newEmail": "new-address@example.com",
	})
}

// CheckAccountTemplate checks that an account email of a collection ("":
// all of them) has no template yet, before proposing one.
func CheckAccountTemplate(app core.App, kind, collection string) error {
	col, _ := app.FindCachedCollectionByNameOrId(templatesCollection)
	r := core.NewRecord(col)
	r.Set("account_email", kind)
	r.Set("account_collection", collection)
	if _, ok := accountEmail(kind); !ok {
		return fmt.Errorf("unknown account email %q", kind)
	}
	errs := validation.Errors{}
	validateAccountTemplate(app, r, errs)
	return errs.Filter()
}

func validateAccountTemplate(app core.App, r *core.Record, errs validation.Errors) {
	kind := r.GetString("account_email")
	if kind == "" {
		r.Set("account_collection", "")
		return
	}
	r.Set("marketing", false)
	r.Set("folder", "")
	name := r.GetString("account_collection")
	if name != "" && name != AllAuthCollections {
		c, err := app.FindCachedCollectionByNameOrId(name)
		if err != nil || !c.IsAuth() {
			errs["account_collection"] = validation.NewError("invalid_collection", "Choose an auth collection.")
			return
		}
		r.Set("account_collection", c.Name)
		name = c.Name
	}
	// a single template for all the collections, with or without the superusers
	filter := "account_email = {:kind} && account_collection = {:col} && id != {:id}"
	if name == "" || name == AllAuthCollections {
		filter = "account_email = {:kind} && (account_collection = '' || account_collection = '*') && id != {:id}"
	}
	other, err := app.FindFirstRecordByFilter(templatesCollection, filter, map[string]any{"kind": kind, "col": name, "id": r.Id})
	if err == nil {
		a, _ := accountEmail(kind)
		who := "all the auth collections"
		if name != "" && name != AllAuthCollections {
			who = name
		}
		errs["account_email"] = validation.NewError("account_email_taken",
			fmt.Sprintf("The %s email of %s is already “%s”: edit it, or delete it first.", strings.ToLower(a.Label), who, other.GetString("name")))
	}
}

var (
	pbPlaceholderRegex = regexp.MustCompile(`\{RECORD:([A-Za-z0-9_]+)\}`)
	pbButtonRegex      = regexp.MustCompile(`<a class="btn" `)
)

// accountStarter turns the collection's PocketBase template into a template
// of the designer (its placeholders into variables, a layout with inline
// styles in place of PocketBase's).
func accountStarter(a AccountEmail, col *core.Collection) (subject, body string) {
	tpl := a.template(col)
	r := strings.NewReplacer(
		core.EmailPlaceholderAppURL+"/_/#/auth/"+a.linkPath+"/"+core.EmailPlaceholderToken, "{{.link}}",
		core.EmailPlaceholderAppName, "{{.app.name}}",
		core.EmailPlaceholderAppURL, "{{.app.url}}",
		core.EmailPlaceholderToken, "{{.token}}",
		core.EmailPlaceholderOTPId, "{{.otp_id}}",
		core.EmailPlaceholderOTP, "{{.otp}}",
		core.EmailPlaceholderAlertInfo, "{{.alert_info}}",
	)
	convert := func(s string) string {
		return pbPlaceholderRegex.ReplaceAllString(r.Replace(s), "{{.record.$1}}")
	}
	body = pbButtonRegex.ReplaceAllString(convert(tpl.Body),
		`<a style="display:inline-block;padding:10px 18px;border-radius:6px;background:#16161a;color:#fff;text-decoration:none;font-weight:600" `)
	body = `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;font-size:15px;line-height:1.5;color:#16161a;max-width:560px;margin:0 auto;padding:24px">
` + body + `
</div>`
	return convert(tpl.Subject), body
}

// customizeAccount creates the template of an account email, from the
// collection's PocketBase template.
func (e *Engine) customizeAccount(re *core.RequestEvent) error {
	var body struct {
		Kind       string `json:"kind"`
		Collection string `json:"collection"`
	}
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}
	a, ok := accountEmail(body.Kind)
	if !ok {
		return re.BadRequestError("Unknown account email.", nil)
	}
	// the starter comes from the collection, or "users" (else PocketBase's defaults)
	all := body.Collection == "" || body.Collection == AllAuthCollections
	source := body.Collection
	if all {
		source = "users"
	}
	col, err := re.App.FindCachedCollectionByNameOrId(source)
	if err != nil || !col.IsAuth() {
		if !all {
			return re.BadRequestError("Choose an auth collection.", nil)
		}
		col = core.NewAuthCollection("users")
	}
	subject, html := accountStarter(a, col)
	templates, err := re.App.FindCachedCollectionByNameOrId(templatesCollection)
	if err != nil {
		return err
	}
	name := a.Label
	suffix := body.Collection
	switch body.Collection {
	case "":
	case AllAuthCollections:
		name += " (all, superusers included)"
		suffix = "all"
	default:
		name += " (" + body.Collection + ")"
	}
	rec := core.NewRecord(templates)
	rec.Load(map[string]any{
		"name": name, "slug": uniqueTemplateSlug(re.App, "account-"+strings.ReplaceAll(a.Kind, "_", "-")+"-"+strings.Trim(slugChars.ReplaceAllString(strings.ToLower(suffix), "-"), "-")),
		"subject": subject, "html": html, "account_email": a.Kind, "account_collection": body.Collection,
		"sample_collection": col.Name,
	})
	if col.Id == "" { // PocketBase's defaults: no collection to preview with
		rec.Set("sample_collection", "")
	}
	if err := re.App.Save(rec); err != nil {
		return re.BadRequestError("Failed to create the template.", err)
	}
	return re.JSON(http.StatusOK, map[string]any{"id": rec.Id})
}

var slugChars = regexp.MustCompile(`[^a-z0-9]+`)

func uniqueTemplateSlug(app core.App, base string) string {
	base = strings.Trim(base, "-")
	slug := base
	for i := 2; ; i++ {
		if _, err := app.FindFirstRecordByData(templatesCollection, "slug", slug); err != nil {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func listAccountEmails(re *core.RequestEvent) error {
	return re.JSON(http.StatusOK, AccountEmails)
}
