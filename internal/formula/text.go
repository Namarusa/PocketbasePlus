package formula

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const maxTextLen = 100_000 // of the texts the functions build (repeat, pad)

// capitalize upper-cases the first letter.
func capitalize(v any) string {
	s := toStr(v)
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// titleCase upper-cases the first letter of every word.
func titleCase(v any) string {
	s := toStr(v)
	var b strings.Builder
	start := true
	for _, r := range s {
		if start && unicode.IsLetter(r) {
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(r)
		}
		start = unicode.IsSpace(r) || r == '-' || r == '\''
	}
	return b.String()
}

// truncate shortens a text to n letters, with "…" when it's cut.
func truncate(n, v any) string {
	s := toStr(v)
	max := toInt(n)
	if max < 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimRightFunc(string(runes[:max]), unicode.IsSpace) + "…"
}

var slugDrop = regexp.MustCompile(`[^a-z0-9]+`)

// slug returns "hello-world" for "Hello, World!" (accents removed).
func slug(v any) string {
	s := norm.NFD.String(strings.ToLower(toStr(v)))
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	return strings.Trim(slugDrop.ReplaceAllString(s, "-"), "-")
}

func pad(n, ch, v any, left bool) string {
	s := toStr(v)
	fill := toStr(ch)
	if fill == "" {
		fill = " "
	}
	missing := min(toInt(n), maxTextLen) - utf8.RuneCountInString(s)
	if missing <= 0 {
		return s
	}
	f := []rune(fill)
	p := make([]rune, missing)
	for i := range p {
		p[i] = f[i%len(f)]
	}
	if left {
		return string(p) + s
	}
	return s + string(p)
}

// substr returns the letters from start to end (excluded; a negative end
// counts from the end).
func substr(start, end, v any) string {
	runes := []rune(toStr(v))
	a, b := toInt(start), toInt(end)
	if b < 0 {
		b += len(runes) + 1
	}
	a = max(0, min(a, len(runes)))
	b = max(a, min(b, len(runes)))
	return string(runes[a:b])
}

func repeat(n, v any) string {
	s := toStr(v)
	count := toInt(n)
	if count <= 0 || s == "" {
		return ""
	}
	count = min(count, maxTextLen/len(s)+1)
	out := strings.Repeat(s, count)
	if len(out) > maxTextLen {
		out = out[:maxTextLen]
	}
	return out
}

var tagRegex = regexp.MustCompile(`(?s)<[^>]*>`)

// stripHTML returns the text of HTML (e.g. a rich text field).
func stripHTML(v any) string {
	s := tagRegex.ReplaceAllString(toStr(v), " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

func initials(v any) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(toStr(v), func(r rune) bool { return unicode.IsSpace(r) || r == '-' }) {
		if r, _ := utf8.DecodeRuneInString(w); unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// contains reports whether a text contains another, or a list an item.
func contains(needle, hay any) bool {
	if isList(hay) {
		for _, item := range toList(hay) {
			if looseEqual(item, needle) {
				return true
			}
		}
		return false
	}
	return strings.Contains(toStr(hay), toStr(needle))
}

func split(sep, v any) List {
	s := toStr(v)
	if s == "" {
		return []any{}
	}
	parts := strings.Split(s, toStr(sep))
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func join(sep, v any) string {
	items := toList(v)
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = toStr(item)
	}
	return strings.Join(parts, toStr(sep))
}

// length counts the letters of a text, the items of a list or object.
func length(v any) int {
	v = plain(v)
	switch val := v.(type) {
	case nil:
		return 0
	case string:
		return utf8.RuneCountInString(val)
	}
	if m, ok := asMap(v); ok {
		return len(m)
	}
	if isList(v) {
		return len(toList(v))
	}
	return utf8.RuneCountInString(toStr(v))
}

func concat(values ...any) string {
	var b strings.Builder
	for _, v := range values {
		b.WriteString(toStr(v))
		if b.Len() > maxTextLen {
			break
		}
	}
	return b.String()
}

func urlEncode(v any) string {
	return url.QueryEscape(toStr(v))
}
