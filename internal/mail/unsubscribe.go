package mail

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Marketing emails (templates flagged "marketing") get an unsubscribe link
// and List-Unsubscribe headers, and are never sent to the addresses of
// email_unsubscribes. Transactional emails are not affected.

const (
	unsubscribesCollection = "email_unsubscribes"

	// UnsubscribeSecretEnv sets the key signing the unsubscribe links. By
	// default it's derived from the superusers token secret (rotating that
	// secret invalidates the links of the emails already sent).
	UnsubscribeSecretEnv = "POCKETBASEPLUS_UNSUBSCRIBE_SECRET"
)

func unsubscribeKey(app core.App) []byte {
	if v := os.Getenv(UnsubscribeSecretEnv); v != "" {
		return []byte(v)
	}
	base := ""
	if c, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers); err == nil {
		base = c.AuthToken.Secret
	}
	m := hmac.New(sha256.New, []byte(base))
	// the project's former name, kept so the links already sent stay valid
	m.Write([]byte("gopocket-unsubscribe"))
	return m.Sum(nil)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// UnsubscribeToken signs an address.
func UnsubscribeToken(app core.App, email string) string {
	m := hmac.New(sha256.New, unsubscribeKey(app))
	m.Write([]byte(normalizeEmail(email)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))[:24]
}

func validUnsubscribeToken(app core.App, email, token string) bool {
	return email != "" && subtle.ConstantTimeCompare([]byte(UnsubscribeToken(app, email)), []byte(token)) == 1
}

// UnsubscribeURL is the unsubscribe page of an address ("" without an
// application URL in the settings, or without address).
func UnsubscribeURL(app core.App, email string) string {
	base := strings.TrimRight(app.Settings().Meta.AppURL, "/")
	if base == "" || strings.TrimSpace(email) == "" {
		return ""
	}
	return base + "/email/unsubscribe?e=" + url.QueryEscape(normalizeEmail(email)) + "&t=" + UnsubscribeToken(app, email)
}

// IsUnsubscribed reports whether the address opted out of marketing emails.
func IsUnsubscribed(app core.App, email string) bool {
	r, err := app.FindFirstRecordByData(unsubscribesCollection, "email", normalizeEmail(email))
	return err == nil && r != nil
}

// Unsubscribe adds the address to the opt-out list (no-op when present).
func Unsubscribe(app core.App, email, source string) error {
	email = normalizeEmail(email)
	if IsUnsubscribed(app, email) {
		return nil
	}
	col, err := app.FindCollectionByNameOrId(unsubscribesCollection)
	if err != nil {
		return err
	}
	r := core.NewRecord(col)
	r.Set("email", email)
	r.Set("source", source)
	return app.Save(r)
}

// withUnsubscribe adds an unsubscribe link to the content of a marketing
// email, unless the template already shows {{.unsubscribe_url}}.
func withUnsubscribe(out Rendered, link string) Rendered {
	if link == "" {
		return out
	}
	escaped := html.EscapeString(link)
	if out.HTML != "" && !strings.Contains(out.HTML, link) && !strings.Contains(out.HTML, escaped) {
		footer := `<p style="margin:24px 0 0;font-size:12px;color:#888;text-align:center">` +
			`<a href="` + escaped + `" style="color:#888">Unsubscribe</a></p>`
		if i := strings.LastIndex(strings.ToLower(out.HTML), "</body>"); i >= 0 {
			out.HTML = out.HTML[:i] + footer + out.HTML[i:]
		} else {
			out.HTML += footer
		}
	}
	if out.Text != "" && !strings.Contains(out.Text, link) {
		out.Text += "\n\nUnsubscribe: " + link
	}
	return out
}

// --- public page ---------------------------------------------------------------

func unsubscribePage(app core.App, title, message, form string) string {
	name := html.EscapeString(app.Settings().Meta.AppName)
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex">` +
		`<title>` + html.EscapeString(title) + ` - ` + name + `</title><style>` +
		`body{margin:0;font-family:system-ui,sans-serif;background:#f4f5f7;color:#1a1a1a;display:flex;min-height:100vh;align-items:center;justify-content:center}` +
		`main{background:#fff;max-width:420px;margin:16px;padding:32px;border-radius:12px;box-shadow:0 2px 12px rgba(0,0,0,.08);text-align:center}` +
		`h1{font-size:20px;margin:0 0 12px}p{color:#555;line-height:1.5}button{font:inherit;padding:10px 20px;border:0;border-radius:8px;background:#1a1a1a;color:#fff;cursor:pointer}` +
		`@media (prefers-color-scheme:dark){body{background:#16171a;color:#eee}main{background:#222327}p{color:#aaa}button{background:#eee;color:#111}}` +
		`</style></head><body><main><h1>` + html.EscapeString(title) + `</h1><p>` + message + `</p>` + form + `</main></body></html>`
}

func writePage(re *core.RequestEvent, status int, body string) error {
	re.Response.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	re.Response.Header().Set("Referrer-Policy", "no-referrer")
	return re.HTML(status, body)
}

// unsubscribeRoute shows a confirmation page (GET, so that link scanners
// don't unsubscribe anybody) and unsubscribes on POST, including the
// RFC 8058 one-click POST of mail clients.
func unsubscribeRoute(re *core.RequestEvent) error {
	q := re.Request.URL.Query()
	email, token := q.Get("e"), q.Get("t")
	if re.Request.Method == http.MethodPost {
		_ = re.Request.ParseForm()
		if v := re.Request.PostForm.Get("e"); v != "" {
			email, token = v, re.Request.PostForm.Get("t")
		}
	}
	if !validUnsubscribeToken(re.App, email, token) {
		return writePage(re, http.StatusBadRequest, unsubscribePage(re.App, "Invalid link",
			"This unsubscribe link is invalid or has expired. Use the link of a recent email.", ""))
	}

	if re.Request.Method == http.MethodPost {
		source := "link"
		if re.Request.PostForm.Get("List-Unsubscribe") == "One-Click" {
			source = "one-click"
		}
		if err := Unsubscribe(re.App, email, source); err != nil {
			return writePage(re, http.StatusInternalServerError, unsubscribePage(re.App, "Something went wrong", "Please try again later.", ""))
		}
		return writePage(re, http.StatusOK, unsubscribePage(re.App, "You're unsubscribed",
			html.EscapeString(email)+" won't receive our marketing emails anymore.", ""))
	}

	if IsUnsubscribed(re.App, email) {
		return writePage(re, http.StatusOK, unsubscribePage(re.App, "Already unsubscribed",
			html.EscapeString(email)+" doesn't receive our marketing emails.", ""))
	}
	form := `<form method="post" action="/email/unsubscribe"><input type="hidden" name="e" value="` + html.EscapeString(email) +
		`"><input type="hidden" name="t" value="` + html.EscapeString(token) + `"><button type="submit">Unsubscribe</button></form>`
	return writePage(re, http.StatusOK, unsubscribePage(re.App, "Unsubscribe",
		"Stop sending marketing emails to "+html.EscapeString(email)+"?", form))
}
