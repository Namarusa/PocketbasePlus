package pages

import (
	"net/url"
	"strings"
	"testing"
)

func TestCharts(t *testing.T) {
	f := setup(t)
	check := func(name, src string, query url.Values, want, unwanted []string) {
		t.Helper()
		out, script, err := render(t.Context(), f.app, pageView{HTML: src}, query, false)
		if err != nil {
			t.Fatal(err)
		}
		if !script {
			t.Errorf("%s: expected the pages script", name)
		}
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q in\n%.3000s", name, w, out)
			}
		}
		for _, u := range unwanted {
			if strings.Contains(out, u) {
				t.Errorf("%s: unexpected %q in\n%.3000s", name, u, out)
			}
		}
	}
	check("bar by group", `<div data-pb-chart="bar" data-pb-collection="products" data-pb-fn="sum" data-pb-value="price" data-pb-group="name"></div>`, nil,
		[]string{`class="gp-chart gp-chart--bar"`, `class="gp-chart-bar"`, "Lamp &lt;b&gt;", "Chair", "title=\"Chair\nTotal price: 40\"", "/p/_static/gp-ui-", "data-gp-chart="},
		[]string{"data-pb-", "<b>"})
	check("filter placeholders", `<div data-pb-chart="bar" data-pb-collection="products" data-pb-group="name" data-pb-filter="name = {{query.n}}"></div>`, url.Values{"n": {"Chair"}},
		[]string{"Chair"}, []string{"Lamp"})
	check("donut", `<div data-pb-chart="donut" data-pb-collection="products" data-pb-group="published"></div>`, nil,
		[]string{`class="gp-chart-slice"`, `class="gp-chart-center"><strong>2</strong><span>Records</span>`, `class="chart-legend-item" data-i="1"`}, nil)
	check("line over time", `<div data-pb-chart="line" data-pb-collection="products" data-pb-date="launch" data-pb-precision="month"></div>`, nil,
		[]string{`class="gp-chart-line"`, "Mar 2026"}, nil)
	check("area", `<div data-pb-chart="area" data-pb-collection="products" data-pb-date="launch" data-pb-range="30d"></div>`, nil,
		[]string{`class="gp-chart-area"`, `fill="url(#gpc1-0)"`}, nil)
	check("stacked bars by split", `<div data-pb-chart="bar" data-pb-stacked="true" data-pb-collection="products" data-pb-group="name" data-pb-split="published"></div>`, nil,
		[]string{`&#34;stacked&#34;:true`, `class="chart-legend-item" data-s="1"`}, nil)
	// the visitor's API rules: products are superusers only (no data, no error shown)
	check("rules", `<div data-pb-chart="bar" data-pb-access="rules" data-pb-collection="products" data-pb-group="name"></div>`, nil,
		[]string{"No data"}, []string{"Chair"})

	// the chart settings are checked when the page is saved
	if err := checkBindings(f.app, `<div data-pb-chart="bar" data-pb-collection="nope"></div>`); err == nil {
		t.Error("expected an error for an unknown collection")
	}
	if err := checkBindings(f.app, `<div data-pb-chart="bar" data-pb-collection="products" data-pb-filter="nope = 1"></div>`); err == nil {
		t.Error("expected an error for an invalid filter")
	}
}

func TestCalendarFields(t *testing.T) {
	f := setup(t)
	out := f.render(t, `<form><div data-pb-calendar="range" data-pb-picker data-pb-name="stay" data-pb-min="2026-01-01" data-pb-required="true"></div>`+
		`<div data-pb-calendar="multiple" data-pb-name="days"></div><div data-pb-calendar="single" data-pb-time="true"></div></form>`,
		scope{}, url.Values{"stay_start": {"2026-02-03"}, "stay_end": {"<x>"}, "days": {"2026-01-02, bad,2026-01-05"}, "date": {"2026-01-02T10:30"}})
	for _, want := range []string{
		`class="gp-cal-root gp-datepicker"`, `&#34;mode&#34;:&#34;range&#34;`, `&#34;picker&#34;:true`,
		`name="stay_start" value="2026-02-03" aria-label="Start date" min="2026-01-01" required=""`,
		`name="stay_end" value=""`,
		`name="days" value="2026-01-02,2026-01-05"`,
		`type="datetime-local" name="date" value="2026-01-02T10:30"`,
		"/p/_static/gp-ui-",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-pb-") || strings.Contains(out, "<x>") {
		t.Errorf("unexpected binding or value in\n%s", out)
	}
}

