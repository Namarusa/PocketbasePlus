package flows

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/session"
)

// Webhook flows: endpoints of the app. Who may call one (trigger "access"):
//
//   - token (default): the secret token, as ?token= or the X-Flow-Token header
//   - public: anyone
//   - users: a signed-in user (a PocketBase token in the Authorization
//     header, or the session of the pages for the requests of the site)
//   - roles: a signed-in user with one of the roles
//
// Without the token, the caller is the visitor of the run: the record steps
// check their API rules and the visitor limits apply (as the page actions).
// With "Wait for the result", a Respond step sets the response (status,
// headers, JSON, text, HTML or a redirect); else it's the last output as
// JSON. A webhook can have a readable address: /hooks/{slug}.

// callerState is the visitor who started a run.
type callerState struct {
	ip   string
	info *core.RequestInfo
	user *core.Record // nil: a guest
}

// respondState is the response a Respond step set.
type respondState struct {
	set      bool
	status   int
	format   string // json | text | html
	body     any
	headers  map[string]string
	redirect string
}

const (
	webhookTimeout = 30 * time.Second
	maxWebhookBody = 5 << 20
	// the calls of a webhook without token per visitor
	maxWebhookCalls   = 60
	webhookCallWindow = time.Minute
)

var (
	slugRegex      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	webhookCalls   = newIPLimiter(maxWebhookCalls, webhookCallWindow)
	respondFormats = map[string]string{"json": "application/json", "text": "text/plain; charset=utf-8", "html": "text/html; charset=utf-8"}
	// headers the Respond step can't set (the server's, the cookies of the site)
	blockedHeaders = map[string]bool{
		"set-cookie": true, "content-length": true, "transfer-encoding": true, "connection": true,
		"content-security-policy": true, "strict-transport-security": true, "content-type": true,
	}
)

// webhook runs the webhook flow with this id.
func (e *Engine) webhook(re *core.RequestEvent) error {
	flow, err := re.App.FindRecordById(flowsCollection, re.Request.PathValue("id"))
	if err != nil || flow.GetString("trigger_type") != TriggerWebhook || !flow.GetBool("active") {
		return re.NotFoundError("", nil)
	}
	return e.runWebhook(re, flow)
}

// webhookBySlug runs the webhook flow with this address (/hooks/{slug}).
func (e *Engine) webhookBySlug(re *core.RequestEvent) error {
	slug := re.Request.PathValue("slug")
	if !slugRegex.MatchString(slug) {
		return re.NotFoundError("", nil)
	}
	flow, err := findBySlug(re.App, slug, "")
	if err != nil || !flow.GetBool("active") {
		return re.NotFoundError("", nil)
	}
	return e.runWebhook(re, flow)
}

func findBySlug(app core.App, slug, except string) (*core.Record, error) {
	return app.FindFirstRecordByFilter(flowsCollection,
		"trigger_type = {:type} && id != {:id} && trigger ~ {:quoted}",
		dbx.Params{"type": TriggerWebhook, "id": except, "quoted": `"slug":"` + slug + `"`})
}

