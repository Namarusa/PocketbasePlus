package pages

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"

	"github.com/Namarusa/PocketbasePlus/internal/dashboards"
)

// Charts are drawn by the server: HTML bars and grid, SVG lines, areas and
// pie slices, in percents of the plot so they fill any width without a
// script (labels keep their size). The data comes from the dashboards query
// engine, so a page shows the same numbers as a dashboard panel. The small
// pages script (gp-ui.js) adds the tooltips and the legend toggles; without
// it the values are in the titles.

// Chart bindings (attributes of the placeholder element).
const (
	attrChart      = "data-pb-chart"      // bar | line | area | pie | donut
	attrChartCol   = "data-pb-collection" // collection name
	attrChartFn    = "data-pb-fn"         // count | count_distinct | sum | avg | min | max
	attrChartValue = "data-pb-value"      // field aggregated by the function (not for count)
	attrChartGroup = "data-pb-group"      // one bar / point / slice per value of this field
	attrChartDate  = "data-pb-date"       // over time: date field (bar, line, area)
	attrChartPrec  = "data-pb-precision"  // hour | day | week | month | year
	attrChartRange = "data-pb-range"      // 24h | 7d | 30d | 90d | 365d | "" (all)
	attrChartSplit = "data-pb-split"      // one series per value of this field
	attrChartStack = "data-pb-stacked"    // "true": stacked bars / areas
	attrChartOrder = "data-pb-order"      // groups: value (default) | label
	attrChartLabel = "data-pb-label"      // name of the values (tooltip, donut center)
	attrChartH     = "data-pb-height"     // plot height in px
	attrChartColor = "data-pb-colors"     // "": the theme's chart colors, a palette, "primary", "custom:#..,#.."
)

var chartKinds = map[string]bool{"bar": true, "line": true, "area": true, "pie": true, "donut": true}

// chartSeries is the normalized data of a chart.
type chartSeries struct {
	labels []string // x values (categories, time buckets or slices)
	series []chartSerie
}

type chartSerie struct {
	label  string
	values []float64
}

// chartOptions returns the dashboards panel type and options of a chart.
func chartOptions(n *xhtml.Node) (kind, panelType string, options map[string]any) {
	kind = getAttr(n, attrChart)
	if !chartKinds[kind] {
		kind = "bar"
	}
	fn := getAttr(n, attrChartFn)
	if fn == "" {
		fn = "count"
	}
	options = map[string]any{
		"collection": getAttr(n, attrChartCol),
		"fn":         fn,
		"field":      getAttr(n, attrChartValue),
	}
	if date := getAttr(n, attrChartDate); date != "" && kind != "pie" && kind != "donut" {
		panelType = dashboards.TypeTimeSeries
		options["dateField"] = date
		options["precision"] = getAttr(n, attrChartPrec)
		options["range"] = getAttr(n, attrChartRange)
	} else {
		panelType = dashboards.TypeBar
		if kind == "pie" || kind == "donut" {
			panelType = dashboards.TypePie
		}
		options["groupField"] = getAttr(n, attrChartGroup)
		options["order"] = getAttr(n, attrChartOrder)
		if v, err := strconv.Atoi(getAttr(n, attrLimit)); err == nil && v > 0 {
			options["limit"] = v
		}
	}
	if split := getAttr(n, attrChartSplit); split != "" && panelType != dashboards.TypePie {
		options["splitField"] = split
	}
	return kind, panelType, options
}

