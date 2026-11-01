package media

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/assistant"
	"github.com/Namarusa/PocketbasePlus/internal/medialib"
)

const assistantGuide = `Assets (list_assets): the library of files of the app (images, videos, PDFs, fonts, documents), in the Assets menu. Each file has a public address that never changes: /media/{id}/{name}. Use it for the images of pages (<img src="/media/…" alt="…">, a smaller copy with ?w=320|640|960|1280|1920 for srcset), the image blocks of documents (url), the PDF viewers (data-pb-pdf-src) and the emails (<img src="{{.app.url}}/media/…">). List the files before using one; never invent an address.`

func bindAssistant() {
	assistant.AddExtension(assistant.Extension{
		Name:  "assets",
		Guide: assistantGuide,
		Tools: []assistant.ExtensionTool{
			{Def: ai.Tool{Name: "list_assets", Description: "Files of the Assets library (the newest first): name, address, kind, size, folder, description.",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{
					"kind":   map[string]any{"type": "string", "description": "image | video | audio | pdf | font | document (optional)"},
					"folder": map[string]any{"type": "string", "description": "Only this folder (optional)"},
					"search": map[string]any{"type": "string", "description": "Words of the name or description (optional)"},
				}}}, Run: toolListAssets},
		},
	})
}

func toolListAssets(c *assistant.ToolContext, args map[string]any) (any, error) {
	col, err := c.App.FindCachedCollectionByNameOrId(medialib.Collection)
	if err != nil {
		return nil, medialib.ErrNotInstalled
	}
	q := c.App.RecordQuery(col).OrderBy("created DESC").Limit(100)
	if k, _ := args["kind"].(string); k != "" {
		q.AndWhere(dbx.HashExp{"kind": k})
	}
	if f, _ := args["folder"].(string); f != "" {
		q.AndWhere(dbx.HashExp{"folder": f})
	}
	var recs []*core.Record
	if err := q.All(&recs); err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(stringArg(args, "search")))
	out := []map[string]any{}
	for _, r := range recs {
		text := strings.ToLower(r.GetString("name") + " " + r.GetString("alt"))
		match := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				match = false
			}
		}
		if !match {
			continue
		}
		a := map[string]any{
			"name": r.GetString("name"), "address": medialib.Link(r.Id, linkName(r.GetString("name"), r.GetString("file"))),
			"kind": r.GetString("kind"), "size": r.GetInt("size"),
		}
		if w := r.GetInt("width"); w > 0 {
			a["width"], a["height"] = w, r.GetInt("height")
		}
		for _, k := range []string{"folder", "alt"} {
			if v := r.GetString(k); v != "" {
				a[k] = v
			}
		}
		out = append(out, a)
	}
	return map[string]any{"files": out, "count": len(out)}, nil
}

func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}
