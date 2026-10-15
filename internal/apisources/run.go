package apisources

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/httpcall"
	"github.com/Namarusa/PocketbasePlus/internal/secrets"
)

// Variable is a value given to a call by the flow step ({{name}}, or
// {{.vars.name}}).
type Variable struct {
	Name string `json:"name"`
	// Default is used by the Test button and when the flow gives no value
	Default  string `json:"default,omitempty"`
	Test     string `json:"test,omitempty"` // before the defaults: used by the Test button only
	Required bool   `json:"required,omitempty"`
}

// reservedNames can't be written without .vars: the names of the functions
// and the words of the templates ({{len}} is the function).
var reservedNames = func() map[string]bool {
	m := map[string]bool{"vars": true, "secret": true, "nil": true, "true": true, "false": true}
	for _, w := range []string{"if", "else", "end", "range", "with", "define", "template", "block", "break", "continue",
		"and", "call", "html", "index", "slice", "js", "len", "not", "or", "print", "printf", "println", "urlquery", "eq", "ne", "lt", "le", "gt", "ge"} {
		m[w] = true
	}
	for name := range formula.Funcs() {
		m[name] = true
	}
	return m
}()

// shortNames returns the variables usable without .vars.
func shortNames(vars []Variable) []string {
	var out []string
	for _, v := range vars {
		if !reservedNames[v.Name] {
			out = append(out, v.Name)
		}
	}
	return out
}

// expand writes the variables used without .vars ({{id}}, {{upper id}}) as
// {{.vars.id}}, the form the escaping of the requests recognizes.
func expand(tpl string, names []string) string {
	return formula.RewriteNames(tpl, ".vars.", names...)
}

// Output is a named value of the response (a path in the parsed body).
type Output struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// variablesOf returns the variables declared by an API or a call.
func variablesOf(rec *core.Record) []Variable {
	var list []Variable
	_ = rec.UnmarshalJSONField("variables", &list)
	return list
}

// allVariables returns the variables of a call: the API's, then its own.
func allVariables(source, call *core.Record) []Variable {
	return append(variablesOf(source), variablesOf(call)...)
}

// signInVariables are the variables the sign-in of an API uses (their values
// are hidden in the run logs like the secret).
func signInVariables(source *core.Record) map[string]bool {
	out := map[string]bool{}
	names := shortNames(variablesOf(source))
	for _, f := range []string{"secret", "username"} {
		for _, m := range varRefRegex.FindAllStringSubmatch(expand(source.GetString(f), names), -1) {
			out[m[1]] = true
		}
	}
	return out
}

func outputsOf(call *core.Record) []Output {
	var list []Output
	_ = call.UnmarshalJSONField("outputs", &list)
	return list
}

func stringMap(rec *core.Record, field string) map[string]string {
	m := map[string]string{}
	_ = rec.UnmarshalJSONField(field, &m)
	return m
}

// Call runs a saved call with the values of its variables (the "API call"
// step of the flows).
func Call(ctx context.Context, app core.App, callID string, vars map[string]any) (any, map[string]any, error) {
	call, err := app.FindRecordById(CallsCollection, callID)
	if err != nil {
		return nil, nil, errors.New("the API call doesn't exist anymore")
	}
	source, err := app.FindRecordById(SourcesCollection, call.GetString("source"))
	if err != nil {
		return nil, nil, errors.New("the API of the call doesn't exist anymore")
	}
	return run(ctx, app, source, call, vars)
}

// secretsOf returns the secrets used by an API and a call ({{secret.NAME}}):
// their values, or their names for the preview.
func secretsOf(app core.App, source, call *core.Record, preview bool) (map[string]any, []string, error) {
	var fields []any
	for _, f := range []string{"base_url", "auth_name", "username", "secret", "headers"} {
		fields = append(fields, source.GetString(f))
	}
	for _, f := range []string{"path", "query", "headers", "body", "form"} {
		fields = append(fields, call.GetString(f))
	}
	if preview {
		return secrets.Placeholders(fields), nil, nil
	}
	return secrets.Load(app, fields)
}

