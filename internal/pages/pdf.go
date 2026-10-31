package pages

import (
	"encoding/json"
	"html"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Namarusa/PocketbasePlus/internal/offline"
	"github.com/Namarusa/PocketbasePlus/internal/pdfview"
)

// PDF viewers: a placeholder (data-pb-pdf) showing a PDF in the page, drawn
// by PDF.js (the pages script gp-pdf.js) with the controls chosen in the
// builder: a toolbar (or none) and its buttons, a sidebar of page thumbnails,
// the zoom, one page at a time or all of them... The PDF is an address (a
// formula: {{record.manual_url}}), a file uploaded to the page, a file field
// of the record, or a Documents template drawn for the record (a signed
// link, internal/pdfview). Without the script, a link opens the PDF.

const (
	attrPDF         = "data-pb-pdf"          // the source: url | file | field | document
	attrPDFSrc      = "data-pb-pdf-src"      // url, file: the address (url: a formula)
	attrPDFField    = "data-pb-pdf-field"    // field: a file field of the record ("brand.catalog")
	attrPDFDocument = "data-pb-pdf-document" // document: the template (id or slug)
	attrPDFToolbar  = "data-pb-pdf-toolbar"  // top (default) | bottom | none
	attrPDFTools    = "data-pb-pdf-tools"    // the buttons of the toolbar (pdfTools), all by default
	attrPDFSidebar  = "data-pb-pdf-sidebar"  // the page thumbnails: none (default) | closed | open
	attrPDFZoom     = "data-pb-pdf-zoom"     // width (default) | page | 50 to 400 (%)
	attrPDFLayout   = "data-pb-pdf-layout"   // scroll (default) | single: one page at a time
	attrPDFPage     = "data-pb-pdf-page"     // the page shown first
	attrPDFHeight   = "data-pb-pdf-height"   // px (640 by default), or "auto": as tall as the pages
	attrPDFBg       = "data-pb-pdf-background"
	attrPDFFrame    = "data-pb-pdf-frame" // the pages: shadow (default) | border | none
	attrPDFText     = "data-pb-pdf-text"  // "false": no text selection
	attrPDFLinks    = "data-pb-pdf-links" // "false": the links of the PDF aren't clickable
	attrPDFName     = "data-pb-pdf-name"  // the name of the downloaded file
	attrPDFLabel    = "data-pb-pdf-label" // the accessible name of the viewer
	attrPDFEmpty    = "data-pb-pdf-empty" // shown when there's no PDF (a record without a file)
)

// Document links: a link (or button) downloading a Documents template drawn
// for the record of the page or list item, through the same signed link as
// the viewers.
const (
	attrDocument     = "data-pb-document"      // the template (id or slug)
	attrDocumentMode = "data-pb-document-mode" // download (default) | open: in a new tab
)

// pdfMigrationFile lets the pages have PDF files (the viewers show them).
const pdfMigrationFile = "1763000000_pages_pdf.go"

func init() {
	m.Register(func(app core.App) error {
		return setPDFAssets(app, true)
	}, func(app core.App) error {
		return setPDFAssets(app, false)
	}, pdfMigrationFile)
}

// setPDFAssets adds or removes the PDF files of the page files (assets),
// up to 30 MB a file (the images were up to 10 MB).
func setPDFAssets(app core.App, on bool) error {
	pages, err := app.FindCollectionByNameOrId(Collection)
	if err != nil {
		return nil
	}
	f, ok := pages.Fields.GetByName("assets").(*core.FileField)
	if !ok {
		return nil
	}
	f.MimeTypes = slices.DeleteFunc(f.MimeTypes, func(t string) bool { return t == "application/pdf" })
	if on {
		f.MimeTypes = append(f.MimeTypes, "application/pdf")
		f.MaxSize = max(f.MaxSize, 30<<20)
	}
	return app.Save(pages)
}

// pdfTools are the buttons a toolbar may have, in their order.
var pdfTools = []string{"sidebar", "pages", "zoom", "search", "download", "print", "fullscreen"}

// pdfBackgrounds are the backgrounds behind the pages.
var pdfBackgrounds = []string{"muted", "none", "dark"}