// chart replaces the placeholder with the chart of its data.
func (r *renderer) chart(n *xhtml.Node, s scope) {
	kind, panelType, options := chartOptions(n)
	filter, params := r.bind(getAttr(n, attrFilter), s)
	options["filter"] = filter

	// the visitor's API rules, or the page as the gate (read as a superuser)
	auth := r.visitor.user
	if getAttr(n, attrAccess) != "rules" || r.visitor.all {
		auth = r.superuser()
	}
	var data chartSeries
	raw, err := dashboards.RunPanelParams(r.ctx, r.app, auth, panelType, options, params)
	if err == nil {
		data, err = normalizeChart(raw, getAttr(n, attrChartPrec))
	}
	height := 260
	if v, err := strconv.Atoi(getAttr(n, attrChartH)); err == nil && v >= 80 && v <= 1200 {
		height = v
	}
	label := getAttr(n, attrChartLabel)
	if label == "" {
		label = valueLabel(options)
	}
	stacked := getAttr(n, attrChartStack) == "true" && len(data.series) > 1 && (kind == "bar" || kind == "area")

	clearChildren(n)
	addClass(n, "gp-chart")
	addClass(n, "gp-chart--"+kind)
	style := strings.TrimSpace(getAttr(n, "style"))
	if style != "" && !strings.HasSuffix(style, ";") {
		style += ";"
	}
	style += "--gp-chart-h:" + strconv.Itoa(height) + "px"
	// its own colors (light and dark): the CSS picks them by the mode
	if light, dark, ok := chartColors(getAttr(n, attrChartColor)); ok {
		addClass(n, "gp-chart-pal")
		for i := range light {
			k := strconv.Itoa(i + 1)
			style += ";--gp-l" + k + ":" + light[i] + ";--gp-d" + k + ":" + dark[i]
		}
	}
	setAttr(n, "style", style)
	r.widgets = true
	if err != nil {
		r.fail(n, "chart: "+err.Error())
		if !r.preview {
			appendHTML(n, `<p class="gp-chart-empty">No data</p>`)
		}
		return
	}
	if len(data.series) == 1 && data.series[0].label == "" {
		data.series[0].label = label
	}
	var out string
	var script map[string]any
	if kind == "pie" || kind == "donut" {
		out, script = pieHTML(data, kind == "donut", label, r.chartID())
	} else {
		out, script = xyHTML(data, kind, stacked, r.chartID())
	}
	if js, err := json.Marshal(script); err == nil {
		setAttr(n, "data-gp-chart", string(js))
	}
	appendHTML(n, out)
}

// superuser is the reader of the charts of a page that is the access gate.
func (r *renderer) superuser() *core.Record {
	col, err := r.app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return nil
	}
	return core.NewRecord(col)
}

// chartID numbers the charts of a page (ids of their SVG gradients).
func (r *renderer) chartID() string {
	r.charts++
	return "gpc" + strconv.Itoa(r.charts)
}

func valueLabel(options map[string]any) string {
	field, _ := options["field"].(string)
	switch options["fn"] {
	case "count":
		return "Records"
	case "count_distinct":
		return "Distinct " + field
	case "sum":
		return "Total " + field
	case "avg":
		return "Average " + field
	case "min":
		return "Min " + field
	case "max":
		return "Max " + field
	}
	return "Value"
}

// normalizeChart turns the data of a dashboards panel into series.
func normalizeChart(raw any, precision string) (chartSeries, error) {
	var d struct {
		Points []struct {
			X string  `json:"x"`
			Y float64 `json:"y"`
		} `json:"points"`
		Groups []struct {
			Label string  `json:"label"`
			Value float64 `json:"value"`
		} `json:"groups"`
		Categories []struct {
			Label string `json:"label"`
		} `json:"categories"`
		Series []struct {
			Label  string `json:"label"`
			Points []struct {
				X string  `json:"x"`
				Y float64 `json:"y"`
			} `json:"points"`
			Values []float64 `json:"values"`
		} `json:"series"`
	}
	js, err := json.Marshal(raw)
	if err != nil {
		return chartSeries{}, err
	}
	if err := json.Unmarshal(js, &d); err != nil {
		return chartSeries{}, err
	}
	var out chartSeries
	switch {
	case len(d.Series) > 0 && len(d.Categories) > 0: // split bars
		for _, c := range d.Categories {
			out.labels = append(out.labels, c.Label)
		}
		for _, s := range d.Series {
			out.series = append(out.series, chartSerie{label: s.Label, values: s.Values})
		}
	case len(d.Series) > 0: // split time series (same buckets)
		for _, p := range d.Series[0].Points {
			out.labels = append(out.labels, timeLabel(p.X, precision))
		}
		for _, s := range d.Series {
			values := make([]float64, len(s.Points))
			for i, p := range s.Points {
				values[i] = p.Y
			}
			out.series = append(out.series, chartSerie{label: s.Label, values: values})
		}
	case d.Points != nil:
		s := chartSerie{}
		for _, p := range d.Points {
			out.labels = append(out.labels, timeLabel(p.X, precision))
			s.values = append(s.values, p.Y)
		}
		out.series = []chartSerie{s}
	default:
		s := chartSerie{}
		for _, g := range d.Groups {
			label := g.Label
			if label == "" {
				label = "(empty)"
			}
			out.labels = append(out.labels, label)
			s.values = append(s.values, g.Value)
		}
		out.series = []chartSerie{s}
	}
	return out, nil
}

