package formula

import (
	"sort"
	"strings"
	"time"
)

// list makes a list of its values; with one value, it's that value as a list
// (a list stays as is, no value is an empty list).
func list(values ...any) List {
	if len(values) == 1 {
		return toList(values[0])
	}
	return append([]any{}, values...)
}

func first(v any) any {
	items := toList(v)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func last(v any) any {
	items := toList(v)
	if len(items) == 0 {
		return ""
	}
	return items[len(items)-1]
}

// nth returns the item at a position (0 is the first, -1 the last).
func nth(i, v any) any {
	items := toList(v)
	n := toInt(i)
	if n < 0 {
		n += len(items)
	}
	if n < 0 || n >= len(items) {
		return ""
	}
	return items[n]
}

func take(n, v any) List {
	items := toList(v)
	c := max(0, min(toInt(n), len(items)))
	return items[:c]
}

func reverse(v any) List {
	items := toList(v)
	out := make([]any, len(items))
	for i, item := range items {
		out[len(items)-1-i] = item
	}
	return out
}

func unique(v any) List {
	var out []any
	seen := map[string]bool{}
	for _, item := range toList(v) {
		key := toStr(item)
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	if out == nil {
		return []any{}
	}
	return out
}

func sortList(v any) List {
	items := append([]any{}, toList(v)...)
	sort.SliceStable(items, func(i, j int) bool { return compare(items[i], items[j]) < 0 })
	return items
}

// sortBy sorts objects by a field ("-price": the largest first).
func sortBy(key, v any) List {
	k := toStr(key)
	desc := strings.HasPrefix(k, "-")
	k = strings.TrimPrefix(k, "-")
	items := append([]any{}, toList(v)...)
	sort.SliceStable(items, func(i, j int) bool {
		c := compare(Lookup(items[i], k), Lookup(items[j], k))
		if desc {
			return c > 0
		}
		return c < 0
	})
	return items
}

// pluck returns a field of every object of a list.
func pluck(key, v any) List {
	k := toStr(key)
	items := toList(v)
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, Lookup(item, k))
	}
	return out
}

// where keeps the objects of a list whose field equals a value.
func where(key, value, v any) List {
	k := toStr(key)
	out := []any{}
	for _, item := range toList(v) {
		if looseEqual(Lookup(item, k), value) {
			out = append(out, item)
		}
	}
	return out
}

// Lookup returns the value at a dotted path ("customer.name", "items.0.id")
// of maps and lists.
func Lookup(data any, path string) any {
	cur := plain(data)
	if path == "" {
		return cur
	}
	for _, part := range strings.Split(path, ".") {
		if m, ok := asMap(cur); ok {
			cur = plain(m[part])
			continue
		}
		if !isList(cur) || part == "" || len(part) > 8 || strings.Trim(part, "0123456789") != "" {
			return nil
		}
		items := toList(cur)
		i := toInt(part)
		if i >= len(items) {
			return nil
		}
		cur = plain(items[i])
	}
	return cur
}

// --- comparisons ---------------------------------------------------------------------------

// compare orders two values: as numbers when both are, as dates when both
// are, else as texts.
func compare(a, b any) int {
	fa, okA := toFloat(a)
	fb, okB := toFloat(b)
	if okA && okB {
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	}
	if isDate(a) || isDate(b) {
		ta, okA := toTime(a)
		tb, okB := toTime(b)
		if okA && okB {
			return ta.Compare(tb)
		}
	}
	return strings.Compare(toStr(a), toStr(b))
}

func isDate(v any) bool {
	switch v.(type) {
	case time.Time:
		return true
	}
	_, ok := v.(interface{ Time() time.Time })
	return ok
}

func looseEqual(a, b any) bool {
	if a == nil || b == nil {
		return toStr(a) == toStr(b)
	}
	return compare(a, b) == 0
}

// eq reports whether the first value equals any of the others (1 equals
// 1.0 and "1": no "incompatible types" error).
func eq(a any, others ...any) bool {
	for _, b := range others {
		if looseEqual(a, b) {
			return true
		}
	}
	return false
}

func ne(a, b any) bool { return !looseEqual(a, b) }
func lt(a, b any) bool { return compare(a, b) < 0 }
func le(a, b any) bool { return compare(a, b) <= 0 }
func gt(a, b any) bool { return compare(a, b) > 0 }
func ge(a, b any) bool { return compare(a, b) >= 0 }

func ifElse(cond, yes, no any) any {
	if truthy(cond) {
		return yes
	}
	return no
}

// coalesce returns the first value that isn't empty.
func coalesce(values ...any) any {
	for _, v := range values {
		if truthy(v) || isZeroNumber(v) {
			return v
		}
	}
	return ""
}

func isZeroNumber(v any) bool {
	if _, ok := v.(string); ok {
		return false
	}
	f, ok := toFloat(v)
	return ok && f == 0
}

// isEmpty reports whether a value is empty: no value, "", an empty list or
// object (0 and false aren't empty).
func isEmpty(v any) bool {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case bool:
		return false
	}
	if m, ok := asMap(v); ok {
		return len(m) == 0
	}
	if isList(v) {
		return len(toList(v)) == 0
	}
	if _, ok := toFloat(v); ok {
		return false
	}
	if d, ok := v.(interface{ IsZero() bool }); ok {
		return d.IsZero()
	}
	return false
}

// defaultValue returns the value, or the default when it's empty (no value
// or ""; 0 and false are kept).
func defaultValue(def, v any) any {
	if v == nil || v == "" {
		return def
	}
	return v
}
