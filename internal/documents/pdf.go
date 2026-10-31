package documents

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
	_ "golang.org/x/image/webp"

	"github.com/Namarusa/PocketbasePlus/internal/formula"
)

// The PDF engine: the blocks laid out on the pages (text wrapped with its
// inline styles, tables split across pages with their header repeated,
// columns side by side), with go-pdf/fpdf and the embedded Noto fonts.

//go:embed fonts/*.ttf
var fontFS embed.FS

const (
	maxPages     = 200
	maxTableRows = 5000
	ptToMM       = 25.4 / 72
	lineFactor   = 1.4 // line height / font size
	columnGap    = 6.0 // mm
	pagesAlias   = "{nb}"
)

var (
	errTooManyPages = fmt.Errorf("the document has more than %d pages", maxPages)
	fontBytes       = map[string][]byte{}
)

func init() {
	for style, file := range map[string]string{"": "NotoSans-Regular.ttf", "B": "NotoSans-Bold.ttf", "I": "NotoSans-Italic.ttf", "BI": "NotoSans-BoldItalic.ttf", "mono": "NotoSansMono-Regular.ttf"} {
		b, err := fontFS.ReadFile("fonts/" + file)
		if err != nil {
			panic(err)
		}
		fontBytes[style] = b
	}
}

// roots are the data written without their dot in the formulas.
var roots = []string{"record", "lists", "values", "app", "page", "pages", "row", "index", "document"}

// BlockError is an error of a block (shown on it in the editor).
type BlockError struct {
	Block string `json:"block"`
	Error string `json:"error"`
}

// imageLoader returns the bytes of an image block: its asset, its record
// file or its address (src, the url formula rendered).
type imageLoader func(b *Block, src string) ([]byte, error)

