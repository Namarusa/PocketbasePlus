// Package activity records a revision of every record change (who, when, what
// changed), allows reverting to a revision, and handles record comments with
// @mentions and the superuser notifications (in the spirit of the Directus
// revisions, activity and notifications).
package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/Namarusa/PocketbasePlus/internal/actor"
	"github.com/Namarusa/PocketbasePlus/internal/internalcols"
)

const (
	revisionsCollection     = "revisions"
	commentsCollection      = "comments"
	notificationsCollection = "notifications"

	// RetentionEnv sets how many days revisions are kept (0 = forever).
	RetentionEnv     = "POCKETBASEPLUS_REVISIONS_RETENTION_DAYS"
	defaultRetention = 180

	cleanupJobId = "gp_revisions_cleanup"
)

// pending maps the records being saved through the API to their author:
// the record model hooks don't receive the request, but the same record
// instance is passed from the request hook to the save.
var pending sync.Map // *core.Record -> actor.Actor

type revertKey struct{}

// Register binds the activity hooks and routes.
func Register(app core.App) {
	// remember the author of API changes
	remember := func(e *core.RecordRequestEvent) error {
		if e.Auth == nil {
			return e.Next()
		}
		pending.Store(e.Record, actor.OfAuth(e.Auth))
		defer pending.Delete(e.Record)
		return e.Next()
	}
	app.OnRecordCreateRequest().BindFunc(remember)
	app.OnRecordUpdateRequest().BindFunc(remember)
	app.OnRecordDeleteRequest().BindFunc(remember)

	// record the revisions in the same transaction as the change
	app.OnRecordCreate().BindFunc(func(e *core.RecordEvent) error {
		if err := e.Next(); err != nil || !tracked(e.Record.Collection()) {
			return err
		}
		return saveRevision(e, "create", snapshot(e.Record), nil)
	})
	app.OnRecordUpdate().BindFunc(func(e *core.RecordEvent) error {
		if !tracked(e.Record.Collection()) {
			return e.Next()
		}
		before := snapshot(e.Record.Original())
		if err := e.Next(); err != nil {
			return err
		}
		after := snapshot(e.Record)
		delta := diff(e.Record.Collection(), before, after)
		if len(delta) == 0 {
			return nil // nothing changed
		}
		return saveRevision(e, "update", after, delta)
	})
	app.OnRecordDelete().BindFunc(func(e *core.RecordEvent) error {
		if !tracked(e.Record.Collection()) {
			return e.Next()
		}
		before := snapshot(e.Record)
		if err := e.Next(); err != nil {
			return err
		}
		return saveRevision(e, "delete", before, nil)
	})

	bindComments(app)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		g := se.Router.Group("/api/activity")
		g.Bind(apis.RequireSuperuserAuth())
		g.POST("/revisions/{id}/revert", revert)

		if days := retentionDays(); days > 0 {
			se.App.Cron().MustAdd(cleanupJobId, "15 3 * * *", func() {
				if err := Cleanup(se.App, days); err != nil {
					se.App.Logger().Error("activity: failed to clean up the revisions", "error", err)
				}
			})
		}
		return se.Next()
	})
}

// tracked reports whether the changes of a collection are recorded.
func tracked(c *core.Collection) bool {
	return !c.IsView() && !internalcols.Is(c)
}

// snapshot exports the record fields (without passwords and token keys).
func snapshot(r *core.Record) map[string]any {
	data := map[string]any{}
	if r == nil {
		return data
	}
	for _, f := range r.Collection().Fields {
		if f.Type() == core.FieldTypePassword || f.GetName() == core.FieldNameTokenKey {
			continue
		}
		data[f.GetName()] = r.Get(f.GetName())
	}
	// normalize to plain JSON values (dates, json raw...)
	var plain map[string]any
	if raw, err := json.Marshal(data); err == nil && json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	return data
}

// diff returns the changed fields ({field: [old, new]}), ignoring the autodate fields.
func diff(c *core.Collection, before, after map[string]any) map[string]any {
	delta := map[string]any{}
	for _, f := range c.Fields {
		name := f.GetName()
		if f.Type() == core.FieldTypeAutodate {
			continue
		}
		a, okA := before[name]
		b, okB := after[name]
		if !okA && !okB {
			continue
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			delta[name] = []any{a, b}
		}
	}
	return delta
}

