package dashboards_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/dashboards"
	"github.com/Namarusa/PocketbasePlus/internal/rbac"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

type fixture struct {
	app                     *tests.TestApp
	superuser, sales, other *core.Record
	dashboard               *core.Record
	panels                  map[string]*core.Record
}

// setup creates an "orders" collection whose list rule only shows the
// orders owned by the signed-in user, and a dashboard shared with "sales".
func setup(t testing.TB) *fixture {
	app := testutil.NewApp(t)
	dashboards.Register(app)
	f := &fixture{app: app, panels: map[string]*core.Record{}}

	users, _ := app.FindCollectionByNameOrId("users")
	orders := testutil.CreateCollection(t, app, "orders",
		&core.TextField{Name: "status"},
		&core.NumberField{Name: "total"},
		&core.SelectField{Name: "tags", MaxSelect: 3, Values: []string{"a", "b", "c"}},
		&core.RelationField{Name: "owner", CollectionId: users.Id, MaxSelect: 1},
		&core.TextField{Name: "secret", Hidden: true},
		&core.DateField{Name: "paid_at"},
	)
	rule := "owner = @request.auth.id"
	orders.ListRule = &rule
	if err := app.Save(orders); err != nil {
		t.Fatal(err)
	}

	sales := testutil.Create(t, app, "roles", map[string]any{"name": "sales"})
	testutil.Create(t, app, "roles", map[string]any{"name": "other"})
	f.superuser = testutil.Create(t, app, core.CollectionNameSuperusers, map[string]any{"email": "admin@test.com", "password": "1234567890"})
	f.sales = testutil.Create(t, app, "users", map[string]any{"email": "sales@test.com", "name": "Sam Sales", "password": "1234567890", "roles": []string{sales.Id}})
	f.other = testutil.Create(t, app, "users", map[string]any{"email": "other@test.com", "password": "1234567890"})

	day := func(d int) string { return types.NowDateTime().Add(-time.Duration(d) * 24 * time.Hour).String() }
	for _, o := range []map[string]any{
		{"status": "paid", "total": 10, "tags": []string{"a", "b"}, "owner": f.sales.Id, "paid_at": day(0)},
		{"status": "paid", "total": 30, "tags": []string{"a"}, "owner": f.sales.Id, "paid_at": day(2)},
		{"status": "new", "total": 5, "owner": f.sales.Id},
		{"status": "paid", "total": 1000, "tags": []string{"c"}, "owner": f.other.Id, "paid_at": day(1)},
	} {
		testutil.Create(t, app, "orders", o)
	}

	f.dashboard = testutil.Create(t, app, "dashboards", map[string]any{"name": "Sales", "roles": []string{sales.Id}})
	for name, p := range map[string]map[string]any{
		"count":  {"type": "metric", "options": map[string]any{"collection": "orders"}},
		"sum":    {"type": "metric", "options": map[string]any{"collection": "orders", "fn": "sum", "field": "total", "filter": "status = 'paid'"}},
		"status": {"type": "pie", "options": map[string]any{"collection": "orders", "groupField": "status"}},
		"tags":   {"type": "bar", "options": map[string]any{"collection": "orders", "groupField": "tags", "order": "label"}},
		"owners": {"type": "bar", "options": map[string]any{"collection": "orders", "groupField": "owner", "fn": "sum", "field": "total"}},
		"series": {"type": "time_series", "options": map[string]any{"collection": "orders", "dateField": "paid_at", "precision": "day", "range": "7d", "fn": "sum", "field": "total"}},
		"list":   {"type": "list", "options": map[string]any{"collection": "orders", "fields": []string{"status", "total", "owner"}, "sort": "-total", "limit": 2}},
		"hidden": {"type": "metric", "options": map[string]any{"collection": "orders", "fn": "count_distinct", "field": "secret"}},
		"label":  {"type": "label", "options": map[string]any{"text": "Hello"}},
	} {
		p["dashboard"] = f.dashboard.Id
		p["name"] = name
		p["x"], p["y"], p["w"], p["h"] = 0, 0, 6, 4
		f.panels[name] = testutil.Create(t, app, "dashboard_panels", p)
	}
	return f
}

type panelResult struct {
	Data  map[string]any `json:"data"`
	Error string         `json:"error"`
}

// user returns a fixture user by name.
func (f *fixture) user(who string) *core.Record {
	switch who {
	case "superuser":
		return f.superuser
	case "sales":
		return f.sales
	}
	return f.other
}

