package formula

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tools/types"
)

func TestCheck(t *testing.T) {
	date, _ := types.ParseDateTime("2026-03-01 10:00:00.000Z")
	for _, c := range []struct {
		op    string
		value any
		right string
		want  bool
	}{
		{OpEmpty, nil, "", true},
		{OpEmpty, "  ", "", true},
		{OpEmpty, []any{}, "", true},
		{OpEmpty, 0.0, "", false},
		{OpNotEmpty, "x", "", true},
		{OpEquals, "Rare", " rare ", true},
		{OpEquals, Number(5), "5.0", true},
		{OpEquals, true, "true", true},
		{OpEquals, false, "false", true},
		{OpEquals, false, "true", false},
		{OpNotEquals, "a", "b", true},
		{OpGreater, Number(120), "100", true},
		{OpGreater, Number(12), "100", false},
		{OpGreater, nil, "1", false},
		{OpLessEq, date, "2026-03-01 10:00:00.000Z", true},
		{OpLess, date, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02 15:04:05.000Z"), true},
		{OpContains, "Pikachu VMAX", "vmax", true},
		{OpContains, List{"Fire", "Water"}, "water", true},
		{OpNotContains, List{"Fire"}, "water", true},
		{OpStartsWith, "INV-1", "inv", true},
		{OpEndsWith, "a.PDF", ".pdf", true},
		{OpTrue, true, "", true},
		{OpTrue, "false", "", false},
		{OpFalse, nil, "", true},
		{"nope", "x", "x", false},
	} {
		if got := Check(c.op, c.value, c.right); got != c.want {
			t.Errorf("%s %v %q = %v, want %v", c.op, c.value, c.right, got, c.want)
		}
	}
}

func TestSecretsAreRefused(t *testing.T) {
	for _, src := range []string{"{{secret.KEY}}", `{{upper secret.KEY}}`} {
		if _, err := Render(src, map[string]any{}, "record"); err != ErrSecretHere {
			t.Errorf("%s: %v", src, err)
		}
	}
}