// actorOf resolves the author of a change: the context (flows, PocketBasePlus
// routes), the API request that saves this record, or the system.
func actorOf(e *core.RecordEvent) actor.Actor {
	if a, ok := actor.From(e.Context); ok {
		return a
	}
	if a, ok := pending.Load(e.Record); ok {
		return a.(actor.Actor)
	}
	return actor.System
}

func saveRevision(e *core.RecordEvent, action string, data, delta map[string]any) error {
	col, err := e.App.FindCachedCollectionByNameOrId(revisionsCollection)
	if err != nil {
		return nil // module not installed (e.g. before the migration)
	}
	if action == "update" && e.Context != nil {
		if _, ok := e.Context.Value(revertKey{}).(string); ok {
			action = "revert"
		}
	}
	a := actorOf(e)
	rev := core.NewRecord(col)
	rev.Set("collection", e.Record.Collection().Id)
	rev.Set("collection_name", e.Record.Collection().Name)
	rev.Set("record_id", e.Record.Id)
	rev.Set("action", action)
	rev.Set("actor_type", a.Type)
	rev.Set("actor_id", a.Id)
	rev.Set("actor_label", a.Label)
	rev.Set("data", data)
	if delta != nil {
		rev.Set("delta", delta)
	}
	if err := e.App.Save(rev); err != nil {
		return fmt.Errorf("failed to save the revision: %w", err)
	}
	return nil
}

// --- revert -----------------------------------------------------------------------

// revert restores a record to the state of a revision (and recreates it
// when it was deleted). Files are not restored (they may no longer exist).
func revert(e *core.RequestEvent) error {
	rev, err := e.App.FindRecordById(revisionsCollection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("Revision not found.", err)
	}
	col, err := e.App.FindCollectionByNameOrId(rev.GetString("collection"))
	if err != nil || !tracked(col) {
		return e.BadRequestError("The collection of this revision no longer exists.", nil)
	}
	var data map[string]any
	if err := rev.UnmarshalJSONField("data", &data); err != nil || data == nil {
		return e.BadRequestError("The revision has no data.", nil)
	}

	ctx := actor.With(context.WithValue(e.Request.Context(), revertKey{}, rev.Id), actor.OfAuth(e.Auth))

	rec, err := e.App.FindRecordById(col, rev.GetString("record_id"))
	restored := false
	if err != nil {
		rec = core.NewRecord(col)
		rec.Id = rev.GetString("record_id")
		if col.IsAuth() {
			rec.SetRandomPassword()
		}
		restored = true
	}
	skipped := []string{}
	for _, f := range col.Fields {
		name := f.GetName()
		switch {
		case name == core.FieldNameId, f.Type() == core.FieldTypeAutodate, f.Type() == core.FieldTypePassword, name == core.FieldNameTokenKey:
			continue
		case f.Type() == core.FieldTypeFile:
			if _, ok := data[name]; ok {
				skipped = append(skipped, name)
			}
			continue
		}
		if v, ok := data[name]; ok {
			rec.Set(name, v)
		}
	}
	if err := e.App.SaveWithContext(ctx, rec); err != nil {
		return e.BadRequestError("Failed to revert: "+err.Error(), err)
	}
	return e.JSON(http.StatusOK, map[string]any{"record": rec, "restored": restored, "skippedFiles": skipped})
}

// --- retention ----------------------------------------------------------------------

func retentionDays() int {
	if v, ok := os.LookupEnv(RetentionEnv); ok {
		if days, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && days >= 0 {
			return days
		}
	}
	return defaultRetention
}

// Cleanup deletes the revisions older than the given number of days.
func Cleanup(app core.App, days int) error {
	if days <= 0 {
		return nil
	}
	before := types.NowDateTime().Add(-time.Duration(days) * 24 * time.Hour).String()
	_, err := app.DB().Delete(revisionsCollection, dbx.NewExp("[[created]] < {:before}", dbx.Params{"before": before})).Execute()
	return err
}

// --- comments and notifications -----------------------------------------------------

