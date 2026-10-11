package flows

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	"golang.org/x/crypto/bcrypt"

	"github.com/Namarusa/PocketbasePlus/internal/session"
)

// Page actions: the "action" flows are started by the buttons and forms of the
// pages (POST /api/flows/action/{id}). They can sign the visitor in or out and
// choose the page shown next.

// TriggerAction is the trigger of the flows started from the pages.
const TriggerAction = "action"

const actionTimeout = 30 * time.Second

// actionState is what the steps of a page action decide for the response.
type actionState struct {
	ip       string
	info     *core.RequestInfo
	user     *core.Record // the signed-in visitor (nil: a guest)
	signIn   *core.Record
	signOut  bool
	redirect string
	message  string
}

// allowed reports whether the visitor can run the action.
func allowed(cfg map[string]any, user *core.Record) bool {
	switch asString(cfg, "access") {
	case "users":
		return user != nil
	case "roles":
		if user == nil {
			return false
		}
		roles, _ := cfg["roles"].([]any)
		mine := session.Roles(user)
		return slices.ContainsFunc(roles, func(r any) bool { return slices.Contains(mine, toString(r)) })
	}
	return true // public
}

// action runs a page action flow.
func (e *Engine) action(re *core.RequestEvent) error {
	flow, err := re.App.FindRecordById(flowsCollection, re.Request.PathValue("id"))
	if err != nil || flow.GetString("trigger_type") != TriggerAction || !flow.GetBool("active") {
		return re.NotFoundError("", nil)
	}
	if !session.SameOrigin(re.Request) {
		return re.ForbiddenError("Actions can only be sent from the pages of the site.", nil)
	}
	wantsJSON := strings.Contains(re.Request.Header.Get("Accept"), "application/json")

	fields := map[string]any{}
	hidden := map[string]string{}
	if strings.HasPrefix(re.Request.Header.Get("Content-Type"), "application/json") {
		body := map[string]any{}
		if err := re.BindBody(&body); err != nil {
			return re.BadRequestError("Invalid request body.", err)
		}
		for k, v := range body {
			if strings.HasPrefix(k, "_") {
				hidden[k] = toString(v)
			} else {
				fields[k] = v
			}
		}
	} else {
		re.Request.Body = http.MaxBytesReader(re.Response, re.Request.Body, 1<<20)
		if err := re.Request.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
			return re.BadRequestError("Invalid form.", err)
		}
		for k, v := range re.Request.PostForm {
			if strings.HasPrefix(k, "_") {
				hidden[k] = v[0]
			} else if len(v) == 1 {
				fields[k] = v[0]
			} else {
				fields[k] = v
			}
		}
	}

	page := session.LocalPath(hidden["_page"])
	if page == "" {
		if ref, err := url.Parse(re.Request.Referer()); err == nil && ref.Host == re.Request.Host {
			page = session.LocalPath(ref.RequestURI())
		}
	}
	if page == "" {
		page = "/"
	}
	next := session.LocalPath(hidden["_next"])

	// the output of the steps is never sent back: it may hold data the visitor can't read
	respond := func(status, redirect, flashType, message string, _ any) error {
		if wantsJSON {
			return re.JSON(http.StatusOK, map[string]any{"status": status, "redirect": redirect, "message": message})
		}
		session.SetFlash(re, flashType, message)
		return session.Redirect(re, redirect)
	}

	user := session.Get(re)
	cfg := triggerConfig(flow)
	if !allowed(cfg, user) {
		msg := "You don't have access to this action."
		if user == nil {
			msg = "Sign in to do this."
		}
		if wantsJSON {
			return re.ForbiddenError(msg, nil)
		}
		return respond(StatusRejected, page, "error", msg, nil)
	}

	trigger := map[string]any{"form": fields, "page": page, "next": next, "user": nil}
	if user != nil {
		trigger["user"] = recordData(user)
	}
	if col, id, ok := session.VerifyRecord(re.App, flow.Id, hidden["_rec"]); ok {
		if rec, err := re.App.FindRecordById(col, id); err == nil {
			trigger["collection"] = rec.Collection().Name
			trigger["record"] = recordData(rec)
		}
	}

	info, _ := re.RequestInfo()
	state := &actionState{ip: re.RealIP(), info: info, user: user}
	res := e.Run(flow, trigger, RunOptions{Ctx: re.Request.Context(), Timeout: actionTimeout, action: state})

	switch res.Status {
	case StatusSuccess:
		if state.signOut {
			session.Clear(re)
		}
		if state.signIn != nil {
			if err := session.Set(re, state.signIn); err != nil {
				return respond(StatusFailed, page, "error", "The sign in failed.", nil)
			}
		}
		to := state.redirect
		if to == "" {
			to = next
		}
		if to == "" {
			to = page
		}
		return respond(res.Status, to, "success", state.message, res.Output)
	case StatusRejected:
		return respond(res.Status, page, "error", res.Error, nil)
	default:
		return respond(res.Status, page, "error", "Something went wrong, please try again.", nil)
	}
}

