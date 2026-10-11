package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Page actions: flows started by the buttons and forms of the pages.
func init() {
	m.Register(func(app core.App) error {
		return setFlowTrigger(app, true)
	}, func(app core.App) error {
		return setFlowTrigger(app, false)
	})
}

func setFlowTrigger(app core.App, add bool) error {
	c, err := app.FindCollectionByNameOrId("flows")
	if err != nil {
		return err
	}
	f, ok := c.Fields.GetByName("trigger_type").(*core.SelectField)
	if !ok {
		return nil
	}
	has := slices.Contains(f.Values, "action")
	switch {
	case add && !has:
		f.Values = append(f.Values, "action")
	case !add && has:
		f.Values = slices.DeleteFunc(f.Values, func(v string) bool { return v == "action" })
	default:
		return nil
	}
	return app.Save(c)
}
