package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Attachments of the queued emails: the ids of generated documents (the
// Documents module), read when the email is sent.
func init() {
	m.Register(func(app core.App) error {
		messages, err := app.FindCollectionByNameOrId("email_messages")
		if err != nil {
			return err
		}
		messages.Fields.Add(&core.JSONField{Name: "attachments", MaxSize: 4 << 10})
		return app.Save(messages)
	}, func(app core.App) error {
		messages, err := app.FindCollectionByNameOrId("email_messages")
		if err != nil {
			return err
		}
		messages.Fields.RemoveByName("attachments")
		return app.Save(messages)
	})
}
