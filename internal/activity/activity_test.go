package activity_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/activity"
	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app          *tests.TestApp
	engine       *flows.Engine
	admin, other *core.Record
	user         *core.Record
	order        *core.Record
}

// setup creates an "orders" collection writable by any signed-in user.
func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	activity.Register(app)
	m := mail.Register(app)
	m.DisableKick = true
	f := &fixture{app: app, engine: flows.Register(app, m)}
	f.engine.Sync = true

	orders := testutil.CreateCollection(t, app, "orders",
		&core.TextField{Name: "title"},
		&core.NumberField{Name: "total"},
	)
	rule := "@request.auth.id != ''"
	orders.ListRule, orders.ViewRule, orders.CreateRule, orders.UpdateRule, orders.DeleteRule = &rule, &rule, &rule, &rule, &rule
	if err := app.Save(orders); err != nil {
		t.Fatal(err)
	}

	f.admin = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	f.other = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "other@test.com", "password": "1234567890"})
	f.user = testutil.Create(t, app, "users", map[string]any{"email": "user@test.com", "password": "1234567890"})
	f.order = testutil.Create(t, app, "orders", map[string]any{"title": "first", "total": 10})
	return f
}

func revisions(t testing.TB, app core.App, recordId string) []*core.Record {
	t.Helper()
	// insertion order (several revisions can share the same millisecond)
	list := []*core.Record{}
	err := app.RecordQuery("revisions").AndWhere(dbx.HashExp{"record_id": recordId}).OrderBy("rowid ASC").All(&list)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// call runs an API request against a fresh fixture, with a token of one of its users.
func call(t *testing.T, name, method string, who func(f *fixture) *core.Record, url func(f *fixture) string, body string, status int, expected []string, after func(t testing.TB, f *fixture)) {
	var f *fixture
	headers := map[string]string{}
	scenario := &tests.ApiScenario{
		Name:    name,
		Method:  method,
		Body:    strings.NewReader(body),
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			return f.app
		},
		ExpectedStatus:  status,
		ExpectedContent: expected,
	}
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		scenario.URL = url(f)
		headers["Authorization"] = testutil.AuthToken(t, who(f))
	}
	if after != nil {
		scenario.AfterTestFunc = func(t testing.TB, app *tests.TestApp, res *http.Response) { after(t, f) }
	}
	scenario.Test(t)
}

func TestRevisionsRecordTheAuthorAndChanges(t *testing.T) {
	asUser := func(f *fixture) *core.Record { return f.user }
	asAdmin := func(f *fixture) *core.Record { return f.admin }
	orderUrl := func(f *fixture) string { return "/api/collections/orders/records/" + f.order.Id }

	call(t, "update by a user", http.MethodPatch, asUser, orderUrl, `{"total": 25}`, 200, []string{`"total":25`},
		func(t testing.TB, f *fixture) {
			revs := revisions(t, f.app, f.order.Id)
			if len(revs) != 2 || revs[0].GetString("action") != "create" || revs[0].GetString("actor_type") != "system" {
				t.Fatalf("expected a system create revision then the update, got %d", len(revs))
			}
			upd := revs[1]
			if upd.GetString("action") != "update" || upd.GetString("actor_type") != "user" || upd.GetString("actor_label") != "user@test.com" {
				t.Fatalf("unexpected update revision %v", upd.FieldsData())
			}
			var delta map[string][]any
			_ = upd.UnmarshalJSONField("delta", &delta)
			if len(delta) != 1 || delta["total"][0] != 10.0 || delta["total"][1] != 25.0 {
				t.Fatalf("unexpected delta %v", delta)
			}
		})

	call(t, "no-op update has no revision", http.MethodPatch, asAdmin, orderUrl, `{"title": "first"}`, 200, []string{`"title":"first"`},
		func(t testing.TB, f *fixture) {
			if revs := revisions(t, f.app, f.order.Id); len(revs) != 1 {
				t.Fatalf("expected only the create revision, got %d", len(revs))
			}
		})

	call(t, "delete by a superuser", http.MethodDelete, asAdmin, orderUrl, ``, 204, nil,
		func(t testing.TB, f *fixture) {
			revs := revisions(t, f.app, f.order.Id)
			last := revs[len(revs)-1]
			var data map[string]any
			_ = last.UnmarshalJSONField("data", &data)
			if last.GetString("action") != "delete" || last.GetString("actor_type") != "superuser" || data["title"] != "first" {
				t.Fatalf("expected a superuser delete revision with the last state, got %v", last.FieldsData())
			}
		})
}

func TestFlowChangesAreAttributedToTheFlow(t *testing.T) {
	f := setup(t)
	flow := testutil.Create(t, f.app, "flows", map[string]any{
		"name": "Bump", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{{"key": "upd", "type": "update_record", "config": map[string]any{
			"collection": "orders", "recordId": f.order.Id, "data": map[string]any{"total": 99},
		}}},
	})
	if res := f.engine.Run(flow, nil, flows.RunOptions{}); res.Status != "success" {
		t.Fatalf("flow failed: %s", res.Error)
	}
	revs := revisions(t, f.app, f.order.Id)
	last := revs[len(revs)-1]
	if last.GetString("actor_type") != "flow" || last.GetString("actor_label") != "Flow: Bump" {
		t.Fatalf("expected the flow as author, got %v", last.FieldsData())
	}
}

