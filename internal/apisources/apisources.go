// Package apisources is the optional APIs module: outside APIs defined once
// (base URL, sign-in, shared headers, a secret kept out of the flows) with
// their saved calls (variables, response format, named response values), run
// by the "API call" step of the flows. The calls are tested from the
// dashboard (Flows → APIs), the last test response feeding the variable
// picker of the flow editor.
//
// It is removable: run "pocketbaseplus apis uninstall --yes" (deletes the APIs and
// their calls), then remove apisources.Register from main.go and this folder.
package apisources

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
)

const (
	SourcesCollection = "api_sources"
	CallsCollection   = "api_calls"
	// ExtName is the dashboard extension of the module (/_/extensions/{ExtName}/).
	ExtName = "pocketbaseplus-apis"
)

var (
	authTypes = []string{"none", "bearer", "basic", "header", "query"}
	methods   = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}
	bodyTypes = []string{"none", "json", "form", "xml", "text"}

	nameRegex   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	headerRegex = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")
	pathRegex   = regexp.MustCompile(`^[^.\s]+(\.[^.\s]+)*$`)
	varRefRegex = regexp.MustCompile(`\.vars\.([A-Za-z0-9_]+)`)
)

//go:embed all:ext
var extFS embed.FS

// Register adds the module: hooks, routes, dashboard pages and the
// "apis uninstall" command.
func Register(app *pocketbase.PocketBase) {
	app.RootCmd.AddCommand(command(app))
	Bind(app)
}