// prepare renders the request of a call: the defaults of the variables the
// flow left empty, the required ones checked. input is what the run log shows.
func prepare(source, call *core.Record, vars, secretData map[string]any) (input map[string]any, req httpcall.Request, masked []string, err error) {
	given := make(map[string]any, len(vars))
	for k, v := range vars {
		given[k] = v
	}
	for _, v := range allVariables(source, call) {
		if isEmpty(given[v.Name]) && v.Default != "" {
			given[v.Name] = v.Default
		}
	}
	hidden := signInVariables(source)
	shown := make(map[string]any, len(given))
	for k, v := range given {
		if hidden[k] {
			v = "[hidden]"
		}
		shown[k] = v
	}
	input = map[string]any{"api": source.GetString("name"), "call": call.GetString("name"), "vars": shown}
	for _, v := range allVariables(source, call) {
		if v.Required && isEmpty(given[v.Name]) {
			return input, req, nil, fmt.Errorf("the variable %q is required: give it a value in the flow step (or a default value in the call)", v.Name)
		}
	}
	data := map[string]any{"vars": flows.ToPlain(given)}
	if secretData != nil {
		data["secret"] = secretData
	}
	req, logged, masked, err := buildRequest(source, call, data)
	for k, v := range logged {
		input[k] = v
	}
	return input, req, masked, err
}

// run sends a call (saved or a draft being tested).
func run(ctx context.Context, app core.App, source, call *core.Record, vars map[string]any) (any, map[string]any, error) {
	secretData, used, err := secretsOf(app, source, call, false)
	if err != nil {
		return nil, nil, err
	}
	input, req, hidden, err := prepare(source, call, vars, secretData)
	hidden = append(hidden, used...)
	if err != nil {
		return secrets.MaskValue(input, hidden), nil, hideSecrets(err, hidden)
	}

	res, err := httpcall.Do(ctx, req)
	err = hideSecrets(err, hidden)
	if res == nil {
		return secrets.MaskValue(input, hidden), nil, err
	}
	output := res.Output()
	output["attempts"] = res.Attempts
	output["duration_ms"] = res.Duration.Milliseconds()
	data := map[string]any{}
	for _, o := range outputsOf(call) {
		data[o.Name] = flows.LookupPath(res.Body, o.Path)
	}
	output["data"] = data
	// a response may repeat a secret (an echo, an error page)
	if len(hidden) > 0 {
		for k, v := range output {
			output[k] = secrets.MaskValue(v, hidden)
		}
		return secrets.MaskValue(input, hidden), output, err
	}
	return input, output, err
}

// hideSecrets removes the secrets from an error (e.g. a URL with an API key
// parameter in a network error).
func hideSecrets(err error, secrets []string) error {
	if err == nil {
		return err
	}
	msg := err.Error()
	for _, secret := range secrets {
		if len(secret) < 3 {
			continue // too short to replace without garbling the message
		}
		for _, s := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			msg = strings.ReplaceAll(msg, s, "[hidden]")
		}
	}
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

func isEmpty(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	}
	return false
}