func TestRevertAndRestore(t *testing.T) {
	asAdmin := func(f *fixture) *core.Record { return f.admin }

	// revert to the create revision after an update
	call(t, "revert", http.MethodPost, asAdmin, func(f *fixture) string {
		f.order.Set("title", "changed")
		if err := f.app.Save(f.order); err != nil {
			t.Fatal(err)
		}
		return "/api/activity/revisions/" + revisions(t, f.app, f.order.Id)[0].Id + "/revert"
	}, ``, 200, []string{`"restored":false`, `"title":"first"`}, func(t testing.TB, f *fixture) {
		revs := revisions(t, f.app, f.order.Id)
		if last := revs[len(revs)-1]; last.GetString("action") != "revert" || last.GetString("actor_type") != "superuser" {
			t.Fatalf("expected a revert revision, got %v", last.FieldsData())
		}
	})

	// restore a deleted record with the same id
	call(t, "restore", http.MethodPost, asAdmin, func(f *fixture) string {
		if err := f.app.Delete(f.order); err != nil {
			t.Fatal(err)
		}
		revs := revisions(t, f.app, f.order.Id)
		return "/api/activity/revisions/" + revs[len(revs)-1].Id + "/revert"
	}, ``, 200, []string{`"restored":true`}, func(t testing.TB, f *fixture) {
		rec, err := f.app.FindRecordById("orders", f.order.Id)
		if err != nil || rec.GetString("title") != "first" {
			t.Fatalf("expected the record to be restored, got %v", err)
		}
	})

	call(t, "users can't revert", http.MethodPost, func(f *fixture) *core.Record { return f.user },
		func(f *fixture) string {
			return "/api/activity/revisions/" + revisions(t, f.app, f.order.Id)[0].Id + "/revert"
		},
		``, 403, []string{`"data":{}`}, nil)
}

func TestRetentionCleanup(t *testing.T) {
	f := setup(t)
	if _, err := f.app.DB().NewQuery("UPDATE revisions SET created = '2020-01-01 00:00:00.000Z'").Execute(); err != nil {
		t.Fatal(err)
	}
	testutil.Create(t, f.app, "orders", map[string]any{"title": "recent"})
	if err := activity.Cleanup(f.app, 30); err != nil {
		t.Fatal(err)
	}
	all, _ := f.app.FindAllRecords("revisions")
	if len(all) != 1 {
		t.Fatalf("expected only the recent revision, got %d", len(all))
	}
}

func TestCommentsMentionsAndOwnership(t *testing.T) {
	var f *fixture
	headers := map[string]string{}
	scenario := &tests.ApiScenario{
		Name:    "mention notifies",
		Method:  http.MethodPost,
		URL:     "/api/collections/comments/records",
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			return f.app
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"author_label":"admin@test.com"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			list, _ := app.FindAllRecords("notifications")
			if len(list) != 1 || list[0].GetString("recipient") != f.other.Id || list[0].GetString("kind") != "mention" {
				t.Fatalf("expected one mention notification for the other superuser, got %d", len(list))
			}
		},
	}
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		headers["Authorization"] = testutil.AuthToken(t, f.admin)
		scenario.Body = strings.NewReader(`{"collection":"orders","record_id":"` + f.order.Id + `","text":"Look @other","author_id":"spoofed","mentions":["` + f.other.Id + `","notasuperuser"]}`)
	}
	scenario.Test(t)

	call(t, "only the author can edit", http.MethodPatch, func(f *fixture) *core.Record { return f.other }, func(f *fixture) string {
		c := testutil.Create(t, f.app, "comments", map[string]any{"collection": "orders", "record_id": f.order.Id, "text": "hi", "author_id": f.admin.Id})
		return "/api/collections/comments/records/" + c.Id
	}, `{"text":"hacked"}`, 403, []string{`"data":{}`}, nil)
}

func TestNotifyFlowStep(t *testing.T) {
	f := setup(t)
	flow := testutil.Create(t, f.app, "flows", map[string]any{
		"name": "Alert", "active": true, "trigger_type": "manual", "trigger": map[string]any{},
		"steps": []map[string]any{{"key": "n", "type": "notify", "config": map[string]any{
			"to": "all", "subject": "Big order {{.trigger.payload.total}}", "link": "#/collections?collection=orders",
		}}},
	})
	res := f.engine.Run(flow, map[string]any{"payload": map[string]any{"total": 500}}, flows.RunOptions{})
	if res.Status != "success" {
		t.Fatalf("flow failed: %s", res.Error)
	}
	list, _ := f.app.FindAllRecords("notifications")
	if len(list) != 2 || list[0].GetString("subject") != "Big order 500" {
		t.Fatalf("expected a notification per superuser, got %d", len(list))
	}
}
