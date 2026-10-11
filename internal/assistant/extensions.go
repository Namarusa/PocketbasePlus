package assistant

import (
	"context"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
)

// Extension adds tools to the assistant from an optional module (e.g. the
// Pages module), which registers it in its Bind: the assistant doesn't import
// the module, so removing the module removes its tools.
type Extension struct {
	Name  string // module name, e.g. "pages" (listed as supported in the prompt)
	Guide string // added to the system prompt
	Tools []ExtensionTool
	// Apply applies the proposals of the extension, by action type.
	Apply map[string]func(ctx context.Context, app core.App, auth *core.Record, payload map[string]any) (map[string]any, error)
}

// ExtensionTool is a tool of an extension.
type ExtensionTool struct {
	Def ai.Tool
	Run func(c *ToolContext, args map[string]any) (any, error)
}

// ToolContext is what an extension tool gets.
type ToolContext struct {
	App  core.App
	Auth *core.Record
	Ctx  context.Context
	t    *toolbox
}

// Propose adds a proposal card (applied by the user with the extension's
// Apply function of its type).
func (c *ToolContext) Propose(typ, title, summary string, details, payload map[string]any) map[string]any {
	return c.t.propose(&Action{Type: typ, Title: title, Summary: summary, Details: details, Payload: payload})
}

var extensions = map[string]Extension{}

// AddExtension registers (or replaces) an extension.
func AddExtension(e Extension) {
	extensions[e.Name] = e
}

func sortedExtensions() []Extension {
	out := make([]Extension, 0, len(extensions))
	for _, e := range extensions {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func extensionTool(name string) *ExtensionTool {
	for _, e := range extensions {
		for i := range e.Tools {
			if e.Tools[i].Def.Name == name {
				return &e.Tools[i]
			}
		}
	}
	return nil
}

func extensionApply(typ string) func(context.Context, core.App, *core.Record, map[string]any) (map[string]any, error) {
	for _, e := range extensions {
		if fn := e.Apply[typ]; fn != nil {
			return fn
		}
	}
	return nil
}

// unsupported lists what the assistant can't change yet.
func unsupported() string {
	list := []string{"forms", "permissions", "email automations", "the collections' structure"}
	if _, ok := extensions["pages"]; !ok {
		list = append([]string{"pages"}, list...)
	}
	return strings.Join(list[:len(list)-1], ", ") + " or " + list[len(list)-1]
}
