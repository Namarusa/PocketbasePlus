package formula

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the zones of tz, also on systems without them (offline binary)
)

// Now returns the current time (replaced by the tests).
var Now = func() time.Time { return time.Now().UTC() }

// named formats of formatDate
var namedFormats = map[string]string{
	"date":     "MMM D, YYYY",
	"datetime": "MMM D, YYYY HH:mm",
	"time":     "HH:mm",
	"iso":      "YYYY-MM-DDTHH:mm:ssZ",
	"storage":  "YYYY-MM-DD HH:mm:ss.SSSZ", // the date fields (in UTC)
}

// dateTokens are the formatDate tokens, longest first.
var dateTokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "SSS", "YY", "MM", "DD", "HH", "hh", "mm", "ss", "M", "D", "H", "h", "m", "s", "A", "a", "Z"}

// formatDate formats a date with tokens (YYYY-MM-DD, DD/MM/YYYY HH:mm...);
// text in [brackets] is kept as is.
func formatDate(layout string, v any) string {
	t, ok := toTime(v)
	if !ok {
		return ""
	}
	if named, ok := namedFormats[strings.TrimSpace(layout)]; ok {
		if strings.TrimSpace(layout) == "storage" {
			t = t.UTC()
		}
		layout = named
	}
	var b strings.Builder
	for i := 0; i < len(layout); {
		if layout[i] == '[' {
			if end := strings.IndexByte(layout[i:], ']'); end > 0 {
				b.WriteString(layout[i+1 : i+end])
				i += end + 1
				continue
			}
		}
		matched := false
		for _, tok := range dateTokens {
			if strings.HasPrefix(layout[i:], tok) {
				b.WriteString(dateToken(t, tok))
				i += len(tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(layout[i])
			i++
		}
	}
	return b.String()
}

func dateToken(t time.Time, tok string) string {
	h12 := t.Hour() % 12
	if h12 == 0 {
		h12 = 12
	}
	switch tok {
	case "YYYY":
		return fmt.Sprintf("%04d", t.Year())
	case "YY":
		return fmt.Sprintf("%02d", t.Year()%100)
	case "MMMM":
		return t.Month().String()
	case "MMM":
		return t.Month().String()[:3]
	case "MM":
		return fmt.Sprintf("%02d", int(t.Month()))
	case "M":
		return strconv.Itoa(int(t.Month()))
	case "DD":
		return fmt.Sprintf("%02d", t.Day())
	case "D":
		return strconv.Itoa(t.Day())
	case "dddd":
		return t.Weekday().String()
	case "ddd":
		return t.Weekday().String()[:3]
	case "HH":
		return fmt.Sprintf("%02d", t.Hour())
	case "H":
		return strconv.Itoa(t.Hour())
	case "hh":
		return fmt.Sprintf("%02d", h12)
	case "h":
		return strconv.Itoa(h12)
	case "mm":
		return fmt.Sprintf("%02d", t.Minute())
	case "m":
		return strconv.Itoa(t.Minute())
	case "ss":
		return fmt.Sprintf("%02d", t.Second())
	case "s":
		return strconv.Itoa(t.Second())
	case "SSS":
		return fmt.Sprintf("%03d", t.Nanosecond()/1e6)
	case "A":
		if t.Hour() < 12 {
			return "AM"
		}
		return "PM"
	case "a":
		if t.Hour() < 12 {
			return "am"
		}
		return "pm"
	case "Z":
		if t.Location() == time.UTC {
			return "Z"
		}
		return t.Format("-07:00")
	}
	return tok
}

// goDate formats a date with a Go layout ("02 Jan 2006"): the first date
// function of the email templates, kept for them.
func goDate(layout string, v any) string {
	t, ok := toTime(v)
	if !ok {
		if s, isText := v.(string); isText {
			return s
		}
		return ""
	}
	return t.Format(layout)
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

// timeAgo describes a date relative to now ("3 days ago", "in 2 hours").
func timeAgo(v any) string {
	t, ok := toTime(v)
	if !ok {
		return ""
	}
	d := Now().Sub(t)
	future := d < 0
	if future {
		d = -d
	}
	var text string
	switch s := d.Seconds(); {
	case s < 45:
		return "just now"
	case s < 90:
		text = "1 minute"
	case d < 45*time.Minute:
		text = plural(int(math.Round(d.Minutes())), "minute")
	case d < 90*time.Minute:
		text = "1 hour"
	case d < 22*time.Hour:
		text = plural(int(math.Round(d.Hours())), "hour")
	case d < 36*time.Hour:
		text = "1 day"
	case d < 26*24*time.Hour:
		text = plural(int(math.Round(d.Hours()/24)), "day")
	case d < 320*24*time.Hour:
		text = plural(max(1, int(math.Round(d.Hours()/24/30.44))), "month")
	default:
		text = plural(max(1, int(math.Round(d.Hours()/24/365.25))), "year")
	}
	if future {
		return "in " + text
	}
	return text + " ago"
}

// duration writes a number of seconds as "2h 5m" ("1d 3h", "45s").
func duration(v any) string {
	total, ok := toFloat(v)
	if !ok {
		return ""
	}
	sign := ""
	if total < 0 {
		sign, total = "-", -total
	}
	s := int64(math.Round(total))
	if s == 0 {
		return "0s"
	}
	units := []struct {
		size int64
		name string
	}{{86400, "d"}, {3600, "h"}, {60, "m"}, {1, "s"}}
	var parts []string
	for _, u := range units {
		if s >= u.size {
			parts = append(parts, strconv.FormatInt(s/u.size, 10)+u.name)
			s %= u.size
		}
		if len(parts) == 2 {
			break
		}
	}
	return sign + strings.Join(parts, " ")
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// addMonths adds months, staying in the month (Jan 31 + 1 month: Feb 28).
func addMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}

// dateFunc wraps a function of a date: no date, no result.
func dateFunc(fn func(time.Time) any) func(any) any {
	return func(v any) any {
		t, ok := toTime(v)
		if !ok {
			return ""
		}
		return fn(t)
	}
}

// addFunc wraps "add n units to a date".
func addFunc(fn func(time.Time, float64) time.Time) func(any, any) any {
	return func(n, v any) any {
		t, ok := toTime(v)
		if !ok {
			return ""
		}
		return dateOut(fn(t, num(n)))
	}
}

// diffFunc returns the whole units from a to b (b - a).
func diffFunc(unit time.Duration) func(any, any) any {
	return func(a, b any) any {
		ta, ok1 := toTime(a)
		tb, ok2 := toTime(b)
		if !ok1 || !ok2 {
			return ""
		}
		return int(tb.Sub(ta) / unit)
	}
}

func tz(zone string, v any) any {
	t, ok := toTime(v)
	if !ok {
		return ""
	}
	loc, err := time.LoadLocation(strings.TrimSpace(zone))
	if err != nil {
		return dateOut(t)
	}
	return dateOut(t.In(loc))
}

// goLayouts turns the formatDate tokens into a Go layout for parsing: the
// numbers accept one or two digits (D/M/YYYY reads 5/3/2026 and 05/03/2026).
var parseTokens = map[string]string{
	"YYYY": "2006", "YY": "06", "MMMM": "January", "MMM": "Jan", "MM": "1", "M": "1", "DD": "2", "D": "2",
	"dddd": "Monday", "ddd": "Mon", "HH": "15", "H": "15", "hh": "3", "h": "3", "mm": "4", "m": "4",
	"ss": "5", "s": "5", "SSS": "000", "A": "PM", "a": "pm", "Z": "Z07:00",
}

func goLayout(layout string) string {
	var b strings.Builder
	for i := 0; i < len(layout); {
		if layout[i] == '[' {
			if end := strings.IndexByte(layout[i:], ']'); end > 0 {
				b.WriteString(layout[i+1 : i+end])
				i += end + 1
				continue
			}
		}
		matched := false
		for _, tok := range dateTokens {
			if strings.HasPrefix(layout[i:], tok) {
				b.WriteString(parseTokens[tok])
				i += len(tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(layout[i])
			i++
		}
	}
	return b.String()
}

// parseDate reads a date written in a format (DD/MM/YYYY HH:mm...), as a
// time of a zone when given (UTC otherwise), and returns it in UTC: the
// format of the date fields.
func parseDate(layout, v any, zone ...any) (any, error) {
	text := strings.TrimSpace(toStr(v))
	if text == "" {
		return "", nil
	}
	loc := time.UTC
	if len(zone) > 0 {
		if z := strings.TrimSpace(toStr(zone[0])); z != "" {
			l, err := time.LoadLocation(z)
			if err != nil {
				return "", fmt.Errorf("unknown time zone %s", z)
			}
			loc = l
		}
	}
	f := strings.TrimSpace(toStr(layout))
	if named, ok := namedFormats[f]; ok {
		f = named
	}
	t, err := time.ParseInLocation(goLayout(f), text, loc)
	if err != nil {
		return "", fmt.Errorf("%q doesn't match the format %s", text, f)
	}
	return dateOut(t.UTC()), nil
}

// fromUnix returns the date of a Unix time in seconds (or milliseconds).
func fromUnix(v any) any {
	n, ok := toFloat(v)
	if !ok {
		return ""
	}
	if math.Abs(n) > 1e11 {
		return dateOut(time.UnixMilli(int64(n)).UTC())
	}
	return dateOut(time.Unix(int64(n), int64((n-math.Trunc(n))*1e9)).UTC())
}
