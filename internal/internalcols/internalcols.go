// Package internalcols lists the collections owned by the PocketBasePlus modules.
// They never get RBAC generated rules, never trigger email automations or
// flows, and can't be targeted by them.
package internalcols

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

var names = map[string]bool{
	"roles":              true,
	"rbac_collections":   true,
	"permissions":        true,
	"email_templates":    true,
	"email_automations":  true,
	"email_messages":     true,
	"flows":              true,
	"flow_runs":          true,
	"dashboards":         true,
	"dashboard_panels":   true,
	"collection_groups":  true,
	"revisions":          true,
	"comments":           true,
	"notifications":      true,
	"saved_views":        true,
	"forms":              true,
	"ai_providers":       true,
	"ai_models":          true,
	"email_unsubscribes": true,
	"ai_settings":        true,
	"ai_conversations":   true,
	"secrets":            true,
}

// Add declares the collections of an optional module (registered from its
// Register function, so removing the module removes them from the list).
func Add(collections ...string) {
	for _, n := range collections {
		names[n] = true
	}
}

// Is reports whether the collection is a module or PocketBase system collection.
func Is(c *core.Collection) bool {
	return names[c.Name] || strings.HasPrefix(c.Name, "_")
}
