package dashboards

import (
	"context"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// IsPanelType reports whether t is a known panel type.
func IsPanelType(t string) bool {
	return panelTypes[t]
}

// RunPanel computes the data of a panel (unsaved, without dashboard
// variables) with the access of auth (superusers see all the data).
func RunPanel(ctx context.Context, app core.App, auth *core.Record, panelType string, options map[string]any) (any, error) {
	return RunPanelParams(ctx, app, auth, panelType, options, nil)
}

// RunPanelParams is RunPanel with params bound in the filter ({:name}).
func RunPanelParams(ctx context.Context, app core.App, auth *core.Record, panelType string, options map[string]any, params dbx.Params) (any, error) {
	if !panelTypes[panelType] {
		return nil, fmt.Errorf("unknown panel type %q", panelType)
	}
	r := &runner{app: app, auth: auth, ctx: ctx, vars: map[string]resolvedVar{}, params: params}
	res := runPanel(r, panelType, options)
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return res.Data, nil
}
