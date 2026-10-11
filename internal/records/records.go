// Package records adds data tools to the collection pages: export (CSV,
// JSON, XLSX), import with a dry run, bulk edit and saved views.
package records

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/actor"
	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
	"github.com/Namarusa/PocketbasePlus/internal/offline"
)

const (
	viewsCollection = "saved_views"
	maxBulkRecords  = 1000
)

// Register binds the records routes and the saved views hooks.
func Register(app core.App) {
	bindViews(app)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		g := se.Router.Group("/api/gp")
		g.Bind(apis.RequireSuperuserAuth())
		g.GET("/export/{collection}", exportRecords)
		g.POST("/import/{collection}/preview", importPreview).Bind(apis.BodyLimit(maxImportBytes))
		g.POST("/import/{collection}", importRecords).Bind(apis.BodyLimit(maxImportBytes))
		g.PATCH("/bulk/{collection}", bulkUpdate)
		// deployment settings used by the dashboard (map tiles of the Map layout)
		g.GET("/config", func(e *core.RequestEvent) error {
			return e.JSON(http.StatusOK, map[string]any{"offline": offline.Enabled(), "mapTiles": offline.MapTiles()})
		})
		return se.Next()
	})
}

// findCollection returns the collection of the route (user collections only).
func findCollection(e *core.RequestEvent) (*core.Collection, error) {
	c, err := e.App.FindCollectionByNameOrId(e.Request.PathValue("collection"))
	if err != nil || internalcols.Is(c) {
		return nil, e.NotFoundError("Collection not found.", err)
	}
	return c, nil
}

// actorContext attributes the changes to the signed-in superuser (revisions).
func actorContext(e *core.RequestEvent) context.Context {
	return actor.With(e.Request.Context(), actor.OfAuth(e.Auth))
}

// --- bulk edit ---------------------------------------------------------------------

type bulkBody struct {
	Ids  []string       `json:"ids"`
	Data map[string]any `json:"data"`
}

// bulkUpdate sets the same values on several records, in one transaction
// (the record hooks run: revisions, flows...).
func bulkUpdate(e *core.RequestEvent) error {
	c, err := findCollection(e)
	if err != nil {
		return err
	}
	if c.IsView() {
		return e.BadRequestError("View collections are read-only.", nil)
	}
	var body bulkBody
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body.", err)
	}
	if len(body.Ids) == 0 || len(body.Data) == 0 {
		return e.BadRequestError("Select records and at least one field.", nil)
	}
	if len(body.Ids) > maxBulkRecords {
		return e.BadRequestError(fmt.Sprintf("Too many records (max %d).", maxBulkRecords), nil)
	}
	for name := range body.Data {
		f := c.Fields.GetByName(name)
		if f == nil || name == core.FieldNameId || f.Type() == core.FieldTypeAutodate || f.Type() == core.FieldTypeFile || f.Type() == core.FieldTypePassword {
			return e.BadRequestError(fmt.Sprintf("The field %q can't be bulk edited.", name), nil)
		}
	}

	ctx := actorContext(e)
	failedId := ""
	err = e.App.RunInTransaction(func(txApp core.App) error {
		records, err := txApp.FindRecordsByIds(c, body.Ids)
		if err != nil {
			return err
		}
		if len(records) != len(body.Ids) {
			return errors.New("some records no longer exist")
		}
		for _, r := range records {
			for name, value := range body.Data {
				r.Set(name, value)
			}
			if err := txApp.SaveWithContext(ctx, r); err != nil {
				failedId = r.Id
				return err
			}
		}
		return nil
	})
	if err != nil {
		msg := "Failed to update the records"
		if failedId != "" {
			msg += " (record " + failedId + ")"
		}
		return e.BadRequestError(msg+": "+err.Error(), err)
	}
	return e.JSON(http.StatusOK, map[string]any{"updated": len(body.Ids)})
}

// --- saved views -------------------------------------------------------------------

func bindViews(app core.App) {
	// the owner is the superuser creating the view
	app.OnRecordCreateRequest(viewsCollection).BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Auth == nil || !e.Auth.IsSuperuser() {
			return e.ForbiddenError("", nil)
		}
		e.Record.Set("owner", e.Auth.Id)
		return e.Next()
	})
	// only the owner can change or delete a view (shared ones included)
	owner := func(e *core.RecordRequestEvent) error {
		if e.Auth == nil || e.Record.Original().GetString("owner") != e.Auth.Id {
			return e.ForbiddenError("Only the owner can change this view.", nil)
		}
		e.Record.Set("owner", e.Auth.Id)
		return e.Next()
	}
	app.OnRecordUpdateRequest(viewsCollection).BindFunc(owner)
	app.OnRecordDeleteRequest(viewsCollection).BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Auth == nil || e.Record.GetString("owner") != e.Auth.Id {
			return e.ForbiddenError("Only the owner can delete this view.", nil)
		}
		return e.Next()
	})
}
