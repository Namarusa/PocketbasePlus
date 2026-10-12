// Package formula is the function library of the templates: the email
// templates, the flows ({{upper .trigger.record.name}}) and the formulas of
// the pages ({{formatDate "DD/MM/YYYY" record.created}}). Every function is
// listed in the catalog (name, syntax, description, example) shown by the
// dashboard pickers.
//
// The functions never fail on a value of the wrong type: they treat it as
// empty (no date, no number) and return "". Options come first and the value
// last, so they also work in pipelines ({{.record.price | round 2}}).
package formula

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"text/template"
	"time"
)

// Func is a function of the catalog.
type Func struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Syntax      string `json:"syntax"`
	Description string `json:"description"`
	Example     string `json:"example"`
	Result      string `json:"result"`
	Snippet     string `json:"snippet,omitempty"` // inserted by the pickers (the example's action by default)

	fn any // nil: a template builtin or keyword (documented only)
}

// Categories in the order of the pickers.
var Categories = []string{"Text", "Number", "Date", "List", "Logic", "Other"}

var library = []Func{
	// --- Text
	{Name: "upper", Category: "Text", Syntax: "upper text", Description: "Upper case.", Example: `{{upper "Ada Lovelace"}}`, Result: "ADA LOVELACE", fn: func(v any) string { return strings.ToUpper(toStr(v)) }},
	{Name: "lower", Category: "Text", Syntax: "lower text", Description: "Lower case.", Example: `{{lower "Ada Lovelace"}}`, Result: "ada lovelace", fn: func(v any) string { return strings.ToLower(toStr(v)) }},
	{Name: "capitalize", Category: "Text", Syntax: "capitalize text", Description: "Upper-cases the first letter.", Example: `{{capitalize "élodie martin"}}`, Result: "Élodie martin", fn: capitalize},
	{Name: "title", Category: "Text", Syntax: "title text", Description: "Same as capitalize.", Example: `{{title "ada"}}`, Result: "Ada", fn: capitalize},
	{Name: "titleCase", Category: "Text", Syntax: "titleCase text", Description: "Upper-cases the first letter of every word.", Example: `{{titleCase "jean-pierre de la tour"}}`, Result: "Jean-Pierre De La Tour", fn: titleCase},
	{Name: "trim", Category: "Text", Syntax: "trim text", Description: "Removes the spaces at the start and the end.", Example: `{{trim "  hello  "}}`, Result: "hello", fn: func(v any) string { return strings.TrimSpace(toStr(v)) }},
	{Name: "truncate", Category: "Text", Syntax: "truncate length text", Description: "Shortens a text to a number of letters, with … when it's cut.", Example: `{{truncate 12 "The quick brown fox jumps"}}`, Result: "The quick br…", fn: truncate},
	{Name: "replace", Category: "Text", Syntax: "replace old new text", Description: "Replaces every occurrence of a text.", Example: `{{replace "-" " " "2026-10-11"}}`, Result: "2026 10 11", fn: func(old, repl, v any) string { return strings.ReplaceAll(toStr(v), toStr(old), toStr(repl)) }},
	{Name: "contains", Category: "Text", Syntax: "contains part text-or-list", Description: "Whether a text contains another (or a list an item).", Example: `{{contains "fox" "The quick brown fox"}}`, Result: "true", fn: contains},
	{Name: "startsWith", Category: "Text", Syntax: "startsWith prefix text", Description: "Whether a text starts with another.", Example: `{{startsWith "INV-" "INV-2026-001"}}`, Result: "true", fn: func(p, v any) bool { return strings.HasPrefix(toStr(v), toStr(p)) }},
	{Name: "endsWith", Category: "Text", Syntax: "endsWith suffix text", Description: "Whether a text ends with another.", Example: `{{endsWith ".pdf" "report.pdf"}}`, Result: "true", fn: func(s, v any) bool { return strings.HasSuffix(toStr(v), toStr(s)) }},
	{Name: "split", Category: "Text", Syntax: "split separator text", Description: "Cuts a text into a list (the parts are trimmed).", Example: `{{split "," "red, green, blue"}}`, Result: "red, green, blue", fn: split},
	{Name: "join", Category: "Text", Syntax: "join separator list", Description: "Joins the items of a list with a separator.", Example: `{{join " / " (list "a" "b" "c")}}`, Result: "a / b / c", fn: join},
	{Name: "concat", Category: "Text", Syntax: "concat value1 value2 ...", Description: "Joins values into one text.", Example: `{{concat "INV-" 2026 "-" 7}}`, Result: "INV-2026-7", fn: concat},
	{Name: "substr", Category: "Text", Syntax: "substr start end text", Description: "The letters from start to end (excluded); -1 as end goes to the end.", Example: `{{substr 0 3 "Pikachu"}}`, Result: "Pik", fn: substr},
	{Name: "padLeft", Category: "Text", Syntax: "padLeft length fill text", Description: "Fills a text on the left up to a length.", Example: `{{padLeft 5 "0" 42}}`, Result: "00042", fn: func(n, ch, v any) string { return pad(n, ch, v, true) }},
	{Name: "padRight", Category: "Text", Syntax: "padRight length fill text", Description: "Fills a text on the right up to a length.", Example: `{{padRight 5 "." "ab"}}`, Result: "ab...", fn: func(n, ch, v any) string { return pad(n, ch, v, false) }},
	{Name: "repeat", Category: "Text", Syntax: "repeat count text", Description: "Repeats a text.", Example: `{{repeat 3 "★"}}`, Result: "★★★", fn: repeat},
	{Name: "slug", Category: "Text", Syntax: "slug text", Description: "A URL-friendly version of a text (no accents, dashes).", Example: `{{slug "Crème Brûlée, 2 pers."}}`, Result: "creme-brulee-2-pers", fn: slug},
	{Name: "initials", Category: "Text", Syntax: "initials text", Description: "The first letter of every word.", Example: `{{initials "Ada Lovelace"}}`, Result: "AL", fn: initials},
	{Name: "stripHtml", Category: "Text", Syntax: "stripHtml html", Description: "The text of HTML, e.g. of a rich text field.", Example: `{{stripHtml "<p>Hello <b>world</b></p>"}}`, Result: "Hello world", fn: stripHTML},
	{Name: "wordCount", Category: "Text", Syntax: "wordCount text", Description: "The number of words.", Example: `{{wordCount "The quick brown fox"}}`, Result: "4", fn: func(v any) int { return len(strings.Fields(toStr(v))) }},
	{Name: "length", Category: "Text", Syntax: "length text-or-list", Description: "The number of letters of a text, or of items of a list.", Example: `{{length "Pikachu"}}`, Result: "7", fn: length},
	{Name: "urlEncode", Category: "Text", Syntax: "urlEncode text", Description: "Escapes a text for a URL query.", Example: `{{urlEncode "a&b c"}}`, Result: "a%26b+c", fn: urlEncode},

	// --- Number
	{Name: "round", Category: "Number", Syntax: "round [decimals] number", Description: "Rounds a number (to a whole number, or to decimals).", Example: `{{round 2 3.14159}}`, Result: "3.14", fn: round},
	{Name: "floor", Category: "Number", Syntax: "floor number", Description: "Rounds down.", Example: `{{floor 4.8}}`, Result: "4", fn: func(v any) Number { return Number(math.Floor(num(v))) }},
	{Name: "ceil", Category: "Number", Syntax: "ceil number", Description: "Rounds up.", Example: `{{ceil 4.2}}`, Result: "5", fn: func(v any) Number { return Number(math.Ceil(num(v))) }},
	{Name: "abs", Category: "Number", Syntax: "abs number", Description: "The number without its sign.", Example: `{{abs -7}}`, Result: "7", fn: func(v any) Number { return Number(math.Abs(num(v))) }},
	{Name: "add", Category: "Number", Syntax: "add a b ...", Description: "The sum of numbers (or of a list).", Example: `{{add 10 5.5 2}}`, Result: "17.5", fn: sum},
	{Name: "subtract", Category: "Number", Syntax: "subtract a b", Description: "a minus b.", Example: `{{subtract 10 3}}`, Result: "7", fn: subtract},
	{Name: "multiply", Category: "Number", Syntax: "multiply a b ...", Description: "The product of numbers.", Example: `{{multiply 19.9 3}}`, Result: "59.7", fn: multiply},
	{Name: "divide", Category: "Number", Syntax: "divide a b", Description: "a divided by b.", Example: `{{divide 10 4}}`, Result: "2.5", fn: divide},
	{Name: "modulo", Category: "Number", Syntax: "modulo a b", Description: "The remainder of a divided by b.", Example: `{{modulo 10 3}}`, Result: "1", fn: modulo},
	{Name: "power", Category: "Number", Syntax: "power a b", Description: "a to the power of b.", Example: `{{power 2 10}}`, Result: "1024", fn: func(a, b any) Number { return Number(math.Pow(num(a), num(b))) }},
	{Name: "sqrt", Category: "Number", Syntax: "sqrt number", Description: "The square root.", Example: `{{sqrt 16}}`, Result: "4", fn: func(v any) Number { return Number(math.Sqrt(math.Max(0, num(v)))) }},
	{Name: "min", Category: "Number", Syntax: "min a b ... (or a list)", Description: "The smallest value.", Example: `{{min 4 2 8}}`, Result: "2", fn: func(values ...any) any { return extreme(values, true) }},
	{Name: "max", Category: "Number", Syntax: "max a b ... (or a list)", Description: "The largest value.", Example: `{{max 4 2 8}}`, Result: "8", fn: func(values ...any) any { return extreme(values, false) }},
	{Name: "percent", Category: "Number", Syntax: "percent part total", Description: "part as a percentage of total.", Example: `{{percent 15 60}}`, Result: "25", fn: percent},
	{Name: "formatNumber", Category: "Number", Syntax: "formatNumber [decimals] number", Description: "A number with thousands separators.", Example: `{{formatNumber 2 1234567.891}}`, Result: "1,234,567.89", fn: formatNumber},
	{Name: "currency", Category: "Number", Syntax: "currency code amount", Description: "An amount with its currency (EUR, USD, GBP...).", Example: `{{currency "EUR" 1234.5}}`, Result: "€1,234.50", fn: currency},
	{Name: "fileSize", Category: "Number", Syntax: "fileSize bytes", Description: "A size in bytes as KB, MB, GB.", Example: `{{fileSize 1572864}}`, Result: "1.5 MB", fn: fileSize},
	{Name: "toNumber", Category: "Number", Syntax: "toNumber value", Description: "The number of a text (0 when it isn't one).", Example: `{{add (toNumber "12.5") 1}}`, Result: "13.5", fn: toNumber},

	// --- Date
	{Name: "now", Category: "Date", Syntax: "now", Description: "The current date and time (UTC).", Example: `{{formatDate "YYYY-MM-DD" now}}`, Result: "2026-10-11", Snippet: "{{now}}", fn: func() any { return dateOut(Now()) }},
	{Name: "today", Category: "Date", Syntax: "today", Description: "Today at midnight (UTC).", Example: `{{today}}`, Result: "2026-10-11 00:00:00.000Z", fn: func() any { return dateOut(startOfDay(Now())) }},
	{Name: "formatDate", Category: "Date", Syntax: "formatDate format date", Description: "Formats a date: YYYY YY MMMM MMM MM M DD D dddd ddd HH H hh h mm ss SSS A Z, [text] kept as is; or date, datetime, time, iso, storage (the format of the date fields, in UTC).", Example: `{{formatDate "dddd DD/MM/YYYY HH:mm" "2026-10-01 09:30:00.000Z"}}`, Result: "Thursday 01/10/2026 09:30", fn: formatDate},
	{Name: "timeAgo", Category: "Date", Syntax: "timeAgo date", Description: "A date relative to now: 3 days ago, in 2 hours.", Example: `{{timeAgo "2026-10-08 12:00:00.000Z"}}`, Result: "3 days ago", fn: timeAgo},
	{Name: "toDate", Category: "Date", Syntax: "toDate text", Description: "The date of a text in a standard format (2026-10-11, 2026-10-11 09:30, ISO 8601 with its offset), in the format of the date fields.", Example: `{{toDate "2026-10-01T11:30:00+02:00"}}`, Result: "2026-10-01 09:30:00.000Z", fn: dateFunc(func(t time.Time) any { return dateOut(t.UTC()) })},
	{Name: "parseDate", Category: "Date", Syntax: "parseDate format text [zone]", Description: "Reads a date written in a format (the formatDate tokens), as a time of a zone when given, in the format of the date fields (UTC). Use it before storing dates typed like 25/12/2026.", Example: `{{parseDate "DD/MM/YYYY HH:mm" "25/12/2026 14:30" "Europe/Paris"}}`, Result: "2026-12-25 13:30:00.000Z", fn: parseDate},
	{Name: "fromUnix", Category: "Date", Syntax: "fromUnix seconds", Description: "The date of a Unix time (seconds, or milliseconds), in the format of the date fields.", Example: `{{fromUnix 1791676800}}`, Result: "2026-10-11 00:00:00.000Z", fn: fromUnix},
	{Name: "toUTC", Category: "Date", Syntax: "toUTC date", Description: "A date (e.g. of a zone, see tz) in UTC, in the format of the date fields.", Example: `{{toUTC (tz "Europe/Paris" "2026-10-01 09:30:00.000Z")}}`, Result: "2026-10-01 09:30:00.000Z", fn: dateFunc(func(t time.Time) any { return dateOut(t.UTC()) })},
	{Name: "addMinutes", Category: "Date", Syntax: "addMinutes count date", Description: "Adds minutes (negative: removes).", Example: `{{addMinutes 90 "2026-10-01 09:30:00.000Z"}}`, Result: "2026-10-01 11:00:00.000Z", fn: addFunc(func(t time.Time, n float64) time.Time { return t.Add(time.Duration(n * float64(time.Minute))) })},
	{Name: "addHours", Category: "Date", Syntax: "addHours count date", Description: "Adds hours.", Example: `{{addHours 2 "2026-10-01 09:30:00.000Z"}}`, Result: "2026-10-01 11:30:00.000Z", fn: addFunc(func(t time.Time, n float64) time.Time { return t.Add(time.Duration(n * float64(time.Hour))) })},
	{Name: "addDays", Category: "Date", Syntax: "addDays count date", Description: "Adds days.", Example: `{{addDays 7 "2026-10-01 09:30:00.000Z"}}`, Result: "2026-10-08 09:30:00.000Z", fn: addFunc(func(t time.Time, n float64) time.Time { return t.AddDate(0, 0, int(n)) })},
	{Name: "addMonths", Category: "Date", Syntax: "addMonths count date", Description: "Adds months (Jan 31 + 1 month: Feb 28).", Example: `{{addMonths 1 "2026-01-31"}}`, Result: "2026-02-28 00:00:00.000Z", fn: addFunc(func(t time.Time, n float64) time.Time { return addMonths(t, int(n)) })},
	{Name: "addYears", Category: "Date", Syntax: "addYears count date", Description: "Adds years.", Example: `{{addYears 1 "2026-10-01"}}`, Result: "2027-10-01 00:00:00.000Z", fn: addFunc(func(t time.Time, n float64) time.Time { return addMonths(t, int(n)*12) })},
	{Name: "diffDays", Category: "Date", Syntax: "diffDays from to", Description: "The whole days from a date to another.", Example: `{{diffDays "2026-10-01" "2026-10-11"}}`, Result: "10", fn: diffFunc(24 * time.Hour)},
	{Name: "diffHours", Category: "Date", Syntax: "diffHours from to", Description: "The whole hours from a date to another.", Example: `{{diffHours "2026-10-01 08:00" "2026-10-01 17:30"}}`, Result: "9", fn: diffFunc(time.Hour)},
	{Name: "diffMinutes", Category: "Date", Syntax: "diffMinutes from to", Description: "The whole minutes from a date to another.", Example: `{{diffMinutes "2026-10-01 08:00" "2026-10-01 09:45"}}`, Result: "105", fn: diffFunc(time.Minute)},
	{Name: "duration", Category: "Date", Syntax: "duration seconds", Description: "A number of seconds as 2h 5m.", Example: `{{duration 7500}}`, Result: "2h 5m", fn: duration},
	{Name: "startOfDay", Category: "Date", Syntax: "startOfDay date", Description: "The date at midnight.", Example: `{{startOfDay "2026-10-01 09:30:00.000Z"}}`, Result: "2026-10-01 00:00:00.000Z", fn: dateFunc(func(t time.Time) any { return dateOut(startOfDay(t)) })},
	{Name: "endOfDay", Category: "Date", Syntax: "endOfDay date", Description: "The last second of the day.", Example: `{{endOfDay "2026-10-01 09:30:00.000Z"}}`, Result: "2026-10-01 23:59:59.000Z", fn: dateFunc(func(t time.Time) any { return dateOut(startOfDay(t).AddDate(0, 0, 1).Add(-time.Second)) })},
	{Name: "startOfWeek", Category: "Date", Syntax: "startOfWeek date", Description: "The Monday of the week, at midnight.", Example: `{{startOfWeek "2026-10-01"}}`, Result: "2026-09-28 00:00:00.000Z", fn: dateFunc(func(t time.Time) any {
		return dateOut(startOfDay(t).AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)))
	})},
	{Name: "startOfMonth", Category: "Date", Syntax: "startOfMonth date", Description: "The first day of the month, at midnight.", Example: `{{startOfMonth "2026-10-11"}}`, Result: "2026-10-01 00:00:00.000Z", fn: dateFunc(func(t time.Time) any {
		return dateOut(time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()))
	})},
	{Name: "endOfMonth", Category: "Date", Syntax: "endOfMonth date", Description: "The last second of the month.", Example: `{{endOfMonth "2026-02-10"}}`, Result: "2026-02-28 23:59:59.000Z", fn: dateFunc(func(t time.Time) any {
		return dateOut(time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location()).Add(-time.Second))
	})},
	{Name: "startOfYear", Category: "Date", Syntax: "startOfYear date", Description: "January 1st of the year, at midnight.", Example: `{{startOfYear "2026-10-11"}}`, Result: "2026-01-01 00:00:00.000Z", fn: dateFunc(func(t time.Time) any {
		return dateOut(time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location()))
	})},
	{Name: "year", Category: "Date", Syntax: "year date", Description: "The year.", Example: `{{year "2026-10-11"}}`, Result: "2026", fn: dateFunc(func(t time.Time) any { return t.Year() })},
	{Name: "month", Category: "Date", Syntax: "month date", Description: "The month number (1 to 12).", Example: `{{month "2026-10-11"}}`, Result: "10", fn: dateFunc(func(t time.Time) any { return int(t.Month()) })},
	{Name: "monthName", Category: "Date", Syntax: "monthName date", Description: "The month name.", Example: `{{monthName "2026-10-11"}}`, Result: "October", fn: dateFunc(func(t time.Time) any { return t.Month().String() })},
	{Name: "day", Category: "Date", Syntax: "day date", Description: "The day of the month.", Example: `{{day "2026-10-11"}}`, Result: "11", fn: dateFunc(func(t time.Time) any { return t.Day() })},
	{Name: "weekday", Category: "Date", Syntax: "weekday date", Description: "The day name.", Example: `{{weekday "2026-10-11"}}`, Result: "Sunday", fn: dateFunc(func(t time.Time) any { return t.Weekday().String() })},
	{Name: "hour", Category: "Date", Syntax: "hour date", Description: "The hour (0 to 23).", Example: `{{hour "2026-10-01 09:30:00.000Z"}}`, Result: "9", fn: dateFunc(func(t time.Time) any { return t.Hour() })},
	{Name: "minute", Category: "Date", Syntax: "minute date", Description: "The minutes (0 to 59).", Example: `{{minute "2026-10-01 09:30:00.000Z"}}`, Result: "30", fn: dateFunc(func(t time.Time) any { return t.Minute() })},
	{Name: "isPast", Category: "Date", Syntax: "isPast date", Description: "Whether a date is before now.", Example: `{{isPast "2026-10-01"}}`, Result: "true", fn: func(v any) bool { t, ok := toTime(v); return ok && t.Before(Now()) }},
	{Name: "isFuture", Category: "Date", Syntax: "isFuture date", Description: "Whether a date is after now.", Example: `{{isFuture "2026-12-25"}}`, Result: "true", fn: func(v any) bool { t, ok := toTime(v); return ok && t.After(Now()) }},
	{Name: "isToday", Category: "Date", Syntax: "isToday date", Description: "Whether a date is today (UTC, or in its zone).", Example: `{{isToday "2026-10-11 08:00:00.000Z"}}`, Result: "true", fn: func(v any) bool {
		t, ok := toTime(v)
		return ok && startOfDay(t).Equal(startOfDay(Now().In(t.Location())))
	}},
	{Name: "isBefore", Category: "Date", Syntax: "isBefore a b", Description: "Whether date a is before date b.", Example: `{{isBefore "2026-10-01" "2026-10-11"}}`, Result: "true", fn: func(a, b any) bool { ta, ok1 := toTime(a); tb, ok2 := toTime(b); return ok1 && ok2 && ta.Before(tb) }},
	{Name: "isAfter", Category: "Date", Syntax: "isAfter a b", Description: "Whether date a is after date b.", Example: `{{isAfter "2026-10-01" "2026-10-11"}}`, Result: "false", fn: func(a, b any) bool { ta, ok1 := toTime(a); tb, ok2 := toTime(b); return ok1 && ok2 && ta.After(tb) }},
	{Name: "tz", Category: "Date", Syntax: "tz zone date", Description: "The date in a time zone (Europe/Paris, America/New_York...), to format it. Stored as is in a date field, it's converted back to UTC.", Example: `{{formatDate "HH:mm" (tz "Europe/Paris" "2026-10-01 09:30:00.000Z")}}`, Result: "11:30", fn: tzFunc},
	{Name: "unix", Category: "Date", Syntax: "unix date", Description: "The seconds since 1970 (Unix time).", Example: `{{unix "2026-10-11"}}`, Result: "1791676800", fn: dateFunc(func(t time.Time) any { return t.Unix() })},
	{Name: "date", Category: "Date", Syntax: "date go-layout date", Description: "Formats a date with a Go layout (02 Jan 2006 15:04); formatDate is simpler.", Example: `{{date "02 Jan 2006" "2026-10-01"}}`, Result: "01 Oct 2026", fn: goDate},

	// --- List
	{Name: "list", Category: "List", Syntax: "list value1 value2 ...", Description: "Makes a list; with one value, that value as a list.", Example: `{{list "a" "b" "c"}}`, Result: "a, b, c", fn: list},
	{Name: "count", Category: "List", Syntax: "count list", Description: "The number of items.", Example: `{{count (list "a" "b" "c")}}`, Result: "3", fn: func(v any) int { return len(toList(v)) }},
	{Name: "first", Category: "List", Syntax: "first list", Description: "The first item.", Example: `{{first (list "a" "b" "c")}}`, Result: "a", fn: first},
	{Name: "last", Category: "List", Syntax: "last list", Description: "The last item.", Example: `{{last (list "a" "b" "c")}}`, Result: "c", fn: last},
	{Name: "nth", Category: "List", Syntax: "nth position list", Description: "The item at a position (0 is the first, -1 the last).", Example: `{{nth 1 (list "a" "b" "c")}}`, Result: "b", fn: nth},
	{Name: "take", Category: "List", Syntax: "take count list", Description: "The first items.", Example: `{{take 2 (list "a" "b" "c")}}`, Result: "a, b", fn: take},
	{Name: "sum", Category: "List", Syntax: "sum list", Description: "The total of the numbers.", Example: `{{sum (list 10 20 12.5)}}`, Result: "42.5", fn: sum},
	{Name: "avg", Category: "List", Syntax: "avg list", Description: "The average of the numbers.", Example: `{{avg (list 10 20 30)}}`, Result: "20", fn: avg},
	{Name: "sort", Category: "List", Syntax: "sort list", Description: "Sorts the items (numbers, dates or texts).", Example: `{{sort (list 3 1 2)}}`, Result: "1, 2, 3", fn: sortList},
	{Name: "sortBy", Category: "List", Syntax: "sortBy field list", Description: "Sorts objects by a field (-field: the largest first).", Example: `{{pluck "name" (sortBy "-price" .items)}}`, Result: "Lamp, Pen", fn: sortBy},
	{Name: "reverse", Category: "List", Syntax: "reverse list", Description: "The items in the opposite order.", Example: `{{reverse (list 1 2 3)}}`, Result: "3, 2, 1", fn: reverse},
	{Name: "unique", Category: "List", Syntax: "unique list", Description: "The items without duplicates.", Example: `{{unique (list "a" "b" "a")}}`, Result: "a, b", fn: unique},
	{Name: "pluck", Category: "List", Syntax: "pluck field list", Description: "A field of every object of a list.", Example: `{{pluck "name" .items}}`, Result: "Pen, Lamp", fn: pluck},
	{Name: "where", Category: "List", Syntax: "where field value list", Description: "The objects whose field equals a value.", Example: `{{pluck "name" (where "stock" 0 .items)}}`, Result: "Lamp", fn: where},
	{Name: "range", Category: "List", Syntax: "range list ... end", Description: "Repeats for every item ({{.}} is the item; {{else}} when the list is empty).", Example: `{{range .items}}[{{.name}}]{{end}}`, Result: "[Pen][Lamp]", Snippet: "{{range LIST}}{{.}}{{end}}"},

	// --- Logic
	{Name: "if", Category: "Logic", Syntax: "if condition ... else ... end", Description: "Shows a part when a condition is true (false, 0, empty texts and lists are false).", Example: `{{if gt 15 10}}big{{else}}small{{end}}`, Result: "big", Snippet: "{{if CONDITION}}...{{else}}...{{end}}"},
	{Name: "ifElse", Category: "Logic", Syntax: "ifElse condition yes no", Description: "One value or the other, depending on a condition.", Example: `{{ifElse (gt 3 2) "yes" "no"}}`, Result: "yes", fn: ifElse},
	{Name: "eq", Category: "Logic", Syntax: "eq a b [c ...]", Description: "Whether a equals b (or any of the next values).", Example: `{{eq "paid" "paid"}}`, Result: "true", fn: eq},
	{Name: "ne", Category: "Logic", Syntax: "ne a b", Description: "Whether a differs from b.", Example: `{{ne 1 2}}`, Result: "true", fn: ne},
	{Name: "gt", Category: "Logic", Syntax: "gt a b", Description: "Whether a is greater than b (numbers, dates, texts).", Example: `{{gt 10 9.5}}`, Result: "true", fn: gt},
	{Name: "ge", Category: "Logic", Syntax: "ge a b", Description: "Whether a is greater than or equal to b.", Example: `{{ge 10 10}}`, Result: "true", fn: ge},
	{Name: "lt", Category: "Logic", Syntax: "lt a b", Description: "Whether a is less than b.", Example: `{{lt "2026-01-01" "2026-10-11"}}`, Result: "true", fn: lt},
	{Name: "le", Category: "Logic", Syntax: "le a b", Description: "Whether a is less than or equal to b.", Example: `{{le 3 2}}`, Result: "false", fn: le},
	{Name: "and", Category: "Logic", Syntax: "and a b ...", Description: "Whether all the values are true.", Example: `{{and true (gt 2 1)}}`, Result: "true"},
	{Name: "or", Category: "Logic", Syntax: "or a b ...", Description: "The first true value.", Example: `{{or "" "fallback"}}`, Result: "fallback"},
	{Name: "not", Category: "Logic", Syntax: "not value", Description: "The opposite of a condition.", Example: `{{not false}}`, Result: "true"},
	{Name: "isEmpty", Category: "Logic", Syntax: "isEmpty value", Description: "Whether a value is empty (no value, empty text or list; 0 isn't empty).", Example: `{{isEmpty ""}}`, Result: "true", fn: isEmpty},
	{Name: "default", Category: "Logic", Syntax: "default fallback value", Description: "The value, or the fallback when it's empty.", Example: `{{default "n/a" ""}}`, Result: "n/a", fn: defaultValue},
	{Name: "coalesce", Category: "Logic", Syntax: "coalesce a b ...", Description: "The first value that isn't empty.", Example: `{{coalesce "" "" "third"}}`, Result: "third", fn: coalesce},

	// --- Other
	{Name: "get", Category: "Other", Syntax: "get path object", Description: "The value at a path of an object or list (items.0.name).", Example: `{{get "0.name" .items}}`, Result: "Pen", fn: func(path, v any) any { return Lookup(v, toStr(path)) }},
	{Name: "json", Category: "Other", Syntax: "json value", Description: "A value as JSON.", Example: `{{json (list 1 "a")}}`, Result: `[1,"a"]`, fn: toJSON},
	{Name: "toString", Category: "Other", Syntax: "toString value", Description: "A value as text.", Example: `{{toString 42}}`, Result: "42", fn: toStr},
	{Name: "printf", Category: "Other", Syntax: "printf format values...", Description: "Formats values (Go fmt: %s text, %d whole number, %.2f decimals, %05d).", Example: `{{printf "%05d" 42}}`, Result: "00042"},
}