// fetchData requests the dashboard data as a user of a fresh fixture
// (optionally changed first) and returns it by panel name.
func fetchData(t *testing.T, who string, change func(f *fixture)) map[string]panelResult {
	var f *fixture
	var result struct {
		Panels map[string]panelResult `json:"panels"`
	}
	headers := map[string]string{}
	scenario := &tests.ApiScenario{
		Method:  http.MethodPost,
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			if change != nil {
				change(f)
			}
			return f.app
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"panels"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
		},
	}
	// the URL and token depend on the fixture: ApiScenario reads them after BeforeTestFunc
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		scenario.URL = "/api/dashboards/" + f.dashboard.Id + "/data"
		headers["Authorization"] = testutil.AuthToken(t, f.user(who))
	}
	scenario.Test(t)

	byName := map[string]panelResult{}
	for name, p := range f.panels {
		byName[name] = result.Panels[p.Id]
	}
	return byName
}

// request runs an API scenario against a fresh fixture.
func request(t *testing.T, name, method, who string, url func(f *fixture) string, body string, status int, expected ...string) {
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
		headers["Authorization"] = testutil.AuthToken(t, f.user(who))
	}
	scenario.Test(t)
}

func groupsOf(t testing.TB, r panelResult) map[string]float64 {
	t.Helper()
	if r.Error != "" {
		t.Fatalf("unexpected error %q", r.Error)
	}
	out := map[string]float64{}
	for _, g := range r.Data["groups"].([]any) {
		m := g.(map[string]any)
		out[m["label"].(string)] = m["value"].(float64)
	}
	return out
}

func TestSuperuserSeesAllData(t *testing.T) {
	d := fetchData(t, "superuser", nil)

	if v := d["count"].Data["value"]; v != 4.0 {
		t.Errorf("count: want 4, got %v (%s)", v, d["count"].Error)
	}
	if v := d["sum"].Data["value"]; v != 1040.0 {
		t.Errorf("sum of paid: want 1040, got %v (%s)", v, d["sum"].Error)
	}
	if g := groupsOf(t, d["status"]); g["paid"] != 3 || g["new"] != 1 {
		t.Errorf("status groups: %v", g)
	}
	// multiple select values are counted once per value
	if g := groupsOf(t, d["tags"]); g["a"] != 2 || g["b"] != 1 || g["c"] != 1 || len(g) != 3 {
		t.Errorf("tags groups: %v", g)
	}
	// relation groups are labelled with the related record name
	if g := groupsOf(t, d["owners"]); g["Sam Sales"] != 45 {
		t.Errorf("owner groups: %v", g)
	}
	if d["hidden"].Error != "" {
		t.Errorf("superusers can use hidden fields, got %q", d["hidden"].Error)
	}

	points, _ := d["series"].Data["points"].([]any)
	if len(points) != 8 { // 7 days back + today, empty days included
		t.Errorf("expected 8 daily points, got %d (%s)", len(points), d["series"].Error)
	}
	var total float64
	for _, p := range points {
		total += p.(map[string]any)["y"].(float64)
	}
	if total != 1040 {
		t.Errorf("series total: want 1040, got %v", total)
	}

	items, _ := d["list"].Data["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["total"] != 1000.0 {
		t.Errorf("list: want the 2 biggest orders, got %v (%s)", items, d["list"].Error)
	}
	if d["label"].Error != "" {
		t.Errorf("label: %s", d["label"].Error)
	}
}