// --- steps -----------------------------------------------------------------------------

var errNotAction = errors.New("only available in page action flows")

// signInLimiter counts the failed sign ins per IP.
var signInLimiter = struct {
	sync.Mutex
	failures map[string][]time.Time
}{failures: map[string][]time.Time{}}

const (
	maxSignInFailures  = 10 // per IP
	maxAccountFailures = 20 // per account, from any IP (slow distributed guessing)
	signInWindow       = 10 * time.Minute
)

func recentFailures(key string, add bool) int {
	signInLimiter.Lock()
	defer signInLimiter.Unlock()
	now := time.Now()
	list := slices.DeleteFunc(signInLimiter.failures[key], func(t time.Time) bool { return now.Sub(t) > signInWindow })
	if add {
		list = append(list, now)
	}
	if len(list) == 0 {
		delete(signInLimiter.failures, key)
	} else {
		signInLimiter.failures[key] = list
	}
	if len(signInLimiter.failures) > 10000 { // bounded memory
		signInLimiter.failures = map[string][]time.Time{}
	}
	return len(list)
}

// dummyHash is compared when the identity is unknown (same timing).
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("pocketbaseplus-dummy-password"), bcrypt.DefaultCost)

func stepSignIn(r *run, s Step) (any, any, error) {
	if r.action == nil {
		return nil, nil, errNotAction
	}
	cfg, err := r.resolved(s, "collection", "identity", "password")
	if err != nil {
		return nil, nil, err
	}
	identity := strings.TrimSpace(toString(cfg["identity"]))
	password := toString(cfg["password"])
	input := map[string]any{"collection": cfg["collection"], "identity": identity} // never the password

	name := toString(cfg["collection"])
	if name == "" {
		name = "users"
	}
	col, err := r.app.FindCachedCollectionByNameOrId(name)
	if err != nil || !col.IsAuth() || col.Name == core.CollectionNameSuperusers {
		return input, nil, errors.New("select an auth collection (superusers can't sign in on pages)")
	}
	if !col.PasswordAuth.Enabled || col.AuthRule == nil {
		return input, nil, errors.New("password sign in is disabled in the collection " + col.Name)
	}
	if col.MFA.Enabled {
		return input, nil, errors.New("the collection " + col.Name + " requires MFA, not supported on pages")
	}
	account := "account:" + col.Id + ":" + strings.ToLower(identity)
	if recentFailures(r.action.ip, false) >= maxSignInFailures || recentFailures(account, false) >= maxAccountFailures {
		return input, nil, &rejectError{message: "Too many failed attempts, try again in a few minutes."}
	}

	var user *core.Record
	if identity != "" && password != "" {
		for _, field := range col.PasswordAuth.IdentityFields {
			rec, err := r.app.FindFirstRecordByData(col, field, identity)
			if err == nil {
				user = rec
				break
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return input, nil, err
			}
		}
	}
	ok := false
	if user != nil {
		ok = user.ValidatePassword(password)
	} else {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	}
	if ok && r.action.info != nil {
		ok, _ = r.app.CanAccessRecord(user, r.action.info, col.AuthRule)
	}
	if !ok {
		recentFailures(r.action.ip, true)
		if identity != "" {
			recentFailures(account, true)
		}
		return input, nil, &rejectError{message: "Invalid email or password."}
	}
	r.action.signIn = user
	r.action.signOut = false
	return input, map[string]any{"user": recordData(user)}, nil
}

func stepSignOut(r *run, s Step) (any, any, error) {
	if r.action == nil {
		return nil, nil, errNotAction
	}
	r.action.signOut = true
	r.action.signIn = nil
	return nil, map[string]any{"signedOut": true}, nil
}

func stepRedirect(r *run, s Step) (any, any, error) {
	if r.action == nil {
		return nil, nil, errNotAction
	}
	cfg, err := r.resolved(s, "url", "message")
	if err != nil {
		return nil, nil, err
	}
	if u := strings.TrimSpace(toString(cfg["url"])); u != "" {
		local := session.LocalPath(u)
		if local == "" {
			return cfg, nil, errors.New("only pages of the site can be opened (a path starting with /)")
		}
		r.action.redirect = local
	}
	if m := toString(cfg["message"]); m != "" {
		r.action.message = m
	}
	return cfg, map[string]any{"redirect": r.action.redirect}, nil
}

// --- API rules of the visitor ---------------------------------------------------------------

// asVisitor reports whether a record step checks the API rules of the visitor:
// in page action flows, unless turned off in the step.
func (r *run) asVisitor(s Step) bool {
	if r.action == nil {
		return false
	}
	v, ok := s.Config["checkRules"].(bool)
	return !ok || v
}

