package pages

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Site parts are pages of kind "part" (a header, a footer, a sidebar...)
// placed on other pages by a placeholder: editing a part changes every page
// using it. They're expanded before the bindings are filled, so their lists
// and fields show the data of the page they're on.

// expandParts replaces the placeholders of the site parts with their HTML and
// returns their CSS. Parts don't nest: placeholders inside parts are removed.
func expandParts(app core.App, body *xhtml.Node, preview bool) string {
	var holders []*xhtml.Node
	var visit func(n *xhtml.Node)
	visit = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && hasAttr(c, attrPart) {
				holders = append(holders, c)
				continue
			}
			visit(c)
		}
	}
	visit(body)
	if len(holders) == 0 {
		return ""
	}

	parts := map[string]*core.Record{}
	var css strings.Builder
	for _, n := range holders {
		slug := getAttr(n, attrPart)
		part, seen := parts[slug]
		if !seen {
			part, _ = app.FindFirstRecordByFilter(Collection, "slug = {:slug} && kind = {:kind}", dbx.Params{"slug": slug, "kind": kindPart})
			parts[slug] = part
			if part != nil {
				css.WriteString("\n" + part.GetString("css"))
			}
		}
		var content *xhtml.Node
		if part != nil {
			content, _ = parseBody(part.GetString("html"))
		}
		if content == nil {
			if preview {
				n.Parent.InsertBefore(previewHint("Unknown site part: "+slug), n)
			}
			n.Parent.RemoveChild(n)
			continue
		}
		removeParts(content)
		for c := content.FirstChild; c != nil; {
			next := c.NextSibling
			content.RemoveChild(c)
			n.Parent.InsertBefore(c, n)
			c = next
		}
		n.Parent.RemoveChild(n)
	}
	return css.String()
}

func removeParts(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == xhtml.ElementNode && hasAttr(c, attrPart) {
			n.RemoveChild(c)
		} else {
			removeParts(c)
		}
		c = next
	}
}

func previewHint(text string) *xhtml.Node {
	p := &xhtml.Node{Type: xhtml.ElementNode, Data: "p", DataAtom: atom.P,
		Attr: []xhtml.Attribute{{Key: "style", Val: "padding:8px;margin:0;background:#fef3c7;color:#92400e;font:13px system-ui,sans-serif"}}}
	p.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: text})
	return p
}
