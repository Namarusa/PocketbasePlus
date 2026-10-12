package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The test data of the flow editor: the runs started from the editor keep
// the trigger data and the output of each step (shortened, without the
// secrets), so the variable picker can show the fields and their values.
func init() {
	m.Register(func(app core.App) error {
		runs, err := app.FindCollectionByNameOrId("flow_runs")
		if err != nil {
			return err
		}
		if runs.Fields.GetByName("sample") == nil {
			runs.Fields.Add(&core.JSONField{Name: "sample", MaxSize: 1 << 20})
		}
		return app.Save(runs)
	}, func(app core.App) error {
		runs, err := app.FindCollectionByNameOrId("flow_runs")
		if err != nil {
			return err
		}
		runs.Fields.RemoveByName("sample")
		return app.Save(runs)
	})
}
