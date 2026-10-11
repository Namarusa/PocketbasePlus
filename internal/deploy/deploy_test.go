package deploy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestEnsureAdmin(t *testing.T) {
	app := testutil.NewApp(t)
	superusers := func() int {
		n, _ := app.CountRecords(core.CollectionNameSuperusers)
		return int(n)
	}
	before := superusers()

	// nothing set: nothing created
	if err := ensureAdmin(app); err != nil || superusers() != before {
		t.Fatalf("no variables: %v", err)
	}
	t.Setenv(AdminEmailEnv, "admin@example.com")
	if err := ensureAdmin(app); err == nil || !strings.Contains(err.Error(), AdminPasswordEnv) {
		t.Fatalf("email without password must be refused, got %v", err)
	}
	t.Setenv(AdminPasswordEnv, "a-long-password-123")

	// the test app has superusers already: never changed
	if before > 0 {
		if err := ensureAdmin(app); err != nil || superusers() != before {
			t.Fatalf("existing superusers: %v", err)
		}
		if _, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@example.com"); err == nil {
			t.Fatal("no account may be created when superusers exist")
		}
	}

	// an empty install (only PocketBase's installer account): created
	recs, _ := app.FindAllRecords(core.CollectionNameSuperusers)
	for _, r := range recs {
		if err := app.Delete(r); err != nil {
			t.Fatal(err)
		}
	}
	col, _ := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	installer := core.NewRecord(col)
	installer.SetEmail(core.DefaultInstallerEmail)
	installer.SetPassword("installer-password-123")
	if err := app.Save(installer); err != nil {
		t.Fatal(err)
	}
	if err := ensureAdmin(app); err != nil {
		t.Fatal(err)
	}
	admin, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@example.com")
	if err != nil || !admin.ValidatePassword("a-long-password-123") {
		t.Fatalf("admin not created: %v", err)
	}
	// a second start doesn't change it (even with another password)
	t.Setenv(AdminPasswordEnv, "another-password-456")
	if err := ensureAdmin(app); err != nil {
		t.Fatal(err)
	}
	admin, _ = app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@example.com")
	if !admin.ValidatePassword("a-long-password-123") {
		t.Fatal("the existing account must not change")
	}
}

func TestCheckDataDir(t *testing.T) {
	dir := t.TempDir()
	if err := CheckDataDir(dir + "/pb_data"); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	locked := dir + "/locked"
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := CheckDataDir(locked); err == nil || !strings.Contains(err.Error(), "PUID") {
		t.Fatalf("expected an explanation, got %v", err)
	}
}

func TestEncryptionKey(t *testing.T) {
	t.Setenv(EncryptionKeyEnv, "")
	if CheckEncryptionKey() != nil {
		t.Fatal("no key is fine")
	}
	t.Setenv(EncryptionKeyEnv, "short")
	if CheckEncryptionKey() == nil {
		t.Fatal("a short key must be refused")
	}
	t.Setenv(EncryptionKeyEnv, strings.Repeat("k", 32))
	if CheckEncryptionKey() != nil {
		t.Fatal("32 characters is fine")
	}
}

func TestServeDefaults(t *testing.T) {
	app := testutil.NewApp(t)
	serve := cmd.NewServeCommand(app, false)
	t.Setenv(HTTPEnv, "")
	if err := SetServeDefaults(serve); err != nil || serve.PersistentFlags().Lookup("http").Value.String() != "" {
		t.Fatal("no variable: PocketBase's default")
	}
	t.Setenv(HTTPEnv, "nonsense")
	if SetServeDefaults(serve) == nil {
		t.Fatal("a bad address must be refused")
	}
	t.Setenv(HTTPEnv, "0.0.0.0:9000")
	if err := SetServeDefaults(serve); err != nil {
		t.Fatal(err)
	}
	if v := serve.PersistentFlags().Lookup("http").Value.String(); v != "0.0.0.0:9000" {
		t.Fatalf("default: %s", v)
	}
	// the flag still wins
	if err := serve.ParseFlags([]string{"--http", "127.0.0.1:7000"}); err != nil {
		t.Fatal(err)
	}
	if v := serve.PersistentFlags().Lookup("http").Value.String(); v != "127.0.0.1:7000" {
		t.Fatalf("flag: %s", v)
	}
}

func TestHealth(t *testing.T) {
	t.Setenv(HTTPEnv, "0.0.0.0:9000")
	if u := HealthURL(""); u != "http://127.0.0.1:9000/api/health" {
		t.Fatal(u)
	}
	if u := HealthURL("[::]:8090"); u != "http://127.0.0.1:8090/api/health" {
		t.Fatal(u)
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(404)
		}
	}))
	defer ok.Close()
	addr := strings.TrimPrefix(ok.URL, "http://")
	if Health([]string{"--http", addr}) != 0 || Health([]string{"--http=" + addr}) != 0 {
		t.Fatal("healthy server reported unhealthy")
	}
	// nothing listening
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	free := l.Addr().String()
	l.Close()
	if Health([]string{"--http", free}) == 0 {
		t.Fatal("no server must be unhealthy")
	}
}

func TestPrivilegeSettings(t *testing.T) {
	t.Setenv(PUIDEnv, "1000")
	if id, err := envID(PUIDEnv); err != nil || id != 1000 {
		t.Fatal(id, err)
	}
	for _, bad := range []string{"abc", "-1", "1000:1000"} {
		t.Setenv(PUIDEnv, bad)
		if _, err := envID(PUIDEnv); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	// not root: nothing happens, even with a bad value
	if os.Getuid() != 0 {
		if err := DropPrivileges(t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
}