// Funcs returns the functions for text/template and html/template.
func Funcs() template.FuncMap {
	out := template.FuncMap{}
	for _, f := range library {
		if f.fn != nil {
			out[f.Name] = f.fn
		}
	}
	return out
}

// Catalog returns the documented functions.
func Catalog() []Func {
	return append([]Func{}, library...)
}

func toJSON(v any) string {
	b, err := json.Marshal(plain(v))
	if err != nil {
		return ""
	}
	return string(b)
}

func tzFunc(zone, v any) (any, error) {
	z := strings.TrimSpace(toStr(zone))
	if _, err := time.LoadLocation(z); err != nil || z == "" {
		return "", errors.New("unknown time zone " + z)
	}
	return tz(z, v), nil
}

// MaxOutput is the largest text a formula renders.
const MaxOutput = 256 << 10

// ErrTooLong is returned for formulas whose result exceeds MaxOutput.
var ErrTooLong = errors.New("the result is too long")

type limitedWriter struct {
	buf bytes.Buffer
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > MaxOutput {
		return 0, ErrTooLong
	}
	return w.buf.Write(p)
}

// Parse parses a formula ("Hello {{upper record.name}}"). Roots are the data
// names that can be written without the leading dot (record, auth, query).
func Parse(src string, roots ...string) (*template.Template, error) {
	return template.New("formula").Funcs(Funcs()).Option("missingkey=zero").Parse(Normalize(src, roots...))
}

