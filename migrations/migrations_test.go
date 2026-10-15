package migrations_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/secrets"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestModuleCollectionsAreSystem(t *testing.T) {
	app := testutil.NewApp(t)

	for _, name := range []string{"roles", "permissions", "email_templates", "flows", "dashboards", "collection_groups"} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatal(err)
		}
		if !c.System {
			t.Errorf("%s: expected a system collection", name)
		}
	}

	// user collections stay regular
	users, _ := app.FindCollectionByNameOrId("users")
	if users.System {
		t.Error("users must not be a system collection")
	}

	// system collections are protected
	roles, _ := app.FindCollectionByNameOrId("roles")
	if err := app.Delete(roles); err == nil {
		t.Error("expected the deletion of a system collection to fail")
	}
	roles.Name = "renamed"
	if err := app.Save(roles); err == nil {
		t.Error("expected the rename of a system collection to fail")
	}

	// records can still be managed
	testutil.Create(t, app, "collection_groups", map[string]any{"name": "Sales", "order": 1, "collections": []string{"abc"}})
}

// The keys of the AI providers move to the secrets (the migration is run
// again on an app with a provider key).
func TestKeysMoveToSecrets(t *testing.T) {
	app := testutil.NewApp(t)
	var mig *core.Migration
	for _, item := range core.AppMigrations.Items() {
		if item.File == "1762500000_secrets.go" {
			mig = item
		}
	}
	if mig == nil {
		t.Fatal("migration not found")
	}
	if err := mig.Down(app); err != nil {
		t.Fatal(err)
	}
	p := testutil.Create(t, app, "ai_providers", map[string]any{"name": "Open Router", "kind": "openrouter", "api_key": "sk-or-1234567890"})
	testutil.Create(t, app, "ai_providers", map[string]any{"name": "open-router!", "kind": "openrouter", "api_key": "sk-or-other-123"})
	if err := mig.Up(app); err != nil {
		t.Fatal(err)
	}
	p, _ = app.FindRecordById("ai_providers", p.Id)
	if p.GetString("api_key") != "{{secret.AI_OPEN_ROUTER}}" {
		t.Errorf("api_key: %s", p.GetString("api_key"))
	}
	if v, err := secrets.Value(app, "AI_OPEN_ROUTER"); err != nil || v != "sk-or-1234567890" {
		t.Errorf("secret: %q %v", v, err)
	}
	if v, _ := secrets.Value(app, "AI_OPEN_ROUTER_2"); v != "sk-or-other-123" {
		t.Errorf("second secret: %q", v)
	}
	// and back
	if err := mig.Down(app); err != nil {
		t.Fatal(err)
	}
	p, _ = app.FindRecordById("ai_providers", p.Id)
	if p.GetString("api_key") != "sk-or-1234567890" {
		t.Errorf("restored api_key: %s", p.GetString("api_key"))
	}
}
