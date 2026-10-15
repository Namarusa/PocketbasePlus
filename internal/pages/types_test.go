package pages

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestPageTypes(t *testing.T) {
	f := setup(t)
	col, _ := f.app.FindCollectionByNameOrId(Collection)
	save := func(data map[string]any) (*core.Record, error) {
		r := core.NewRecord(col)
		r.Load(map[string]any{"title": "T", "published": true, "html": "<p>x</p>"})
		r.Load(data)
		return r, f.app.Save(r)
	}
	if _, err := save(map[string]any{"slug": "c1", "kind": kindCollection}); err == nil || !strings.Contains(err.Error(), "list_collection") {
		t.Errorf("a collection page without its collection: %v", err)
	}
	r, err := save(map[string]any{"slug": "c2", "kind": kindCollection, "list_collection": "products"})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetString("list_collection") != f.products.Id || pageKind(r) != "collection" {
		t.Errorf("collection page: %s %s", r.GetString("list_collection"), pageKind(r))
	}
	if _, err := save(map[string]any{"slug": "c3", "kind": kindCollection, "list_collection": f.products.Id, "record_collection": f.products.Id}); err == nil {
		t.Error("a collection page can't be a single item page")
	}
	r, err = save(map[string]any{"slug": "s1", "list_collection": f.products.Id})
	if err != nil || r.GetString("list_collection") != "" {
		t.Errorf("a single page keeps no list collection: %v %q", err, r.GetString("list_collection"))
	}
	// served like a single page
	if out := f.render(t, `<body><h1>Shop</h1></body>`, scope{}, nil); !strings.Contains(out, "<h1>Shop</h1>") {
		t.Error("render")
	}
}