// timeLabel formats a time bucket of the dashboards engine.
func timeLabel(x, precision string) string {
	layouts := map[string]string{"hour": "2006-01-02 15:00", "week": "2006-01-02", "month": "2006-01", "year": "2006"}
	layout, ok := layouts[precision]
	if !ok {
		layout = "2006-01-02"
	}
	t, err := time.Parse(layout, x)
	if err != nil {
		return x
	}
	switch precision {
	case "hour":
		return t.Format("Jan 2 15:00")
	case "month":
		return t.Format("Jan 2006")
	case "year":
		return t.Format("2006")
	}
	return t.Format("Jan 2")
}

// --- x / y charts (bars, lines, areas) --------------------------------------------------------

func seriesColor(i int) string {
	if i < 5 {
		return "var(--gp-c" + strconv.Itoa(i+1) + ")"
	}
	return "color-mix(in oklab, var(--gp-c" + strconv.Itoa(i%5+1) + ") 55%, var(--gp-bg))"
}

func xyHTML(d chartSeries, kind string, stacked bool, id string) (string, map[string]any) {
	n := len(d.labels)
	if n == 0 || len(d.series) == 0 {
		return `<p class="gp-chart-empty">No data</p>`, nil
	}
	// the y domain: 0 and the values (stacked: the sums)
	lo, hi := 0.0, 0.0
	for i := 0; i < n; i++ {
		pos, neg := 0.0, 0.0
		for _, s := range d.series {
			v := s.values[i]
			if stacked {
				if v >= 0 {
					pos += v
				} else {
					neg += v
				}
			} else {
				pos, neg = math.Max(pos, v), math.Min(neg, v)
			}
		}
		hi, lo = math.Max(hi, pos), math.Min(lo, neg)
	}
	lo, hi, step := niceScale(lo, hi, 4)
	pct := func(v float64) float64 { return (v - lo) / (hi - lo) * 100 }
	zero := pct(0)

	var b strings.Builder
	b.WriteString(`<div class="gp-chart-body"><div class="gp-chart-y" aria-hidden="true">`)
	for v := lo; v <= hi+step/2; v += step {
		fmt.Fprintf(&b, `<span style="bottom:%s%%">%s</span>`, pf(pct(v)), html.EscapeString(compactNumber(v)))
	}
	b.WriteString(`</div><div class="gp-chart-plot"><div class="gp-chart-grid" aria-hidden="true">`)
	for v := lo; v <= hi+step/2; v += step {
		fmt.Fprintf(&b, `<i style="bottom:%s%%"></i>`, pf(pct(v)))
	}
	b.WriteString(`</div>`)

	// x positions (percent): bars in columns, lines from edge to edge
	x := func(i int) float64 {
		if kind == "bar" {
			return (float64(i) + 0.5) / float64(n) * 100
		}
		if n == 1 {
			return 50
		}
		return float64(i) / float64(n-1) * 100
	}

	if kind == "bar" {
		col := 100 / float64(n)
		group := col * 0.8
		groups := len(d.series)
		if stacked {
			groups = 1
		}
		bw := group / float64(groups)
		b.WriteString(`<div class="gp-chart-bars">`)
		for i := 0; i < n; i++ {
			pos, neg := 0.0, 0.0
			for si, s := range d.series {
				v := s.values[i]
				left := float64(i)*col + col*0.1 + float64(si)*bw
				from, to := 0.0, v
				if stacked {
					left = float64(i)*col + col*0.1
					if v >= 0 {
						from, to = pos, pos+v
						pos += v
					} else {
						from, to = neg, neg+v
						neg += v
					}
				}
				bottom, top := pct(math.Min(from, to)), pct(math.Max(from, to))
				cls := "gp-chart-bar"
				if v < 0 {
					cls += " gp-neg"
				}
				fmt.Fprintf(&b, `<div class="%s" data-s="%d" style="left:%s%%;width:%s%%;bottom:%s%%;height:%s%%;background:%s"></div>`,
					cls, si, pf(left), pf(bw), pf(bottom), pf(top-bottom), seriesColor(si))
			}
		}
		b.WriteString(`</div>`)
	} else {
		// SVG in a 1000 x 1000 box stretched to the plot (strokes keep their width)
		b.WriteString(`<svg class="gp-chart-svg" viewBox="0 0 1000 1000" preserveAspectRatio="none" aria-hidden="true"><defs>`)
		for si := range d.series {
			fmt.Fprintf(&b, `<linearGradient id="%s-%d" x1="0" y1="0" x2="0" y2="1"><stop offset="5%%" style="stop-color:%s;stop-opacity:.8"/><stop offset="95%%" style="stop-color:%s;stop-opacity:.1"/></linearGradient>`,
				id, si, seriesColor(si), seriesColor(si))
		}
		b.WriteString(`</defs>`)
		base := make([]pt, n) // the bottom line of the next area (stacked)
		for i := range base {
			base[i] = pt{x(i) * 10, 1000 - zero*10}
		}
		cum := make([]float64, n)
		var lines strings.Builder
		for si, s := range d.series {
			top := make([]pt, n)
			for i, v := range s.values {
				if stacked {
					cum[i] += v
					v = cum[i]
				}
				top[i] = pt{x(i) * 10, 1000 - pct(v)*10}
			}
			if n == 1 { // a single bucket: a flat line across the plot
				top = []pt{{0, top[0].y}, {1000, top[0].y}}
			}
			line := monotonePath(top)
			if kind == "area" {
				bottom := base
				if n == 1 {
					bottom = []pt{{0, base[0].y}, {1000, base[0].y}}
				}
				rev := make([]pt, len(bottom))
				for i, p := range bottom {
					rev[len(bottom)-1-i] = p
				}
				back := monotonePath(rev)
				fmt.Fprintf(&b, `<path class="gp-chart-area" data-s="%d" d="%s L%s Z" fill="url(#%s-%d)"/>`,
					si, line, strings.TrimPrefix(back, "M"), id, si)
				if stacked {
					base = top
				}
			}
			fmt.Fprintf(&lines, `<path class="gp-chart-line" data-s="%d" d="%s" pathLength="1" style="stroke:%s"/>`, si, line, seriesColor(si))
		}
		b.WriteString(lines.String())
		b.WriteString(`</svg>`)
	}

	// hover columns: the values in their titles (the script shows a tooltip instead)
	b.WriteString(`<div class="gp-chart-hits">`)
	for i := 0; i < n; i++ {
		w := 100 / float64(n)
		left := x(i) - w/2
		if kind != "bar" && n > 1 {
			w = 100 / float64(n-1)
			left = x(i) - w/2
		}
		var title strings.Builder
		title.WriteString(d.labels[i])
		for _, s := range d.series {
			title.WriteString("\n" + s.label + ": " + fullNumber(s.values[i]))
		}
		fmt.Fprintf(&b, `<span data-i="%d" style="left:%s%%;width:%s%%" title="%s"></span>`, i, pf(math.Max(left, 0)), pf(math.Min(w, 100-math.Max(left, 0))), html.EscapeString(title.String()))
	}
	b.WriteString(`</div></div><div class="gp-chart-x" aria-hidden="true">`)
	for i, l := range d.labels {
		fmt.Fprintf(&b, `<span style="left:%s%%" data-l="%d"%s>%s</span>`, pf(x(i)), labelLevel(i, n), edgeAttr(kind, i, n), html.EscapeString(l))
	}
	b.WriteString(`</div></div>`)
	b.WriteString(legendHTML(d.series, nil))
	b.WriteString(tableHTML(d))

	script := map[string]any{"type": kind, "labels": d.labels, "min": lo, "max": hi, "stacked": stacked, "series": scriptSeries(d.series, func(si, _ int) string { return seriesColor(si) })}
	return b.String(), script
}

