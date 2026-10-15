package pages

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/assistant"
)

// The tools of the AI assistant for the pages: read a page (its settings and
// an outline of its elements, with their bindings), propose a new page, and
// propose changes to one (settings, the whole HTML, or edits of elements).
// Proposals show a preview rendered by the server; applying one keeps a
// version of the page before and after (History in the builder).

const (
	actionPage       = "page"
	maxProposalHTML  = 300 << 10
	maxPreviewHTML   = 400 << 10
	maxOutline       = 220
	maxInlineHTML    = 7000
	maxElementHTML   = 9000
	previewWarnLabel = " (only shown in previews)"
)

const pagesGuide = `Pages (get_page, propose_page, propose_page_change): web pages of the site, rendered by the server from their HTML at /{slug} (record pages at /{slug}/{record id}). Read a page with get_page before changing it.
- Page HTML is plain HTML with inline styles or classes (CSS in "css"), no scripts. Bindings are data-pb-* attributes:
  - Collection list: <div data-pb-list="products" data-pb-filter="published = true" data-pb-sort="-created" data-pb-limit="12" data-pb-access="rules"> with one child data-pb-item (repeated per record) and optionally one data-pb-empty child (no records). data-pb-access="rules" applies the visitor's API rules (use it unless the page is the gate). Filters can use {{record.field}} (the enclosing record), {{query.param}} (page address), {{auth.id}} (signed-in visitor).
  - Inside an item, a record page, or with data-pb-user (the signed-in visitor): <h3 data-pb-field="name"></h3>, <img data-pb-field="photo">, relations "brand.name" (2 hops max), dates with data-pb-format="date|datetime|time", <a data-pb-link="product">View</a> (link to the record page with slug "product"), <a data-pb-href="website">Site</a> (URL, email or file field as the target).
  - Formula: <p data-pb-formula="{{currency &quot;EUR&quot; record.price}} · added {{timeAgo record.created}}"></p> (the functions; record, auth, query without a dot; escape quotes as &quot; in the attribute).
  - Form: <iframe data-pb-form="contact" style="width:100%;height:640px;border:0"></iframe> (a form's slug).
  - Chart: <div data-pb-chart="bar|line|area|pie|donut" data-pb-collection="orders" data-pb-fn="count|sum|avg|min|max" data-pb-value="total" data-pb-group="status" data-pb-date="created" data-pb-precision="day|week|month" data-pb-range="7d|30d|90d|365d"></div>.
  - Members: data-pb-visible="guests|users|role:<id>" shows an element to some visitors; <form data-pb-action="<page action flow id>"> runs a flow with the form fields (trigger.form.<name>); <form data-pb-logout="/"> signs out; <div data-pb-message></div> shows the message of the last action.
- Pages use the site theme (kit "basecoat", older pages may be plain) and the kit's component classes (btn, btn-outline, btn-secondary, card, badge, alert, input, label, table); Tailwind utility classes aren't available, so lay out with inline styles. Plain pages: inline styles or CSS.
- New pages are drafts (published false) unless the user asked to publish. Site parts (kind "part") are reusable headers and footers placed with <div data-pb-part="slug"></div>.
- propose_page_change edits elements by ref (from get_page's outline): replace, before, after, append, prepend, remove; or "html" replaces the whole body. Keep the existing design: change only what's asked. The card shows the rendered result; binding errors come back to you to fix.`