func (e *Engine) runWebhook(re *core.RequestEvent, flow *core.Record) error {
	cfg := triggerConfig(flow)
	access := asString(cfg, "access")

	var caller *callerState
	var user *core.Record
	if access == "" || access == "token" {
		token := re.Request.Header.Get("X-Flow-Token")
		if q := re.Request.URL.Query(); q.Has("token") {
			if token == "" {
				token = q.Get("token")
			}
			// the request logger records the URL once the handler returns: keep the secret out of it
			q.Del("token")
			re.Request.URL.RawQuery = q.Encode()
		}
		expected := asString(cfg, "token")
		if expected == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			return re.UnauthorizedError("Invalid flow token.", nil)
		}
	} else {
		user = webhookUser(re)
		if !allowed(cfg, user) {
			if user == nil {
				return re.UnauthorizedError("Sign in to call this endpoint.", nil)
			}
			return re.ForbiddenError("You don't have access to this endpoint.", nil)
		}
		if !webhookCalls.allow(re.RealIP(), 1) {
			return re.TooManyRequestsError("Too many requests, try again later.", nil)
		}
		info, _ := re.RequestInfo()
		caller = &callerState{ip: re.RealIP(), info: info, user: user}
	}
	if m := strings.ToUpper(asString(cfg, "method")); m != "" && m != "ANY" && m != re.Request.Method {
		return re.Error(http.StatusMethodNotAllowed, "Method not allowed.", nil)
	}

	raw, err := io.ReadAll(io.LimitReader(re.Request.Body, maxWebhookBody))
	if err != nil {
		return re.BadRequestError("Failed to read the body.", err)
	}
	var body any = string(raw)
	if len(raw) > 0 && json.Valid(raw) {
		_ = json.Unmarshal(raw, &body)
	} else if strings.HasPrefix(re.Request.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if form, err := url.ParseQuery(string(raw)); err == nil {
			fields := map[string]any{}
			for k, v := range form {
				fields[k] = strings.Join(v, ",")
			}
			body = fields
		}
	}
	query := map[string]any{}
	for k, v := range re.Request.URL.Query() {
		if k != "token" {
			query[k] = strings.Join(v, ",")
		}
	}
	headers := map[string]any{}
	for k := range re.Request.Header {
		if !strings.EqualFold(k, "X-Flow-Token") && !strings.EqualFold(k, "Authorization") && !strings.EqualFold(k, "Cookie") {
			headers[k] = re.Request.Header.Get(k)
		}
	}
	trigger := map[string]any{"method": re.Request.Method, "query": query, "headers": headers, "body": body}
	if access != "" && access != "token" {
		// a guest: empty ({{if .trigger.user}} is false, {{.trigger.user.email}} is "")
		trigger["user"] = map[string]any{}
		if user != nil {
			trigger["user"] = recordData(user)
		}
	}

	if !asBool(cfg, "wait") {
		e.RunAsync(flow, trigger, nil)
		return re.JSON(http.StatusAccepted, map[string]any{"accepted": true})
	}

	resp := &respondState{}
	res := e.Run(flow, trigger, RunOptions{Ctx: re.Request.Context(), Timeout: webhookTimeout, caller: caller, respond: resp})
	if resp.set && res.Status != StatusFailed {
		return writeResponse(re, resp)
	}
	switch res.Status {
	case StatusSuccess:
		return re.JSON(http.StatusOK, res.Output)
	case StatusRejected:
		return re.BadRequestError(res.Error, nil)
	default:
		return re.InternalServerError("The flow failed.", errors.New(res.Error))
	}
}

// webhookUser is the signed-in caller: a PocketBase auth token, or the
// session of the pages for the requests of the site.
func webhookUser(re *core.RequestEvent) *core.Record {
	if re.Auth != nil {
		return re.Auth
	}
	if session.SameOrigin(re.Request) {
		return session.Get(re)
	}
	return nil
}

func writeResponse(re *core.RequestEvent, resp *respondState) error {
	h := re.Response.Header()
	for k, v := range resp.headers {
		h.Set(k, v)
	}
	if resp.redirect != "" {
		return re.Redirect(resp.status, resp.redirect)
	}
	switch resp.format {
	case "html":
		// served from the site's address: no scripts, no access to its cookies
		h.Set("Content-Security-Policy", "sandbox")
		h.Set("Content-Type", respondFormats["html"])
		re.Response.WriteHeader(resp.status)
		_, err := io.WriteString(re.Response, toString(resp.body))
		return err
	case "text":
		h.Set("Content-Type", respondFormats["text"])
		re.Response.WriteHeader(resp.status)
		_, err := io.WriteString(re.Response, toString(resp.body))
		return err
	}
	if resp.status == http.StatusNoContent {
		return re.NoContent(resp.status)
	}
	return re.JSON(resp.status, resp.body)
}

