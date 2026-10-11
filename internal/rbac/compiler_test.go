package rbac

import "testing"

func TestCompile(t *testing.T) {
	rules, err := Compile("owner", []Grant{
		{RoleId: "editor", Action: "list", Scope: ScopeAll},
		{RoleId: "author", Action: "list", Scope: ScopeOwn},
		{RoleId: "author", Action: "create", Scope: ScopeOwn},
		{RoleId: "editor", Action: "delete", Scope: ScopeAll},
		{RoleId: "author", Action: "update", Scope: ScopeOwn},
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]string{
		"list":   `(@request.auth.roles.id ?= "author" && owner = @request.auth.id) || @request.auth.roles.id ?= "editor"`,
		"create": `(@request.auth.roles.id ?= "author" && @request.body.owner = @request.auth.id)`,
		"update": `(@request.auth.roles.id ?= "author" && owner = @request.auth.id && (@request.body.owner:isset = false || @request.body.owner = @request.auth.id))`,
		"delete": `@request.auth.roles.id ?= "editor"`,
	}
	for _, action := range Actions {
		got := rules[action]
		want, ok := expected[action]
		if !ok {
			if got != nil {
				t.Errorf("%s: expected nil rule, got %q", action, *got)
			}
			continue
		}
		if got == nil || *got != want {
			t.Errorf("%s:\nwant %q\ngot  %v", action, want, deref(got))
		}
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		grant Grant
	}{
		{"own without owner field", "", Grant{RoleId: "r1", Action: "view", Scope: ScopeOwn}},
		{"invalid action", "owner", Grant{RoleId: "r1", Action: "manage", Scope: ScopeAll}},
		{"invalid scope", "owner", Grant{RoleId: "r1", Action: "view", Scope: "team"}},
		{"injected role id", "owner", Grant{RoleId: `r1" || 1=1 || "`, Action: "view", Scope: ScopeAll}},
		{"injected owner field", "owner || 1=1", Grant{RoleId: "r1", Action: "view", Scope: ScopeOwn}},
		{"create own with id owner", "id", Grant{RoleId: "r1", Action: "create", Scope: ScopeOwn}},
		{"custom without filter", "owner", Grant{RoleId: "r1", Action: "view", Scope: ScopeCustom}},
	}
	for _, c := range cases {
		if _, err := Compile(c.owner, []Grant{c.grant}); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func TestCompileCustomScope(t *testing.T) {
	rules, err := Compile("", []Grant{{RoleId: "r1", Action: "list", Scope: ScopeCustom, Filter: `status = "published"`}})
	if err != nil {
		t.Fatal(err)
	}
	want := `(@request.auth.roles.id ?= "r1" && (status = "published"))`
	if got := deref(rules["list"]); got != want {
		t.Fatalf("want %q, got %v", want, got)
	}
}
