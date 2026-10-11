package mail

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	data := map[string]any{
		"record": map[string]any{"name": `<script>alert(1)</script>`, "created": "2026-10-03 12:30:00.000Z"},
		"app":    map[string]any{"name": "Acme"},
	}
	out, err := Render(
		"Hello\n{{.record.name}} from {{.app.name}}",
		`<p>{{.record.name}}</p><p>{{date "2006-01-02" .record.created}}</p><p>{{.record.missing}}</p>`,
		`Hi {{default "there" .record.missing}}`,
		data,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.HTML, "<script>") {
		t.Errorf("html values must be escaped, got %s", out.HTML)
	}
	if !strings.Contains(out.HTML, "<p>2026-10-03</p>") {
		t.Errorf("date helper failed, got %s", out.HTML)
	}
	if strings.Contains(out.HTML, "no value") || strings.Contains(out.Text, "no value") {
		t.Errorf("missing values must render empty, got %q / %q", out.HTML, out.Text)
	}
	if out.Text != "Hi there" {
		t.Errorf("unexpected text %q", out.Text)
	}
	if strings.Contains(out.Subject, "\n") {
		t.Errorf("subject must be a single line, got %q", out.Subject)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("ok", "<p>{{.record.name</p>", ""); err == nil {
		t.Fatal("expected parse error")
	}
	if err := Validate("{{if .x}}", "", ""); err == nil {
		t.Fatal("expected parse error for unclosed if")
	}
}

func TestParseAddresses(t *testing.T) {
	got := ParseAddresses("a@x.com, B@x.com;b@x.com\ninvalid  c@x.com")
	want := []string{"a@x.com", "B@x.com", "c@x.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestTitleHelper(t *testing.T) {
	title := funcs["title"].(func(any) string)
	for in, want := range map[string]string{"élodie": "Élodie", "ada": "Ada", "": "", "1st": "1st"} {
		if got := title(in); got != want {
			t.Errorf("title(%q): want %q, got %q", in, want, got)
		}
	}
}