// stepRespond sets the response of a webhook waiting for the result (the
// last Respond step wins).
func stepRespond(r *run, s Step) (any, any, error) {
	if r.respond == nil {
		return nil, nil, errors.New(`only available in "Webhook" flows with "Wait for the result"`)
	}
	cfg, err := r.resolved(s, "status", "headers", "redirect")
	if err != nil {
		return cfg, nil, err
	}
	format := asString(s.Config, "format")
	if respondFormats[format] == "" {
		format = "json"
	}
	resp := respondState{set: true, format: format, headers: map[string]string{}}

	resp.status = http.StatusOK
	if v := strings.TrimSpace(toString(cfg["status"])); v != "" && v != "<nil>" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 200 || n > 599 {
			return cfg, nil, fmt.Errorf("invalid status %q (200 to 599)", v)
		}
		resp.status = n
	}
	for k, v := range asMap(cfg["headers"]) {
		if blockedHeaders[strings.ToLower(k)] || !headerNameRegex.MatchString(k) {
			return cfg, nil, fmt.Errorf("the header %q can't be set", k)
		}
		value := toString(v)
		if strings.ContainsAny(value, "\r\n") {
			return cfg, nil, fmt.Errorf("the header %q has a line break", k)
		}
		resp.headers[k] = value
	}

	if to := strings.TrimSpace(toString(cfg["redirect"])); to != "" && to != "<nil>" {
		u, err := url.Parse(to)
		if err != nil || (session.LocalPath(to) == "" && (u.Scheme != "https" && u.Scheme != "http" || u.Host == "")) {
			return cfg, nil, errors.New("redirect: a path of the site (/thanks) or an http(s) address")
		}
		resp.redirect = to
		if resp.status < 300 || resp.status > 308 {
			resp.status = http.StatusSeeOther
		}
	} else if format == "json" {
		// the body: a value of the run, or JSON text
		v, err := resolveValue(s.Config["body"], r.data)
		if err != nil {
			return cfg, nil, fmt.Errorf("body: %w", err)
		}
		if str, ok := v.(string); ok && json.Valid([]byte(str)) {
			var parsed any
			_ = json.Unmarshal([]byte(str), &parsed)
			v = parsed
		}
		resp.body = toPlain(v)
	} else {
		render := resolveString
		if format == "html" {
			render = renderHTML // the values are escaped (the caller may send any)
		}
		body, err := render(s.Config["body"], r.data)
		if err != nil {
			return cfg, nil, fmt.Errorf("body: %w", err)
		}
		resp.body = body
	}
	*r.respond = resp
	out := map[string]any{"status": resp.status, "format": format}
	if resp.redirect != "" {
		out["redirect"] = resp.redirect
	}
	return cfg, out, nil
}

var headerNameRegex = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// renderHTML renders an HTML template: the values are escaped for their
// place (text, attributes, URLs), as in the emails.
func renderHTML(v any, data map[string]any) (string, error) {
	src := toString(v)
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	t, err := htmltemplate.New("v").Funcs(templateFuncs()).Option("missingkey=zero").Parse(normalize(src))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.ReplaceAll(buf.String(), "&lt;no value&gt;", ""), nil
}

// validateWebhook checks the access and the address of a webhook trigger.
func validateWebhook(app core.App, f *core.Record, cfg map[string]any) error {
	switch asString(cfg, "access") {
	case "", "token":
		cfg["access"] = "token"
		delete(cfg, "roles")
	case "public", "users":
		delete(cfg, "roles")
	case "roles":
		roles, _ := cfg["roles"].([]any)
		if len(roles) == 0 {
			return validation.NewError("required_roles", "Select at least one role.")
		}
		for _, id := range roles {
			if _, err := app.FindRecordById("roles", toString(id)); err != nil {
				return validation.NewError("invalid_role", "Unknown role.")
			}
		}
	default:
		return validation.NewError("invalid_access", "Unknown access.")
	}
	slug := strings.ToLower(strings.TrimSpace(asString(cfg, "slug")))
	if slug == "" {
		delete(cfg, "slug")
		return nil
	}
	if !slugRegex.MatchString(slug) {
		return validation.NewError("invalid_slug", "The address: lowercase letters, digits and dashes (e.g. new-order).")
	}
	if other, err := findBySlug(app, slug, f.Id); err == nil {
		return validation.NewError("slug_taken", fmt.Sprintf("The address /hooks/%s is used by the flow %q.", slug, other.GetString("name")))
	}
	cfg["slug"] = slug
	return nil
}

// validateRespondSteps checks that the Respond steps are in webhook flows.
func validateRespondSteps(steps []Step, webhook bool) error {
	for _, s := range steps {
		if s.Type == "respond" && !webhook {
			label := s.Name
			if label == "" {
				label = s.Type
			}
			return fmt.Errorf("step %q: only available in \"Webhook\" flows", label)
		}
		for _, nested := range [][]Step{s.Then, s.Else, s.Steps} {
			if err := validateRespondSteps(nested, webhook); err != nil {
				return err
			}
		}
	}
	return nil
}
