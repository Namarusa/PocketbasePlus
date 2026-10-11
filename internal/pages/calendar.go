package pages

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

// Calendars and date pickers (like shadcn/ui's) are form fields: the server
// writes the browser's date inputs (they work without a script), and the
// pages script (gp-ui.js) replaces them with the calendar, keeping the same
// names and values. Modes: one date (optionally with a time), several dates
// (comma separated) or a range (name_start and name_end).

const (
	attrCalendar  = "data-pb-calendar"      // single | multiple | range
	attrCalPicker = "data-pb-picker"        // a date picker: a field opening the calendar in a popover
	attrCalName   = "data-pb-name"          // field name ("date" by default)
	attrCalTime   = "data-pb-time"          // "true": a time too (single)
	attrCalMonths = "data-pb-months"        // months shown side by side (1 or 2)
	attrCalMin    = "data-pb-min"           // YYYY-MM-DD or "today"
	attrCalMax    = "data-pb-max"           // YYYY-MM-DD or "today"
	attrCalWeek   = "data-pb-week-start"    // 0 (Sunday) to 6, empty: the visitor's locale
	attrCalCapt   = "data-pb-captions"      // label | dropdown (month and year selects)
	attrCalOff    = "data-pb-disabled-days" // week days that can't be picked, e.g. "0,6"
	attrCalHolder = "data-pb-placeholder"   // date picker text when empty
	attrCalReq    = "data-pb-required"      // "true": the form needs a value
)

var (
	isoDate      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	isoDateTime  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
	fieldNameRgx = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`)
)

// calDate resolves a min / max option.
func calDate(v string) string {
	if v == "today" {
		return time.Now().Format("2006-01-02")
	}
	if isoDate.MatchString(v) {
		return v
	}
	return ""
}

// calendar replaces the placeholder with the date inputs and the options of
// the script. The values come back from the query (a search form sent with
// GET keeps its dates).
func (r *renderer) calendar(n *xhtml.Node) {
	mode := getAttr(n, attrCalendar)
	if mode != "multiple" && mode != "range" {
		mode = "single"
	}
	name := getAttr(n, attrCalName)
	if !fieldNameRgx.MatchString(name) {
		name = "date"
	}
	picker := hasAttr(n, attrCalPicker) && getAttr(n, attrCalPicker) != "false"
	withTime := mode == "single" && getAttr(n, attrCalTime) == "true"
	months := 1
	if getAttr(n, attrCalMonths) == "2" {
		months = 2
	}
	off := []int{}
	for _, d := range strings.Split(getAttr(n, attrCalOff), ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(d)); err == nil && v >= 0 && v <= 6 {
			off = append(off, v)
		}
	}
	week := -1
	if v, err := strconv.Atoi(getAttr(n, attrCalWeek)); err == nil && v >= 0 && v <= 6 {
		week = v
	}
	required := getAttr(n, attrCalReq) == "true"
	minDate, maxDate := calDate(getAttr(n, attrCalMin)), calDate(getAttr(n, attrCalMax))
	placeholder := getAttr(n, attrCalHolder)
	if placeholder == "" {
		placeholder = map[string]string{"single": "Pick a date", "multiple": "Pick dates", "range": "Pick a date range"}[mode]
	}

	opts := map[string]any{"mode": mode, "months": months, "min": minDate, "max": maxDate, "time": withTime,
		"captions": getAttr(n, attrCalCapt), "disabled": off, "required": required, "placeholder": placeholder, "picker": picker}
	if week >= 0 {
		opts["weekStart"] = week
	}
	js, _ := json.Marshal(opts)

	// a date input (the fallback, and the value the form sends)
	input := func(name, label, typ string) string {
		v := r.query.Get(name)
		if typ == "datetime-local" {
			if !isoDateTime.MatchString(v) {
				v = ""
			}
		} else if !isoDate.MatchString(v) {
			v = ""
		}
		s := `<input class="input" type="` + typ + `" name="` + html.EscapeString(name) + `" value="` + v + `" aria-label="` + label + `"`
		if minDate != "" {
			s += ` min="` + minDate + `"`
		}
		if maxDate != "" {
			s += ` max="` + maxDate + `"`
		}
		if required {
			s += ` required`
		}
		return s + `>`
	}
	var fallback string
	switch mode {
	case "range":
		fallback = input(name+"_start", "Start date", "date") + `<span aria-hidden="true">–</span>` + input(name+"_end", "End date", "date")
	case "multiple":
		v := r.query.Get(name)
		var dates []string
		for _, d := range strings.Split(v, ",") {
			if d = strings.TrimSpace(d); isoDate.MatchString(d) {
				dates = append(dates, d)
			}
		}
		fallback = `<input class="input" type="text" name="` + html.EscapeString(name) + `" value="` + strings.Join(dates, ",") + `" aria-label="Dates"` +
			` placeholder="YYYY-MM-DD, YYYY-MM-DD" pattern="\d{4}-\d{2}-\d{2}(\s*,\s*\d{4}-\d{2}-\d{2})*"`
		if required {
			fallback += ` required`
		}
		fallback += `>`
	default:
		typ := "date"
		if withTime {
			typ = "datetime-local"
		}
		fallback = input(name, "Date", typ)
	}

	clearChildren(n)
	addClass(n, "gp-cal-root")
	if picker {
		addClass(n, "gp-datepicker")
	} else {
		addClass(n, "gp-calendar")
	}
	setAttr(n, "data-gp-cal", string(js))
	appendHTML(n, `<div class="gp-cal-fallback">`+fallback+`</div>`)
	r.widgets = true
}