func bindAssistant() {
	assistant.AddExtension(assistant.Extension{
		Name:  "pages",
		Guide: pagesGuide,
		Tools: []assistant.ExtensionTool{
			{Def: ai.Tool{Name: "get_page", Description: "A page: its settings and an outline of its elements (ref, tag, bindings, text); its HTML when small, or one element's HTML with element_ref.",
				Parameters: obj(map[string]any{
					"page":        str("Page id or slug"),
					"element_ref": str("A ref of the outline: that element's HTML"),
				}, "page")}, Run: toolGetPage},
			{Def: ai.Tool{Name: "propose_page", Description: "Proposes a new page (single page, collection page, single item page or site part); it uses the site theme. Checked and previewed by the server. The user applies it.",
				Parameters: obj(map[string]any{
					"title":             str("Page title"),
					"slug":              str("Address: lowercase letters, digits and dashes (/slug)"),
					"kind":              str("page (default: a single page) | collection (lists a collection: set list_collection) | record (a single item page, one record per address: set record_collection) | part (site part)"),
					"list_collection":   str("Collection pages: the collection listed"),
					"record_collection": str("Single item pages: the collection"),
					"record_filter":     str("Single item pages: a filter the shown records must match (optional)"),
					"description":       str("Meta description (optional)"),
					"html":              str("The body HTML (see the pages guide)"),
					"css":               str("CSS (optional)"),
					"published":         boolean("Publish it now (default false: a draft)"),
					"access":            str("public (default) | users | roles"),
					"roles":             strList("Role ids (access roles)"),
				}, "title", "slug", "html")}, Run: toolProposePage},
			{Def: ai.Tool{Name: "propose_page_change", Description: "Proposes changes to a page: settings, edits of elements by ref, or the whole HTML. Previewed by the server. The user applies it (a version is kept).",
				Parameters: obj(map[string]any{
					"page":          str("Page id or slug"),
					"change":        str("What changes, in a few words"),
					"title":         str("New title"),
					"slug":          str("New address"),
					"description":   str("New meta description"),
					"published":     boolean("Publish / unpublish"),
					"record_filter": str("Record pages: new filter"),
					"html":          str("New body HTML (replaces everything; prefer edits)"),
					"css":           str("New CSS (replaces it)"),
					"css_add":       str("CSS added at the end"),
					"edits": map[string]any{"type": "array", "description": "Edits of elements, in order", "items": obj(map[string]any{
						"ref":    str("Element ref from get_page"),
						"action": str("replace | before | after | append | prepend | remove"),
						"html":   str("HTML to insert (not for remove)"),
					}, "ref", "action")},
				}, "page")}, Run: toolProposePageChange},
		},
		Apply: map[string]func(context.Context, core.App, *core.Record, map[string]any) (map[string]any, error){
			actionPage: applyPageProposal,
		},
	})
}

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func argStr(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

func findPage(app core.App, ref string) (*core.Record, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "/")
	if ref == "" {
		return nil, errors.New("page is required")
	}
	if p, err := app.FindRecordById(Collection, ref); err == nil {
		return p, nil
	}
	if p, err := app.FindFirstRecordByData(Collection, "slug", ref); err == nil {
		return p, nil
	}
	return nil, fmt.Errorf("unknown page %q (list them with list_resources kind pages)", ref)
}

// --- reading -----------------------------------------------------------------------

func toolGetPage(c *assistant.ToolContext, args map[string]any) (any, error) {
	page, err := findPage(c.App, argStr(args, "page"))
	if err != nil {
		return nil, err
	}
	body, err := parseBody(page.GetString("html"))
	if err != nil {
		return nil, err
	}
	if ref := argStr(args, "element_ref"); ref != "" {
		n := findRef(body, ref)
		if n == nil {
			return nil, fmt.Errorf("no element %q (refs come from the outline)", ref)
		}
		out := nodeHTML(n)
		if len(out) > maxElementHTML {
			return nil, fmt.Errorf("the element is too large (%d characters): read its children", len(out))
		}
		return map[string]any{"ref": ref, "html": out}, nil
	}
	out := map[string]any{
		"id": page.Id, "title": page.GetString("title"), "slug": page.GetString("slug"), "address": pagePath(page),
		"published": page.GetBool("published"), "home": page.GetBool("home"), "description": page.GetString("description"),
		"kind": pageKind(page), "kit": page.GetString("kit"), "access": page.GetString("access"), "roles": page.GetStringSlice("roles"),
		"link": "#/pages/" + page.Id, "updated": page.GetString("updated"),
	}
	if id := page.GetString("record_collection"); id != "" {
		if col, err := c.App.FindCachedCollectionByNameOrId(id); err == nil {
			out["record_collection"] = col.Name
		}
		out["record_filter"] = page.GetString("record_filter")
	}
	items := []map[string]any{}
	outline(body, "", 0, &items)
	out["outline"] = items
	if len(items) >= maxOutline {
		out["outline_note"] = "cut: read the elements with element_ref"
	}
	if h := innerHTML(body); len(h) <= maxInlineHTML {
		out["html"] = h
		if css := page.GetString("css"); len(css) <= maxInlineHTML {
			out["css"] = css
		}
	} else {
		out["html_note"] = fmt.Sprintf("%d characters: read elements with element_ref", len(h))
	}
	return out, nil
}

func pageKind(p *core.Record) string {
	switch {
	case p.GetString("kind") == kindPart:
		return "part"
	case p.GetString("kind") == kindCollection:
		return "collection"
	case p.GetString("record_collection") != "":
		return "record"
	}
	return "page"
}

func pagePath(p *core.Record) string {
	switch pageKind(p) {
	case "part":
		return "(site part)"
	case "record":
		return "/" + p.GetString("slug") + "/{record id}"
	}
	if p.GetBool("home") {
		return "/"
	}
	return "/" + p.GetString("slug")
}

