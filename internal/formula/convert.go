package formula

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/tools/types"
)

// Number is the result of the number functions: it prints without an
// exponent (1234567.5, not 1.2345675e+06).
type Number float64

func (n Number) String() string {
	return formatFloat(float64(n))
}

// List is the result of the list functions: it prints as "a, b, c".
type List []any

func (l List) String() string {
	return toStr([]any(l))
}

// formatFloat prints a number without exponent nor float noise (59.7, not
// 59.699999999999996).
func formatFloat(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ""
	}
	s := strconv.FormatFloat(f, 'f', 10, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}

// plain decodes the raw JSON of JSON fields (types.JSONRaw) so that its
// values can be used like the others.
func plain(v any) any {
	switch val := v.(type) {
	case types.JSONRaw:
		var out any
		if len(val) == 0 || json.Unmarshal(val, &out) != nil {
			return nil
		}
		return out
	case json.RawMessage:
		var out any
		if len(val) == 0 || json.Unmarshal(val, &out) != nil {
			return nil
		}
		return out
	}
	return v
}

var anyMapType = reflect.TypeOf(map[string]any{})

// asMap returns an object as a map (also the named map types).
func asMap(v any) (map[string]any, bool) {
	switch val := v.(type) {
	case nil:
		return nil, false
	case map[string]any:
		return val, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map && rv.Type().ConvertibleTo(anyMapType) {
		return rv.Convert(anyMapType).Interface().(map[string]any), true
	}
	return nil, false
}

// toFloat returns the number a value holds (numbers and numeric texts).
func toFloat(v any) (float64, bool) {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return 0, false
	case bool:
		return 0, false
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// num is toFloat with 0 for the values that aren't numbers.
func num(v any) float64 {
	f, _ := toFloat(v)
	return f
}

func toInt(v any) int {
	f := num(v)
	if f > math.MaxInt32 {
		return math.MaxInt32
	}
	if f < math.MinInt32 {
		return math.MinInt32
	}
	return int(f)
}

// toStr returns the text of a value as the templates print it, lists joined
// with ", ".
func toStr(v any) string {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case []byte:
		return string(val)
	case bool:
		return strconv.FormatBool(val)
	case float64:
		return formatFloat(val)
	case float32:
		return formatFloat(float64(val))
	case types.DateTime:
		if val.IsZero() {
			return ""
		}
		return val.String()
	case time.Time:
		if val.IsZero() {
			return ""
		}
		return val.Format("2006-01-02 15:04:05.000Z07:00")
	case fmt.Stringer:
		return val.String()
	case map[string]any:
		b, _ := json.Marshal(val)
		return string(b)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = toStr(rv.Index(i).Interface())
		}
		return strings.Join(parts, ", ")
	}
	if f, ok := toFloat(v); ok {
		return formatFloat(f)
	}
	return fmt.Sprint(v)
}

// toList returns the items of a list (a single value is a list of one, no
// value or an empty text an empty list).
func toList(v any) []any {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return []any{}
	case []any:
		return val
	case List:
		return val
	case string:
		if val == "" {
			return []any{}
		}
		return []any{val}
	case []byte:
		return []any{string(val)}
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out
	}
	return []any{v}
}

// isList reports whether a value is a list (not a text).
func isList(v any) bool {
	v = plain(v)
	if _, ok := v.([]byte); ok {
		return false
	}
	if v == nil {
		return false
	}
	k := reflect.ValueOf(v).Kind()
	return k == reflect.Slice || k == reflect.Array
}

// truthy reports whether a value counts as true in conditions (as the
// templates' "if" does: false, 0, empty texts and lists are false).
func truthy(v any) bool {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case string:
		return val != ""
	case types.DateTime:
		return !val.IsZero()
	case time.Time:
		return !val.IsZero()
	}
	if f, ok := toFloat(v); ok {
		return f != 0
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	case reflect.Pointer, reflect.Interface:
		return !rv.IsNil()
	}
	return true
}

// toTime returns the date a value holds: dates, texts in the PocketBase
// or ISO formats. Times of a zone (see tz) keep it.
func toTime(v any) (time.Time, bool) {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return val, !val.IsZero()
	case ZonedTime:
		return val.t, !val.t.IsZero()
	case types.DateTime:
		return val.Time(), !val.IsZero()
	case *types.DateTime:
		if val == nil {
			return time.Time{}, false
		}
		return val.Time(), !val.IsZero()
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{"2006-01-02", "2006-01-02 15:04", "2006-01-02T15:04"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		d, err := types.ParseDateTime(s)
		if err != nil || d.IsZero() {
			return time.Time{}, false
		}
		return d.Time(), true
	}
	return time.Time{}, false
}

// ZonedTime is a date in a time zone (see tz). It prints with its offset
// ("2026-10-01 11:30:00.000+02:00"), which the date fields also accept (they
// store it in UTC).
type ZonedTime struct{ t time.Time }

func (z ZonedTime) Time() time.Time { return z.t }
func (z ZonedTime) IsZero() bool    { return z.t.IsZero() }
func (z ZonedTime) String() string {
	if z.t.IsZero() {
		return ""
	}
	return z.t.Format("2006-01-02 15:04:05.000-07:00")
}
func (z ZonedTime) MarshalJSON() ([]byte, error) { return json.Marshal(z.String()) }

// dateOut returns a date result: UTC dates print in the PocketBase format
// ("2026-10-11 09:30:00.000Z", the format of the date fields), the dates of a
// zone keep it (with their offset).
func dateOut(t time.Time) any {
	if t.Location() != time.UTC {
		return ZonedTime{t}
	}
	d, err := types.ParseDateTime(t)
	if err != nil {
		return t
	}
	return d
}
