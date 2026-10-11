package flows

import (
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