// labelLevel thins the x labels on narrow charts (CSS container queries):
// level 0 always, 1 from 480px, 2 from 720px.
func labelLevel(i, n int) int {
	step := func(max int) int { return int(math.Ceil(float64(n) / float64(max))) }
	switch {
	case i%step(5) == 0:
		return 0
	case i%step(9) == 0:
		return 1
	case i%step(14) == 0:
		return 2
	}
	return 3
}

// edgeAttr keeps the first and last labels of line charts inside the plot.
func edgeAttr(kind string, i, n int) string {
	if kind == "bar" || n < 2 {
		return ""
	}
	if i == 0 {
		return ` data-edge="start"`
	}
	if i == n-1 {
		return ` data-edge="end"`
	}
	return ""
}

func legendHTML(series []chartSerie, labels []string) string {
	var b strings.Builder
	if labels != nil { // pie: one item per slice
		b.WriteString(`<div class="chart-legend gp-chart-legend">`)
		for i, l := range labels {
			fmt.Fprintf(&b, `<button type="button" class="chart-legend-item" data-i="%d" aria-pressed="true"><span class="chart-legend-indicator" style="--chart-indicator-color:%s"></span>%s</button>`, i, seriesColor(i), html.EscapeString(l))
		}
		b.WriteString(`</div>`)
		return b.String()
	}
	if len(series) < 2 {
		return ""
	}
	b.WriteString(`<div class="chart-legend gp-chart-legend">`)
	for i, s := range series {
		fmt.Fprintf(&b, `<button type="button" class="chart-legend-item" data-s="%d" aria-pressed="true"><span class="chart-legend-indicator" style="--chart-indicator-color:%s"></span>%s</button>`, i, seriesColor(i), html.EscapeString(s.label))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// tableHTML is the data of the chart for screen readers.
func tableHTML(d chartSeries) string {
	var b strings.Builder
	b.WriteString(`<table class="gp-sr-only"><thead><tr><th></th>`)
	for _, s := range d.series {
		b.WriteString("<th>" + html.EscapeString(s.label) + "</th>")
	}
	b.WriteString(`</tr></thead><tbody>`)
	for i, l := range d.labels {
		b.WriteString("<tr><th>" + html.EscapeString(l) + "</th>")
		for _, s := range d.series {
			b.WriteString("<td>" + fullNumber(s.values[i]) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func scriptSeries(series []chartSerie, color func(si, i int) string) []map[string]any {
	out := make([]map[string]any, len(series))
	for si, s := range series {
		formatted := make([]string, len(s.values))
		for i, v := range s.values {
			formatted[i] = fullNumber(v)
		}
		out[si] = map[string]any{"label": s.label, "color": color(si, 0), "values": s.values, "formatted": formatted}
	}
	return out
}

// --- pie and donut --------------------------------------------------------------------------

func pieHTML(d chartSeries, donut bool, label, id string) (string, map[string]any) {
	total := 0.0
	var values []float64
	if len(d.series) > 0 {
		values = d.series[0].values
	}
	for _, v := range values {
		if v > 0 {
			total += v
		}
	}
	if total == 0 {
		return `<p class="gp-chart-empty">No data</p>`, nil
	}
	var b strings.Builder
	b.WriteString(`<div class="gp-chart-pie-wrap"><svg class="gp-chart-pie-svg" viewBox="-1 -1 2 2" role="img">`)
	inner := 0.0
	if donut {
		inner = 0.6
	}
	start := -math.Pi / 2
	for i, v := range values {
		if v <= 0 {
			continue
		}
		end := start + v/total*2*math.Pi
		fmt.Fprintf(&b, `<path class="gp-chart-slice" data-i="%d" d="%s" style="fill:%s"><title>%s: %s</title></path>`,
			i, arcPath(start, end, inner), seriesColor(i), html.EscapeString(d.labels[i]), fullNumber(v))
		start = end
	}
	b.WriteString(`</svg>`)
	if donut {
		fmt.Fprintf(&b, `<div class="gp-chart-center"><strong>%s</strong><span>%s</span></div>`, compactNumber(total), html.EscapeString(label))
	}
	b.WriteString(`</div>`)
	b.WriteString(legendHTML(nil, d.labels))
	b.WriteString(tableHTML(d))
	formatted := make([]string, len(values))
	colors := make([]string, len(values))
	for i, v := range values {
		formatted[i] = fullNumber(v)
		colors[i] = seriesColor(i)
	}
	script := map[string]any{"type": map[bool]string{true: "donut", false: "pie"}[donut], "labels": d.labels, "label": label,
		"series": []map[string]any{{"label": label, "values": values, "formatted": formatted, "colors": colors}}}
	return b.String(), script
}

// arcPath is a pie slice (inner > 0: a donut slice) in a unit circle.
func arcPath(a0, a1, inner float64) string {
	if a1-a0 >= 2*math.Pi-1e-9 {
		a1 = a0 + 2*math.Pi - 1e-4 // a full circle isn't an arc
	}
	large := 0
	if a1-a0 > math.Pi {
		large = 1
	}
	p := func(r, a float64) string { return pf(r*math.Cos(a)) + " " + pf(r*math.Sin(a)) }
	if inner == 0 {
		return fmt.Sprintf("M0 0L%sA1 1 0 %d 1 %sZ", p(1, a0), large, p(1, a1))
	}
	return fmt.Sprintf("M%sA1 1 0 %d 1 %sL%sA%s %s 0 %d 0 %sZ", p(1, a0), large, p(1, a1), p(inner, a1), pf(inner), pf(inner), large, p(inner, a0))
}

// --- geometry and numbers --------------------------------------------------------------------

type pt struct{ x, y float64 }

// monotonePath is a smooth line through the points that doesn't overshoot
// them (monotone cubic interpolation, like the charts of shadcn/ui).
func monotonePath(p []pt) string {
	n := len(p)
	if n == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("M" + pf(p[0].x) + " " + pf(p[0].y))
	if n == 1 {
		return b.String()
	}
	slopes := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		dx := p[i+1].x - p[i].x
		if dx == 0 {
			slopes[i] = 0
		} else {
			slopes[i] = (p[i+1].y - p[i].y) / dx
		}
	}
	tangents := make([]float64, n)
	tangents[0], tangents[n-1] = slopes[0], slopes[n-2]
	for i := 1; i < n-1; i++ {
		if slopes[i-1]*slopes[i] <= 0 {
			tangents[i] = 0
		} else {
			tangents[i] = harmonic(slopes[i-1], slopes[i])
		}
	}
	for i := 0; i < n-1; i++ {
		dx := (p[i+1].x - p[i].x) / 3
		fmt.Fprintf(&b, "C%s %s %s %s %s %s",
			pf(p[i].x+dx), pf(p[i].y+tangents[i]*dx), pf(p[i+1].x-dx), pf(p[i+1].y-tangents[i+1]*dx), pf(p[i+1].x), pf(p[i+1].y))
	}
	return b.String()
}

// harmonic is the tangent of the Fritsch-Carlson method at a point between
// two slopes of the same sign.
func harmonic(a, b float64) float64 {
	return 2 / (1/a + 1/b)
}

// niceScale returns a domain with round ticks containing lo and hi.
func niceScale(lo, hi float64, ticks int) (float64, float64, float64) {
	if hi == lo {
		hi = lo + 1
	}
	raw := (hi - lo) / float64(ticks)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := mag * 10
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*mag {
			step = m * mag
			break
		}
	}
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}

// pf formats a coordinate (2 decimals, no trailing zeros).
func pf(v float64) string {
	if math.Abs(v) < 0.005 {
		v = 0
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// fullNumber formats a value with thousands separators and up to 2 decimals.
func fullNumber(v float64) string {
	s := strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if frac != "" {
		return sign + b.String() + "." + frac
	}
	return sign + b.String()
}

// compactNumber formats an axis value (1.2k, 3M).
func compactNumber(v float64) string {
	a := math.Abs(v)
	for _, u := range []struct {
		div    float64
		suffix string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "k"}} {
		if a >= u.div {
			return strconv.FormatFloat(math.Round(v/u.div*10)/10, 'f', -1, 64) + u.suffix
		}
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// --- html helpers ------------------------------------------------------------------------

// appendHTML parses trusted markup (built by the server, values escaped) into n.
func appendHTML(n *xhtml.Node, src string) {
	nodes, err := xhtml.ParseFragment(strings.NewReader(src), n)
	if err != nil {
		return
	}
	for _, c := range nodes {
		n.AppendChild(c)
	}
}

func addClass(n *xhtml.Node, class string) {
	classes := strings.Fields(getAttr(n, "class"))
	for _, c := range classes {
		if c == class {
			return
		}
	}
	setAttr(n, "class", strings.TrimSpace(strings.Join(append(classes, class), " ")))
}

// checkChart validates the collection and the filter of a chart.
func checkChart(app core.App, n *xhtml.Node) error {
	col, err := listCollection(app, getAttr(n, attrChartCol))
	if err != nil {
		if getAttr(n, attrChartCol) == "" {
			return nil // not set up yet: nothing is shown
		}
		return err
	}
	return checkFilter(app, col, getAttr(n, attrFilter), "")
}
