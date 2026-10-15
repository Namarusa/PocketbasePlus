package main

import (
	"log"
	"os"
	"slices"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"

	"github.com/Namarusa/PocketbasePlus/internal/activity"
	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/apisources"
	"github.com/Namarusa/PocketbasePlus/internal/assistant"
	"github.com/Namarusa/PocketbasePlus/internal/dashboard"
	"github.com/Namarusa/PocketbasePlus/internal/dashboards"
	"github.com/Namarusa/PocketbasePlus/internal/deploy"
	"github.com/Namarusa/PocketbasePlus/internal/flows"
	"github.com/Namarusa/PocketbasePlus/internal/forms"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
	"github.com/Namarusa/PocketbasePlus/internal/pages"
	"github.com/Namarusa/PocketbasePlus/internal/rbac"
	"github.com/Namarusa/PocketbasePlus/internal/records"
	"github.com/Namarusa/PocketbasePlus/internal/secrets"
	"github.com/Namarusa/PocketbasePlus/internal/session"
	_ "github.com/Namarusa/PocketbasePlus/migrations"
)

func main() {
	// the Docker health check: answered before the app opens its database
	if len(os.Args) > 1 && os.Args[1] == "health" {
		os.Exit(deploy.Health(os.Args[2:]))
	}
	if err := deploy.CheckEncryptionKey(); err != nil {
		log.Fatal(err)
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDev: osutils.IsProbablyGoRun(),
		// encrypts the stored settings (SMTP password, S3 keys...) when set
		DefaultEncryptionEnv: deploy.EncryptionKeyEnv,
	})

	// in Docker: give the data folder to PUID/PGID and stop being root,
	// before anything opens the database
	if err := deploy.DropPrivileges(app.DataDir()); err != nil {
		log.Fatal(err)
	}

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangGo,
		Dir:          "migrations",
		// generate migration files only during development (go run)
		Automigrate: osutils.IsProbablyGoRun(),
	})

	rbac.Register(app)
	session.Register(app) // visitors signed in on the pages
	activity.Register(app)
	mailEngine := mail.Register(app)
	secrets.Register(app) // keys of the flows, APIs and AI providers
	ai.Register(app)
	flowsEngine := flows.Register(app, mailEngine)
	assistant.Register(app, mailEngine, flowsEngine)
	dashboards.Register(app)
	records.Register(app)
	forms.Register(app)

	// Permissions and Emails pages inside the native dashboard (/_/)
	dashboard.Register(app)

	// optional visual page builder (Pages tab, /{slug}): remove this line to
	// remove the feature (run "pocketbaseplus pages uninstall" first to delete its data)
	pages.Register(app)

	// optional saved APIs of the flows (Flows → APIs, "API call" step): remove
	// this line to remove the feature (run "pocketbaseplus apis uninstall" first)
	apisources.Register(app)

	if slices.Contains(os.Args[1:], "serve") {
		if err := deploy.CheckDataDir(app.DataDir()); err != nil {
			log.Fatal(err)
		}
	}

	// first superuser from POCKETBASEPLUS_ADMIN_EMAIL / _PASSWORD (containers)
	deploy.BindAdmin(app)

	// app.Start() with POCKETBASEPLUS_HTTP as the default address of serve
	serve := cmd.NewServeCommand(app, true)
	if err := deploy.SetServeDefaults(serve); err != nil {
		log.Fatal(err)
	}
	app.RootCmd.AddCommand(cmd.NewSuperuserCommand(app), serve, deploy.HealthCommand())

	if err := app.Execute(); err != nil {
		log.Fatal(err)
	}
}