// visitorInfo is the visitor as PocketBase sees a request of theirs
// (@request.auth, @request.body).
func (r *run) visitorInfo(body map[string]any) *core.RequestInfo {
	if body == nil {
		body = map[string]any{}
	}
	info := &core.RequestInfo{Context: core.RequestInfoContextDefault, Method: http.MethodPost, Query: map[string]string{}, Headers: map[string]string{}, Body: body}
	if r.action != nil && r.action.user != nil {
		info.Auth = r.action.user
	}
	return info
}

var errNoAccess = &rejectError{message: "You don't have access to this record."}

// allowedBy reports whether the visitor passes a rule for the record (nil: superusers only).
func (r *run) allowedBy(rec *core.Record, info *core.RequestInfo, rule *string) bool {
	if rule == nil {
		return false
	}
	ok, err := r.app.CanAccessRecord(rec, info, rule)
	return err == nil && ok
}

// findAsVisitor lists records with the list rule of the collection for the
// visitor; the fields hidden from them are left out.
func (r *run) findAsVisitor(col *core.Collection, filter, sort string, limit int, params map[string]any) ([]map[string]any, error) {
	if col.ListRule == nil {
		return nil, &rejectError{message: "You don't have access to these records."}
	}
	info := r.visitorInfo(nil)
	q := r.app.RecordQuery(col).WithContext(r.ctx)
	resolver := core.NewRecordFieldResolver(r.app, col, info, true)
	for _, f := range []string{filter, *col.ListRule} {
		if strings.TrimSpace(f) == "" {
			continue
		}
		expr, err := search.FilterData(f).BuildExpr(resolver, dbx.Params(params))
		if err != nil {
			return nil, err
		}
		q.AndWhere(expr)
	}
	for _, sf := range search.ParseSortFromString(sort) {
		if strings.TrimSpace(sf.Name) == "" {
			continue
		}
		expr, err := sf.BuildExpr(resolver)
		if err != nil {
			return nil, err
		}
		if expr != "" {
			q.AndOrderBy(expr)
		}
	}
	if err := resolver.UpdateQuery(q); err != nil {
		return nil, err
	}
	var records []*core.Record
	if err := q.Limit(int64(limit)).All(&records); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		out = append(out, r.visibleData(rec, info))
	}
	return out, nil
}

// visibleData is the data of a record without the fields hidden from the
// visitor (record enrich hooks, e.g. the field permissions of the roles).
func (r *run) visibleData(rec *core.Record, info *core.RequestInfo) map[string]any {
	ev := &core.RecordEnrichEvent{App: r.app, RequestInfo: info}
	ev.Record = rec
	_ = r.app.OnRecordEnrich().Trigger(ev, func(*core.RecordEnrichEvent) error { return nil })
	data := recordData(rec)
	export := rec.PublicExport()
	for k := range data {
		if _, ok := export[k]; !ok && k != "collectionId" && k != "collectionName" && k != core.FieldNameEmail {
			delete(data, k)
		}
	}
	return data
}

func fieldKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, strings.Trim(k, "+-"))
	}
	return keys
}

// emails sent and API calls made by the page actions, per IP
const (
	maxActionRecipients = 5
	maxActionEmails     = 10
	actionEmailWindow   = time.Hour
	maxActionHTTP       = 30
	actionHTTPWindow    = 10 * time.Minute
)

// ipLimiter counts the uses per IP in a sliding window.
type ipLimiter struct {
	sync.Mutex
	max    int
	window time.Duration
	used   map[string][]time.Time
}

func newIPLimiter(max int, window time.Duration) *ipLimiter {
	return &ipLimiter{max: max, window: window, used: map[string][]time.Time{}}
}

// allow records n uses, or reports false when they'd exceed the limit.
func (l *ipLimiter) allow(ip string, n int) bool {
	l.Lock()
	defer l.Unlock()
	now := time.Now()
	list := slices.DeleteFunc(l.used[ip], func(t time.Time) bool { return now.Sub(t) > l.window })
	if len(list)+n > l.max {
		l.used[ip] = list
		return false
	}
	for range n {
		list = append(list, now)
	}
	if len(l.used) > 10000 {
		l.used = map[string][]time.Time{}
	}
	l.used[ip] = list
	return true
}

func (l *ipLimiter) reset() {
	l.Lock()
	l.used = map[string][]time.Time{}
	l.Unlock()
}

var (
	actionEmails = newIPLimiter(maxActionEmails, actionEmailWindow)
	actionHTTP   = newIPLimiter(maxActionHTTP, actionHTTPWindow)
)

func allowActionEmails(ip string, n int) bool {
	return actionEmails.allow(ip, n)
}

// allowActionHTTP limits the HTTP requests and API calls of the visitors.
func allowActionHTTP(ip string) bool {
	return actionHTTP.allow(ip, 1)
}