func TestChartHelpers(t *testing.T) {
	if lo, hi, step := niceScale(0, 37, 4); lo != 0 || hi != 40 || step != 10 {
		t.Errorf("niceScale(0, 37) = %v %v %v", lo, hi, step)
	}
	for v, want := range map[float64]string{1234567.891: "1,234,567.89", -1200: "-1,200", 0.5: "0.5"} {
		if got := fullNumber(v); got != want {
			t.Errorf("fullNumber(%v) = %q, want %q", v, got, want)
		}
	}
	for v, want := range map[float64]string{1500: "1.5k", 2e6: "2M", 12: "12"} {
		if got := compactNumber(v); got != want {
			t.Errorf("compactNumber(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestChartColors(t *testing.T) {
	for _, v := range []string{"blue", "multicolor", "primary", "custom:#2563eb,#16a34a", "custom:#111,#222,#333,#444,#555"} {
		if _, _, ok := chartColors(v); !ok {
			t.Errorf("%q: expected valid colors", v)
		}
	}
	for _, v := range []string{"", "nope", "custom:", "custom:red", "custom:#1,#2,#3,#4,#5,#6", "custom:#2563eb;}body{x"} {
		if _, _, ok := chartColors(v); ok {
			t.Errorf("%q: expected invalid colors", v)
		}
	}
	if css := chartColorsCSS("multicolor"); !strings.Contains(css, ":root{--chart-1:oklch(0.646 0.222 41.116);") || !strings.Contains(css, ".dark{--chart-1:oklch(0.488 0.243 264.376);") {
		t.Errorf("unexpected palette CSS %q", css)
	}
	if css := (theme{Charts: "custom:#123456,#abcdef", CSS: ":root{--chart-1:red}"}).css(); !strings.HasSuffix(css, ".dark{--chart-1:#123456;--chart-2:#abcdef;--chart-3:#123456;--chart-4:#abcdef;--chart-5:#123456;}\n") {
		t.Errorf("the picked chart colors should come last (over a pasted theme):\n%s", css)
	}

	// a chart with its own colors
	f := setup(t)
	out := f.render(t, `<div data-pb-chart="bar" data-pb-collection="products" data-pb-group="name" data-pb-colors="emerald"></div>`, scope{}, nil)
	if !strings.Contains(out, "gp-chart-pal") || !strings.Contains(out, "--gp-l1:oklch(0.845 0.143 164.978);--gp-d1:oklch(0.845 0.143 164.978)") {
		t.Errorf("expected the chart's own colors in\n%.2000s", out)
	}
	out = f.render(t, `<div data-pb-chart="bar" data-pb-collection="products" data-pb-group="name" data-pb-colors="custom:#1;x:y"></div>`, scope{}, nil)
	if strings.Contains(out, "gp-chart-pal") || strings.Contains(out, "x:y") {
		t.Errorf("invalid colors should be ignored\n%.2000s", out)
	}

	// the setting is checked when saved
	settings, _ := f.app.FindFirstRecordByFilter(SettingsCollection, "id != ''")
	settings.Set("chart_colors", "custom:not-a-color")
	if err := f.app.Save(settings); err == nil {
		t.Error("expected invalid chart colors to be refused")
	}
	settings.Set("chart_colors", "violet")
	if err := f.app.Save(settings); err != nil {
		t.Errorf("expected a palette to be saved: %v", err)
	}
}