var skipOutline = map[string]bool{"br": true, "wbr": true, "source": true, "track": true, "path": true, "svg": true}

// outline lists the elements with their ref: their id, or the positions of
// the element children from the body ("2.0.1").
func outline(n *xhtml.Node, path string, depth int, items *[]map[string]any) {
	i := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		p := strconv.Itoa(i)
		if path != "" {
			p = path + "." + p
		}
		i++
		if len(*items) >= maxOutline {
			return
		}
		if skipOutline[c.Data] {
			continue
		}
		item := map[string]any{"ref": p, "tag": c.Data, "depth": depth}
		if id := getAttr(c, "id"); id != "" {
			item["ref"] = "#" + id
		}
		bindings := map[string]string{}
		for _, a := range c.Attr {
			if strings.HasPrefix(a.Key, "data-pb-") {
				bindings[strings.TrimPrefix(a.Key, "data-pb-")] = a.Val
			}
		}
		if len(bindings) > 0 {
			item["bindings"] = bindings
		}
		if cls := strings.Fields(getAttr(c, "class")); len(cls) > 0 {
			item["class"] = strings.Join(cls[:min(len(cls), 4)], " ")
		}
		if t := ownText(c); t != "" {
			item["text"] = t
		}
		*items = append(*items, item)
		if depth < 10 {
			outline(c, p, depth+1, items)
		}
	}
}

func ownText(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode {
			b.WriteString(c.Data)
		}
	}
	t := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(t); len(r) > 70 {
		t = string(r[:70]) + "…"
	}
	return t
}

var pathRefRegex = regexp.MustCompile(`^\d+(\.\d+)*$`)

// findRef finds an element by its outline ref.
func findRef(body *xhtml.Node, ref string) *xhtml.Node {
	ref = strings.TrimSpace(ref)
	if id, ok := strings.CutPrefix(ref, "#"); ok {
		return find(body, func(n *xhtml.Node) bool { return getAttr(n, "id") == id })
	}
	if !pathRefRegex.MatchString(ref) {
		return find(body, func(n *xhtml.Node) bool { return getAttr(n, "id") == ref })
	}
	n := body
	for _, part := range strings.Split(ref, ".") {
		want, _ := strconv.Atoi(part)
		var next *xhtml.Node
		i := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			if i == want {
				next = c
				break
			}
			i++
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

func nodeHTML(n *xhtml.Node) string {
	var b strings.Builder
	_ = xhtml.Render(&b, n)
	return b.String()
}

func innerHTML(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = xhtml.Render(&b, c)
	}
	return b.String()
}

// --- proposals ----------------------------------------------------------------------

// cleanBody parses the body HTML of a proposal, without scripts and handlers.
func cleanBody(src string) (*xhtml.Node, error) {
	if len(src) > maxProposalHTML {
		return nil, fmt.Errorf("the HTML is too long (max %d KB)", maxProposalHTML>>10)
	}
	if !strings.Contains(strings.ToLower(src), "<body") {
		src = "<body>" + src + "</body>"
	}
	body, err := parseBody(src)
	if err != nil {
		return nil, err
	}
	sanitize(body)
	return body, nil
}

var previewErrorRegex = regexp.MustCompile(`⚠ (.*?)` + regexp.QuoteMeta(previewWarnLabel))

// previewPage renders a page as the superuser preview does (a record page
// with its first record) and returns the HTML and the binding errors.
func previewPage(ctx context.Context, app core.App, page *core.Record) (string, []string, error) {
	view := pageView{Title: page.GetString("title"), Description: page.GetString("description"), HTML: page.GetString("html"), CSS: page.GetString("css"), Visitor: visitor{all: true}, Path: "/"}
	if page.GetString("kit") == kitBasecoat {
		t := loadTheme(app)
		view.Kit = &t
	}
	if colID := page.GetString("record_collection"); colID != "" {
		col, err := listCollection(app, colID)
		if err != nil {
			return "", nil, err
		}
		filter, params := bindPlaceholders(page.GetString("record_filter"), scope{}, nil, nil)
		if strings.TrimSpace(filter) == "" {
			filter = "id != ''"
		}
		if list, err := app.FindRecordsByFilter(col, filter, "-created", 1, 0, params); err == nil && len(list) > 0 {
			view.Scope = scope{col: col, rec: list[0]}
		}
	}
	out, _, err := render(ctx, app, view, url.Values{}, true)
	if err != nil {
		return "", nil, err
	}
	var problems []string
	for _, m := range previewErrorRegex.FindAllStringSubmatch(out, 20) {
		problems = append(problems, xhtml.UnescapeString(m[1]))
	}
	return out, problems, nil
}

func previewDetails(details map[string]any, html string) {
	if len(html) <= maxPreviewHTML {
		details["preview"] = html
	} else {
		details["preview_note"] = "The page is too large to preview here: preview it after applying."
	}
}

func toolProposePage(c *assistant.ToolContext, args map[string]any) (any, error) {
	col, err := c.App.FindCachedCollectionByNameOrId(Collection)
	if err != nil {
		return nil, err
	}
	body, err := cleanBody(argStr(args, "html"))
	if err != nil {
		return nil, err
	}
	page := core.NewRecord(col)
	slug := argStr(args, "slug")
	page.Load(map[string]any{
		"title": argStr(args, "title"), "slug": slug, "description": argStr(args, "description"),
		"html": nodeHTML(body), "css": argStr(args, "css"), "published": args["published"] == true,
	})
	if _, err := c.App.FindFirstRecordByData(Collection, "slug", slug); err == nil {
		return nil, fmt.Errorf("the slug %q is taken: pick another one", slug)
	}
	// every new page uses the site theme (Pages → Theme)
	page.Set("kit", kitBasecoat)
	switch argStr(args, "kind") {
	case "part":
		page.Set("kind", kindPart)
		page.Set("published", false)
	case kindCollection:
		page.Set("kind", kindCollection)
		page.Set("list_collection", argStr(args, "list_collection"))
	case "record":
		if argStr(args, "record_collection") == "" {
			return nil, errors.New("a record page needs record_collection")
		}
	}
	if rc := argStr(args, "record_collection"); rc != "" && argStr(args, "kind") != "part" {
		page.Set("record_collection", rc)
		page.Set("record_filter", argStr(args, "record_filter"))
		page.Set("record_rules", true)
	}
	if a := argStr(args, "access"); a != "" {
		page.Set("access", a)
		page.Set("roles", args["roles"])
	}
	if err := c.App.Validate(page); err != nil {
		return nil, fmt.Errorf("invalid page: %w", err)
	}
	preview, problems, err := previewPage(c.Ctx, c.App, page)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("the page has binding errors, fix them: %s", strings.Join(problems, "; "))
	}
	details := map[string]any{"address": pagePath(page), "published": page.GetBool("published")}
	previewDetails(details, preview)
	payload := map[string]any{}
	for _, k := range []string{"title", "slug", "description", "html", "css", "published", "kit", "kind", "list_collection", "record_collection", "record_filter", "record_rules", "access", "roles"} {
		payload[k] = page.Get(k)
	}
	state := "draft"
	if page.GetBool("published") {
		state = "published"
	}
	return c.Propose(actionPage, "New page “"+page.GetString("title")+"”", fmt.Sprintf("%s at %s (%s)", map[string]string{"page": "Single page", "collection": "Collection page", "record": "Single item page", "part": "Site part"}[pageKind(page)], pagePath(page), state), details, payload), nil
}