func bindComments(app core.App) {
	// the author is always the signed-in superuser
	app.OnRecordCreateRequest(commentsCollection).BindFunc(func(e *core.RecordRequestEvent) error {
		if e.Auth == nil || !e.Auth.IsSuperuser() {
			return e.ForbiddenError("Only superusers can comment.", nil)
		}
		e.Record.Set("author_id", e.Auth.Id)
		e.Record.Set("author_label", e.Auth.Email())
		e.Record.Set("mentions", validMentions(e.App, e.Record, e.Auth.Id))
		return e.Next()
	})
	// only the author can edit or delete a comment
	ownComment := func(e *core.RecordRequestEvent) error {
		if e.Auth == nil || e.Record.GetString("author_id") != e.Auth.Id {
			return e.ForbiddenError("Only the author can change this comment.", nil)
		}
		return e.Next()
	}
	app.OnRecordUpdateRequest(commentsCollection).BindFunc(func(e *core.RecordRequestEvent) error {
		orig := e.Record.Original()
		// the target and the author can't change
		for _, f := range []string{"collection", "record_id", "author_id", "author_label"} {
			e.Record.Set(f, orig.Get(f))
		}
		if e.Auth != nil {
			e.Record.Set("mentions", validMentions(e.App, e.Record, e.Auth.Id))
		}
		return ownComment(e)
	})
	app.OnRecordDeleteRequest(commentsCollection).BindFunc(ownComment)

	// notify the mentioned superusers (new mentions only on updates)
	notify := func(e *core.RecordEvent, previous []string) {
		var mentions []string
		_ = e.Record.UnmarshalJSONField("mentions", &mentions)
		for _, id := range mentions {
			if slices.Contains(previous, id) || id == e.Record.GetString("author_id") {
				continue
			}
			link := fmt.Sprintf("#/collections?collection=%s&record=%s", e.Record.GetString("collection"), e.Record.GetString("record_id"))
			if err := Notify(e.App, id, "mention", e.Record.GetString("author_label")+" mentioned you", excerpt(e.Record.GetString("text"), 300), link); err != nil {
				e.App.Logger().Error("activity: failed to notify a mention", "error", err)
			}
		}
	}
	app.OnRecordAfterCreateSuccess(commentsCollection).BindFunc(func(e *core.RecordEvent) error {
		notify(e, nil)
		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess(commentsCollection).BindFunc(func(e *core.RecordEvent) error {
		var previous []string
		_ = e.Record.Original().UnmarshalJSONField("mentions", &previous)
		notify(e, previous)
		return e.Next()
	})
}

// validMentions keeps the mentioned ids that are superusers.
func validMentions(app core.App, comment *core.Record, authorId string) []string {
	var ids []string
	_ = comment.UnmarshalJSONField("mentions", &ids)
	valid := []string{}
	for _, id := range ids {
		if id == authorId || slices.Contains(valid, id) {
			continue
		}
		if _, err := app.FindRecordById(core.CollectionNameSuperusers, id); err == nil {
			valid = append(valid, id)
		}
	}
	return valid
}

func excerpt(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// Notify creates a notification for a superuser.
func Notify(app core.App, recipientId, kind, subject, message, link string) error {
	col, err := app.FindCachedCollectionByNameOrId(notificationsCollection)
	if err != nil {
		return err
	}
	n := core.NewRecord(col)
	n.Set("recipient", recipientId)
	n.Set("kind", kind)
	n.Set("subject", excerpt(subject, 300))
	n.Set("message", excerpt(message, 5000))
	n.Set("link", link)
	return app.Save(n)
}

// NotifySuperusers notifies all superusers ("all") or the ones with the given emails.
func NotifySuperusers(app core.App, to []string, kind, subject, message, link string) (int, error) {
	var recipients []*core.Record
	if len(to) == 1 && strings.EqualFold(strings.TrimSpace(to[0]), "all") {
		list, err := app.FindAllRecords(core.CollectionNameSuperusers)
		if err != nil {
			return 0, err
		}
		recipients = list
	} else {
		for _, email := range to {
			email = strings.TrimSpace(email)
			if email == "" {
				continue
			}
			su, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
			if err != nil {
				return 0, fmt.Errorf("no superuser with the email %q", email)
			}
			recipients = append(recipients, su)
		}
	}
	for _, su := range recipients {
		if err := Notify(app, su.Id, kind, subject, message, link); err != nil {
			return 0, err
		}
	}
	return len(recipients), nil
}
