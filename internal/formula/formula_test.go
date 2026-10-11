package formula

import (
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tools/types"
)

func fixedNow(t *testing.T) {
	prev := Now
	Now = func() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { Now = prev })
}

var exampleData = map[string]any{
	"items": []any{
		map[string]any{"name": "Pen", "price": 2.5, "stock": 10},
		map[string]any{"name": "Lamp", "price": 30.0, "stock": 0},
	},
}

// Every example of the catalog gives its documented result.
func TestCatalogExamples(t *testing.T) {
	fixedNow(t)
	seen := map[string]bool{}
	for _, f := range Catalog() {
		if seen[f.Name] {
			t.Errorf("%s is listed twice", f.Name)
		}
		seen[f.Name] = true
		if f.Example == "" || f.Syntax == "" || f.Description == "" {
			t.Errorf("%s: missing documentation", f.Name)
			continue
		}
		got, err := Render(f.Example, exampleData)
		if err != nil {
			t.Errorf("%s: %s: %v", f.Name, f.Example, err)
			continue
		}
		if got != f.Result {
			t.Errorf("%s: %s = %q, documented %q", f.Name, f.Example, got, f.Result)
		}
	}
	for _, c := range Catalog() {
		found := false
		for _, cat := range Categories {
			found = found || cat == c.Category
		}
		if !found {
			t.Errorf("%s: unknown category %q", c.Name, c.Category)
		}
	}
}

func TestValuesOfRecords(t *testing.T) {
	fixedNow(t)
	created, _ := types.ParseDateTime("2026-10-01 09:30:00.000Z")
	data := map[string]any{"record": map[string]any{
		"price":   19.9,
		"qty":     3,
		"created": created,
		"tags":    []string{"b", "a"},
		"meta":    types.JSONRaw(`{"color":"red","sizes":[1,2]}`),
		"empty":   nil,
	}}
	cases := map[string]string{
		`{{currency "EUR" (multiply record.price record.qty)}}`:           "€59.70",
		`{{formatDate "DD/MM/YYYY" record.created}}`:                      "01/10/2026",
		`{{timeAgo record.created}}`:                                      "10 days ago",
		`{{formatDate "D MMM" (addDays 30 record.created)}}`:              "31 Oct",
		`{{join "+" (sort record.tags)}}`:                                 "a+b",
		`{{get "color" record.meta}} {{count (get "sizes" record.meta)}}`: "red 2",
		`{{if gt record.price 10}}expensive{{end}}`:                       "expensive", // float vs int: no "incompatible types"
		`{{if eq record.qty 3.0 "x"}}three{{end}}`:                        "three",
		`{{default "-" record.empty}}{{record.missing}}`:                  "-",
		`{{record.price | round 0}}`:                                      "20",
		`{{range record.tags}}[{{.}}{{record.qty}}]{{end}}`:               "[b3][a3]", // the root stays reachable in a range
		`{{"record.price"}} {{ $.record.qty }}`:                           "record.price 3",
		`{{formatNumber 1234567}}`:                                        "1,234,567",
		`{{currency "XOF" -5}}`:                                           "-XOF 5.00",
		`{{isToday now}} {{isPast (addMinutes -1 now)}}`:                  "true true",
		`{{timeAgo (addHours 5 now)}}`:                                    "in 5 hours",
		`{{formatDate "[Week of] MMM D" (startOfWeek now)}}`:              "Week of Oct 5",
	}
	for src, want := range cases {
		got, err := Render(src, data, "record")
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", src, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	if _, err := Render(`{{divide 1 0}}`, nil); err == nil || err.Error() != "divide: division by zero" {
		t.Errorf("want a clean division error, got %v", err)
	}
	if _, err := Render(`{{tz "Mars/Base" now}}`, nil); err == nil || !strings.Contains(err.Error(), "Mars/Base") {
		t.Errorf("want an unknown zone error, got %v", err)
	}
	if _, err := Render(`{{nope 1}}`, nil); err == nil {
		t.Error("unknown functions must fail")
	}
	if _, err := Render(`{{repeat 100000 "abcdefgh"}}{{repeat 100000 "abcdefgh"}}{{repeat 100000 "abcdefgh"}}`, nil); err != ErrTooLong {
		t.Errorf("want ErrTooLong, got %v", err)
	}
	// wrong types are empty values, not errors
	for _, src := range []string{`{{round 2 "abc"}}`, `{{formatDate "YYYY" 12}}`, `{{upper nil}}`, `{{first 5}}`, `{{sum "x"}}`} {
		if _, err := Render(src, nil); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		`{{upper record.name}}`:                `{{upper $.record.name}}`,
		`{{ .record.name }} {{$.auth.id}}`:     `{{ .record.name }} {{$.auth.id}}`,
		`{{ "record.name" }} record.name`:      `{{ "record.name" }} record.name`,
		`{{ if query.q }}{{ records }}{{end}}`: `{{ if $.query.q }}{{ records }}{{end}}`,
		`{{/* record */}}{{ json record }}`:    `{{/* record */}}{{ json $.record }}`,
		`{{ .trigger.record.id }}`:             `{{ .trigger.record.id }}`,
	}
	for in, want := range cases {
		if got := Normalize(in, "record", "auth", "query"); got != want {
			t.Errorf("Normalize(%s) = %s, want %s", in, got, want)
		}
	}
}

