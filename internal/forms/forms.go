// Package forms publishes forms built from the fields of a collection, as a
// full page (/f/{slug}) and as an iframe embed (/f/{slug}/embed) limited to
// allowed domains. Submissions create records of the collection: the form is
// the access gate (the collection rules can stay closed), with signed
// tokens, a honeypot and a per IP rate limit against spam.
package forms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/actor"
	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
	"github.com/Namarusa/PocketbasePlus/internal/publicpage"
)

const (
	formsCollection = "forms"
	tokenTTL        = 2 * time.Hour
	honeypotField   = "website"
	maxUploadBytes  = 64 << 20
	maxOptions      = 500
)

// FieldConfig is a field shown by a form.
type FieldConfig struct {
	Field       string `json:"field"`
	Label       string `json:"label,omitempty"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Width       string `json:"width,omitempty"` // full | half
	// relations: list the related records publicly as options
	ListRecords bool `json:"listRecords,omitempty"`
}

// supported field types
var fieldTypes = map[string]bool{
	core.FieldTypeText: true, core.FieldTypeEmail: true, core.FieldTypeURL: true,
	core.FieldTypeNumber: true, core.FieldTypeBool: true, core.FieldTypeSelect: true,
	core.FieldTypeDate: true, core.FieldTypeEditor: true, core.FieldTypeFile: true,
	core.FieldTypeRelation: true,
}

// Register binds the forms hooks, pages and routes.
func Register(app core.App) {
	app.OnRecordValidate(formsCollection).BindFunc(func(e *core.RecordEvent) error {
		if err := validateForm(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.Router.GET("/f/{slug}", page(false))
		se.Router.GET("/f/{slug}/embed", page(true))
		se.Router.GET("/api/forms/{slug}", schema)
		se.Router.POST("/api/forms/{slug}/submit", submit)
		se.Router.POST("/api/forms/{slug}/draft", draftPreview).Bind(apis.RequireSuperuserAuth())
		return se.Next()
	})
}

// --- form definition ----------------------------------------------------------------

type form struct {
	rec     *core.Record
	col     *core.Collection
	fields  []FieldConfig
	fixed   map[string]any
	domains []string
}

func loadForm(app core.App, slug string, onlyActive bool) (*form, error) {
	rec, err := app.FindFirstRecordByData(formsCollection, "slug", slug)
	if err != nil || (onlyActive && !rec.GetBool("active")) {
		return nil, errors.New("form not found")
	}
	return formFromRecord(app, rec)
}

func formFromRecord(app core.App, rec *core.Record) (*form, error) {
	f := &form{rec: rec}
	_ = rec.UnmarshalJSONField("fields", &f.fields)
	_ = rec.UnmarshalJSONField("fixed", &f.fixed)
	_ = rec.UnmarshalJSONField("allowed_domains", &f.domains)
	var err error
	f.col, err = app.FindCachedCollectionByNameOrId(rec.GetString("collection"))
	if err != nil {
		return nil, errors.New("the form collection no longer exists")
	}
	return f, nil
}

// normalizeOrigin turns "https://example.com/page" into "https://example.com".
func normalizeOrigin(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("invalid domain %q (use https://example.com)", s)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func validateForm(app core.App, rec *core.Record) error {
	errs := validation.Errors{}
	c, err := app.FindCollectionByNameOrId(rec.GetString("collection"))
	switch {
	case err != nil || internalcols.Is(c):
		errs["collection"] = validation.NewError("invalid_collection", "Select a collection.")
	case c.IsView():
		errs["collection"] = validation.NewError("invalid_collection", "View collections are read-only.")
	case c.IsAuth():
		errs["collection"] = validation.NewError("invalid_collection", "Auth collections aren't supported (use their sign-up).")
	}
	if len(errs) > 0 {
		return errs
	}
	rec.Set("collection", c.Id)

	var fields []FieldConfig
	_ = rec.UnmarshalJSONField("fields", &fields)
	var fixed map[string]any
	_ = rec.UnmarshalJSONField("fixed", &fixed)
	seen := map[string]bool{}
	for i := range fields {
		cfg := &fields[i]
		f := c.Fields.GetByName(cfg.Field)
		switch {
		case f == nil:
			errs["fields"] = validation.NewError("invalid_field", fmt.Sprintf("Unknown field %q.", cfg.Field))
		case !fieldTypes[f.Type()] || f.GetName() == core.FieldNameId:
			errs["fields"] = validation.NewError("invalid_field", fmt.Sprintf("The field %q (%s) can't be used in a form.", cfg.Field, f.Type()))
		case seen[cfg.Field]:
			errs["fields"] = validation.NewError("invalid_field", fmt.Sprintf("The field %q is used twice.", cfg.Field))
		case f.Type() == core.FieldTypeRelation && !cfg.ListRecords:
			errs["fields"] = validation.NewError("invalid_field", fmt.Sprintf("The relation %q needs \"list its records publicly\" (its options are shown to everyone).", cfg.Field))
		}
		if _, ok := fixed[cfg.Field]; ok {
			errs["fields"] = validation.NewError("invalid_field", fmt.Sprintf("The field %q is both shown and fixed.", cfg.Field))
		}
		seen[cfg.Field] = true
		if cfg.Width != "half" {
			cfg.Width = "full"
		}
	}
	for name := range fixed {
		f := c.Fields.GetByName(name)
		if f == nil || name == core.FieldNameId || f.Type() == core.FieldTypeAutodate || f.Type() == core.FieldTypeFile || f.Type() == core.FieldTypePassword {
			errs["fixed"] = validation.NewError("invalid_fixed", fmt.Sprintf("The field %q can't have a fixed value.", name))
		}
	}

	var domains []string
	_ = rec.UnmarshalJSONField("allowed_domains", &domains)
	normalized := []string{}
	for _, d := range domains {
		if strings.TrimSpace(d) == "" {
			continue
		}
		o, err := normalizeOrigin(d)
		if err != nil {
			errs["allowed_domains"] = validation.NewError("invalid_domain", err.Error())
			continue
		}
		if !slices.Contains(normalized, o) {
			normalized = append(normalized, o)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	rec.Set("fields", fields)
	rec.Set("allowed_domains", normalized)
	if rec.GetString("theme") == "" {
		rec.Set("theme", "auto")
	}
	return nil
}

// --- pages --------------------------------------------------------------------------

// page serves the form page. The embed can only be framed by the site
// itself (admin preview) and the allowed domains; the full page by none.
func page(embed bool) func(e *core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		ancestors := "'none'"
		title := "Form"
		// inactive forms too: the dashboard previews them in an iframe
		if f, err := loadForm(e.App, slug, false); err == nil {
			title = f.rec.GetString("title")
			if title == "" {
				title = f.rec.GetString("name")
			}
			if embed {
				ancestors = strings.TrimSpace("'self' " + strings.Join(f.domains, " "))
			}
		}
		h := e.Response.Header()
		h.Set("Content-Security-Policy", publicpage.CSP(ancestors))
		h.Set("Cache-Control", "no-cache")
		if !embed {
			h.Set("X-Frame-Options", "DENY")
		}
		mode := "page"
		if embed {
			mode = "embed"
		}
		return e.HTML(http.StatusOK, publicpage.HTML(publicpage.Options{
			Title:     title,
			BodyClass: "gp-form-body gp-form-" + mode,
			BodyAttrs: map[string]string{"slug": slug, "mode": mode},
			Module:    "/_/extensions/pocketbaseplus/form.js",
		}))
	}
}

// --- schema -------------------------------------------------------------------------

func requiredOf(f core.Field) bool {
	switch v := f.(type) {
	case *core.TextField:
		return v.Required
	case *core.EmailField:
		return v.Required
	case *core.URLField:
		return v.Required
	case *core.NumberField:
		return v.Required
	case *core.BoolField:
		return v.Required
	case *core.SelectField:
		return v.Required
	case *core.DateField:
		return v.Required
	case *core.EditorField:
		return v.Required
	case *core.FileField:
		return v.Required
	case *core.RelationField:
		return v.Required
	}
	return false
}

type option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// fieldSchema describes a form field for the public renderer.
func fieldSchema(app core.App, cfg FieldConfig, f core.Field) map[string]any {
	label := cfg.Label
	if label == "" {
		label = f.GetName()
	}
	out := map[string]any{
		"name":        f.GetName(),
		"type":        f.Type(),
		"label":       label,
		"help":        cfg.Help,
		"placeholder": cfg.Placeholder,
		"required":    cfg.Required || requiredOf(f),
		"width":       cfg.Width,
	}
	switch v := f.(type) {
	case *core.TextField:
		out["min"], out["max"] = v.Min, v.Max
	case *core.NumberField:
		out["min"], out["max"], out["onlyInt"] = v.Min, v.Max, v.OnlyInt
	case *core.SelectField:
		opts := make([]option, len(v.Values))
		for i, val := range v.Values {
			opts[i] = option{Value: val, Label: val}
		}
		out["options"], out["multiple"], out["maxSelect"] = opts, v.IsMultiple(), v.MaxSelect
	case *core.FileField:
		out["multiple"], out["maxSelect"], out["maxSize"], out["mimeTypes"] = v.IsMultiple(), v.MaxSelect, v.MaxSize, v.MimeTypes
	case *core.RelationField:
		out["multiple"], out["maxSelect"] = v.IsMultiple(), v.MaxSelect
		opts := []option{}
		if relCol, err := app.FindCachedCollectionByNameOrId(v.CollectionId); err == nil {
			names := labels.Fields(relCol, false)
			sort := ""
			if len(names) > 0 {
				sort = names[0]
			}
			if records, err := app.FindRecordsByFilter(relCol, "", sort, maxOptions, 0); err == nil {
				for _, r := range records {
					opts = append(opts, option{Value: r.Id, Label: labels.Of(r, names)})
				}
			}
		}
		out["options"] = opts
	}
	return out
}

// previewing reports whether a superuser previews the form (inactive forms allowed).
func previewing(e *core.RequestEvent) bool {
	return e.HasSuperuserAuth() && e.Request.URL.Query().Get("preview") == "1"
}

func schema(e *core.RequestEvent) error {
	f, err := loadForm(e.App, e.Request.PathValue("slug"), !previewing(e))
	if err != nil {
		return e.NotFoundError("Form not found.", nil)
	}
	return e.JSON(http.StatusOK, schemaJSON(e.App, f, issueToken(f.rec, time.Now())))
}

// the form settings a draft preview can change
var draftKeys = []string{
	"collection", "fields", "fixed", "title", "description", "submit_label",
	"success_message", "redirect_url", "require_auth", "theme", "accent",
}

// draftPreview returns the schema of unsaved form settings (the editor live
// preview): nothing is saved, and the draft can't be submitted (no token).
func draftPreview(e *core.RequestEvent) error {
	saved, err := e.App.FindFirstRecordByData(formsCollection, "slug", e.Request.PathValue("slug"))
	if err != nil {
		return e.NotFoundError("Form not found.", nil)
	}
	var body map[string]any
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid draft.", err)
	}
	draft := saved.Clone()
	for _, k := range draftKeys {
		if v, ok := body[k]; ok {
			draft.Set(k, v)
		}
	}
	if err := validateForm(e.App, draft); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	f, err := formFromRecord(e.App, draft)
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	out := schemaJSON(e.App, f, "")
	out["draft"] = true
	return e.JSON(http.StatusOK, out)
}

func schemaJSON(app core.App, f *form, token string) map[string]any {
	fields := []map[string]any{}
	for _, cfg := range f.fields {
		fl := f.col.Fields.GetByName(cfg.Field)
		if fl == nil || !fieldTypes[fl.Type()] {
			continue
		}
		if _, ok := f.fixed[cfg.Field]; ok {
			continue
		}
		fields = append(fields, fieldSchema(app, cfg, fl))
	}
	r := f.rec
	return map[string]any{
		"id":             r.Id,
		"slug":           r.GetString("slug"),
		"title":          r.GetString("title"),
		"description":    r.GetString("description"),
		"submitLabel":    r.GetString("submit_label"),
		"successMessage": r.GetString("success_message"),
		"redirectUrl":    r.GetString("redirect_url"),
		"requireAuth":    r.GetBool("require_auth"),
		"theme":          r.GetString("theme"),
		"accent":         r.GetString("accent"),
		"allowedOrigins": f.domains,
		"fields":         fields,
		"token":          token,
		"honeypot":       honeypotField,
	}
}

// --- tokens and rate limits ---------------------------------------------------------------

func sign(r *core.Record, ts string) string {
	mac := hmac.New(sha256.New, []byte(r.GetString("secret")))
	mac.Write([]byte(r.Id + "|" + ts))
	return hex.EncodeToString(mac.Sum(nil))
}

// issueToken returns a submit token: the issue time and its signature.
func issueToken(r *core.Record, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	return ts + "." + sign(r, ts)
}

func verifyToken(r *core.Record, token string, now time.Time) bool {
	ts, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	issued, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	age := now.Sub(time.Unix(issued, 0))
	if age > tokenTTL || age < -time.Minute {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(sign(r, ts)))
}

// limiter counts the submissions per form and IP over the last hour.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

var submissions = &limiter{hits: map[string][]time.Time{}}

func (l *limiter) allow(key string, limit int, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	// keep the map small
	if len(l.hits) > 10000 {
		for k, list := range l.hits {
			if len(list) == 0 || now.Sub(list[len(list)-1]) > time.Hour {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// --- submit -------------------------------------------------------------------------------

// allowedOrigin checks the browser origin: the site itself or an allowed
// domain (requests without Origin/Referer, e.g. from servers, rely on the token).
func allowedOrigin(e *core.RequestEvent, f *form) bool {
	origin := e.Request.Header.Get("Origin")
	if origin == "" || origin == "null" {
		if ref := e.Request.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil && u.Host != "" {
				origin = u.Scheme + "://" + u.Host
			}
		}
	}
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, e.Request.Host) {
		return true
	}
	o, err := normalizeOrigin(origin)
	return err == nil && slices.Contains(f.domains, o)
}

type submission struct {
	values map[string][]string // multipart values
	json   map[string]any      // JSON body
}

func (s submission) get(name string) (any, bool) {
	if s.json != nil {
		v, ok := s.json[name]
		return v, ok
	}
	v, ok := s.values[name]
	if !ok {
		return nil, false
	}
	if len(v) == 1 {
		return v[0], true
	}
	return v, true
}

func (s submission) str(name string) string {
	v, _ := s.get(name)
	switch val := v.(type) {
	case string:
		return val
	case []string:
		if len(val) > 0 {
			return val[0]
		}
	}
	return ""
}

func parseSubmission(e *core.RequestEvent) (submission, error) {
	ct := e.Request.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") || strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		if strings.HasPrefix(ct, "multipart/") {
			if err := e.Request.ParseMultipartForm(maxUploadBytes); err != nil {
				return submission{}, err
			}
		} else if err := e.Request.ParseForm(); err != nil {
			return submission{}, err
		}
		return submission{values: e.Request.PostForm}, nil
	}
	var body map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(e.Response, e.Request.Body, 1<<20)).Decode(&body); err != nil {
		return submission{}, err
	}
	return submission{json: body}, nil
}

func truthy(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "on", "1", "yes":
			return true
		}
	case []string:
		return len(val) > 0 && truthy(val[len(val)-1])
	}
	return false
}

func list(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case string:
		if val == "" {
			return []string{}
		}
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, fmt.Sprint(item))
		}
		return out
	}
	return []string{}
}

// fixedValue resolves the macros of a fixed value.
func fixedValue(v any, auth *core.Record, query map[string]string) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	switch {
	case s == "@now":
		return types.NowDateTime()
	case s == "@request.auth.id":
		if auth != nil {
			return auth.Id
		}
		return ""
	case strings.HasPrefix(s, "@query."):
		return query[strings.TrimPrefix(s, "@query.")]
	}
	return v
}

func submit(e *core.RequestEvent) error {
	f, err := loadForm(e.App, e.Request.PathValue("slug"), !previewing(e))
	if err != nil {
		return e.NotFoundError("Form not found.", nil)
	}
	if !allowedOrigin(e, f) {
		return e.ForbiddenError("This form can't be submitted from this site.", nil)
	}
	if f.rec.GetBool("require_auth") && e.Auth == nil {
		return e.UnauthorizedError("Sign in to submit this form.", nil)
	}
	sub, err := parseSubmission(e)
	if err != nil {
		return e.BadRequestError("Invalid form data.", err)
	}
	if !verifyToken(f.rec, sub.str("_token"), time.Now()) {
		return e.BadRequestError("The form expired, reload the page and submit again.", nil)
	}
	// bots fill the hidden field: pretend it worked
	if sub.str(honeypotField) != "" {
		return e.JSON(http.StatusOK, map[string]any{"ok": true, "message": f.rec.GetString("success_message")})
	}
	if !submissions.allow(f.rec.Id+"|"+e.RealIP(), f.rec.GetInt("rate_limit"), time.Now()) {
		return e.TooManyRequestsError("Too many submissions, try again later.", nil)
	}

	rec := core.NewRecord(f.col)
	// only the fields of the form are read
	for _, cfg := range f.fields {
		fl := f.col.Fields.GetByName(cfg.Field)
		if fl == nil {
			continue
		}
		if _, isFixed := f.fixed[cfg.Field]; isFixed {
			continue
		}
		name := fl.GetName()
		switch field := fl.(type) {
		case *core.FileField:
			files, _ := e.FindUploadedFiles(name)
			if len(files) == 0 {
				continue
			}
			if field.IsMultiple() {
				rec.Set(name, files)
			} else {
				rec.Set(name, files[0])
			}
			continue
		case *core.BoolField:
			v, _ := sub.get(name)
			rec.Set(name, truthy(v))
			continue
		case *core.SelectField:
			if field.IsMultiple() {
				v, _ := sub.get(name)
				rec.Set(name, list(v))
				continue
			}
		case *core.RelationField:
			if field.IsMultiple() {
				v, _ := sub.get(name)
				rec.Set(name, list(v))
				continue
			}
		}
		if v, ok := sub.get(name); ok {
			if values, isList := v.([]string); isList && len(values) > 0 {
				v = values[0]
			}
			rec.Set(name, v)
		}
	}

	// fixed values (macros: @now, @request.auth.id, @query.<param>)
	query := map[string]string{}
	if raw := sub.str("_query"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &query)
	} else if m, ok := sub.json["_query"].(map[string]any); ok {
		for k, v := range m {
			query[k] = fmt.Sprint(v)
		}
	}
	for name, v := range f.fixed {
		rec.Set(name, fixedValue(v, e.Auth, query))
	}

	label := "Form: " + f.rec.GetString("name")
	if e.Auth != nil {
		label += " (" + e.Auth.Email() + ")"
	}
	ctx := actor.With(context.WithoutCancel(e.Request.Context()), actor.Actor{Type: actor.TypeForm, Id: f.rec.Id, Label: label})
	if err := e.App.SaveWithContext(ctx, rec); err != nil {
		var verrs validation.Errors
		if errors.As(err, &verrs) {
			return e.BadRequestError("Please check the highlighted fields.", verrs)
		}
		return e.BadRequestError("Failed to submit the form.", err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"ok":          true,
		"message":     f.rec.GetString("success_message"),
		"redirectUrl": f.rec.GetString("redirect_url"),
	})
}
