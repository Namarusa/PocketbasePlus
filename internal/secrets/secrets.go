// Package secrets keeps the keys, tokens and passwords used by the flows,
// the saved APIs and the AI providers in one place (Settings → Secrets).
//
// A secret is entered once, stored encrypted with POCKETBASEPLUS_ENCRYPTION_KEY
// (as text when it isn't set) and never returned by the API, even to
// superusers. It's used as {{secret.NAME}} in the HTTP requests of the
// flows, the saved APIs and the AI provider keys: its value is only read
// when the request is sent, and hidden by value in the run logs and errors.
package secrets

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
)

// Collection is the name of the secrets collection.
const Collection = "secrets"

const (
	encPrefix   = "enc:v1:"
	plainPrefix = "plain:"
)

// NameRegex is the form of the secret names (OPENAI_KEY).
var NameRegex = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// refRegex finds the secrets used in a template: {{secret.NAME}},
// {{printf "Bearer %s" secret.NAME}}, {{.secret.NAME}}.
var (
	actionRegex = regexp.MustCompile(`\{\{(.*?)\}\}`)
	refRegex    = regexp.MustCompile(`(?:^|[\s(|,-])(?:\$?\.)?secret\.([A-Za-z0-9_]+)`)
)

// defaultKeyEnv is deploy.EncryptionKeyEnv, the variable of the app (not
// imported: the deploy tests use the migrations, which use this package).
const defaultKeyEnv = "POCKETBASEPLUS_ENCRYPTION_KEY"

// keyEnv returns the name of the variable holding the encryption key.
func keyEnv(app core.App) string {
	if name := app.EncryptionEnv(); name != "" {
		return name
	}
	return defaultKeyEnv
}

// key returns the encryption key ("" when not set).
func key(app core.App) string {
	return os.Getenv(keyEnv(app))
}

// Encrypted reports whether new secrets are stored encrypted.
func Encrypted(app core.App) bool {
	return key(app) != ""
}

func seal(app core.App, value string) (string, error) {
	k := key(app)
	if k == "" {
		return plainPrefix + value, nil
	}
	out, err := security.Encrypt([]byte(value), k)
	if err != nil {
		return "", err
	}
	return encPrefix + out, nil
}

func open(app core.App, name, stored string) (string, error) {
	if v, ok := strings.CutPrefix(stored, plainPrefix); ok {
		return v, nil
	}
	v, ok := strings.CutPrefix(stored, encPrefix)
	if !ok {
		return stored, nil // written outside the hooks (e.g. a SQL import)
	}
	k := key(app)
	if k == "" {
		return "", fmt.Errorf("the secret %s is encrypted but %s isn't set", name, keyEnv(app))
	}
	out, err := security.Decrypt(v, k)
	if err != nil {
		return "", fmt.Errorf("the secret %s can't be decrypted (did %s change?): enter its value again in Settings → Secrets", name, keyEnv(app))
	}
	return string(out), nil
}

// Hint shows the end of a value, after the prefix of the API keys that have
// one ("sk-…a3f9", "ghp_…1a2b", "…1a2b").
func Hint(v string) string {
	switch {
	case v == "":
		return ""
	case len(v) <= 8:
		return "…"
	}
	prefix := ""
	if i := strings.IndexAny(v, "-_"); i > 0 && i <= 5 && len(v) >= 20 {
		prefix = v[:i+1]
	}
	return prefix + "…" + v[len(v)-4:]
}

// Refs returns the names of the secrets used in a value (a template, or the
// strings of maps and lists), sorted and without duplicates.
func Refs(v any) []string {
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch val := v.(type) {
		case string:
			if !strings.Contains(val, "secret.") {
				return
			}
			for _, a := range actionRegex.FindAllStringSubmatch(val, -1) {
				for _, m := range refRegex.FindAllStringSubmatch(a[1], -1) {
					out = append(out, m[1])
				}
			}
		case map[string]any:
			for _, item := range val {
				walk(item)
			}
		case map[string]string:
			for _, item := range val {
				walk(item)
			}
		case []any:
			for _, item := range val {
				walk(item)
			}
		case []string:
			for _, item := range val {
				walk(item)
			}
		}
	}
	walk(v)
	slices.Sort(out)
	return slices.Compact(out)
}

// Load returns the values of the secrets used in v, as the "secret" data of
// a template, and the list of the values (to hide them).
func Load(app core.App, v any) (map[string]any, []string, error) {
	names := Refs(v)
	if len(names) == 0 {
		return nil, nil, nil
	}
	data := make(map[string]any, len(names))
	values := make([]string, 0, len(names))
	for _, name := range names {
		val, err := Value(app, name)
		if err != nil {
			return nil, nil, err
		}
		data[name] = val
		values = append(values, val)
	}
	return data, values, nil
}

// Placeholders returns the "secret" data of a preview: [secret NAME] in
// place of the values.
func Placeholders(v any) map[string]any {
	names := Refs(v)
	if len(names) == 0 {
		return nil
	}
	data := make(map[string]any, len(names))
	for _, name := range names {
		data[name] = "[secret " + name + "]"
	}
	return data
}

// Value returns the value of a secret.
func Value(app core.App, name string) (string, error) {
	rec, err := app.FindFirstRecordByData(Collection, "name", name)
	if err != nil {
		return "", fmt.Errorf("the secret %s doesn't exist (Settings → Secrets)", name)
	}
	return open(app, name, rec.GetString("value"))
}

// Check returns an error naming the first secret used in v that doesn't exist.
func Check(app core.App, v any) error {
	for _, name := range Refs(v) {
		if !NameRegex.MatchString(name) {
			return fmt.Errorf("invalid secret name %q (capital letters, digits and _)", name)
		}
		if _, err := app.FindFirstRecordByData(Collection, "name", name); err != nil {
			return fmt.Errorf("the secret %s doesn't exist: add it in Settings → Secrets", name)
		}
	}
	return nil
}