// Execute runs a parsed formula; missing values are empty.
func Execute(t *template.Template, data any) (string, error) {
	var w limitedWriter
	if err := t.Execute(&w, data); err != nil {
		return "", cleanError(err)
	}
	return strings.ReplaceAll(w.buf.String(), "<no value>", ""), nil
}

// Render parses and runs a formula.
func Render(src string, data any, roots ...string) (string, error) {
	t, err := Parse(src, roots...)
	if err != nil {
		return "", cleanError(err)
	}
	return Execute(t, data)
}

// cleanError drops the template name and position noise of the errors:
// "template: formula:1:9: executing "formula" at <divide 1 0>: error calling
// divide: division by zero" is "divide: division by zero".
func cleanError(err error) error {
	if errors.Is(err, ErrTooLong) {
		return ErrTooLong
	}
	msg := err.Error()
	if i := strings.Index(msg, "error calling "); i >= 0 {
		return errors.New(msg[i+len("error calling "):])
	}
	msg = strings.TrimPrefix(msg, "template: ")
	msg = strings.TrimPrefix(msg, "formula:")
	return errors.New(msg)
}

// Normalize prefixes the roots used without their dot inside the actions
// ({{upper record.name}}: {{upper $.record.name}}, also right inside a
// range), outside the quoted texts.
func Normalize(src string, roots ...string) string {
	return RewriteNames(src, "$.", roots...)
}

