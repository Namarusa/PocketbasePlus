// Package session keeps the visitors of the public pages signed in: an
// HttpOnly cookie holding a regular auth token of their record (users). It's
// only read by the pages and the page actions (never by the REST API, which
// keeps its Authorization header), so the cookie can't be used against the API.
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	cookieName = "gp_session"
	flashName  = "gp_flash"
	eventKey   = "gpSession"
	// RolesField is the field of the users listing their roles (rbac).
	RolesField = "roles"
)

// Register adds the sign out route.
func Register(app core.App) {
	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.POST("/api/session/logout", logout)
		return se.Next()
	})
}

// secure reports whether the site is served over https (directly, or behind a
// proxy trusted in the settings that says so).
func secure(e *core.RequestEvent) bool {
	if e.IsTLS() {
		return true
	}
	if len(e.App.Settings().TrustedProxy.Headers) == 0 {
		return false
	}
	proto, _, _ := strings.Cut(e.Request.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// name is the cookie name: on https with the __Host- prefix, which the
// browsers only accept from this exact origin (not from another port or
// subdomain of the host).
func name(e *core.RequestEvent, base string) string {
	if secure(e) {
		return "__Host-" + base
	}
	return base
}

func setCookie(e *core.RequestEvent, base, value string, maxAge int) {
	http.SetCookie(e.Response, &http.Cookie{
		Name: name(e, base), Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: secure(e), MaxAge: maxAge,
	})
}

func readCookie(e *core.RequestEvent, base string) string {
	if c, err := e.Request.Cookie(name(e, base)); err == nil {
		return c.Value
	}
	return ""
}

// Get returns the signed-in visitor, or nil.
func Get(e *core.RequestEvent) *core.Record {
	if v, ok := e.Get(eventKey).(*core.Record); ok {
		return v
	}
	var rec *core.Record
	if token := readCookie(e, cookieName); token != "" {
		r, err := e.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
		if err == nil && !r.IsSuperuser() {
			rec = r
		}
	}
	e.Set(eventKey, rec)
	return rec
}

// Set signs the visitor in (superusers can't be).
func Set(e *core.RequestEvent, rec *core.Record) error {
	if rec == nil || rec.IsSuperuser() || !rec.Collection().IsAuth() {
		return http.ErrNotSupported
	}
	token, err := rec.NewAuthToken()
	if err != nil {
		return err
	}
	setCookie(e, cookieName, token, int(rec.Collection().AuthToken.Duration))
	e.Set(eventKey, rec)
	return nil
}

// Clear signs the visitor out.
func Clear(e *core.RequestEvent) {
	setCookie(e, cookieName, "", -1)
	e.Set(eventKey, (*core.Record)(nil))
}

// Roles returns the role ids of a visitor.
func Roles(rec *core.Record) []string {
	if rec == nil || rec.Collection().Fields.GetByName(RolesField) == nil {
		return nil
	}
	return rec.GetStringSlice(RolesField)
}

// SameOrigin reports whether a request comes from the site itself (forms
// posted by other sites are refused).
func SameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser cross-site request (browsers send it with POST)
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// LocalPath returns p when it's a path of the site ("/..."), else "".
func LocalPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) > 2000 || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n\t") {
		return ""
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	return p
}

// --- flash messages ----------------------------------------------------------------------

// Flash is a message shown once by the next page.
type Flash struct {
	Type string `json:"type"` // success | error | info
	Text string `json:"text"`
}

// SetFlash keeps a message for the next page.
func SetFlash(e *core.RequestEvent, typ, text string) {
	if text == "" {
		return
	}
	if len(text) > 500 {
		text = text[:500]
	}
	raw, _ := json.Marshal(Flash{Type: typ, Text: text})
	setCookie(e, flashName, base64.RawURLEncoding.EncodeToString(raw), 60)
}

// TakeFlash returns the message of the page (once), or nil.
func TakeFlash(e *core.RequestEvent) *Flash {
	value := readCookie(e, flashName)
	if value == "" {
		return nil
	}
	setCookie(e, flashName, "", -1)
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil
	}
	var f Flash
	if json.Unmarshal(raw, &f) != nil || f.Text == "" {
		return nil
	}
	switch f.Type {
	case "success", "error", "info":
	default:
		f.Type = "info"
	}
	return &f
}

// Redirect answers a form post with a redirection (303: the next page is a GET).
func Redirect(e *core.RequestEvent, to string) error {
	if to = LocalPath(to); to == "" {
		to = "/"
	}
	return e.Redirect(http.StatusSeeOther, to)
}

// logout signs the visitor out and goes to ?next= (or the posted next).
func logout(e *core.RequestEvent) error {
	if !SameOrigin(e.Request) {
		return e.ForbiddenError("", nil)
	}
	Clear(e)
	next := e.Request.URL.Query().Get("next")
	if next == "" {
		next = e.Request.FormValue("_next")
	}
	SetFlash(e, "info", "You're signed out.")
	return Redirect(e, next)
}

// --- record references ---------------------------------------------------------------------

func refKey(app core.App) []byte {
	base := ""
	if c, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers); err == nil {
		base = c.AuthToken.Secret
	}
	m := hmac.New(sha256.New, []byte(base))
	// the project's former name, kept so the links already sent stay valid
	m.Write([]byte("gopocket-record-ref"))
	return m.Sum(nil)
}

func refSig(app core.App, flow, collection, id, exp string) string {
	m := hmac.New(sha256.New, refKey(app))
	m.Write([]byte(flow + "|" + collection + "|" + id + "|" + exp))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// refTTL is how long a page keeps working actions on its records.
const refTTL = 7 * 24 * time.Hour

// SignRecord returns a reference to a record shown by a page, for one page
// action flow ("collection:id:expiry:sig"): the flow trusts it, as visitors
// can't forge it nor reuse it with another flow.
func SignRecord(app core.App, flow, collection, id string) string {
	exp := strconv.FormatInt(time.Now().Add(refTTL).Unix(), 10)
	return collection + ":" + id + ":" + exp + ":" + refSig(app, flow, collection, id, exp)
}

// VerifyRecord returns the collection and id of a signed reference of the flow.
func VerifyRecord(app core.App, flow, ref string) (collection, id string, ok bool) {
	parts := strings.Split(ref, ":")
	if len(parts) != 4 {
		return "", "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", "", false
	}
	if !hmac.Equal([]byte(parts[3]), []byte(refSig(app, flow, parts[0], parts[1], parts[2]))) {
		return "", "", false
	}
	return parts[0], parts[1], true
}