func toolProposePageChange(c *assistant.ToolContext, args map[string]any) (any, error) {
	page, err := findPage(c.App, argStr(args, "page"))
	if err != nil {
		return nil, err
	}
	draft := page.Fresh()
	changes := []string{}
	for _, k := range []string{"title", "slug", "description", "record_filter"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != page.GetString(k) {
			draft.Set(k, strings.TrimSpace(v))
			changes = append(changes, k)
		}
	}
	if v, ok := args["published"].(bool); ok && v != page.GetBool("published") {
		draft.Set("published", v)
		changes = append(changes, map[bool]string{true: "published", false: "unpublished"}[v])
	}
	if s := argStr(args, "slug"); s != "" && s != page.GetString("slug") {
		if _, err := c.App.FindFirstRecordByData(Collection, "slug", s); err == nil {
			return nil, fmt.Errorf("the slug %q is taken", s)
		}
	}

	var body *xhtml.Node
	if h := argStr(args, "html"); h != "" {
		if body, err = cleanBody(h); err != nil {
			return nil, err
		}
		changes = append(changes, "whole HTML")
	} else if edits, _ := args["edits"].([]any); len(edits) > 0 {
		if body, err = parseBody(page.GetString("html")); err != nil {
			return nil, err
		}
		for i, raw := range edits {
			e, _ := raw.(map[string]any)
			if err := applyEdit(body, argStr(e, "ref"), argStr(e, "action"), fmt.Sprint(e["html"])); err != nil {
				return nil, fmt.Errorf("edit %d: %w", i+1, err)
			}
		}
		sanitize(body)
		changes = append(changes, fmt.Sprintf("%d element edit(s)", len(edits)))
	}
	if body != nil {
		draft.Set("html", nodeHTML(body))
		if len(draft.GetString("html")) > maxProposalHTML {
			return nil, errors.New("the page HTML is too long")
		}
	}
	if css, ok := args["css"].(string); ok {
		draft.Set("css", css)
		changes = append(changes, "CSS")
	}
	if add := argStr(args, "css_add"); add != "" {
		draft.Set("css", draft.GetString("css")+"\n"+add)
		changes = append(changes, "CSS")
	}
	if len(changes) == 0 {
		return nil, errors.New("nothing changes")
	}
	if err := c.App.Validate(draft); err != nil {
		return nil, fmt.Errorf("invalid page: %w", err)
	}
	preview, problems, err := previewPage(c.Ctx, c.App, draft)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("the page has binding errors, fix them: %s", strings.Join(problems, "; "))
	}
	details := map[string]any{"address": pagePath(draft), "changes": changes}
	previewDetails(details, preview)
	payload := map[string]any{"page_id": page.Id, "updated": page.GetString("updated")}
	for _, k := range []string{"title", "slug", "description", "record_filter", "published", "html", "css"} {
		payload[k] = draft.Get(k)
	}
	payload["html_changed"] = body != nil
	summary := strings.Join(changes, ", ")
	if ch := argStr(args, "change"); ch != "" {
		summary = ch + " (" + summary + ")"
	}
	return c.Propose(actionPage, "Change the page “"+page.GetString("title")+"”", summary, details, payload), nil
}