// RewriteNames writes the given names used alone inside the actions (not
// after a dot, outside the quoted texts) with a prefix: the variables of the
// API calls, {{id}} written {{.vars.id}}.
func RewriteNames(src, prefix string, roots ...string) string {
	if len(roots) == 0 || !strings.Contains(src, "{{") {
		return src
	}
	isRoot := map[string]bool{}
	for _, r := range roots {
		isRoot[r] = true
	}
	var b strings.Builder
	for {
		start := strings.Index(src, "{{")
		if start < 0 {
			b.WriteString(src)
			return b.String()
		}
		b.WriteString(src[:start+2])
		src = src[start+2:]
		end := actionEnd(src)
		if end < 0 {
			b.WriteString(src)
			return b.String()
		}
		b.WriteString(prefixRoots(src[:end], isRoot, prefix))
		b.WriteString("}}")
		src = src[end+2:]
	}
}

// actionEnd returns the index of the "}}" closing an action, past its quoted
// texts and comments.
func actionEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				i += j + 1
			}
		case '\'':
			for i++; i < len(s) && s[i] != '\''; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '/':
			if strings.HasPrefix(s[i:], "/*") {
				if j := strings.Index(s[i:], "*/"); j >= 0 {
					i += j + 1
				}
			}
		case '}':
			if strings.HasPrefix(s[i:], "}}") {
				return i
			}
		}
	}
	return -1
}

