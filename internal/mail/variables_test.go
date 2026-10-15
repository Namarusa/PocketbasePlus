package mail

import (
	"strings"
	"testing"
)

func TestTemplateVariables(t *testing.T) {
	vars, err := TemplateVariables(
		"Hello {{.username}}",
		`<p>{{.record.title}} {{default "x" .record.status}} {{.app.name}} {{.recipient.email}}</p>
		{{if .vip}}VIP{{end}}{{range .items}}{{.name}}{{end}}{{upper .company.name}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(vars, ",")
	want := "username,record.title,record.status,vip,items,company.name"
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
}

func TestSecretsAreRefused(t *testing.T) {
	for _, parts := range [][3]string{{"Key {{secret.KEY}}", "", ""}, {"", "<p>{{secret.KEY}}</p>", ""}, {"", "", "{{upper secret.KEY}}"}} {
		if err := Validate(parts[0], parts[1], parts[2]); err == nil || !strings.Contains(err.Error(), "secrets can only be used") {
			t.Errorf("%v: %v", parts, err)
		}
	}
	if _, err := TemplateVariables("{{secret.KEY}}", "", ""); err == nil || !strings.Contains(err.Error(), "secrets can only be used") {
		t.Errorf("variables: %v", err)
	}
}
