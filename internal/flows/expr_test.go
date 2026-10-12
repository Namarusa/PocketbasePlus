package flows

import (
	"fmt"
	"testing"
)

func TestResolveValue(t *testing.T) {
	data := map[string]any{
		"trigger": map[string]any{"record": map[string]any{"title": "Hi", "total": 42.0, "tags": []any{"a", "b"}}},
	}
	if v, _ := resolveValue("{{ .trigger.record.total }}", data); v != 42.0 {
		t.Errorf("single variable must keep its type, got %#v", v)
	}
	if v, _ := resolveValue("{{.trigger.record.tags}}", data); len(v.([]any)) != 2 {
		t.Errorf("lists must be kept, got %#v", v)
	}
	if v, _ := resolveValue("Title: {{.trigger.record.title}}!", data); v != "Title: Hi!" {
		t.Errorf("unexpected string %#v", v)
	}
	if v, _ := resolveValue(map[string]any{"a": "{{.trigger.record.title}}", "b": 1.0}, data); v.(map[string]any)["a"] != "Hi" {
		t.Errorf("maps must be resolved recursively, got %#v", v)
	}
	if v, _ := resolveValue("{{.missing.path}}", data); v != nil {
		t.Errorf("missing path must be nil, got %#v", v)
	}
	// a function giving a list is a list (e.g. the items of a "For each")
	if v, _ := resolveValue(`{{ reverse .trigger.record.tags }}`, data); len(v.([]any)) != 2 || v.([]any)[0] != "b" {
		t.Errorf("function lists must be kept, got %#v", v)
	}
	// other results are texts, as before
	if v, _ := resolveValue(`{{ add .trigger.record.total 1 }}`, data); v != "43" {
		t.Errorf("want the text 43, got %#v", v)
	}
	if v, _ := resolveValue(`{{ formatDate "DD/MM/YYYY" "2026-10-01" }}`, data); v != "01/10/2026" {
		t.Errorf("want the formatted date, got %#v", v)
	}
}

func TestBindFilterPreventsInjection(t *testing.T) {
	data := map[string]any{"trigger": map[string]any{"q": `x" || id != "`}}
	filter, params, err := bindFilter(`title = {{.trigger.q}} && status = "ok"`, data)
	if err != nil {
		t.Fatal(err)
	}
	if filter != `title = {:flowp0} && status = "ok"` || params["flowp0"] != `x" || id != "` {
		t.Fatalf("unexpected %q %#v", filter, params)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		l    any
		op   string
		r    any
		want bool
	}{
		{"10", "gt", "9", true}, // numeric, not lexicographic
		{42.0, "eq", "42", true},
		{"paid", "neq", "new", true},
		{[]any{"a", "b"}, "contains", "b", true},
		{"", "empty", nil, true},
		{"2026-01-02", "gt", "2025-12-31", true},
		{"hello", "starts", "he", true},
	}
	for _, c := range cases {
		got, err := compare(c.l, c.op, c.r)
		if err != nil || got != c.want {
			t.Errorf("%v %s %v: want %v got %v (%v)", c.l, c.op, c.r, c.want, got, err)
		}
	}
}

// the roots without their dot give the same results as with it
func TestShortNotation(t *testing.T) {
	data := map[string]any{
		"trigger": map[string]any{"record": map[string]any{"title": "Hi", "total": 42.0, "tags": []any{"a", "b"}}},
		"steps":   map[string]any{"api": map[string]any{"body": map[string]any{"cards": []any{map[string]any{"name": "Pikachu"}}}}},
		"item":    map[string]any{"name": "pika"},
		"index":   1,
		"now":     "2026-10-12T10:00:00Z",
		"last":    "previous",
	}
	same := [][2]string{
		{"{{trigger.record.total}}", "{{.trigger.record.total}}"},
		{"{{ steps.api.body.cards }}", "{{.steps.api.body.cards}}"},
		{"{{index steps.api.body.cards 0}}", "{{index .steps.api.body.cards 0}}"},
		{"Hi {{upper item.name}}!", "Hi {{upper .item.name}}!"},
		{"{{item}}", "{{.item}}"},
		{`{{if eq trigger.record.title "Hi"}}yes{{end}}`, `{{if eq .trigger.record.title "Hi"}}yes{{end}}`},
		{"{{range steps.api.body.cards}}{{.name}}{{end}}", "{{range .steps.api.body.cards}}{{.name}}{{end}}"},
		{"{{reverse trigger.record.tags}}", "{{reverse .trigger.record.tags}}"},
		{"{{.index}}-{{.now}}-{{.last}}", "{{.index}}-{{.now}}-{{.last}}"},
		{`{{"item.name"}}`, `{{"item.name"}}`}, // quoted text isn't a reference
	}
	for _, c := range same {
		short, err1 := resolveValue(c[0], data)
		long, err2 := resolveValue(c[1], data)
		if err1 != nil || err2 != nil || fmt.Sprint(short) != fmt.Sprint(long) {
			t.Errorf("%s: %#v (%v), want %#v (%v)", c[0], short, err1, long, err2)
		}
	}
	// a single reference keeps its type
	if v, _ := resolveValue("{{trigger.record.total}}", data); v != 42.0 {
		t.Errorf("want 42.0, got %#v", v)
	}
	// "last" is a function: the short form calls it
	if v, _ := resolveValue("{{last trigger.record.tags}}", data); v != "b" {
		t.Errorf("want b, got %#v", v)
	}
	if err := validateTemplates(map[string]any{"a": "{{upper item.name}}"}); err != nil {
		t.Errorf("short form must validate: %v", err)
	}
	// filters bind the short form as a parameter too
	if f, p, err := bindFilter("title = {{trigger.record.title}}", data); err != nil || f != "title = {:flowp0}" || p["flowp0"] != "Hi" {
		t.Errorf("filter: %q %#v %v", f, p, err)
	}
}