// ErrNotHere is the error of a secret used where it can't be.
var ErrNotHere = formula.ErrSecretHere

// Register binds the hooks of the secrets collection and its routes.
func Register(app core.App) {
	app.OnRecordValidate(Collection).BindFunc(func(e *core.RecordEvent) error {
		r := e.Record
		r.Set("name", strings.ToUpper(strings.TrimSpace(r.GetString("name"))))
		value := r.GetString("value")
		switch {
		case !r.IsNew() && value == "":
			// not replaced (the value is never sent back to the dashboard)
			if old, err := e.App.FindRecordById(Collection, r.Id); err == nil {
				r.Set("value", old.GetString("value"))
			}
		case strings.HasPrefix(value, encPrefix) || strings.HasPrefix(value, plainPrefix):
			// already stored (a backup restored as records)
		default:
			sealed, err := seal(e.App, value)
			if err != nil {
				return err
			}
			r.Set("value", sealed)
			r.Set("hint", Hint(value))
		}
		return e.Next()
	})

	// the value is never returned, even to superusers
	app.OnRecordEnrich(Collection).BindFunc(func(e *core.RecordEnrichEvent) error {
		e.Record.WithCustomData(true)
		e.Record.Set("encrypted", strings.HasPrefix(e.Record.GetString("value"), encPrefix))
		// hidden fields are unhidden for superusers at the end of the enrich chain
		err := e.Next()
		e.Record.Hide("value")
		return err
	})

	// filtering or sorting on the value would reveal it piece by piece
	app.OnRecordsListRequest(Collection).BindFunc(func(e *core.RecordsListRequestEvent) error {
		q := e.Request.URL.Query()
		for _, p := range []string{"filter", "sort"} {
			if strings.Contains(q.Get(p), "value") {
				return apis.NewBadRequestError("The secret values can't be searched.", nil)
			}
		}
		return e.Next()
	})

	app.OnRecordDelete(Collection).BindFunc(func(e *core.RecordEvent) error {
		if uses := Uses(e.App, e.Record.GetString("name")); len(uses) > 0 {
			return apis.NewBadRequestError("The secret is used by "+strings.Join(uses, ", ")+": change them first.", nil)
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		if err := sealPlain(se.App); err != nil {
			se.App.Logger().Error("secrets: encryption failed", "error", err)
		}
		g := se.Router.Group("/api/secrets")
		g.Bind(apis.RequireSuperuserAuth())
		g.GET("/status", func(e *core.RequestEvent) error {
			return e.JSON(http.StatusOK, map[string]any{"encrypted": Encrypted(e.App), "env": keyEnv(e.App)})
		})
		g.GET("/uses", func(e *core.RequestEvent) error {
			names := []string{}
			_ = e.App.DB().Select("name").From(Collection).Column(&names)
			out := map[string][]string{}
			for _, n := range names {
				out[n] = Uses(e.App, n)
			}
			return e.JSON(http.StatusOK, out)
		})
		return se.Next()
	})
}

// sealPlain encrypts the secrets stored as text once the key is set.
func sealPlain(app core.App) error {
	if !Encrypted(app) {
		return nil
	}
	if _, err := app.FindCachedCollectionByNameOrId(Collection); err != nil {
		return nil
	}
	recs, err := app.FindAllRecords(Collection, dbx.NewExp("value NOT LIKE {:p}", dbx.Params{"p": encPrefix + "%"}))
	if err != nil {
		return err
	}
	for _, r := range recs {
		v, _ := strings.CutPrefix(r.GetString("value"), plainPrefix)
		sealed, err := seal(app, v)
		if err != nil {
			return err
		}
		r.Set("value", sealed)
		if err := app.SaveNoValidate(r); err != nil {
			return err
		}
	}
	return nil
}

// usedIn are the tables and columns that may use secrets, with the label of
// their rows in the messages.
var usedIn = []struct{ table, column, label string }{
	{"flows", "steps", "the flow"},
	{"api_sources", "secret", "the API"},
	{"api_sources", "username", "the API"},
	{"api_sources", "headers", "the API"},
	{"api_sources", "base_url", "the API"},
	{"api_calls", "path", "the API call"},
	{"api_calls", "headers", "the API call"},
	{"api_calls", "query", "the API call"},
	{"api_calls", "body", "the API call"},
	{"api_calls", "form", "the API call"},
	{"ai_providers", "api_key", "the AI provider"},
}

// Uses lists what uses a secret ("the flow Sync cards").
func Uses(app core.App, name string) []string {
	var out []string
	for _, u := range usedIn {
		if _, err := app.FindCachedCollectionByNameOrId(u.table); err != nil {
			continue // an optional module removed
		}
		rows := []struct {
			Name  string `db:"name"`
			Value string `db:"value"`
		}{}
		err := app.DB().Select("name", u.column+" AS value").From(u.table).
			Where(dbx.Like(u.column, "secret."+name).Escape()).All(&rows) // "_" matches any character: checked below
		if err != nil {
			app.Logger().Debug("secrets: uses", "error", err)
			continue
		}
		for _, row := range rows {
			if !slices.Contains(Refs(row.Value), name) {
				continue
			}
			label := u.label + " " + row.Name
			if !slices.Contains(out, label) {
				out = append(out, label)
			}
		}
	}
	return out
}

// Seal returns the stored form of a value (encrypted when the key is set),
// for the migrations that create secrets.
func Seal(app core.App, value string) (string, error) {
	return seal(app, value)
}