// applyEdit changes the body at an element.
func applyEdit(body *xhtml.Node, ref, action, src string) error {
	n := findRef(body, ref)
	if n == nil {
		return fmt.Errorf("no element %q", ref)
	}
	if action == "remove" {
		if n.Parent == nil || n == body {
			return errors.New("the body can't be removed")
		}
		n.Parent.RemoveChild(n)
		return nil
	}
	context := n
	if action == "replace" || action == "before" || action == "after" {
		if n.Parent == nil || n == body {
			return errors.New("use html to replace the whole body")
		}
		context = n.Parent
	}
	ctxNode := &xhtml.Node{Type: xhtml.ElementNode, Data: context.Data, DataAtom: context.DataAtom}
	if context.DataAtom == atom.Body {
		ctxNode = &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	}
	nodes, err := xhtml.ParseFragment(strings.NewReader(src), ctxNode)
	if err != nil {
		return err
	}
	switch action {
	case "replace", "before":
		for _, c := range nodes {
			n.Parent.InsertBefore(c, n)
		}
		if action == "replace" {
			n.Parent.RemoveChild(n)
		}
	case "after":
		next := n.NextSibling
		for _, c := range nodes {
			n.Parent.InsertBefore(c, next)
		}
	case "append":
		for _, c := range nodes {
			n.AppendChild(c)
		}
	case "prepend":
		first := n.FirstChild
		for _, c := range nodes {
			n.InsertBefore(c, first)
		}
	default:
		return fmt.Errorf("unknown action %q (replace, before, after, append, prepend, remove)", action)
	}
	return nil
}

// --- apply -------------------------------------------------------------------------

func applyPageProposal(ctx context.Context, app core.App, auth *core.Record, p map[string]any) (map[string]any, error) {
	by := "AI assistant"
	if auth != nil {
		by = auth.Email() + " (AI assistant)"
	}
	var page *core.Record
	if id, _ := p["page_id"].(string); id != "" {
		var err error
		if page, err = app.FindRecordById(Collection, id); err != nil {
			return nil, errors.New("the page doesn't exist anymore")
		}
		if u, _ := p["updated"].(string); u != "" && page.GetString("updated") != u {
			return nil, errors.New("the page was changed since this proposal: ask for a new one")
		}
		_ = snapshot(app, page, "before the AI assistant's change")
		for _, k := range []string{"title", "slug", "description", "record_filter", "published", "html", "css"} {
			page.Set(k, p[k])
		}
		if p["html_changed"] == true {
			page.Set("project", nil) // the builder loads the new HTML
		}
	} else {
		col, err := app.FindCollectionByNameOrId(Collection)
		if err != nil {
			return nil, err
		}
		page = core.NewRecord(col)
		for k, v := range p {
			page.Set(k, v)
		}
	}
	if err := app.SaveWithContext(ctx, page); err != nil {
		return nil, err
	}
	_ = snapshot(app, page, by)
	out := map[string]any{"link": "#/pages/" + page.Id, "label": "Open in the builder", "id": page.Id}
	if k := pageKind(page); page.GetBool("published") && (k == "page" || k == "collection") {
		out["url"] = pageURL(page.GetString("slug"), "")
	}
	return out, nil
}
