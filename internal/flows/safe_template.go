package flows

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The values inserted in the URL and the JSON body of an HTTP request come from
// trigger data (form fields of the visitors, webhook bodies...): they're
// escaped for their place, so a value can't add query parameters or JSON
// fields ("x","admin":true).

// controlActionRegex matches the template actions that aren't a value
// (if/range/with... blocks): such templates are rendered as written.
var controlActionRegex = regexp.MustCompile(`^\{\{-?\s*(if|else|end|range|with|define|template|block|break|continue)\b`)

func hasControlActions(actions []string) bool {
	for _, a := range actions {
		if controlActionRegex.MatchString(a) {
			return true
		}
	}
	return false
}

// renderURL renders a URL template: a value at the start is the base URL
// (kept as is); the others are escaped as a path segment, or as a query value
// after the "?".
func renderURL(v any, data map[string]any) (string, error) {
	src, ok := v.(string)
	if !ok || !strings.Contains(src, "{{") {
		return resolveString(v, data)
	}
	locs := actionRegex.FindAllStringIndex(src, -1)
	actions := make([]string, len(locs))
	for i, l := range locs {
		actions[i] = src[l[0]:l[1]]
	}
	if hasControlActions(actions) {
		return resolveString(v, data)
	}
	query := strings.Index(src, "?")
	var b strings.Builder
	last := 0
	for i, l := range locs {
		b.WriteString(src[last:l[0]])
		val, err := resolveString(actions[i], data)
		if err != nil {
			return "", err
		}
		switch {
		case l[0] == 0:
			b.WriteString(val)
		case query >= 0 && l[0] > query:
			b.WriteString(url.QueryEscape(val))
		default:
			b.WriteString(url.PathEscape(val))
		}
		last = l[1]
	}
	b.WriteString(src[last:])
	return b.String(), nil
}

// renderJSONBody renders a body template that is JSON: a value inside a JSON
// string is escaped as string content, a value in place of a JSON value is
// encoded as JSON ({{json ...}} and other helpers are inserted as written).
// Other bodies are rendered as written.
func renderJSONBody(v any, data map[string]any) (string, error) {
	src, ok := v.(string)
	if !ok || !strings.Contains(src, "{{") {
		return resolveString(v, data)
	}
	locs := actionRegex.FindAllStringIndex(src, -1)
	actions := make([]string, len(locs))
	inString := make([]bool, len(locs))
	for i, l := range locs {
		actions[i] = src[l[0]:l[1]]
	}
	if hasControlActions(actions) {
		return resolveString(v, data)
	}
	// where each action is: inside a JSON string or in place of a value
	var probe strings.Builder
	str, esc, last := false, false, 0
	for i, l := range locs {
		for _, c := range src[last:l[0]] {
			switch {
			case esc:
				esc = false
			case c == '\\' && str:
				esc = true
			case c == '"':
				str = !str
			}
		}
		probe.WriteString(src[last:l[0]])
		inString[i] = str
		token := fmt.Sprintf("__gp_value_%d__", i)
		if str {
			probe.WriteString(token)
		} else {
			probe.WriteString(`"` + token + `"`)
		}
		last = l[1]
	}
	probe.WriteString(src[last:])
	if !json.Valid([]byte(probe.String())) {
		return resolveString(v, data) // not JSON (text, form data...)
	}

	var b strings.Builder
	last = 0
	for i, l := range locs {
		b.WriteString(src[last:l[0]])
		last = l[1]
		_, single := singleVar(actions[i])
		val, err := resolveValue(actions[i], data)
		if err != nil {
			return "", err
		}
		switch {
		case inString[i]:
			raw, _ := json.Marshal(toString(val))
			b.Write(raw[1 : len(raw)-1])
		case single:
			raw, err := json.Marshal(toPlain(val))
			if err != nil {
				return "", err
			}
			b.Write(raw)
		default:
			b.WriteString(toString(val)) // {{json .x}}: already JSON
		}
	}
	b.WriteString(src[last:])
	return b.String(), nil
}

// renderXMLBody renders an XML body template: every value is escaped as XML
// text (it can't add elements or attributes, in an element or an attribute
// value). Templates with blocks are rendered as written.
func renderXMLBody(v any, data map[string]any) (string, error) {
	src, ok := v.(string)
	if !ok || !strings.Contains(src, "{{") {
		return resolveString(v, data)
	}
	locs := actionRegex.FindAllStringIndex(src, -1)
	actions := make([]string, len(locs))
	for i, l := range locs {
		actions[i] = src[l[0]:l[1]]
	}
	if hasControlActions(actions) {
		return resolveString(v, data)
	}
	var b strings.Builder
	last := 0
	for i, l := range locs {
		b.WriteString(src[last:l[0]])
		val, err := resolveString(actions[i], data)
		if err != nil {
			return "", err
		}
		_ = xml.EscapeText(&b, []byte(val))
		last = l[1]
	}
	b.WriteString(src[last:])
	return b.String(), nil
}
