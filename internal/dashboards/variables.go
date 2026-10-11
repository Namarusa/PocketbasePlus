package dashboards

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Variable is a dashboard input (shown above the panels) that the panel
// filters reference as {{$key}} (date ranges as {{$key.from}} / {{$key.to}}).
type Variable struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"` // date_range | select | text | number
	Options []string `json:"options,omitempty"`
	Default any      `json:"default,omitempty"`
}

var (
	varKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,49}$`)
	varRefRegex = regexp.MustCompile(`\{\{\s*\$([A-Za-z_][A-Za-z0-9_]*)(?:\.(from|to))?\s*\}\}`)
	varTypes    = map[string]bool{"date_range": true, "select": true, "text": true, "number": true}
)

// resolvedVar is the value of a variable for a request.
type resolvedVar struct {
	Type     string
	Value    any    // select/text: string, number: float64
	From, To string // date ranges (PocketBase datetime strings, To excluded)
}

// validateVariables checks the variable definitions of a dashboard.
func validateVariables(defs []Variable) error {
	seen := map[string]bool{}
	for _, v := range defs {
		if !varKeyRegex.MatchString(v.Key) {
			return fmt.Errorf("invalid variable key %q (letters, digits and _)", v.Key)
		}
		if seen[v.Key] {
			return fmt.Errorf("the variable key %q is used more than once", v.Key)
		}
		seen[v.Key] = true
		if !varTypes[v.Type] {
			return fmt.Errorf("variable %q: unknown type %q", v.Key, v.Type)
		}
	}
	return nil
}

const dtLayout = "2006-01-02 15:04:05.000Z"

// resolveRange turns a date range value (a preset like "30d", or
// {preset} / {from, to} with YYYY-MM-DD dates) into from/to datetimes.
func resolveRange(value any, now time.Time) (string, string, error) {
	now = now.UTC()
	preset := ""
	var from, to string
	switch v := value.(type) {
	case nil:
		preset = "all"
	case string:
		preset = v
	case map[string]any:
		preset, _ = v["preset"].(string)
		from, _ = v["from"].(string)
		to, _ = v["to"].(string)
	default:
		return "", "", fmt.Errorf("invalid date range")
	}

	if preset != "" && preset != "custom" {
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end := now.Add(time.Second).Format(dtLayout)
		switch preset {
		case "all":
			return "0001-01-01 00:00:00.000Z", "9999-12-31 23:59:59.999Z", nil
		case "today":
			return day.Format(dtLayout), day.AddDate(0, 0, 1).Format(dtLayout), nil
		case "this_month":
			return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format(dtLayout), end, nil
		case "this_year":
			return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC).Format(dtLayout), end, nil
		}
		if d, ok := ranges[preset]; ok {
			return now.Add(-d).Format(dtLayout), end, nil
		}
		return "", "", fmt.Errorf("unknown date range %q", preset)
	}

	parse := func(s string, endOfDay bool) (string, error) {
		s = strings.TrimSpace(s)
		if len(s) == 10 { // YYYY-MM-DD
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return "", err
			}
			if endOfDay {
				t = t.AddDate(0, 0, 1) // the "to" day is included
			}
			return t.Format(dtLayout), nil
		}
		dt, err := types.ParseDateTime(s)
		if err != nil || dt.IsZero() {
			return "", fmt.Errorf("invalid date %q", s)
		}
		return dt.Time().UTC().Format(dtLayout), nil
	}
	f, t := "0001-01-01 00:00:00.000Z", "9999-12-31 23:59:59.999Z"
	var err error
	if from != "" {
		if f, err = parse(from, false); err != nil {
			return "", "", err
		}
	}
	if to != "" {
		if t, err = parse(to, true); err != nil {
			return "", "", err
		}
	}
	return f, t, nil
}

// resolveVariables returns the values of the variables (or their defaults).
func resolveVariables(defs []Variable, values map[string]any) (map[string]resolvedVar, error) {
	out := map[string]resolvedVar{}
	for _, d := range defs {
		v, ok := values[d.Key]
		if !ok {
			v = d.Default
		}
		rv := resolvedVar{Type: d.Type}
		switch d.Type {
		case "date_range":
			from, to, err := resolveRange(v, time.Now())
			if err != nil {
				return nil, fmt.Errorf("variable %s: %w", d.Key, err)
			}
			rv.From, rv.To = from, to
		case "number":
			switch n := v.(type) {
			case float64:
				rv.Value = n
			case nil, string:
				if s, _ := n.(string); strings.TrimSpace(s) == "" {
					rv.Value = ""
				} else {
					var f float64
					if _, err := fmt.Sscan(s, &f); err != nil {
						return nil, fmt.Errorf("variable %s: invalid number", d.Key)
					}
					rv.Value = f
				}
			default:
				return nil, fmt.Errorf("variable %s: invalid number", d.Key)
			}
		default:
			if v == nil {
				v = ""
			}
			rv.Value = fmt.Sprint(v)
		}
		out[d.Key] = rv
	}
	return out, nil
}

// bindVariables replaces the {{$key}} references of a filter with bound
// parameters, so that variable values can't inject filter syntax.
func bindVariables(filter string, vars map[string]resolvedVar) (string, dbx.Params, error) {
	params := dbx.Params{}
	var firstErr error
	out := varRefRegex.ReplaceAllStringFunc(filter, func(ref string) string {
		m := varRefRegex.FindStringSubmatch(ref)
		key, part := m[1], m[2]
		v, ok := vars[key]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown dashboard variable $%s", key)
			}
			return ref
		}
		name := "gpv_" + key
		switch {
		case v.Type == "date_range" && part == "from":
			name += "_from"
			params[name] = v.From
		case v.Type == "date_range" && part == "to":
			name += "_to"
			params[name] = v.To
		case v.Type == "date_range":
			if firstErr == nil {
				firstErr = fmt.Errorf("use {{$%s.from}} and {{$%s.to}} for a date range", key, key)
			}
			return ref
		case part != "":
			if firstErr == nil {
				firstErr = fmt.Errorf("$%s is not a date range", key)
			}
			return ref
		default:
			params[name] = v.Value
		}
		return "{:" + name + "}"
	})
	return out, params, firstErr
}