// Bind adds the hooks and routes of the module.
func Bind(app core.App) {
	internalcols.Add(SourcesCollection, CallsCollection)
	flows.SetAPICaller(&flows.APICaller{Call: Call, Variables: variableNames})

	files, err := fs.Sub(extFS, "ext")
	if err != nil {
		panic(err)
	}

	app.OnRecordValidate(SourcesCollection).BindFunc(func(e *core.RecordEvent) error {
		if err := validateSource(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordValidate(CallsCollection).BindFunc(func(e *core.RecordEvent) error {
		if err := validateCall(e.App, e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	// the secret is never returned, even to superusers: whether it's set and its end instead
	app.OnRecordEnrich(SourcesCollection).BindFunc(func(e *core.RecordEnrichEvent) error {
		rec := e.Record
		rec.WithCustomData(true)
		secret := rec.GetString("secret")
		if strings.Contains(secret, "{{") {
			// a secret made of variables: show which ones (not the rest of it)
			var refs []string
			for _, m := range varRefRegex.FindAllStringSubmatch(expand(secret, shortNames(variablesOf(rec))), -1) {
				refs = append(refs, "{{.vars."+m[1]+"}}")
			}
			rec.Set("secret_hint", "from "+strings.Join(refs, " "))
		} else {
			rec.Set("secret_hint", secretHint(secret))
		}
		// hidden fields are unhidden for superusers at the end of the enrich chain
		err := e.Next()
		rec.Hide("secret")
		return err
	})

	app.OnRecordDelete(SourcesCollection).BindFunc(func(e *core.RecordEvent) error {
		n, _ := e.App.CountRecords(CallsCollection, dbx.HashExp{"source": e.Record.Id})
		if n > 0 {
			return apis.NewBadRequestError("Delete the calls of this API first.", nil)
		}
		return e.Next()
	})
	// a deleted call would break the flows using it
	app.OnRecordDelete(CallsCollection).BindFunc(func(e *core.RecordEvent) error {
		var names []string
		_ = e.App.DB().Select("name").From("flows").Where(dbx.Like("steps", e.Record.Id)).Column(&names)
		if len(names) > 0 {
			return apis.NewBadRequestError(fmt.Sprintf("The call is used by the flows: %s. Change their API call steps first.", strings.Join(names, ", ")), nil)
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		se.UIExtensions = append(se.UIExtensions, core.UIExtension{Name: ExtName, FS: files})
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			if strings.HasPrefix(e.Request.URL.Path, "/_/extensions/"+ExtName+"/") {
				e.Response.Header().Set("Cache-Control", "no-cache") // revalidated after upgrades
			}
			return e.Next()
		})
		g := se.Router.Group("/api/apisources")
		g.Bind(apis.RequireSuperuserAuth())
		g.POST("/test", testRoute)
		g.GET("/calls", callsRoute)
		return se.Next()
	})
}

// secretHint shows the end of a secret ("…1a2b").
func secretHint(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "…"
	}
	return "…" + s[len(s)-4:]
}

func variableNames(app core.App, callID string) ([]string, error) {
	call, err := app.FindRecordById(CallsCollection, callID)
	if err != nil {
		return nil, fmt.Errorf("select an API call (Flows → APIs)")
	}
	var names []string
	source, _ := app.FindRecordById(SourcesCollection, call.GetString("source"))
	if source != nil {
		for _, v := range variablesOf(source) {
			names = append(names, v.Name)
		}
	}
	for _, v := range variablesOf(call) {
		names = append(names, v.Name)
	}
	return names, nil
}

// --- validation -------------------------------------------------------------------------

func validateSource(app core.App, r *core.Record) error {
	errs := validation.Errors{}
	r.Set("name", strings.TrimSpace(r.GetString("name")))
	r.Set("auth_name", strings.TrimSpace(r.GetString("auth_name")))
	base := strings.TrimRight(strings.TrimSpace(r.GetString("base_url")), "/")
	r.Set("base_url", base)

	declared := checkVariables(r, errs)
	check := templateChecker(declared, errs)
	if base != "" {
		if strings.Contains(base, "{{") {
			// e.g. https://{{.vars.host}}/v1, or {{.vars.base}} (a whole address)
			if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "{{") {
				errs["base_url"] = validation.NewError("invalid_base_url", "Start with http://, https:// or a variable.")
			}
			check("base_url", base)
		} else if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs["base_url"] = validation.NewError("invalid_base_url", "Enter an http:// or https:// address.")
		}
	}
	if r.GetString("auth") == "" {
		r.Set("auth", "none")
	}
	switch r.GetString("auth") {
	case "header", "query":
		name := r.GetString("auth_name")
		if !headerRegex.MatchString(name) && !(strings.Contains(name, "{{") && strings.TrimSpace(name) != "") {
			errs["auth_name"] = validation.NewError("invalid_auth_name", "Enter the name of the header or parameter holding the key (e.g. X-API-Key).")
		}
	}
	for _, f := range []string{"auth_name", "username", "secret"} {
		check(f, r.GetString(f))
	}
	if err := checkPairs(r, "headers", true, true); err != nil {
		errs["headers"] = err
	} else {
		for _, v := range stringMap(r, "headers") {
			check("headers", v)
		}
	}

	// the calls can't declare a variable of the API
	if _, done := errs["variables"]; !done && !r.IsNew() && len(declared) > 0 {
		calls, _ := app.FindAllRecords(CallsCollection, dbx.HashExp{"source": r.Id})
		for _, c := range calls {
			for _, v := range variablesOf(c) {
				if declared[v.Name] {
					errs["variables"] = validation.NewError("variable_in_call", fmt.Sprintf("The call %q has a variable %q too: rename one of them.", c.GetString("name"), v.Name))
				}
			}
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// checkVariables checks the variables declared by an API or a call and
// returns their names.
func checkVariables(r *core.Record, errs validation.Errors) map[string]bool {
	declared := map[string]bool{}
	for _, v := range variablesOf(r) {
		if !nameRegex.MatchString(v.Name) || declared[v.Name] {
			errs["variables"] = validation.NewError("invalid_variable", fmt.Sprintf("Variable names are unique and made of letters, digits and _ (%q).", v.Name))
			break
		}
		declared[v.Name] = true
	}
	return declared
}

// a {{name}} that isn't a declared variable (nor a function)
var undefinedFuncRegex = regexp.MustCompile(`function "([^"]+)" not defined`)

func shortNamesOf(declared map[string]bool) []string {
	list := make([]Variable, 0, len(declared))
	for name := range declared {
		list = append(list, Variable{Name: name})
	}
	return shortNames(list)
}

// templateChecker checks that templates parse and only use declared variables.
func templateChecker(declared map[string]bool, errs validation.Errors) func(field, src string) {
	return func(field, src string) {
		if _, done := errs[field]; done || !strings.Contains(src, "{{") {
			return
		}
		src = expand(src, shortNamesOf(declared))
		if err := flows.ValidateTemplate(src); err != nil {
			// {{name}} of a variable that isn't declared is read as a function
			if m := undefinedFuncRegex.FindStringSubmatch(err.Error()); m != nil && nameRegex.MatchString(m[1]) {
				errs[field] = validation.NewError("unknown_variable", fmt.Sprintf("Add the variable %q (or fix its name).", m[1]))
				return
			}
			errs[field] = validation.NewError("invalid_template", "Invalid template: "+err.Error())
			return
		}
		for _, m := range varRefRegex.FindAllStringSubmatch(src, -1) {
			if !declared[m[1]] {
				errs[field] = validation.NewError("unknown_variable", fmt.Sprintf("Add the variable %q (or fix its name).", m[1]))
				return
			}
		}
	}
}

// checkPairs checks a JSON field of name/value pairs (header names, template
// values). The values may have templates only in calls.
func checkPairs(r *core.Record, field string, headers, templates bool) error {
	raw := r.GetString(field)
	if raw == "" || raw == "null" {
		return nil
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return validation.NewError("invalid_pairs", "Expected names and text values.")
	}
	for k, v := range m {
		if strings.TrimSpace(k) == "" || (headers && !headerRegex.MatchString(k)) {
			return validation.NewError("invalid_name", fmt.Sprintf("Invalid name %q.", k))
		}
		if strings.ContainsAny(v, "\r\n") && headers {
			return validation.NewError("invalid_value", fmt.Sprintf("%s: the value has a line break.", k))
		}
		_ = templates // the templates are checked with the variables (templateChecker)
	}
	return nil
}

func validateCall(app core.App, r *core.Record) error {
	errs := validation.Errors{}
	r.Set("name", strings.TrimSpace(r.GetString("name")))
	r.Set("path", strings.TrimSpace(r.GetString("path")))
	if r.GetString("method") == "" {
		r.Set("method", "GET")
	}
	if r.GetString("body_type") == "" {
		r.Set("body_type", "none")
	}
	if r.GetString("response_format") == "" {
		r.Set("response_format", "auto")
	}

	declared := checkVariables(r, errs)
	// the variables of the API can be used too, but not declared again
	if source, err := app.FindRecordById(SourcesCollection, r.GetString("source")); err == nil {
		for _, v := range variablesOf(source) {
			if declared[v.Name] {
				errs["variables"] = validation.NewError("variable_of_api", fmt.Sprintf("The API already has a variable %q.", v.Name))
			}
			declared[v.Name] = true
		}
	}
	outs := map[string]bool{}
	for _, o := range outputsOf(r) {
		if !nameRegex.MatchString(o.Name) || outs[o.Name] {
			errs["outputs"] = validation.NewError("invalid_output", fmt.Sprintf("Output names are unique and made of letters, digits and _ (%q).", o.Name))
			break
		}
		if !pathRegex.MatchString(o.Path) {
			errs["outputs"] = validation.NewError("invalid_output_path", fmt.Sprintf("%s: enter the path of the value (e.g. items.0.id).", o.Name))
			break
		}
		outs[o.Name] = true
	}

	// templates: they parse and use declared variables
	check := templateChecker(declared, errs)
	check("path", r.GetString("path"))
	check("body", r.GetString("body"))
	for _, field := range []string{"query", "headers", "form"} {
		if err := checkPairs(r, field, field == "headers", true); err != nil {
			errs[field] = err
			continue
		}
		for _, v := range stringMap(r, field) {
			check(field, v)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// --- routes -----------------------------------------------------------------------------

// testRoute runs a call as edited (not saved yet) with the test values of
// its variables. With the id of a saved call, the response is kept as its
// sample (the fields offered by the flow editor).
func testRoute(e *core.RequestEvent) error {
	var body struct {
		ID     string         `json:"id"`
		Source string         `json:"source"`
		Call   map[string]any `json:"call"`
		Vars   map[string]any `json:"vars"`
		// only render the request (the live preview of the editor)
		DryRun bool `json:"dryRun"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid body.", err)
	}
	source, err := e.App.FindRecordById(SourcesCollection, body.Source)
	if err != nil {
		return e.BadRequestError("Save the API first.", nil)
	}
	col, err := e.App.FindCachedCollectionByNameOrId(CallsCollection)
	if err != nil {
		return err
	}
	call := core.NewRecord(col)
	delete(body.Call, "id")
	call.Load(body.Call)
	call.Set("source", source.Id)
	if err := validateCall(e.App, call); err != nil {
		if body.DryRun {
			return e.JSON(http.StatusOK, map[string]any{"error": firstError(err)})
		}
		return e.BadRequestError("Fix the call first.", err)
	}
	vars := body.Vars
	if vars == nil {
		// the default values (used by prepare), or the test values of the calls
		// saved before the defaults
		vars = map[string]any{}
		for _, v := range allVariables(source, call) {
			if v.Default == "" && v.Test != "" {
				vars[v.Name] = v.Test
			}
		}
	}

	if body.DryRun {
		input, req, _, err := prepare(source, call, vars)
		res := map[string]any{"request": input}
		if err == nil {
			full := req.URL
			if len(req.Query) > 0 {
				sep := "?"
				if strings.Contains(full, "?") {
					sep = "&"
				}
				full += sep + hideQuerySecret(source, req.Query).Encode()
			}
			res["url"] = full
		} else {
			res["error"] = err.Error()
		}
		return e.JSON(http.StatusOK, res)
	}

	input, output, err := run(e.Request.Context(), source, call, vars)
	res := map[string]any{"request": input}
	if err != nil {
		res["error"] = err.Error()
	}
	status, _ := output["status"].(int)
	if hint := errorHint(err, status, source); hint != "" {
		res["hint"] = hint
	}
	if output != nil {
		res["status"], res["ok"], res["format"] = output["status"], output["ok"], output["format"]
		res["headers"], res["data"] = output["headers"], output["data"]
		res["attempts"], res["duration_ms"] = output["attempts"], output["duration_ms"]
		res["body"] = prune(output["body"], 50, 2000)
		raw, _ := output["raw"].(string)
		if len(raw) > 200<<10 {
			raw = raw[:200<<10]
			res["raw_truncated"] = true
		}
		res["raw"] = raw
	}
	if body.ID != "" && output != nil && err == nil {
		if saved, ferr := e.App.FindRecordById(CallsCollection, body.ID); ferr == nil {
			saved.Set("sample", map[string]any{"format": output["format"], "body": prune(output["body"], 20, 300)})
			_ = e.App.SaveNoValidate(saved)
		}
	}
	return e.JSON(http.StatusOK, res)
}

// prune shortens a value for the dashboard: the first items of the lists,
// the start of the texts.
func prune(v any, maxItems, maxText int) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = prune(item, maxItems, maxText)
		}
		return out
	case []any:
		n := min(len(val), maxItems)
		out := make([]any, n)
		for i := range n {
			out[i] = prune(val[i], maxItems, maxText)
		}
		return out
	case string:
		if len(val) > maxText {
			return val[:maxText] + "…"
		}
	}
	return v
}

// callsRoute lists the calls for the flow editor: their variables, outputs
// and the fields of their last test response.
func callsRoute(e *core.RequestEvent) error {
	sources, err := e.App.FindAllRecords(SourcesCollection)
	if err != nil {
		return err
	}
	byID := map[string]*core.Record{}
	for _, s := range sources {
		byID[s.Id] = s
	}
	calls, err := e.App.FindAllRecords(CallsCollection)
	if err != nil {
		return err
	}
	list := []map[string]any{}
	for _, c := range calls {
		source := byID[c.GetString("source")]
		if source == nil {
			continue
		}
		vars := []map[string]any{}
		for _, v := range variablesOf(source) {
			vars = append(vars, map[string]any{"name": v.Name, "required": v.Required, "default": v.Default, "api": true})
		}
		for _, v := range variablesOf(c) {
			vars = append(vars, map[string]any{"name": v.Name, "required": v.Required, "default": v.Default})
		}
		var sample struct {
			Body any `json:"body"`
		}
		_ = c.UnmarshalJSONField("sample", &sample)
		list = append(list, map[string]any{
			"id":          c.Id,
			"name":        c.GetString("name"),
			"description": c.GetString("description"),
			"method":      c.GetString("method"),
			"source":      c.GetString("source"),
			"sourceName":  source.GetString("name"),
			"variables":   vars,
			"outputs":     outputsOf(c),
			"fields":      samplePaths(sample.Body),
			"sample":      sample.Body, // the last test response (shortened), for the values of the picker
		})
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a["sourceName"] != b["sourceName"] {
			return strings.ToLower(a["sourceName"].(string)) < strings.ToLower(b["sourceName"].(string))
		}
		return strings.ToLower(a["name"].(string)) < strings.ToLower(b["name"].(string))
	})
	return e.JSON(http.StatusOK, list)
}

// samplePaths lists the paths of a sample response (the first item of the
// lists), 4 levels and 80 paths at most.
func samplePaths(v any) []string {
	out := []string{}
	var walk func(v any, prefix string, depth int)
	walk = func(v any, prefix string, depth int) {
		if len(out) >= 80 || depth > 4 {
			return
		}
		switch val := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(val))
			for k := range val {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				if strings.ContainsAny(k, ". ") {
					continue // not reachable with a path
				}
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				out = append(out, p)
				walk(val[k], p, depth+1)
			}
		case []any:
			if len(val) > 0 {
				walk(val[0], prefix+".0", depth+1)
			}
		}
	}
	walk(v, "", 0)
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

// --- uninstall --------------------------------------------------------------------------

func command(app *pocketbase.PocketBase) *cobra.Command {
	cmd := &cobra.Command{Use: "apis", Short: "APIs module (saved API calls of the flows)"}
	var yes bool
	uninstall := &cobra.Command{
		Use:          "uninstall",
		SilenceUsage: true,
		Short:        "Deletes the APIs and their calls (before removing the module from main.go)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := app.FindCollectionByNameOrId(SourcesCollection); err != nil {
				fmt.Println("The APIs collection doesn't exist: nothing to uninstall.")
				return nil
			}
			total, _ := app.CountRecords(CallsCollection)
			if !yes {
				fmt.Printf("This deletes the APIs and their %d calls (flows using them will fail).\n"+
					"Back up pb_data first, then run again with --yes.\n", total)
				return nil
			}
			err := app.RunInTransaction(func(tx core.App) error {
				if err := dropCollections(tx); err != nil {
					return err
				}
				// forget the migration (re-adding the module creates the collections again)
				_, err := tx.DB().Delete("_migrations", dbx.In("file", migrationFile, varsMigrationFile)).Execute()
				return err
			})
			if err != nil {
				return err
			}
			fmt.Printf("Deleted %d API calls. Now remove apisources.Register(app) from main.go and the internal/apisources folder.\n", total)
			return nil
		},
	}
	uninstall.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	cmd.AddCommand(uninstall)
	return cmd
}

// hideQuerySecret returns the query of a preview without the API key of a
// "query" sign-in.
func hideQuerySecret(source *core.Record, q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = v
	}
	if source.GetString("auth") == "query" {
		if name := source.GetString("auth_name"); out.Has(name) {
			out.Set(name, "[hidden]")
		}
	}
	return out
}

// firstError returns the message of the first field error of a validation.
func firstError(err error) string {
	if errs, ok := err.(validation.Errors); ok {
		for field, e := range errs {
			return field + ": " + e.Error()
		}
	}
	return err.Error()
}

// errorHint explains the usual failures of a test in plain words.
func errorHint(err error, status int, source *core.Record) string {
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}
	switch {
	case strings.Contains(msg, "no such host"):
		return "The server name wasn't found: check the address (and that this machine can reach the internet)."
	case strings.Contains(msg, "connection refused"):
		return "Nothing answers at this address: check the host and the port, and that the service is running."
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout"):
		return "The API didn't answer in time: try again, or raise the timeout in the API settings."
	case strings.Contains(msg, "certificate"):
		return "The HTTPS certificate of the server isn't valid."
	case strings.Contains(msg, "isn't valid json") || strings.Contains(msg, "isn't valid xml"):
		return "The response doesn't match the chosen format: set the response format to Auto or Text."
	}
	switch {
	case status == 401 || status == 403:
		if a := source.GetString("auth"); a == "" || a == "none" {
			return "The API refuses the request: it probably needs a sign-in (API settings → Sign-in)."
		}
		return "The API refuses the sign-in: check the key or token in the API settings (and its name for a header or parameter key)."
	case status == 404:
		return "Not found: check the path and the values of the variables in it."
	case status == 405:
		return "The API doesn't accept this method for this address (GET, POST...)."
	case status == 429:
		return "Too many requests: the API limits how often it can be called. Retries can help."
	case status >= 500:
		return "The API failed on its side: try again later, or turn on retries."
	case status == 400 || status == 422:
		return "The API refuses the content of the request: check the parameters and the body (Request tab)."
	}
	return ""
}
