package migrations_test

import (
	"testing"

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
