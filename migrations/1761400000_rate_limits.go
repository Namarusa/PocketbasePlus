package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Turns on PocketBase's rate limiter (off by default): its default rules limit
// the sign in, sign up and other API calls per client, against password
// guessing on the superusers and users.
func init() {
	m.Register(func(app core.App) error {
		settings := app.Settings()
		if settings.RateLimits.Enabled {
			return nil
		}
		settings.RateLimits.Enabled = true
		return app.Save(settings)
	}, func(app core.App) error {
		settings := app.Settings()
		settings.RateLimits.Enabled = false
		return app.Save(settings)
	})
}
