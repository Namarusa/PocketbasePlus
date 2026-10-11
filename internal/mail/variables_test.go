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