// The functions also work in html/template (the email bodies), escaped.
func TestHTMLTemplate(t *testing.T) {
	tpl := template.Must(template.New("x").Funcs(template.FuncMap(Funcs())).Parse(`<p>{{upper .name}} {{currency "USD" .n}}</p>`))
	var b strings.Builder
	if err := tpl.Execute(&b, map[string]any{"name": "<b>x</b>", "n": 3}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "<p>&lt;B&gt;X&lt;/B&gt; $3.00</p>" {
		t.Errorf("got %s", b.String())
	}
}

func TestValue(t *testing.T) {
	data := map[string]any{"record": map[string]any{"tags": []any{"b", "a"}, "n": 2.0}}
	v, ok, err := Value(`{{ sort record.tags }}`, data, "record")
	if !ok || err != nil {
		t.Fatalf("want a value, got ok=%v err=%v", ok, err)
	}
	if l, isList := v.(List); !isList || len(l) != 2 || l[0] != "a" {
		t.Errorf("want the sorted list, got %#v", v)
	}
	if v, ok, _ := Value(`{{- .record.n | add 1 -}}`, data); !ok || v != Number(3) {
		t.Errorf("want 3, got %v (%v)", v, ok)
	}
	for _, src := range []string{`a {{.x}}`, `{{if .x}}y{{end}}`, `{{.x}}{{.y}}`, `{{/* c */}}`, `{{$x := 1}}`, `{{ "}}" }} x`} {
		if _, ok, _ := Value(src, data); ok {
			t.Errorf("%s isn't a single expression", src)
		}
	}
	if _, ok, err := Value(`{{divide 1 0}}`, nil); !ok || err == nil {
		t.Errorf("want the error, got ok=%v err=%v", ok, err)
	}
}

// Every date a function gives is stored as the same moment by a date field
// (types.ParseDateTime is what the date fields use).
func TestDatesForStorage(t *testing.T) {
	fixedNow(t)
	cases := map[string]string{
		`{{now}}`:           "2026-10-11 12:00:00.000Z",
		`{{addDays 1 now}}`: "2026-10-12 12:00:00.000Z",
		`{{tz "Europe/Paris" "2026-10-01 09:30:00.000Z"}}`:                        "2026-10-01 09:30:00.000Z",
		`{{addHours 2 (tz "America/New_York" "2026-10-01 09:30:00.000Z")}}`:       "2026-10-01 11:30:00.000Z",
		`{{parseDate "DD/MM/YYYY" "25/12/2026"}}`:                                 "2026-12-25 00:00:00.000Z",
		`{{parseDate "D/M/YYYY H:mm" "5/3/2026 9:05" "Europe/Paris"}}`:            "2026-03-05 08:05:00.000Z",
		`{{parseDate "MMMM D, YYYY h:mm A" "March 5, 2026 9:05 PM"}}`:             "2026-03-05 21:05:00.000Z",
		`{{parseDate "iso" "2026-10-01T11:30:00+02:00"}}`:                         "2026-10-01 09:30:00.000Z",
		`{{fromUnix 1791676800123}}`:                                              "2026-10-11 00:00:00.123Z",
		`{{toDate "2026-10-01 11:30"}}`:                                           "2026-10-01 11:30:00.000Z",
		`{{formatDate "storage" (tz "Europe/Paris" "2026-10-01 09:30:00.000Z")}}`: "2026-10-01 09:30:00.000Z",
		`{{startOfDay (tz "Europe/Paris" "2026-10-01 23:30:00.000Z")}}`:           "2026-10-01 22:00:00.000Z", // midnight in Paris
	}
	for src, want := range cases {
		out, err := Render(src, nil)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		d, err := types.ParseDateTime(out)
		if err != nil || d.IsZero() {
			t.Errorf("%s = %q: not a date the fields can store", src, out)
			continue
		}
		if d.String() != want {
			t.Errorf("%s = %q, stored as %s, want %s", src, out, d.String(), want)
		}
	}
	if out, _ := Render(`{{tz "Europe/Paris" "2026-10-01 09:30:00.000Z"}}`, nil); out != "2026-10-01 11:30:00.000+02:00" {
		t.Errorf("zoned dates print with their offset, got %q", out)
	}
	if _, err := Render(`{{parseDate "DD/MM/YYYY" "2026-12-25"}}`, nil); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Errorf("want a format error, got %v", err)
	}
	if out, err := Render(`{{parseDate "DD/MM/YYYY" ""}}`, nil); out != "" || err != nil {
		t.Errorf("an empty text is no date, got %q %v", out, err)
	}
}
