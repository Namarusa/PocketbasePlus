package pages

import (
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Rich text of the records (editor fields) is written by the users of the
// app, not by the superuser who built the page: only formatting is kept
// (an allow list). Forms, buttons, styles sheets, frames and media players
// are removed, so record content can't restyle the page or plant a button
// running a page action as the visitor.

var richTags = map[string]bool{
	"p": true, "br": true, "b": true, "strong": true, "i": true, "em": true, "u": true, "s": true, "strike": true,
	"del": true, "ins": true, "sub": true, "sup": true, "mark": true, "small": true, "span": true, "div": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "li": true,
	"blockquote": true, "pre": true, "code": true, "hr": true, "a": true, "img": true, "figure": true,
	"figcaption": true, "table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "th": true,
	"td": true, "caption": true, "colgroup": true, "col": true, "dl": true, "dt": true, "dd": true,
	"abbr": true, "cite": true, "q": true, "kbd": true, "time": true,
}

// richDropped are removed with their content; other unknown elements are
// replaced by their content.
var richDropped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "iframe": true, "frame": true,
	"frameset": true, "object": true, "embed": true, "applet": true, "form": true, "input": true,
	"button": true, "select": true, "textarea": true, "option": true, "svg": true, "math": true,
	"video": true, "audio": true, "source": true, "track": true, "canvas": true, "head": true,
	"title": true, "meta": true, "link": true, "base": true, "dialog": true, "portal": true,
}

var richAttrs = map[string]bool{
	"href": true, "src": true, "alt": true, "title": true, "width": true, "height": true, "colspan": true,
	"rowspan": true, "align": true, "style": true, "target": true, "datetime": true, "start": true,
	"reversed": true, "scope": true, "lang": true, "dir": true,
}

// richStyles are the CSS properties kept in style attributes (text
// formatting; no positioning, so content can't cover the page).
var richStyles = map[string]bool{
	"color": true, "background-color": true, "text-align": true, "font-weight": true, "font-style": true,
	"font-size": true, "text-decoration": true, "vertical-align": true, "width": true, "height": true,
	"max-width": true, "float": true, "list-style-type": true, "border": true, "border-collapse": true,
	"padding": true, "padding-left": true, "padding-right": true, "padding-top": true, "padding-bottom": true,
	"margin": true, "margin-left": true, "margin-right": true, "margin-top": true, "margin-bottom": true,
	"text-indent": true, "line-height": true,
}

// sanitizeRich keeps the allowed elements and attributes of rich text.
func sanitizeRich(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch c.Type {
		case xhtml.ElementNode:
			tag := strings.ToLower(c.Data)
			switch {
			case c.Namespace != "" || richDropped[tag]:
				n.RemoveChild(c)
			case !richTags[tag]:
				// unknown element: its content takes its place
				sanitizeRich(c)
				for gc := c.FirstChild; gc != nil; {
					gnext := gc.NextSibling
					c.RemoveChild(gc)
					n.InsertBefore(gc, c)
					gc = gnext
				}
				n.RemoveChild(c)
			default:
				richElementAttrs(c, tag)
				sanitizeRich(c)
			}
		case xhtml.CommentNode, xhtml.DoctypeNode:
			n.RemoveChild(c)
		}
		c = next
	}
}

func richElementAttrs(n *xhtml.Node, tag string) {
	kept := n.Attr[:0]
	blank := false
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" || !richAttrs[key] {
			continue
		}
		switch key {
		case "href":
			if tag != "a" || !safeURL(a.Val, false) {
				continue
			}
		case "src":
			if tag != "img" || !safeURL(a.Val, true) {
				continue
			}
		case "style":
			a.Val = richStyle(a.Val)
			if a.Val == "" {
				continue
			}
		case "target":
			if a.Val != "_blank" {
				continue
			}
			blank = true
		}
		kept = append(kept, a)
	}
	n.Attr = kept
	if tag == "a" {
		// links of the users: no referrer, not endorsed by the site
		rel := "nofollow ugc"
		if blank {
			rel += " noopener noreferrer"
		}
		setAttr(n, "rel", rel)
	}
	if tag == "img" {
		setAttr(n, "loading", "lazy")
	}
}

// richStyle keeps the allowed declarations of a style attribute, without
// URLs or escapes.
func richStyle(v string) string {
	var out []string
	for _, decl := range strings.Split(v, ";") {
		prop, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		val = strings.TrimSpace(val)
		lower := strings.ToLower(val)
		if !richStyles[prop] || val == "" || strings.ContainsAny(val, `\<>"'`) ||
			strings.Contains(lower, "url(") || strings.Contains(lower, "expression") || strings.Contains(lower, "@") {
			continue
		}
		out = append(out, prop+":"+val)
	}
	return strings.Join(out, ";")
}

// parseHTMLFragment parses and sanitizes rich text (editor fields).
func parseHTMLFragment(src string) []*xhtml.Node {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := xhtml.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return nil
	}
	wrapper := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, c := range nodes {
		wrapper.AppendChild(c)
	}
	sanitizeRich(wrapper)
	var out []*xhtml.Node
	for c := wrapper.FirstChild; c != nil; {
		next := c.NextSibling
		wrapper.RemoveChild(c)
		out = append(out, c)
		c = next
	}
	return out
}
