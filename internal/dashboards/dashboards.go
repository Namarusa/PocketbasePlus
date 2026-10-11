// Package dashboards implements insights dashboards (in the spirit of
// Directus Insights): panels showing metrics, charts and lists computed from
// any collection. Superusers build them in the dashboard; they can be shared
// read-only with roles, whose users only see the data the collections' list
// rules allow them.
package dashboards

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	dashboardsCollection = "dashboards"
	panelsCollection     = "dashboard_panels"

	GridColumns = 24  // grid width in units
	maxRows     = 200 // max panel height in units
	maxPanels   = 100 // per dashboard
)

// Register binds the dashboards hooks and routes.
func Register(app core.App) {
	app.OnRecordValidate(panelsCollection).BindFunc(func(e *core.RecordEvent) error {
		if err := validatePanel(e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordValidate(dashboardsCollection).BindFunc(func(e *core.RecordEvent) error {
		if err := validateVariables(variablesOf(e.Record)); err != nil {
			return validation.Errors{"variables": validation.NewError("invalid_variables", err.Error())}
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		g := se.Router.Group("/api/dashboards")
		g.Bind(apis.RequireAuth())
		g.GET("/shared", listShared)
		g.GET("/{id}", getDashboard)
		g.POST("/{id}/data", dashboardData)
		g.POST("/preview", preview).Bind(apis.RequireSuperuserAuth())
		g.PUT("/{id}/panels", savePanels).Bind(apis.RequireSuperuserAuth())

		registerViewer(se)
		return se.Next()
	})
}

// --- access ---------------------------------------------------------------------

// canView reports whether the authenticated record can view the dashboard:
// superusers always, other users when they have one of its roles.
func canView(auth *core.Record, dashboard *core.Record) bool {
	if auth == nil {
		return false
	}
	if auth.IsSuperuser() {
		return true
	}
	if auth.Collection().Fields.GetByName("roles") == nil {
		return false
	}
	userRoles := auth.GetStringSlice("roles")
	for _, r := range dashboard.GetStringSlice("roles") {
		if slices.Contains(userRoles, r) {
			return true
		}
	}
	return false
}

func findViewable(e *core.RequestEvent) (*core.Record, error) {
	d, err := e.App.FindRecordById(dashboardsCollection, e.Request.PathValue("id"))
	if err != nil || !canView(e.Auth, d) {
		// same response for missing and forbidden dashboards
		return nil, e.NotFoundError("Dashboard not found.", nil)
	}
	return d, nil
}

// --- serialization --------------------------------------------------------------

type panelJSON struct {
	Id         string         `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Icon       string         `json:"icon"`
	Color      string         `json:"color"`
	ShowHeader bool           `json:"show_header"`
	Note       string         `json:"note"`
	X          int            `json:"x"`
	Y          int            `json:"y"`
	W          int            `json:"w"`
	H          int            `json:"h"`
	Options    map[string]any `json:"options"`
}

func exportPanel(r *core.Record) panelJSON {
	p := panelJSON{
		Id: r.Id, Type: r.GetString("type"), Name: r.GetString("name"), Icon: r.GetString("icon"),
		Color: r.GetString("color"), ShowHeader: r.GetBool("show_header"), Note: r.GetString("note"),
		X: r.GetInt("x"), Y: r.GetInt("y"), W: r.GetInt("w"), H: r.GetInt("h"),
	}
	_ = r.UnmarshalJSONField("options", &p.Options)
	if p.Options == nil {
		p.Options = map[string]any{}
	}
	return p
}

// variablesOf returns the variable definitions of a dashboard.
func variablesOf(d *core.Record) []Variable {
	var defs []Variable
	_ = d.UnmarshalJSONField("variables", &defs)
	if defs == nil {
		defs = []Variable{}
	}
	return defs
}

func exportDashboard(d *core.Record, panels []*core.Record) map[string]any {
	list := make([]panelJSON, 0, len(panels))
	for _, p := range panels {
		list = append(list, exportPanel(p))
	}
	return map[string]any{
		"id":        d.Id,
		"name":      d.GetString("name"),
		"icon":      d.GetString("icon"),
		"note":      d.GetString("note"),
		"refresh":   d.GetInt("refresh"),
		"variables": variablesOf(d),
		"columns":   GridColumns,
		"panels":    list,
	}
}

// findPanels returns the panels of a dashboard, top to bottom.
func findPanels(app core.App, dashboardId string) ([]*core.Record, error) {
	return app.FindRecordsByFilter(panelsCollection, "dashboard = {:d}", "y,x", 0, 0, dbx.Params{"d": dashboardId})
}

// --- routes ---------------------------------------------------------------------

func listShared(e *core.RequestEvent) error {
	all, err := e.App.FindRecordsByFilter(dashboardsCollection, "", "name", 0, 0)
	if err != nil {
		return err
	}
	result := []map[string]any{}
	for _, d := range all {
		if canView(e.Auth, d) {
			result = append(result, map[string]any{
				"id": d.Id, "name": d.GetString("name"), "icon": d.GetString("icon"), "note": d.GetString("note"),
			})
		}
	}
	return e.JSON(http.StatusOK, result)
}

func getDashboard(e *core.RequestEvent) error {
	d, err := findViewable(e)
	if err != nil {
		return err
	}
	panels, err := findPanels(e.App, d.Id)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, exportDashboard(d, panels))
}

type panelResult struct {
	Data  any    `json:"data"`
	Error string `json:"error,omitempty"`
}

func runPanel(r *runner, panelType string, options map[string]any) panelResult {
	var q Query
	raw, _ := json.Marshal(options)
	if err := json.Unmarshal(raw, &q); err != nil {
		return panelResult{Error: "invalid options: " + err.Error()}
	}
	data, err := r.Run(panelType, q)
	if err != nil {
		return panelResult{Error: err.Error()}
	}
	return panelResult{Data: data}
}

type dataBody struct {
	Panels []string       `json:"panels"` // optional subset
	Vars   map[string]any `json:"vars"`   // variable values (defaults otherwise)
}

// dashboardData computes the data of every panel (concurrently, with the
// viewer permissions). Panel errors are returned per panel.
func dashboardData(e *core.RequestEvent) error {
	d, err := findViewable(e)
	if err != nil {
		return err
	}
	var body dataBody
	if e.Request.ContentLength > 0 {
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("Invalid request body.", err)
		}
	}
	panels, err := findPanels(e.App, d.Id)
	if err != nil {
		return err
	}
	vars, err := resolveVariables(variablesOf(d), body.Vars)
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}

	r := &runner{app: e.App, auth: e.Auth, ctx: e.Request.Context(), vars: vars}
	results := map[string]panelResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, p := range panels {
		if len(body.Panels) > 0 && !slices.Contains(body.Panels, p.Id) {
			continue
		}
		exported := exportPanel(p)
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := runPanel(r, exported.Type, exported.Options)
			mu.Lock()
			results[exported.Id] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return e.JSON(http.StatusOK, map[string]any{"panels": results})
}

type previewBody struct {
	Type      string         `json:"type"`
	Options   map[string]any `json:"options"`
	Variables []Variable     `json:"variables"` // the (possibly unsaved) dashboard variables
	Vars      map[string]any `json:"vars"`
}

// preview computes the data of an unsaved panel (panel editor).
func preview(e *core.RequestEvent) error {
	var body previewBody
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if !panelTypes[body.Type] {
		return e.BadRequestError("Unknown panel type.", nil)
	}
	if err := validateVariables(body.Variables); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	vars, err := resolveVariables(body.Variables, body.Vars)
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	r := &runner{app: e.App, auth: e.Auth, ctx: e.Request.Context(), vars: vars}
	return e.JSON(http.StatusOK, runPanel(r, body.Type, body.Options))
}

type savePanelsBody struct {
	Panels []panelJSON `json:"panels"`
}

// savePanels replaces the panels of a dashboard (edit mode "Save"): panels
// with a known id are updated, new ones created and missing ones deleted.
func savePanels(e *core.RequestEvent) error {
	d, err := e.App.FindRecordById(dashboardsCollection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("Dashboard not found.", err)
	}
	var body savePanelsBody
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if len(body.Panels) > maxPanels {
		return e.BadRequestError(fmt.Sprintf("A dashboard can have up to %d panels.", maxPanels), nil)
	}

	err = e.App.RunInTransaction(func(txApp core.App) error {
		existing, err := findPanels(txApp, d.Id)
		if err != nil {
			return err
		}
		byId := map[string]*core.Record{}
		for _, r := range existing {
			byId[r.Id] = r
		}
		col, err := txApp.FindCollectionByNameOrId(panelsCollection)
		if err != nil {
			return err
		}
		kept := map[string]bool{}
		for i, p := range body.Panels {
			rec := byId[p.Id]
			if rec == nil {
				rec = core.NewRecord(col)
				rec.Set("dashboard", d.Id)
			}
			rec.Set("type", p.Type)
			rec.Set("name", p.Name)
			rec.Set("icon", p.Icon)
			rec.Set("color", p.Color)
			rec.Set("show_header", p.ShowHeader)
			rec.Set("note", p.Note)
			rec.Set("x", p.X)
			rec.Set("y", p.Y)
			rec.Set("w", p.W)
			rec.Set("h", p.H)
			if p.Options == nil {
				p.Options = map[string]any{}
			}
			rec.Set("options", p.Options)
			if err := txApp.Save(rec); err != nil {
				return fmt.Errorf("panel %d (%s): %w", i+1, labelOf(p), err)
			}
			kept[rec.Id] = true
		}
		for _, r := range existing {
			if !kept[r.Id] {
				if err := txApp.Delete(r); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return e.BadRequestError("Failed to save the panels: "+err.Error(), nil)
	}

	panels, err := findPanels(e.App, d.Id)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, exportDashboard(d, panels))
}

func labelOf(p panelJSON) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Type
}

// --- validation -----------------------------------------------------------------

func validatePanel(r *core.Record) error {
	errs := validation.Errors{}
	if !panelTypes[r.GetString("type")] {
		errs["type"] = validation.NewError("invalid_type", "Unknown panel type.")
	}
	x, y, w, h := r.GetInt("x"), r.GetInt("y"), r.GetInt("w"), r.GetInt("h")
	if w < 1 || w > GridColumns {
		errs["w"] = validation.NewError("invalid_size", fmt.Sprintf("The width must be between 1 and %d.", GridColumns))
	}
	if h < 1 || h > maxRows {
		errs["h"] = validation.NewError("invalid_size", fmt.Sprintf("The height must be between 1 and %d.", maxRows))
	}
	if x < 0 || x+w > GridColumns {
		errs["x"] = validation.NewError("invalid_position", "The panel must fit in the grid.")
	}
	if y < 0 || y > 10000 {
		errs["y"] = validation.NewError("invalid_position", "Invalid vertical position.")
	}
	var options map[string]any
	if raw := r.GetString("options"); raw != "" && raw != "null" {
		if err := json.Unmarshal([]byte(raw), &options); err != nil {
			errs["options"] = validation.NewError("invalid_options", "The options must be an object.")
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}
