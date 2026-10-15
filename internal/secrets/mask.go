package secrets

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Hidden replaces the values of the secrets in the logs and errors.
const Hidden = "[hidden]"

// forms returns the forms of the values found in a text: as is, escaped in
// a URL, in base64 (basic auth); the shorter values aren't replaced (they
// would garble the text).
func forms(values []string) []string {
	var out []string
	for _, v := range values {
		if len(v) < 4 {
			continue
		}
		out = append(out, v, url.QueryEscape(v), url.PathEscape(v), base64.StdEncoding.EncodeToString([]byte(v)))
	}
	return out
}

// Mask replaces the values in a text.
func Mask(s string, values []string) string {
	for _, f := range forms(values) {
		if f != "" && strings.Contains(s, f) {
			s = strings.ReplaceAll(s, f, Hidden)
		}
	}
	return s
}

// MaskError replaces the values in an error message.
func MaskError(err error, values []string) error {
	if err == nil || len(values) == 0 {
		return err
	}
	if msg := Mask(err.Error(), values); msg != err.Error() {
		return errors.New(msg)
	}
	return err
}

// MaskValue replaces the values in the strings of plain data (maps, lists),
// returning a copy when something changed.
func MaskValue(v any, values []string) any {
	if len(values) == 0 {
		return v
	}
	switch val := v.(type) {
	case string:
		return Mask(val, values)
	case []byte:
		return []byte(Mask(string(val), values))
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = MaskValue(item, values)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(val))
		for k, item := range val {
			out[k] = Mask(item, values)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = MaskValue(item, values)
		}
		return out
	case []string:
		out := make([]string, len(val))
		for i, item := range val {
			out[i] = Mask(item, values)
		}
		return out
	case map[string][]string:
		out := make(map[string][]string, len(val))
		for k, items := range val {
			out[k] = MaskValue(items, values).([]string)
		}
		return out
	case url.Values:
		return url.Values(MaskValue(map[string][]string(val), values).(map[string][]string))
	case http.Header:
		return http.Header(MaskValue(map[string][]string(val), values).(map[string][]string))
	}
	return v
}