func TestRoleUserSeesOnlyListableData(t *testing.T) {
	d := fetchData(t, "sales", nil)

	if v := d["count"].Data["value"]; v != 3.0 {
		t.Errorf("count: the list rule must apply, want 3, got %v (%s)", v, d["count"].Error)
	}
	if v := d["sum"].Data["value"]; v != 40.0 {
		t.Errorf("sum: want 40, got %v (%s)", v, d["sum"].Error)
	}
	if g := groupsOf(t, d["tags"]); g["c"] != 0 {
		t.Errorf("tags of other users' orders must not show: %v", g)
	}
	items, _ := d["list"].Data["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["total"] != 30.0 || items[0].(map[string]any)["owner"] != "Sam Sales" {
		t.Errorf("list: want own orders with readable owner, got %v (%s)", items, d["list"].Error)
	}
	if !strings.Contains(d["hidden"].Error, "unknown field") {
		t.Errorf("hidden fields must not be available to role users, got %q", d["hidden"].Error)
	}

	// collections without a list rule are superusers only
	d = fetchData(t, "sales", func(f *fixture) {
		orders, _ := f.app.FindCollectionByNameOrId("orders")
		orders.ListRule = nil
		if err := f.app.Save(orders); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(d["count"].Error, "access") {
		t.Errorf("expected an access error, got %+v", d["count"])
	}
}

func TestDashboardAccess(t *testing.T) {
	dashboard := func(f *fixture) string { return "/api/dashboards/" + f.dashboard.Id }
	request(t, "user without the role", http.MethodGet, "other", dashboard, "", 404, `"data":{}`)
	request(t, "user with the role", http.MethodGet, "sales", dashboard, "", 200, `"name":"Sales"`, `"panels":[`)
	request(t, "data of a dashboard without the role", http.MethodPost, "other", func(f *fixture) string { return dashboard(f) + "/data" }, "", 404, `"data":{}`)
	shared := func(*fixture) string { return "/api/dashboards/shared" }
	request(t, "shared list hides other dashboards", http.MethodGet, "other", shared, "", 200, `[]`)
	request(t, "shared list of a role user", http.MethodGet, "sales", shared, "", 200, `"name":"Sales"`)
}

func TestPreviewAndSavePanels(t *testing.T) {
	preview := func(*fixture) string { return "/api/dashboards/preview" }
	request(t, "invalid filter is reported", http.MethodPost, "superuser", preview,
		`{"type":"metric","options":{"collection":"orders","filter":"total >"}}`, 200, `"error":"invalid filter`)
	request(t, "role users can't preview", http.MethodPost, "sales", preview,
		`{"type":"metric","options":{"collection":"orders"}}`, 403, `"data":{}`)
	request(t, "internal collections can't be queried", http.MethodPost, "superuser", preview,
		`{"type":"metric","options":{"collection":"_superusers"}}`, 200, `"error":"unknown collection`)

	var f *fixture
	headers := map[string]string{}
	body := &strings.Builder{}
	scenario := &tests.ApiScenario{
		Name:    "save replaces the panels",
		Method:  http.MethodPut,
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			return f.app
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{`"name":"moved"`, `"name":"new"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			panels, _ := app.FindAllRecords("dashboard_panels")
			if len(panels) != 2 {
				t.Fatalf("expected 2 panels after save, got %d", len(panels))
			}
		},
	}
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		scenario.URL = "/api/dashboards/" + f.dashboard.Id + "/panels"
		headers["Authorization"] = testutil.AuthToken(t, f.superuser)
		body.WriteString(`{"panels":[{"id":"` + f.panels["count"].Id + `","type":"metric","name":"moved","x":6,"y":2,"w":6,"h":3,"options":{"collection":"orders"}},` +
			`{"type":"label","name":"new","x":0,"y":0,"w":24,"h":2,"options":{"text":"hi"}}]}`)
		scenario.Body = strings.NewReader(body.String())
	}
	scenario.Test(t)

	request(t, "panels must fit in the grid", http.MethodPut, "superuser", func(f *fixture) string { return "/api/dashboards/" + f.dashboard.Id + "/panels" },
		`{"panels":[{"type":"label","x":20,"y":0,"w":6,"h":2}]}`, 400, `fit in the grid`)
}

func TestFieldPermissions(t *testing.T) {
	// sales can only read the status and owner of the orders (RBAC field permissions)
	limit := func(f *fixture) {
		rbac.Register(f.app) // its hooks refresh the cached permissions
		orders, _ := f.app.FindCollectionByNameOrId("orders")
		sales := f.sales.GetStringSlice("roles")[0]
		testutil.Create(t, f.app, "rbac_collections", map[string]any{"collection": orders.Id, "managed": true, "owner_field": "owner"})
		testutil.Create(t, f.app, "permissions", map[string]any{"role": sales, "collection": orders.Id, "action": "list", "scope": "all", "fields": []string{"status", "owner"}})
	}
	data := fetchData(t, "sales", limit)
	if data["sum"].Error == "" || data["series"].Error == "" || data["list"].Error == "" {
		t.Errorf("the total isn't readable by sales: %+v %+v %+v", data["sum"], data["series"], data["list"])
	}
	if data["status"].Error != "" || data["count"].Error != "" {
		t.Errorf("readable fields must work: %+v %+v", data["status"], data["count"])
	}
	if data := fetchData(t, "superuser", limit); data["sum"].Error != "" {
		t.Errorf("superusers aren't limited: %+v", data["sum"])
	}
}