// Box is where a block was drawn, in millimeters from the top left corner of
// its page: the editor selects a block by a click on the preview. A block
// split across pages (a long table) has a box on each of them.
type Box struct {
	Block string  `json:"block"`
	Page  int     `json:"page"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
}

const maxBoxes = 20000

// openBox is a block being drawn.
type openBox struct {
	id     string
	page   int
	x, y   float64
	w      float64
	chrome bool // in the header or footer
	inked  bool // something was drawn on the current page
}

// rendered is a drawn document.
type rendered struct {
	pdf   []byte
	pages int
	errs  []BlockError
	boxes []Box
	pageW float64
	pageH float64
}

type engine struct {
	ctx    context.Context
	pdf    *fpdf.Fpdf
	spec   *Spec
	data   map[string]any
	load   imageLoader
	errs   []BlockError
	parsed map[string]*template.Template

	pageW, pageH       float64
	left, width        float64 // the content box
	top, bottom        float64 // the body limits
	headerH, footerH   float64
	chrome             bool // drawing the header or footer: no page breaks
	noBreak            bool // drawing columns that fit
	accent, ink, muted [3]int
	images             map[string]*fpdf.ImageInfoType // by asset|field|url
	names              map[string]string              // their names in the PDF
	compress           bool
	boxes              []Box
	open               []*openBox
}

// render draws the document and returns the PDF and its page count.
func render(ctx context.Context, spec *Spec, data map[string]any, load imageLoader, compress bool) (*rendered, error) {
	e := &engine{ctx: ctx, spec: spec, data: data, load: load, parsed: map[string]*template.Template{}, images: map[string]*fpdf.ImageInfoType{}, names: map[string]string{}, compress: compress}
	pdf, n, err := e.run()
	if err != nil {
		return nil, err
	}
	return &rendered{pdf: pdf, pages: n, errs: e.errs, boxes: e.boxes, pageW: e.pageW, pageH: e.pageH}, nil
}

func (e *engine) run() (out []byte, pages int, err error) {
	s := e.spec
	e.pageW, e.pageH = s.pageSize()
	e.accent = rgb(s.Accent)
	e.ink = [3]int{31, 35, 40}
	e.muted = [3]int{100, 106, 115}
	e.left = s.Margins.Left
	e.width = e.pageW - s.Margins.Left - s.Margins.Right

	pdf := fpdf.NewCustom(&fpdf.InitType{UnitStr: "mm", Size: fpdf.SizeType{Wd: e.pageW, Ht: e.pageH}})
	e.pdf = pdf
	pdf.SetCompression(e.compress)
	pdf.SetProducer("PocketBasePlus", true)
	pdf.SetCreator("PocketBasePlus documents", true)
	for style, b := range fontBytes {
		if style == "mono" {
			pdf.AddUTF8FontFromBytes("mono", "", b)
			continue
		}
		pdf.AddUTF8FontFromBytes("sans", style, b)
	}
	pdf.SetMargins(s.Margins.Left, s.Margins.Top, s.Margins.Right)
	pdf.SetCellMargin(0) // the text is placed by the engine
	pdf.SetAutoPageBreak(false, 0)
	pdf.AliasNbPages(pagesAlias)
	e.data["pages"] = pagesAlias

	// the header and footer: measured once, drawn on every page
	e.data["page"] = 1
	e.headerH = e.blocksHeight(s.Header, e.width)
	e.footerH = e.blocksHeight(s.Footer, e.width)
	e.top = s.Margins.Top
	if e.headerH > 0 {
		e.top += e.headerH + 4
	}
	e.bottom = e.pageH - s.Margins.Bottom
	if e.footerH > 0 {
		e.bottom -= e.footerH + 4
	}
	if e.bottom-e.top < 20 {
		return nil, 0, errors.New("the header and footer leave no room for the content: reduce them or the margins")
	}
	pdf.SetHeaderFuncMode(func() { e.drawChrome(s.Header, s.Margins.Top) }, false)
	pdf.SetFooterFunc(func() { e.drawChrome(s.Footer, e.pageH-s.Margins.Bottom-e.footerH) })

	defer func() {
		if r := recover(); r != nil {
			if er, ok := r.(error); ok && (errors.Is(er, errTooManyPages) || errors.Is(er, context.DeadlineExceeded) || errors.Is(er, context.Canceled)) {
				out, pages, err = nil, 0, er
				return
			}
			panic(r)
		}
	}()

	pdf.AddPage()
	pdf.SetY(e.top)
	e.drawBlocks(s.Body, e.left, e.width)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), pdf.PageNo(), nil
}

func (e *engine) drawChrome(blocks []Block, y float64) {
	if len(blocks) == 0 {
		return
	}
	e.chrome = true
	e.data["page"] = e.pdf.PageNo()
	e.pdf.SetY(y)
	e.drawBlocks(blocks, e.left, e.width)
	e.chrome = false
	e.pdf.SetY(e.top)
}

// --- page breaks ------------------------------------------------------------------------------

// ensure starts a new page when h doesn't fit; it returns whether it did.
func (e *engine) ensure(h float64) bool {
	if e.chrome || e.noBreak {
		return false
	}
	if err := e.ctx.Err(); err != nil {
		panic(err)
	}
	y := e.pdf.GetY()
	if y+h <= e.bottom+0.01 || y <= e.top+0.01 {
		return false
	}
	e.newPage()
	return true
}

func (e *engine) newPage() {
	if e.pdf.PageNo() >= maxPages {
		panic(errTooManyPages)
	}
	// the blocks being drawn end on this page and go on on the next one
	for _, o := range e.open {
		if !o.chrome && o.inked {
			e.addBox(o, e.bottom)
		}
	}
	e.pdf.AddPage()
	e.pdf.SetY(e.top)
	for _, o := range e.open {
		if !o.chrome {
			o.page, o.y, o.inked = e.pdf.PageNo(), e.top, false
		}
	}
}

// --- boxes ------------------------------------------------------------------------------------

func (e *engine) openBox(b *Block, x, w float64) *openBox {
	o := &openBox{id: b.ID, page: e.pdf.PageNo(), x: x, y: e.pdf.GetY(), w: w, chrome: e.chrome}
	e.open = append(e.open, o)
	return o
}

func (e *engine) closeBox(o *openBox) {
	e.open = e.open[:len(e.open)-1]
	e.addBox(o, e.pdf.GetY())
}

func (e *engine) addBox(o *openBox, bottom float64) {
	if o.id == "" || bottom-o.y < 0.2 || len(e.boxes) >= maxBoxes {
		return
	}
	e.boxes = append(e.boxes, Box{Block: o.id, Page: o.page, X: round2(o.x), Y: round2(o.y), W: round2(o.w), H: round2(bottom - o.y)})
}

// inked records that the blocks being drawn put something on the page.
func (e *engine) inked() {
	for _, o := range e.open {
		if o.chrome == e.chrome {
			o.inked = true
		}
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// --- formulas ---------------------------------------------------------------------------------

func (e *engine) fail(b *Block, err error) {
	if b == nil || err == nil {
		return
	}
	for _, x := range e.errs {
		if x.Block == b.ID && x.Error == err.Error() {
			return
		}
	}
	if len(e.errs) < 50 {
		e.errs = append(e.errs, BlockError{Block: b.ID, Error: err.Error()})
	}
}

// text renders a formula of a block (its errors are recorded on the block).
func (e *engine) text(b *Block, src string) string {
	if !strings.Contains(src, "{{") {
		return src
	}
	t, ok := e.parsed[src]
	if !ok {
		var err error
		t, err = formula.Parse(src, roots...)
		if err != nil {
			e.fail(b, err)
			e.parsed[src] = nil
			return ""
		}
		e.parsed[src] = t
	}
	if t == nil {
		return ""
	}
	out, err := formula.Execute(t, e.data)
	if err != nil {
		e.fail(b, err)
		return ""
	}
	return out
}

func (e *engine) visible(b *Block) bool {
	if strings.TrimSpace(b.ShowIf) == "" {
		return true
	}
	v := strings.TrimSpace(e.text(b, b.ShowIf))
	return v != "" && v != "false" && v != "0" && v != "<nil>"
}

// --- blocks -----------------------------------------------------------------------------------

func (e *engine) drawBlocks(blocks []Block, x, w float64) {
	for i := range blocks {
		b := &blocks[i]
		if !e.visible(b) {
			continue
		}
		o := e.openBox(b, x, w)
		e.drawBlock(b, x, w)
		e.closeBox(o)
	}
}

func (e *engine) blocksHeight(blocks []Block, w float64) float64 {
	h := 0.0
	for i := range blocks {
		b := &blocks[i]
		if e.visible(b) {
			h += e.blockHeight(b, w)
		}
	}
	return h
}

func (e *engine) drawBlock(b *Block, x, w float64) {
	switch b.Type {
	case "heading", "text":
		st := e.textStyle(b)
		lines := e.wrap(parseInline(e.text(b, b.Text)), w, st)
		e.drawLines(lines, x, w, st, b.Align)
		e.space(st.after)
	case "fields":
		e.drawFields(b, b.Rows, x, w, false)
		e.space(2)
	case "columns":
		e.drawColumns(b, x, w)
	case "table":
		e.drawTable(b, x, w)
	case "image":
		e.drawImage(b, x, w)
	case "divider":
		e.ensure(4)
		e.inked()
		y := e.pdf.GetY() + 2
		e.pdf.SetDrawColor(e.muted[0]+100, e.muted[1]+100, e.muted[2]+100)
		e.pdf.SetLineWidth(0.3)
		e.pdf.Line(x, y, x+w, y)
		e.pdf.SetY(y + 2)
	case "spacer":
		e.space(spacerHeight(b))
	case "page_break":
		if !e.chrome && !e.noBreak && e.pdf.GetY() > e.top+0.01 {
			e.newPage()
		}
	}
}

func (e *engine) blockHeight(b *Block, w float64) float64 {
	switch b.Type {
	case "heading", "text":
		st := e.textStyle(b)
		return float64(len(e.wrap(parseInline(e.text(b, b.Text)), w, st)))*st.lh + st.after
	case "fields":
		return e.fieldsHeight(b, b.Rows, w) + 2
	case "columns":
		h := 0.0
		for i, cw := range e.columnWidths(b, w) {
			h = max(h, e.blocksHeight(b.Columns[i].Blocks, cw))
		}
		return h
	case "table":
		return e.tableHeight(b, w)
	case "image":
		_, ih := e.imageSize(b, w)
		return ih + 2
	case "divider":
		return 4
	case "spacer":
		return spacerHeight(b)
	}
	return 0
}

func spacerHeight(b *Block) float64 {
	if b.Height <= 0 || b.Height > 200 {
		return 6
	}
	return b.Height
}

func (e *engine) space(h float64) {
	if h <= 0 {
		return
	}
	y := e.pdf.GetY() + h
	if !e.chrome && !e.noBreak && y > e.bottom {
		y = e.bottom // the next block starts the next page
	}
	e.pdf.SetY(y)
}

// --- text -------------------------------------------------------------------------------------

type textStyle struct {
	size, lh, after float64
	bold, italic    bool
	mono            bool
	color           [3]int
}

func (e *engine) textStyle(b *Block) textStyle {
	base := e.spec.FontSize
	st := textStyle{size: base, bold: b.Bold, italic: b.Italic, mono: b.Mono, color: e.ink, after: 2}
	if b.Size >= 5 && b.Size <= 72 {
		st.size = b.Size
	}
	if b.Type == "heading" {
		st.bold = true
		switch b.Level {
		case 2:
			st.size = max(st.size, base*1.4)
		case 3:
			st.size = max(st.size, base*1.15)
		default:
			st.size = max(st.size, base*2)
			st.color = e.accent
		}
		st.after = 3
	}
	if colorRegex.MatchString(b.Color) {
		st.color = rgb(b.Color)
	}
	st.lh = st.size * ptToMM * lineFactor
	return st
}

// a run of text with its inline style: **bold**, *italic*, [text](url)
type run struct {
	text         string
	bold, italic bool
	link         string
}

// parseInline reads the inline styles of a text.
func parseInline(s string) []run {
	var out []run
	var cur strings.Builder
	bold, italic := false, false
	flush := func(link string) {
		if cur.Len() > 0 {
			out = append(out, run{text: cur.String(), bold: bold, italic: italic, link: link})
			cur.Reset()
		}
	}
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			flush("")
			bold = !bold
			i += 2
		case s[i] == '*' || (s[i] == '_' && (i == 0 || !isWordByte(s[i-1]) || i+1 >= len(s) || !isWordByte(s[i+1]))):
			flush("")
			italic = !italic
			i++
		case s[i] == '[':
			// [text](url)
			end := strings.Index(s[i:], "](")
			close := -1
			if end > 0 {
				close = strings.IndexByte(s[i+end+2:], ')')
			}
			if end > 0 && close >= 0 {
				flush("")
				url := s[i+end+2 : i+end+2+close]
				if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "mailto:") {
					cur.WriteString(s[i+1 : i+end])
					flush(url)
				} else {
					cur.WriteString(s[i+1 : i+end])
				}
				i += end + 3 + close
				continue
			}
			cur.WriteByte(s[i])
			i++
		case s[i] == '\\' && i+1 < len(s) && strings.ContainsRune("*_[\\", rune(s[i+1])):
			cur.WriteByte(s[i+1])
			i += 2
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			cur.WriteRune(r)
			i += size
		}
	}
	flush("")
	return out
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

type seg struct {
	text         string
	bold, italic bool
	link         string
	w            float64
}

type line struct {
	segs []seg
	w    float64
}

func (e *engine) setFont(st textStyle, bold, italic bool) {
	if st.mono {
		e.pdf.SetFont("mono", "", st.size)
		return
	}
	style := ""
	if bold || st.bold {
		style += "B"
	}
	if italic || st.italic {
		style += "I"
	}
	e.pdf.SetFont("sans", style, st.size)
}

func (e *engine) measure(st textStyle, s seg) float64 {
	e.setFont(st, s.bold, s.italic)
	return e.pdf.GetStringWidth(s.text)
}

// wrap breaks the runs into lines of at most w millimeters.
func (e *engine) wrap(runs []run, w float64, st textStyle) []line {
	var lines []line
	cur := line{}
	push := func() {
		// no spaces at the end of a line
		for len(cur.segs) > 0 && strings.TrimSpace(cur.segs[len(cur.segs)-1].text) == "" {
			cur.w -= cur.segs[len(cur.segs)-1].w
			cur.segs = cur.segs[:len(cur.segs)-1]
		}
		lines = append(lines, cur)
		cur = line{}
	}
	add := func(s seg) {
		s.w = e.measure(st, s)
		if strings.TrimSpace(s.text) == "" && len(cur.segs) == 0 {
			return // no spaces at the start of a line
		}
		if cur.w+s.w > w+0.01 && len(cur.segs) > 0 && strings.TrimSpace(s.text) != "" {
			push()
		}
		// a word longer than the line: cut it
		for s.w > w && utf8.RuneCountInString(s.text) > 1 {
			rs := []rune(s.text)
			n := len(rs)
			for n > 1 {
				part := s
				part.text = string(rs[:n])
				if e.measure(st, part) <= w-cur.w {
					break
				}
				n--
			}
			part := s
			part.text = string(rs[:n])
			part.w = e.measure(st, part)
			cur.segs = append(cur.segs, part)
			cur.w += part.w
			push()
			s.text = string(rs[n:])
			s.w = e.measure(st, s)
		}
		cur.segs = append(cur.segs, s)
		cur.w += s.w
	}
	for _, r := range runs {
		for i, para := range strings.Split(strings.ReplaceAll(r.text, "\r\n", "\n"), "\n") {
			if i > 0 {
				push()
			}
			// words and the spaces between them
			start := 0
			for j := 0; j <= len(para); j++ {
				if j == len(para) || para[j] == ' ' || para[j] == '\t' {
					if j > start {
						add(seg{text: para[start:j], bold: r.bold, italic: r.italic, link: r.link})
					}
					if j < len(para) {
						add(seg{text: " ", bold: r.bold, italic: r.italic, link: r.link})
					}
					start = j + 1
				}
			}
		}
	}
	if len(cur.segs) > 0 || len(lines) == 0 {
		push()
	}
	if len(lines) == 1 && len(lines[0].segs) == 0 {
		return nil // nothing to show
	}
	return lines
}

func (e *engine) drawLines(lines []line, x, w float64, st textStyle, align string) {
	for _, l := range lines {
		e.ensure(st.lh)
		e.inked()
		y := e.pdf.GetY()
		lx := x
		switch align {
		case "center":
			lx = x + (w-l.w)/2
		case "right":
			lx = x + w - l.w
		}
		for _, s := range l.segs {
			e.setFont(st, s.bold, s.italic)
			if s.link != "" {
				e.pdf.SetTextColor(e.accent[0], e.accent[1], e.accent[2])
			} else {
				e.pdf.SetTextColor(st.color[0], st.color[1], st.color[2])
			}
			e.pdf.SetXY(lx, y)
			e.pdf.CellFormat(s.w, st.lh, s.text, "", 0, "L", false, 0, s.link)
			lx += s.w
		}
		e.pdf.SetXY(x, y+st.lh)
	}
}

// --- fields -----------------------------------------------------------------------------------

func (e *engine) fieldWidths(b *Block, w float64) (float64, float64) {
	lw := b.LabelWidth
	if lw <= 0 || lw >= 90 {
		lw = 35
	}
	return w * lw / 100, w - w*lw/100
}

func (e *engine) fieldStyles() (textStyle, textStyle) {
	base := e.spec.FontSize
	label := textStyle{size: base, color: e.muted, lh: base * ptToMM * lineFactor}
	value := textStyle{size: base, color: e.ink, lh: base * ptToMM * lineFactor}
	return label, value
}

type fieldRow struct {
	label, value []line
	h            float64
}

func (e *engine) fieldRows(b *Block, rows []Row, w float64, strong bool) []fieldRow {
	lw, vw := e.fieldWidths(b, w)
	ls, vs := e.fieldStyles()
	var out []fieldRow
	for i, r := range rows {
		vst := vs
		if strong && i == len(rows)-1 {
			vst.bold = true
		}
		fr := fieldRow{label: e.wrap(parseInline(e.text(b, r.Label)), lw-2, ls), value: e.wrap(parseInline(e.text(b, r.Value)), vw, vst)}
		fr.h = float64(max(len(fr.label), len(fr.value), 1)) * vs.lh
		out = append(out, fr)
	}
	return out
}

func (e *engine) fieldsHeight(b *Block, rows []Row, w float64) float64 {
	h := 0.0
	for _, r := range e.fieldRows(b, rows, w, false) {
		h += r.h + 0.6
	}
	return h
}

func (e *engine) drawFields(b *Block, rows []Row, x, w float64, strong bool) {
	lw, vw := e.fieldWidths(b, w)
	ls, vs := e.fieldStyles()
	for i, r := range e.fieldRows(b, rows, w, strong) {
		e.ensure(r.h)
		y := e.pdf.GetY()
		e.drawLines(r.label, x, lw-2, ls, "")
		e.pdf.SetY(y)
		vst := vs
		if strong && i == len(rows)-1 {
			vst.bold = true
		}
		align := ""
		if strong {
			align = "right"
		}
		e.drawLines(r.value, x+lw, vw, vst, align)
		e.pdf.SetY(y + r.h + 0.6)
	}
}

// --- columns ----------------------------------------------------------------------------------

func (e *engine) columnWidths(b *Block, w float64) []float64 {
	n := len(b.Columns)
	if n == 0 {
		return nil
	}
	total := 0.0
	for _, c := range b.Columns {
		total += max(c.Width, 0)
	}
	inner := w - columnGap*float64(n-1)
	out := make([]float64, n)
	for i, c := range b.Columns {
		if total <= 0 || c.Width <= 0 {
			out[i] = inner / float64(n)
		} else {
			out[i] = inner * c.Width / total
		}
	}
	return out
}

func (e *engine) drawColumns(b *Block, x, w float64) {
	widths := e.columnWidths(b, w)
	h := 0.0
	for i, cw := range widths {
		h = max(h, e.blocksHeight(b.Columns[i].Blocks, cw))
	}
	room := e.bottom - e.pdf.GetY()
	if !e.chrome && !e.noBreak && h > room && h <= e.bottom-e.top {
		e.newPage()
	}
	if e.chrome || e.noBreak || h <= e.bottom-e.pdf.GetY()+0.01 {
		// side by side
		y := e.pdf.GetY()
		saved := e.noBreak
		e.noBreak = true
		cx := x
		for i, cw := range widths {
			e.pdf.SetY(y)
			e.drawBlocks(b.Columns[i].Blocks, cx, cw)
			cx += cw + columnGap
		}
		e.noBreak = saved
		e.pdf.SetY(y + h)
		return
	}
	// taller than a page: one column after the other
	for i := range widths {
		e.drawBlocks(b.Columns[i].Blocks, x, w)
	}
}

// --- tables -----------------------------------------------------------------------------------

func (e *engine) tableItems(b *Block) []any {
	src := strings.TrimSpace(b.Items)
	if src == "" {
		return nil
	}
	if !strings.HasPrefix(src, "{{") {
		src = "{{" + src + "}}" // lists.items
	}
	v, ok, err := formula.Value(src, e.data, roots...)
	if err != nil {
		e.fail(b, err)
		return nil
	}
	if !ok {
		e.fail(b, errors.New("the rows must be a list, e.g. {{lists.items}}"))
		return nil
	}
	v = formula.Plain(v)
	switch list := v.(type) {
	case []any:
		if len(list) > maxTableRows {
			e.fail(b, fmt.Errorf("only the first %d rows are shown", maxTableRows))
			list = list[:maxTableRows]
		}
		return list
	case []map[string]any:
		out := make([]any, len(list))
		for i, m := range list {
			out[i] = m
		}
		return out
	case nil:
		return nil
	}
	e.fail(b, errors.New("the rows must be a list, e.g. {{lists.items}}"))
	return nil
}

func (e *engine) cellWidths(b *Block, w float64) []float64 {
	total := 0.0
	for _, c := range b.Cells {
		total += max(c.Width, 0)
	}
	out := make([]float64, len(b.Cells))
	for i, c := range b.Cells {
		if total <= 0 || c.Width <= 0 {
			out[i] = w / float64(len(b.Cells))
		} else {
			out[i] = w * c.Width / total
		}
	}
	return out
}

const cellPad = 1.6

type tableRow struct {
	cells [][]line
	h     float64
}

func (e *engine) tableRow(b *Block, texts []string, widths []float64, st textStyle) tableRow {
	r := tableRow{}
	n := 1
	for i, txt := range texts {
		lines := e.wrap(parseInline(txt), widths[i]-2*cellPad, st)
		r.cells = append(r.cells, lines)
		n = max(n, len(lines))
	}
	r.h = float64(n)*st.lh + 2*cellPad
	return r
}

func (e *engine) withRow(item any, i int, fn func()) {
	prevRow, prevIndex := e.data["row"], e.data["index"]
	e.data["row"], e.data["index"] = item, i+1
	fn()
	e.data["row"], e.data["index"] = prevRow, prevIndex
}

func (e *engine) tableHeight(b *Block, w float64) float64 {
	if len(b.Cells) == 0 {
		return 0
	}
	widths := e.cellWidths(b, w)
	st := e.cellStyle(false)
	h := 0.0
	if !b.HideHeader {
		h += e.tableRow(b, e.headers(b), widths, e.cellStyle(true)).h
	}
	for i, item := range e.tableItems(b) {
		e.withRow(item, i, func() { h += e.tableRow(b, e.cellTexts(b), widths, st).h })
	}
	if len(b.Totals) > 0 {
		h += 1 + e.fieldsHeight(b, b.Totals, w)
	}
	return h + 3
}

func (e *engine) cellStyle(header bool) textStyle {
	size := e.spec.FontSize * 0.95
	st := textStyle{size: size, color: e.ink, lh: size * ptToMM * lineFactor}
	if header {
		st.bold = true
		st.color = [3]int{255, 255, 255}
	}
	return st
}

func (e *engine) headers(b *Block) []string {
	out := make([]string, len(b.Cells))
	for i, c := range b.Cells {
		out[i] = e.text(b, c.Header)
	}
	return out
}

func (e *engine) cellTexts(b *Block) []string {
	out := make([]string, len(b.Cells))
	for i, c := range b.Cells {
		out[i] = e.text(b, c.Value)
	}
	return out
}

func (e *engine) drawRow(b *Block, r tableRow, x float64, widths []float64, st textStyle, fill *[3]int) {
	e.inked()
	y := e.pdf.GetY()
	if fill != nil {
		e.pdf.SetFillColor(fill[0], fill[1], fill[2])
		e.pdf.Rect(x, y, sum(widths), r.h, "F")
	}
	cx := x
	for i, lines := range r.cells {
		e.pdf.SetY(y + cellPad)
		saved := e.noBreak
		e.noBreak = true
		e.drawLines(lines, cx+cellPad, widths[i]-2*cellPad, st, b.Cells[i].Align)
		e.noBreak = saved
		cx += widths[i]
	}
	e.pdf.SetDrawColor(220, 223, 228)
	e.pdf.SetLineWidth(0.2)
	e.pdf.Line(x, y+r.h, x+sum(widths), y+r.h)
	e.pdf.SetY(y + r.h)
}

func (e *engine) drawTable(b *Block, x, w float64) {
	if len(b.Cells) == 0 {
		return
	}
	widths := e.cellWidths(b, w)
	hst, st := e.cellStyle(true), e.cellStyle(false)
	header := e.tableRow(b, e.headers(b), widths, hst)
	drawHeader := func() {
		if !b.HideHeader {
			e.drawRow(b, header, x, widths, hst, &e.accent)
		}
	}
	items := e.tableItems(b)
	first := header.h
	if len(items) > 0 {
		e.withRow(items[0], 0, func() { first += e.tableRow(b, e.cellTexts(b), widths, st).h })
	}
	e.ensure(first) // the header stays with the first row
	drawHeader()
	zebra := [3]int{246, 247, 249}
	for i, item := range items {
		e.withRow(item, i, func() {
			r := e.tableRow(b, e.cellTexts(b), widths, st)
			if e.ensure(r.h) {
				drawHeader()
			}
			var fill *[3]int
			if b.Zebra && i%2 == 1 {
				fill = &zebra
			}
			e.drawRow(b, r, x, widths, st, fill)
		})
	}
	if len(items) == 0 && strings.TrimSpace(b.Empty) != "" {
		est := e.cellStyle(false)
		est.color = e.muted
		e.space(cellPad)
		e.drawLines(e.wrap(parseInline(e.text(b, b.Empty)), w-2*cellPad, est), x+cellPad, w-2*cellPad, est, "")
	}
	if len(b.Totals) > 0 {
		e.space(1)
		// under the last columns, right-aligned
		tw := w * 0.45
		tb := *b
		tb.LabelWidth = 55
		e.drawFields(&tb, b.Totals, x+w-tw, tw, true)
	}
	e.space(3)
}

func sum(xs []float64) float64 {
	t := 0.0
	for _, x := range xs {
		t += x
	}
	return t
}

// --- images -----------------------------------------------------------------------------------

// image returns the image of a block and its name in the PDF.
func (e *engine) image(b *Block) (*fpdf.ImageInfoType, string) {
	src := ""
	if b.Asset == "" && b.Field == "" {
		src = strings.TrimSpace(e.text(b, b.URL))
		if src == "" {
			return nil, ""
		}
	}
	key := b.Asset + "|" + b.Field + "|" + src
	if info, ok := e.images[key]; ok {
		return info, e.names[key]
	}
	e.images[key] = nil
	if e.load == nil {
		return nil, ""
	}
	raw, err := e.load(b, src)
	if err != nil {
		e.fail(b, err)
		return nil, ""
	}
	if raw == nil {
		return nil, ""
	}
	img, kind, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		e.fail(b, errors.New("not an image the documents can show (PNG, JPEG, GIF or WebP)"))
		return nil, ""
	}
	typ := map[string]string{"png": "PNG", "jpeg": "JPG", "gif": "GIF"}[kind]
	if typ == "" {
		// WebP and others: as PNG
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			e.fail(b, err)
			return nil, ""
		}
		raw, typ = buf.Bytes(), "PNG"
	}
	name := "img" + strconv.Itoa(len(e.names))
	info := e.pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: typ}, bytes.NewReader(raw))
	if e.pdf.Err() {
		e.fail(b, e.pdf.Error())
		e.pdf.ClearError()
		return nil, ""
	}
	info.SetDpi(96)
	e.images[key] = info
	e.names[key] = name
	return info, name
}

func (e *engine) imageSize(b *Block, w float64) (float64, float64) {
	info, _ := e.image(b)
	if info == nil || info.Width() <= 0 {
		return 0, 0
	}
	iw := b.Width
	if iw <= 0 {
		iw = min(info.Width(), 60)
	}
	iw = min(iw, w)
	return iw, iw * info.Height() / info.Width()
}

func (e *engine) drawImage(b *Block, x, w float64) {
	iw, ih := e.imageSize(b, w)
	if iw == 0 {
		return
	}
	e.ensure(ih)
	e.inked()
	ix := x
	switch b.Align {
	case "center":
		ix = x + (w-iw)/2
	case "right":
		ix = x + w - iw
	}
	if n := len(e.open); n > 0 {
		e.open[n-1].x, e.open[n-1].w = ix, iw // the box of the block is the image
	}
	y := e.pdf.GetY()
	_, name := e.image(b)
	e.pdf.ImageOptions(name, ix, y, iw, ih, false, fpdf.ImageOptions{}, 0, "")
	e.pdf.SetY(y + ih + 2)
}

func rgb(hex string) [3]int {
	if !colorRegex.MatchString(hex) {
		return [3]int{37, 99, 235}
	}
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	return [3]int{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}
}
