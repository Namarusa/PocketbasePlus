package pages

import (
	"net/url"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Namarusa/PocketbasePlus/internal/session"
)

// Page actions are plain forms posted to the server (the pages run no
// scripts): a form or button with data-pb-action runs that page action flow,
// one with data-pb-logout signs the visitor out. The server answers with the
// next page and a message, shown by data-pb-message (or at the top).

// actionForm turns an action element into a form posting to its action, with
// the hidden fields the server needs. A button or link is wrapped in a form.
func (r *renderer) actionForm(n *xhtml.Node, s scope) *xhtml.Node {
	flow, hasFlow := getAttr(n, attrAction), hasAttr(n, attrAction)
	logout := getAttr(n, attrLogout)
	removeAttr(n, attrAction)
	removeAttr(n, attrLogout)

	form := n
	if n.DataAtom != atom.Form {
		form = &xhtml.Node{Type: xhtml.ElementNode, Data: "form", DataAtom: atom.Form, Attr: []xhtml.Attribute{{Key: "style", Val: "display:contents"}}}
		if n.Parent != nil {
			n.Parent.InsertBefore(form, n)
			n.Parent.RemoveChild(n)
		}
		form.AppendChild(n)
		if n.DataAtom == atom.A {
			// a link can't submit: it becomes a button looking the same
			n.Data, n.DataAtom = "button", atom.Button
			removeAttr(n, "href")
			setAttr(n, "type", "submit")
		}
	}
	setAttr(form, "method", "post")
	removeAttr(form, "target")
	removeAttr(form, "enctype")

	if hasFlow {
		if flow == "" {
			setAttr(form, "action", "#")
			r.fail(form, "choose the flow of the action")
			return form
		}
		setAttr(form, "action", "/api/flows/action/"+url.PathEscape(flow))
		r.hidden(form, "_page", r.path)
		if next := session.LocalPath(r.query.Get("next")); next != "" {
			r.hidden(form, "_next", next)
		}
		if s.rec != nil && !s.self {
			r.hidden(form, "_rec", session.SignRecord(r.app, flow, s.col.Name, s.rec.Id))
		}
		return form
	}
	setAttr(form, "action", "/api/session/logout")
	next := session.LocalPath(logout)
	if next == "" {
		next = "/"
	}
	r.hidden(form, "_next", next)
	return form
}

func (r *renderer) hidden(form *xhtml.Node, name, value string) {
	form.AppendChild(&xhtml.Node{Type: xhtml.ElementNode, Data: "input", DataAtom: atom.Input, Attr: []xhtml.Attribute{
		{Key: "type", Val: "hidden"}, {Key: "name", Val: name}, {Key: "value", Val: value},
	}})
}

// message shows the message of the last action in the element (hidden without one).
func (r *renderer) message(n *xhtml.Node) {
	removeAttr(n, attrMessage)
	clearChildren(n)
	if r.flash == nil || r.flashShown {
		setAttr(n, "hidden", "")
		return
	}
	r.flashShown = true
	box := flashNode(r.flash)
	for _, a := range box.Attr {
		if a.Key == "class" {
			a.Val = strings.TrimSpace(getAttr(n, "class") + " " + a.Val)
		}
		setAttr(n, a.Key, a.Val)
	}
	n.AppendChild(textNode(r.flash.Text))
}

func flashNode(f *session.Flash) *xhtml.Node {
	role := "status"
	if f.Type == "error" {
		role = "alert"
	}
	n := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div, Attr: []xhtml.Attribute{
		{Key: "class", Val: "gpp-flash gpp-flash-" + f.Type}, {Key: "role", Val: role},
	}}
	n.AppendChild(textNode(f.Text))
	return n
}

const flashCSS = `
.gpp-flash{padding:12px 16px;margin:0 0 16px;border-radius:8px;border:1px solid;font:14px/1.5 system-ui,sans-serif}
.gpp-flash-success{background:#ecfdf5;border-color:#a7f3d0;color:#065f46}
.gpp-flash-error{background:#fef2f2;border-color:#fecaca;color:#991b1b}
.gpp-flash-info{background:#eff6ff;border-color:#bfdbfe;color:#1e40af}
.dark .gpp-flash-success{background:#052e24;border-color:#065f46;color:#a7f3d0}
.dark .gpp-flash-error{background:#3b0d0d;border-color:#991b1b;color:#fecaca}
.dark .gpp-flash-info{background:#0b1d3a;border-color:#1e40af;color:#bfdbfe}`