func prefixRoots(action string, isRoot map[string]bool, prefix string) string {
	var b strings.Builder
	for i := 0; i < len(action); {
		c := action[i]
		switch {
		case strings.HasPrefix(action[i:], "/*"):
			j := strings.Index(action[i:], "*/")
			if j < 0 {
				j = len(action) - i - 2
			}
			b.WriteString(action[i : i+j+2])
			i += j + 2
		case c == '"' || c == '`' || c == '\'':
			j := i + 1
			for j < len(action) && action[j] != c {
				if action[j] == '\\' && c != '`' {
					j++
				}
				j++
			}
			j = min(j+1, len(action))
			b.WriteString(action[i:j])
			i = j
		case isIdentStart(c):
			j := i
			for j < len(action) && isIdentPart(action[j]) {
				j++
			}
			word := action[i:j]
			prev := byte(' ')
			if i > 0 {
				prev = action[i-1]
			}
			if isRoot[word] && prev != '.' && prev != '$' && (j == len(action) || !isIdentPart(action[j])) {
				b.WriteString(prefix)
			}
			b.WriteString(word)
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

var keywords = map[string]bool{"if": true, "else": true, "end": true, "range": true, "with": true, "define": true, "template": true, "block": true, "break": true, "continue": true}

// Value runs a formula that is a single expression ({{sortBy "price"
// .items}}) and returns its value as is (a list stays a list); ok is false
// for the other formulas (text around, if, range...), to render as text.
func Value(src string, data any, roots ...string) (v any, ok bool, err error) {
	s := strings.TrimSpace(src)
	if !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return nil, false, nil
	}
	inner := s[2:]
	if actionEnd(inner) != len(inner)-2 {
		return nil, false, nil // several actions
	}
	inner = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(inner[:len(inner)-2], "-"), "-"))
	word, _, _ := strings.Cut(inner, " ")
	if inner == "" || keywords[word] || strings.HasPrefix(inner, "/*") || strings.HasPrefix(inner, "$") && strings.Contains(inner, "=") {
		return nil, false, nil
	}
	expr := Normalize("{{"+inner+"}}", roots...)
	expr = expr[2 : len(expr)-2]
	var out any
	t, err := template.New("formula").Funcs(Funcs()).Funcs(template.FuncMap{
		"__value": func(v any) string { out = v; return "" },
	}).Option("missingkey=zero").Parse("{{__value (" + expr + ")}}")
	if err != nil {
		return nil, false, nil // rendered as text, with its error
	}
	if _, err := Execute(t, data); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

// Plain returns a value with the library's types as plain values (List:
// []any, Number: float64, the raw JSON of JSON fields decoded), for the code
// that checks them.
func Plain(v any) any {
	switch val := plain(v).(type) {
	case List:
		return []any(val)
	case Number:
		return float64(val)
	default:
		return val
	}
}
