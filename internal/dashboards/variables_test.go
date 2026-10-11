package dashboards_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/Namarusa/PocketbasePlus/internal/dashboards"
	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

var dashboardsRegister = dashboards.Register

// fetchWithVars adds variables and panels to the fixture dashboard, then
// requests its data with the given variable values.
func fetchWithVars(t *testing.T, who string, variables []map[string]any, panels map[string]map[string]any, vars map[string]any) map[string]panelResult {
	var f *fixture
	var result struct {
		Panels map[string]panelResult `json:"panels"`
	}
	ids := map[string]string{}
	headers := map[string]string{}
	scenario := &tests.ApiScenario{
		Method:  http.MethodPost,
		Headers: headers,
		TestAppFactory: func(t testing.TB) *tests.TestApp {
			f = setup(t)
			f.dashboard.Set("variables", variables)
			if err := f.app.Save(f.dashboard); err != nil {
				t.Fatal(err)
			}
			for name, p := range panels {
				p["dashboard"] = f.dashboard.Id
				p["name"] = name
				p["x"], p["y"], p["w"], p["h"] = 0, 20, 6, 4
				ids[name] = testutil.Create(t, f.app, "dashboard_panels", p).Id
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
	scenario.BeforeTestFunc = func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
		scenario.URL = "/api/dashboards/" + f.dashboard.Id + "/data"
		headers["Authorization"] = testutil.AuthToken(t, f.user(who))
		raw, _ := json.Marshal(map[string]any{"vars": vars})
		scenario.Body = strings.NewReader(string(raw))
		headers["Content-Type"] = "application/json"
	}
	scenario.Test(t)

	out := map[string]panelResult{}
	for name, id := range ids {
		out[name] = result.Panels[id]
	}
	return out
}

var testVariables = []map[string]any{
	{"key": "status", "label": "Status", "type": "select", "options": []string{"paid", "new"}, "default": ""},
	{"key": "period", "label": "Period", "type": "date_range", "default": "7d"},
}

var varPanels = map[string]map[string]any{
	"byStatus": {"type": "metric", "options": map[string]any{"collection": "orders", "filter": `({{$status}} = "" || status = {{$status}})`}},
	"inPeriod": {"type": "metric", "options": map[string]any{"collection": "orders", "filter": `paid_at >= {{$period.from}} && paid_at < {{$period.to}}`}},
	"unknown":  {"type": "metric", "options": map[string]any{"collection": "orders", "filter": `status = {{$nope}}`}},
	"series": {"type": "time_series", "options": map[string]any{
		"collection": "orders", "dateField": "paid_at", "precision": "day", "range": "$period", "fn": "sum", "field": "total",
	}},
}

func TestDashboardVariables(t *testing.T) {
	d := fetchWithVars(t, "superuser", testVariables, varPanels, nil)
	if v := d["byStatus"].Data["value"]; v != 4.0 {
		t.Errorf("empty select = all records, want 4, got %v (%s)", v, d["byStatus"].Error)
	}
	if v := d["inPeriod"].Data["value"]; v != 3.0 {
		t.Errorf("default 7d range, want 3 paid orders, got %v (%s)", v, d["inPeriod"].Error)
	}
	if !strings.Contains(d["unknown"].Error, "unknown dashboard variable $nope") {
		t.Errorf("expected an unknown variable error, got %q", d["unknown"].Error)
	}
	if points, _ := d["series"].Data["points"].([]any); len(points) != 8 {
		t.Errorf("series on the dashboard range: want 8 days, got %d (%s)", len(points), d["series"].Error)
	}

	d = fetchWithVars(t, "superuser", testVariables, varPanels, map[string]any{"status": "paid", "period": map[string]any{"preset": "today"}})
	if v := d["byStatus"].Data["value"]; v != 3.0 {
		t.Errorf("status = paid, want 3, got %v (%s)", v, d["byStatus"].Error)
	}
	if v := d["inPeriod"].Data["value"]; v != 1.0 {
		t.Errorf("today, want 1 order, got %v (%s)", v, d["inPeriod"].Error)
	}

	// variable values are bound: they can't change the filter
	d = fetchWithVars(t, "superuser", testVariables, varPanels, map[string]any{"status": `paid" || 1=1 || "`})
	if v := d["byStatus"].Data["value"]; v != 0.0 {
		t.Errorf("injection attempt must match nothing, got %v (%s)", v, d["byStatus"].Error)
	}

	// role users still only see their records
	d = fetchWithVars(t, "sales", testVariables, varPanels, map[string]any{"status": "paid"})
	if v := d["byStatus"].Data["value"]; v != 2.0 {
		t.Errorf("role user with status = paid, want 2, got %v (%s)", v, d["byStatus"].Error)
	}
}

func TestSplitSeries(t *testing.T) {
	panels := map[string]map[string]any{
		"lines": {"type": "time_series", "options": map[string]any{
			"collection": "orders", "dateField": "paid_at", "precision": "day", "range": "7d", "splitField": "owner", "maxSeries": 1,
		}},
		"bars": {"type": "bar", "options": map[string]any{
			"collection": "orders", "groupField": "status", "splitField": "owner", "fn": "sum", "field": "total",
		}},
	}
	d := fetchWithVars(t, "superuser", nil, panels, nil)

	series, _ := d["lines"].Data["series"].([]any)
	if len(series) != 2 {
		t.Fatalf("expected the top series + Other, got %d (%s)", len(series), d["lines"].Error)
	}
	top := series[0].(map[string]any)
	other := series[1].(map[string]any)
	if top["label"] != "Sam Sales" || other["label"] != "Other" {
		t.Errorf("unexpected series labels %v / %v", top["label"], other["label"])
	}
	count := func(s map[string]any) (n float64) {
		for _, p := range s["points"].([]any) {
			n += p.(map[string]any)["y"].(float64)
		}
		return
	}
	if count(top) != 2 || count(other) != 1 {
		t.Errorf("want 2 paid orders for Sam and 1 for the others, got %v / %v", count(top), count(other))
	}

	cats, _ := d["bars"].Data["categories"].([]any)
	bars, _ := d["bars"].Data["series"].([]any)
	if len(cats) != 2 || len(bars) != 2 {
		t.Fatalf("expected 2 categories and 2 series, got %d / %d (%s)", len(cats), len(bars), d["bars"].Error)
	}
	// categories are sorted by total: paid (1040) then new (5)
	if cats[0].(map[string]any)["key"] != "paid" {
		t.Errorf("unexpected categories %v", cats)
	}
	values := map[string][]any{}
	for _, s := range bars {
		m := s.(map[string]any)
		values[m["label"].(string)] = m["values"].([]any)
	}
	if values["Sam Sales"][0] != 40.0 || values["Sam Sales"][1] != 5.0 {
		t.Errorf("unexpected Sam values %v", values["Sam Sales"])
	}
}

func TestVariablesValidation(t *testing.T) {
	app := testutil.NewApp(t)
	dashboardsRegister(app)
	col, _ := app.FindCollectionByNameOrId("dashboards")
	for name, vars := range map[string][]map[string]any{
		"invalid key":    {{"key": "1bad", "type": "text"}},
		"duplicated key": {{"key": "a", "type": "text"}, {"key": "a", "type": "select"}},
		"unknown type":   {{"key": "a", "type": "color"}},
	} {
		d := core.NewRecord(col)
		d.Set("name", "Test")
		d.Set("variables", vars)
		if err := app.Save(d); err == nil || !strings.Contains(err.Error(), "variables") {
			t.Errorf("%s: expected a variables error, got %v", name, err)
		}
	}
	d := core.NewRecord(col)
	d.Set("name", "Valid")
	d.Set("variables", []map[string]any{{"key": "period", "type": "date_range", "default": "30d"}})
	if err := app.Save(d); err != nil {
		t.Fatalf("expected valid variables, got %v", err)
	}
}