// the PDF.js files of the viewers (served by serveStatic)
const pdfjsBase = "/p/_pdfjs/" + pdfview.Version + "/"

// pdfViewer replaces the placeholder with the viewer: the options of the
// script, and a link to the PDF for the browsers without it.
func (r *renderer) pdfViewer(n *xhtml.Node, s scope) {
	src, name, problem := r.pdfSource(n, s)
	opts := map[string]any{"src": src, "lib": pdfjsBase}

	switch v := getAttr(n, attrPDFToolbar); v {
	case "bottom", "none":
		opts["toolbar"] = v
	default:
		opts["toolbar"] = "top"
	}
	tools := pdfTools
	if hasAttr(n, attrPDFTools) {
		tools = nil
		for _, t := range strings.Split(getAttr(n, attrPDFTools), ",") {
			if t = strings.TrimSpace(t); slices.Contains(pdfTools, t) && !slices.Contains(tools, t) {
				tools = append(tools, t)
			}
		}
	}
	sidebar := getAttr(n, attrPDFSidebar)
	if sidebar != "closed" && sidebar != "open" {
		sidebar = "none"
	}
	if sidebar == "none" {
		tools = slices.DeleteFunc(slices.Clone(tools), func(t string) bool { return t == "sidebar" })
	}
	opts["tools"] = append([]string{}, tools...)
	opts["sidebar"] = sidebar

	zoom := getAttr(n, attrPDFZoom)
	if pct, err := strconv.Atoi(zoom); err == nil {
		zoom = strconv.Itoa(min(max(pct, 25), 400))
	} else if zoom != "page" {
		zoom = "width"
	}
	opts["zoom"] = zoom
	opts["layout"] = "scroll"
	if getAttr(n, attrPDFLayout) == "single" {
		opts["layout"] = "single"
	}
	if p, err := strconv.Atoi(getAttr(n, attrPDFPage)); err == nil && p > 1 {
		opts["page"] = p
	}
	height := getAttr(n, attrPDFHeight)
	if height == "auto" {
		opts["fit"] = true
	} else if h, err := strconv.Atoi(strings.TrimSuffix(height, "px")); err == nil && h >= 120 && h <= 4000 {
		opts["height"] = h
	}
	opts["background"] = "muted"
	if bg := getAttr(n, attrPDFBg); slices.Contains(pdfBackgrounds, bg) {
		opts["background"] = bg
	}
	switch v := getAttr(n, attrPDFFrame); v {
	case "border", "none":
		opts["frame"] = v
	default:
		opts["frame"] = "shadow"
	}
	opts["text"] = getAttr(n, attrPDFText) != "false"
	opts["links"] = getAttr(n, attrPDFLinks) != "false"
	if v := getAttr(n, attrPDFName); v != "" {
		name = v
	}
	if name != "" {
		opts["name"] = safeFileName(name)
	}
	label := getAttr(n, attrPDFLabel)
	if label == "" {
		label = "PDF document"
	}
	opts["label"] = label
	empty := getAttr(n, attrPDFEmpty)
	if empty == "" {
		empty = "No document to show."
	}
	opts["empty"] = empty
	js, _ := json.Marshal(opts)

	clearChildren(n)
	addClass(n, "gp-pdf")
	setAttr(n, "data-gp-pdf", string(js))
	setAttr(n, "role", "region")
	setAttr(n, "aria-label", label)
	if problem != "" {
		r.fail(n, problem)
	}
	if src == "" {
		appendHTML(n, `<div class="gp-pdf-empty">`+html.EscapeString(empty)+`</div>`)
	} else {
		appendHTML(n, `<div class="gp-pdf-fallback"><a href="`+html.EscapeString(src)+`" target="_blank" rel="noopener">`+
			`Open the PDF`+`</a></div>`)
	}
	r.pdf = true
}