// buildRequest renders the request of a call. The values of the variables
// are escaped for their place (path segment, query value, JSON string, XML
// text); the sign-in is added last, so the call's headers can't replace it.
// It also returns what the run log shows (never the secret) and the rendered
// secrets, to remove them from the errors.
func buildRequest(source, call *core.Record, data map[string]any) (req httpcall.Request, logged map[string]any, secrets []string, err error) {
	logged = map[string]any{}
	names := shortNames(allVariables(source, call))
	tpl := func(s string) string { return expand(s, names) }
	req = httpcall.Request{
		Method:      call.GetString("method"),
		Headers:     map[string]string{},
		Query:       url.Values{},
		Retries:     call.GetInt("retries"),
		RetryUnsafe: call.GetBool("retry_unsafe"),
		Format:      call.GetString("response_format"),
		Timeout:     time.Duration(source.GetInt("timeout_seconds")) * time.Second,
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if req.Format == "" {
		req.Format = httpcall.FormatAuto
	}
	logged["method"] = req.Method

	// the URL: a value is escaped as a path segment (or a query value), except
	// a value at the very start, which is the whole base address
	base := strings.TrimRight(source.GetString("base_url"), "/")
	path := strings.TrimSpace(call.GetString("path"))
	src := path
	switch {
	case base == "":
	case path == "":
		src = base
	case strings.HasPrefix(path, "?"):
		src = base + path
	default:
		src = base + "/" + strings.TrimLeft(path, "/")
	}
	u, err := flows.RenderURL(tpl(src), data)
	if err != nil {
		return req, logged, nil, fmt.Errorf("url: %w", err)
	}
	req.URL = u
	logged["url"] = u

	for k, src := range stringMap(call, "query") {
		v, err := flows.RenderString(tpl(src), data)
		if err != nil {
			return req, logged, nil, fmt.Errorf("query %s: %w", k, err)
		}
		if v == "" && strings.Contains(src, "{{") {
			continue // an optional parameter without a value
		}
		req.Query.Set(k, v)
	}
	if len(req.Query) > 0 {
		logged["query"] = req.Query.Encode()
	}

	for _, rec := range []*core.Record{source, call} {
		for k, src := range stringMap(rec, "headers") {
			v, err := renderHeader(tpl(src), data)
			if err != nil {
				return req, logged, nil, fmt.Errorf("header %s: %w", k, err)
			}
			req.Headers[k] = v
		}
	}

	var body string
	switch call.GetString("body_type") {
	case "json":
		body, err = flows.RenderJSONBody(tpl(call.GetString("body")), data)
		req.ContentType = "application/json"
	case "xml":
		body, err = flows.RenderXMLBody(tpl(call.GetString("body")), data)
		req.ContentType = "application/xml"
	case "text":
		body, err = flows.RenderString(tpl(call.GetString("body")), data)
		req.ContentType = "text/plain; charset=utf-8"
	case "form":
		form := url.Values{}
		for k, src := range stringMap(call, "form") {
			v, rerr := flows.RenderString(tpl(src), data)
			if rerr != nil {
				err = fmt.Errorf("%s: %w", k, rerr)
				break
			}
			form.Set(k, v)
		}
		body = form.Encode()
		req.ContentType = "application/x-www-form-urlencoded"
	}
	if err != nil {
		return req, logged, nil, fmt.Errorf("body: %w", err)
	}
	if body != "" {
		req.Body = []byte(body)
		logged["body"] = body
	}
	if len(req.Headers) > 0 {
		logged["headers"] = req.Headers // the flows run log hides the secret-looking ones
	}

	// the sign-in, after the values (its fields may use variables too)
	auth := source.GetString("auth")
	var secret, name, user string
	if auth != "" && auth != "none" {
		if secret, err = renderHeader(tpl(source.GetString("secret")), data); err != nil {
			return req, logged, nil, fmt.Errorf("secret: %w", err)
		}
		if user, err = renderHeader(tpl(source.GetString("username")), data); err != nil {
			return req, logged, nil, fmt.Errorf("username: %w", err)
		}
		if name, err = renderHeader(tpl(source.GetString("auth_name")), data); err != nil {
			return req, logged, nil, fmt.Errorf("key name: %w", err)
		}
		if (auth == "header" || auth == "query") && !headerRegex.MatchString(name) {
			return req, logged, nil, fmt.Errorf("invalid key name %q", name)
		}
		secrets = []string{secret, source.GetString("secret")}
		if auth == "basic" {
			secrets = append(secrets, base64.StdEncoding.EncodeToString([]byte(user+":"+secret)))
		}
	}
	switch auth {
	case "bearer":
		req.Headers = withHeader(req.Headers, "Authorization", "Bearer "+secret)
	case "basic":
		req.Headers = withHeader(req.Headers, "Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+secret)))
	case "header":
		req.Headers = withHeader(req.Headers, name, secret)
	case "query":
		req.Query.Set(name, secret)
	}
	if auth != "" && auth != "none" {
		logged["auth"] = auth
	}
	return req, logged, secrets, nil
}

// renderHeader renders a header value (no line breaks).
func renderHeader(tpl string, data map[string]any) (string, error) {
	v, err := flows.RenderString(tpl, data)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("the value has a line break")
	}
	return v, nil
}

// withHeader returns a copy of the headers with a value (any case of the name
// replaced), so the logged headers never get the secret.
func withHeader(h map[string]string, name, value string) map[string]string {
	out := make(map[string]string, len(h)+1)
	for k, v := range h {
		if !strings.EqualFold(k, name) {
			out[k] = v
		}
	}
	out[name] = value
	return out
}
