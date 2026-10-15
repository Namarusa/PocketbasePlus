package migrations

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/Namarusa/PocketbasePlus/internal/secrets"
)

// The secrets (Settings → Secrets): the keys of the saved APIs and the AI
// providers move there, the fields keep a {{secret.NAME}} reference.
func init() {
	m.Register(func(app core.App) error {
		c := core.NewBaseCollection(secrets.Collection)
		c.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 64, Pattern: secrets.NameRegex.String()},
			// never returned by the API (see the hint)
			&core.TextField{Name: "value", Required: true, Hidden: true, Max: 20000},
			&core.TextField{Name: "hint", Max: 30},
			&core.TextField{Name: "description", Max: 1000},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		c.AddIndex("idx_secrets_name", true, "name", "")
		if err := app.Save(c); err != nil {
			return err
		}
		if err := markSystem(app, true, secrets.Collection); err != nil {
			return err
		}
		return moveKeys(app, c)
	}, func(app core.App) error {
		if err := restoreKeys(app); err != nil {
			return err
		}
		if err := markSystem(app, false, secrets.Collection); err != nil {
			return err
		}
		if c, err := app.FindCollectionByNameOrId(secrets.Collection); err == nil {
			return app.Delete(c)
		}
		return nil
	})
}

// the fields holding a key, with the prefix of the secrets they become
var keyFields = []struct{ collection, field, prefix, label string }{
	{"api_sources", "secret", "API_", "the API"},
	{"ai_providers", "api_key", "AI_", "the AI provider"},
}

var notNameChar = regexp.MustCompile(`[^A-Z0-9]+`)

func secretName(prefix, label string, taken map[string]bool) string {
	base := prefix + strings.Trim(notNameChar.ReplaceAllString(strings.ToUpper(label), "_"), "_")
	if base == prefix {
		base += "KEY"
	}
	if len(base) > 60 {
		base = base[:60]
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = base + "_" + strconv.Itoa(i)
	}
	taken[name] = true
	return name
}

func moveKeys(app core.App, col *core.Collection) error {
	taken := map[string]bool{}
	for _, kf := range keyFields {
		if _, err := app.FindCollectionByNameOrId(kf.collection); err != nil {
			continue // an optional module not installed
		}
		recs, err := app.FindAllRecords(kf.collection)
		if err != nil {
			return err
		}
		for _, r := range recs {
			key := strings.TrimSpace(r.GetString(kf.field))
			if key == "" || strings.Contains(key, "{{") {
				continue // empty, or made of variables
			}
			name := secretName(kf.prefix, r.GetString("name"), taken)
			sealed, err := secrets.Seal(app, key)
			if err != nil {
				return err
			}
			s := core.NewRecord(col)
			s.Set("name", name)
			s.Set("value", sealed)
			s.Set("hint", secrets.Hint(key))
			s.Set("description", "Moved from "+kf.label+" "+r.GetString("name"))
			if err := app.SaveNoValidate(s); err != nil {
				return err
			}
			r.Set(kf.field, "{{secret."+name+"}}")
			if err := app.SaveNoValidate(r); err != nil {
				return err
			}
		}
	}
	return nil
}

var singleRefRegex = regexp.MustCompile(`^\{\{\s*secret\.([A-Z][A-Z0-9_]*)\s*\}\}$`)

// restoreKeys writes the values back in the fields that only reference a secret.
func restoreKeys(app core.App) error {
	for _, kf := range keyFields {
		if _, err := app.FindCollectionByNameOrId(kf.collection); err != nil {
			continue
		}
		recs, err := app.FindAllRecords(kf.collection)
		if err != nil {
			return err
		}
		for _, r := range recs {
			m := singleRefRegex.FindStringSubmatch(r.GetString(kf.field))
			if m == nil {
				continue
			}
			v, err := secrets.Value(app, m[1])
			if err != nil {
				return err
			}
			r.Set(kf.field, v)
			if err := app.SaveNoValidate(r); err != nil {
				return err
			}
		}
	}
	return nil
}
