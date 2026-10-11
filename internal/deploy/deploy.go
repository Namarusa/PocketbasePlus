// Package deploy holds the settings read from the environment for container
// deployments (Docker, NAS app managers, Kubernetes), where a variable is often
// the only setting available: listen address, settings encryption key, first
// superuser, and the "health" command used by the image's health check.
package deploy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

const (
	// HTTPEnv is the default address of "serve" (the --http flag wins).
	HTTPEnv = "POCKETBASEPLUS_HTTP"
	// EncryptionKeyEnv is the 32 characters key encrypting the stored settings
	// (PocketBase's --encryptionEnv).
	EncryptionKeyEnv = "POCKETBASEPLUS_ENCRYPTION_KEY"
	// AdminEmailEnv and AdminPasswordEnv create the first superuser.
	AdminEmailEnv    = "POCKETBASEPLUS_ADMIN_EMAIL"
	AdminPasswordEnv = "POCKETBASEPLUS_ADMIN_PASSWORD"
	// PUIDEnv and PGIDEnv are the user the app runs as when started as root
	// (the NAS convention; see DropPrivileges).
	PUIDEnv = "PUID"
	PGIDEnv = "PGID"

	defaultAddr = "127.0.0.1:8090"
)

// CheckEncryptionKey refuses a key PocketBase can't use (AES-256 needs 32
// bytes), before it fails on the first save of the settings.
func CheckEncryptionKey() error {
	key := os.Getenv(EncryptionKeyEnv)
	if key != "" && len(key) != 32 {
		return fmt.Errorf("%s must be 32 characters long (got %d), e.g. the output of: openssl rand -hex 16", EncryptionKeyEnv, len(key))
	}
	return nil
}

// CheckDataDir explains a data folder the server can't write (typically a
// folder of the host mounted in a container that runs as another user),
// instead of SQLite's "unable to open database file".
func CheckDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return dataDirError(dir, err)
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return dataDirError(dir, err)
	}
	f.Close()
	return os.Remove(f.Name())
}

func dataDirError(dir string, err error) error {
	return fmt.Errorf("the data folder %s isn't writable by this user (uid %d, gid %d): %w\n"+
		"In Docker, set PUID and PGID to the owner of the mounted folder (e.g. PUID=1000, PGID=1000), "+
		"or give the folder to the container's user (chown -R 65532:65532 <host folder>)",
		dir, os.Getuid(), os.Getgid(), err)
}

// SetServeDefaults makes HTTPEnv the default of the --http flag of serve.
func SetServeDefaults(serve *cobra.Command) error {
	addr := strings.TrimSpace(os.Getenv(HTTPEnv))
	if addr == "" {
		return nil
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("%s: %q isn't an address like 0.0.0.0:8090", HTTPEnv, addr)
	}
	flag := serve.PersistentFlags().Lookup("http")
	if flag == nil {
		return errors.New("the serve command has no --http flag")
	}
	if err := flag.Value.Set(addr); err != nil {
		return err
	}
	flag.DefValue = addr
	return nil
}

// BindAdmin creates the superuser of AdminEmailEnv / AdminPasswordEnv when the
// server starts without any superuser (so no installer link is needed). An
// existing account is never changed: after the first start, the variables can
// be removed.
func BindAdmin(app core.App) {
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := ensureAdmin(e.App); err != nil {
			return err
		}
		return e.Next()
	})
}

func ensureAdmin(app core.App) error {
	email := strings.TrimSpace(os.Getenv(AdminEmailEnv))
	password := os.Getenv(AdminPasswordEnv)
	if email == "" && password == "" {
		return nil
	}
	if email == "" || password == "" {
		return fmt.Errorf("set both %s and %s (or neither)", AdminEmailEnv, AdminPasswordEnv)
	}
	total, err := app.CountRecords(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	if installer, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, core.DefaultInstallerEmail); installer != nil {
		total--
	}
	if total > 0 {
		return nil
	}
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	admin := core.NewRecord(col)
	admin.SetEmail(email)
	admin.SetPassword(password)
	if err := app.Save(admin); err != nil {
		return fmt.Errorf("creating the superuser of %s: %w", AdminEmailEnv, err)
	}
	app.Logger().Info("Created the first superuser from the environment", "email", email)
	return nil
}

// HealthURL is the address checked by the health command: the HTTPEnv address
// (or the default), on the loopback when the server listens on all interfaces.
func HealthURL(addr string) string {
	if addr == "" {
		addr = strings.TrimSpace(os.Getenv(HTTPEnv))
	}
	if addr == "" {
		addr = defaultAddr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", "8090"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/health"
}

// Health checks that the server answers (exit status 0) for the image's
// HEALTHCHECK. It runs before the app opens its database (see main.go), so a
// check every few seconds stays cheap.
func Health(args []string) int {
	addr := ""
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--http="); ok {
			addr = v
		} else if a == "--http" && i+1 < len(args) {
			addr = args[i+1]
		}
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(HealthURL(addr))
	if err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy: status", res.StatusCode)
		return 1
	}
	fmt.Println("healthy")
	return 0
}

// HealthCommand lists "health" in the help (main.go runs it before the app
// starts, see Health).
func HealthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "health [--http address]",
		Short: "Checks that the server answers (used by the Docker health check)",
		// flags are read by Health
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if Health(os.Args[2:]) != 0 {
				return errors.New("unhealthy")
			}
			return nil
		},
	}
}
