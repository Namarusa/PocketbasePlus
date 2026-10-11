// Package testutil provides helpers to run the modules against a fresh,
// fully migrated PocketBase instance.
package testutil

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	_ "github.com/Namarusa/PocketbasePlus/migrations"
)

// NewApp creates an empty test app with the system and project migrations
// applied. It is cleaned up automatically at the end of the test.
func NewApp(t testing.TB) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// Create saves a new record with the provided data.
func Create(t testing.TB, app core.App, collection string, data map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRecord(col)
	r.Load(data)
	if pass, ok := data["password"].(string); ok {
		r.SetPassword(pass)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("failed to create %s record: %v", collection, err)
	}
	return r
}

// CreateCollection saves a new base collection with the provided fields.
func CreateCollection(t testing.TB, app core.App, name string, fields ...core.Field) *core.Collection {
	t.Helper()
	c := core.NewBaseCollection(name)
	c.Fields.Add(fields...)
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}
	return c
}

// AuthToken returns an auth token for the record.
func AuthToken(t testing.TB, r *core.Record) string {
	t.Helper()
	token, err := r.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	return token
}