// documentLink points a link at a document of the record.
func (r *renderer) documentLink(n *xhtml.Node, s scope) {
	if n.DataAtom != atom.A {
		// a button: a link that looks the same
		n.Data, n.DataAtom = "a", atom.A
		n.Attr = slices.DeleteFunc(n.Attr, func(a xhtml.Attribute) bool { return a.Key == "type" || a.Key == "disabled" })
	}
	tpl := getAttr(n, attrDocument)
	if tpl == "" {
		setAttr(n, "href", "#")
		r.fail(n, "Document link: choose the document template in its settings")
		return
	}
	col, id := "", ""
	if s.rec != nil {
		col, id = s.col.Name, s.rec.Id
	}
	link := pdfview.ViewLink(r.app, tpl, col, id)
	if getAttr(n, attrDocumentMode) == "open" {
		setAttr(n, "target", "_blank")
		setAttr(n, "rel", "noopener")
	} else {
		link += "&download=1"
		setAttr(n, "download", "")
		n.Attr = slices.DeleteFunc(n.Attr, func(a xhtml.Attribute) bool { return a.Key == "target" })
	}
	setAttr(n, "href", link)
}

// pdfSource returns the address of the PDF, its file name when known, and
// the problem to show in the previews.
func (r *renderer) pdfSource(n *xhtml.Node, s scope) (src, name, problem string) {
	switch kind := getAttr(n, attrPDF); kind {
	case "field":
		p := getAttr(n, attrPDFField)
		if p == "" {
			return "", "", "PDF viewer: choose the file field in its settings"
		}
		if s.rec == nil {
			return "", "", "PDF viewer: a file field needs a record (a single item page or a collection list)"
		}
		scopes, field := r.resolve(s, p)
		for _, sc := range scopes {
			f, ok := sc.col.Fields.GetByName(field).(*core.FileField)
			if !ok || !canShow(sc, field) {
				continue
			}
			if f.Protected {
				return "", "", "PDF viewer: " + field + " is a protected file field, the pages can't show its files"
			}
			files := sc.rec.GetStringSlice(field)
			if len(files) == 0 {
				continue
			}
			// the first PDF of the field (else its first file)
			file := files[0]
			for _, f := range files {
				if strings.EqualFold(path.Ext(f), ".pdf") {
					file = f
					break
				}
			}
			return "/api/files/" + url.PathEscape(sc.col.Id) + "/" + url.PathEscape(sc.rec.Id) + "/" + url.PathEscape(file), originalName(file), ""
		}
		return "", "", ""
	case "document":
		tpl := getAttr(n, attrPDFDocument)
		if tpl == "" {
			return "", "", "PDF viewer: choose the document template in its settings"
		}
		col, id := "", ""
		if s.rec != nil {
			col, id = s.col.Name, s.rec.Id
		}
		return pdfview.ViewLink(r.app, tpl, col, id), "", ""
	default: // url, file
		v := getAttr(n, attrPDFSrc)
		if strings.Contains(v, "{{") {
			out, err := r.evalFormula(s, v)
			if err != nil {
				return "", "", "PDF viewer: the address formula: " + err.Error()
			}
			v = out
		}
		v = strings.TrimSpace(v)
		if v == "" {
			if kind == "file" {
				return "", "", "PDF viewer: upload a PDF or choose one in its settings"
			}
			return "", "", ""
		}
		u, err := url.Parse(v)
		if err != nil || !safeURL(v, false) || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") || strings.HasPrefix(v, "//") {
			return "", "", "PDF viewer: " + v + " isn't an http(s) address"
		}
		if u.Scheme != "" && offline.Enabled() {
			return "", "", "PDF viewer: the offline mode is on, the pages can't read " + u.Host
		}
		return v, originalName(path.Base(u.Path)), ""
	}
}

// originalName removes the random suffix PocketBase adds to the uploaded
// files (report_x1y2z3a4b5.pdf: report.pdf).
func originalName(file string) string {
	ext := path.Ext(file)
	base := strings.TrimSuffix(file, ext)
	if i := strings.LastIndexByte(base, '_'); i > 0 && len(base)-i-1 == 10 {
		base = base[:i]
	}
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return base + ext
}

// safeFileName keeps a name usable as a downloaded file name.
func safeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	if name != "" && !strings.EqualFold(path.Ext(name), ".pdf") {
		name += ".pdf"
	}
	return name
}
