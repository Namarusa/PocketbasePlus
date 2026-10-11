package flows

import "testing"

func TestSafeTemplates(t *testing.T) {
	data := map[string]any{"trigger": map[string]any{"form": map[string]any{
		"name": `x","admin":true,"y":"`, "q": "a&b=c", "n": 3.5, "tags": []any{"a", "b"},
	}}, "base": "https://api.test/v1"}

	u, err := renderURL("{{.base}}/users/{{.trigger.form.q}}?search={{.trigger.form.q}}&n={{.trigger.form.n}}", data)
	if err != nil || u != "https://api.test/v1/users/a&b=c?search=a%26b%3Dc&n=3.5" {
		t.Errorf("unexpected url %q %v", u, err)
	}

	body, err := renderJSONBody(`{"name": "Hi {{.trigger.form.name}}", "n": {{.trigger.form.n}}, "tags": {{.trigger.form.tags}}, "raw": {{json .trigger.form.tags}}}`, data)
	want := `{"name": "Hi x\",\"admin\":true,\"y\":\"", "n": 3.5, "tags": ["a","b"], "raw": ["a","b"]}`
	if err != nil || body != want {
		t.Errorf("unexpected body\n got %s\nwant %s (%v)", body, want, err)
	}

	// not JSON: as written
	if body, _ := renderJSONBody("name={{.trigger.form.q}}", data); body != "name=a&b=c" {
		t.Errorf("unexpected text body %q", body)
	}
	// blocks: as written
	if body, _ := renderJSONBody(`{"a": "{{if .trigger.form.q}}yes{{end}}"}`, data); body != `{"a": "yes"}` {
		t.Errorf("unexpected block body %q", body)
	}
}

func TestActionEmailLimit(t *testing.T) {
	ip := "203.0.113.9"
	for i := range 2 {
		if !allowActionEmails(ip, 5) {
			t.Fatalf("batch %d: expected to be allowed", i)
		}
	}
	if allowActionEmails(ip, 1) {
		t.Error("expected the hourly limit")
	}
	if !allowActionEmails("203.0.113.10", 1) {
		t.Error("other visitors aren't limited")
	}
}
