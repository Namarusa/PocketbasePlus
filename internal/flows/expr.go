package flows

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
)

// a value that is exactly one variable reference, e.g. "{{ .steps.lookup.items }}",
// or without the dot for the roots ("{{steps.lookup.items}}")
var singleVarRegex = regexp.MustCompile(`^\{\{\s*(\.)?([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)\s*\}\}$`)

// roots are the data names usable without their dot: {{steps.api.body}},
// {{upper item.name}}. ("now", "last" and "index" are also function names:
// they keep the dot.) "secret" only has data in the HTTP requests.
var roots = []string{"trigger", "steps", "item", "flow", "secret"}

// singleVar returns the path of a value that is exactly one variable.
func singleVar(s string) (string, bool) {
	m := singleVarRegex.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	if m[1] == "" {
		root, _, _ := strings.Cut(m[2], ".")
		if !slices.Contains(roots, root) {
			return "", false
		}
	}
	return m[2], true
}

// normalize writes the roots used without their dot as data references.
func normalize(src string) string {
	return formula.Normalize(src, roots...)
}

// any template action inside a string
var actionRegex = regexp.MustCompile(`\{\{(.*?)\}\}`)

// resolveValue resolves a step config value against the run data.
//
//   - a string that is exactly one variable returns the raw value (list, map, number...)
//   - any other string containing {{...}} is rendered as a Go text template
//   - maps and lists are resolved recursively
func resolveValue(v any, data map[string]any) (any, error) {
	switch val := v.(type) {
	case string:
		if path, ok := singleVar(val); ok {
			return lookupPath(data, path), nil
		}
		if !strings.Contains(val, "{{") {
			return val, nil
		}
		// a function giving a list or an object ({{where "stock" 0 .items}}):
		// the list itself, e.g. for the "For each" steps
		if out, ok, err := formula.Value(val, data, roots...); ok {
			if err != nil {
				return nil, err
			}
			if p := formula.Plain(out); isCollection(p) {
				return p, nil
			}
		}
		return renderString(val, data)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			r, err := resolveValue(item, data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			r, err := resolveValue(item, data)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

func isCollection(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Map:
		return true
	case reflect.Slice, reflect.Array:
		_, isBytes := v.([]byte)
		return !isBytes
	}
	return false
}

// resolveString resolves a value and converts it to a string.
func resolveString(v any, data map[string]any) (string, error) {
	r, err := resolveValue(v, data)
	if err != nil {
		return "", err
	}
	return toString(r), nil
}

// templateFuncs are the functions of the templates (internal/formula, as
// the emails).
func templateFuncs() map[string]any {
	return mail.TemplateFuncs()
}

func renderString(src string, data map[string]any) (string, error) {
	t, err := template.New("v").Funcs(templateFuncs()).Option("missingkey=zero").Parse(normalize(src))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.ReplaceAll(buf.String(), "<no value>", ""), nil
}

// validateTemplates checks that every string in a config value parses.
func validateTemplates(v any) error {
	switch val := v.(type) {
	case string:
		if strings.Contains(val, "{{") {
			_, err := template.New("v").Funcs(templateFuncs()).Parse(normalize(val))
			return err
		}
	case map[string]any:
		for k, item := range val {
			if err := validateTemplates(item); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	case []any:
		for _, item := range val {
			if err := validateTemplates(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// lookupPath walks a dotted path ("steps.lookup.items.0.title") in plain data.
func lookupPath(data any, path string) any {
	cur := data
	for _, part := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(c) {
				return nil
			}
			cur = c[i]
		default:
			return nil
		}
	}
	return cur
}

// bindFilter replaces each {{...}} of a PocketBase filter with a bound
// parameter holding the resolved value, so that trigger/step data can't
// inject filter syntax.
func bindFilter(filter string, data map[string]any) (string, dbx.Params, error) {
	params := dbx.Params{}
	var firstErr error
	i := 0
	out := actionRegex.ReplaceAllStringFunc(filter, func(action string) string {
		v, err := resolveValue(action, data)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		name := fmt.Sprintf("flowp%d", i)
		i++
		params[name] = scalarParam(v)
		return "{:" + name + "}"
	})
	return out, params, firstErr
}

func scalarParam(v any) any {
	switch val := v.(type) {
	case nil:
		return ""
	case string, bool, float64, int, int64:
		return val
	default:
		return toString(val)
	}
}

// toString converts plain values to a string (maps/lists as JSON).
func toString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case map[string]any, []any:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprint(val)
	}
}

// toPlain converts any value to plain JSON data (maps, lists, strings,
// float64, bools) by a JSON round trip, so it can be walked and stored.
func toPlain(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return toString(v)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return toString(v)
	}
	return out
}

// recordData exports a record as plain data, with the same fields as email
// templates: hidden fields (except the email of auth records) and passwords are
// left out, since flows can return them (e.g. webhook responses).
func recordData(r *core.Record) map[string]any {
	if r == nil {
		return nil
	}
	data := mail.RecordData(r)
	if plain, ok := toPlain(data).(map[string]any); ok {
		return plain
	}
	return data
}

// setPath sets a dotted path value in a nested map ("record.title").
func setPath(m map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	cur := m
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asString(m map[string]any, key string) string {
	return toString(m[key])
}

func asBool(m map[string]any, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}

func asInt(m map[string]any, key string, def int) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return def
}
