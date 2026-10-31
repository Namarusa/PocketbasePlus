package documents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Spec is the layout of a document template: the page and its blocks.
type Spec struct {
	Format      string  `json:"format"`      // a4 | a5 | a3 | letter | legal | custom
	Orientation string  `json:"orientation"` // portrait | landscape
	Width       float64 `json:"width_mm"`    // custom format
	Height      float64 `json:"height_mm"`
	Margins     Margins `json:"margins"`
	FontSize    float64 `json:"font_size"` // points
	Accent      string  `json:"accent_color"`
	Header      []Block `json:"header"`
	Body        []Block `json:"body"`
	Footer      []Block `json:"footer"`
	Filename    string  `json:"filename"` // a formula
	Lists       []List  `json:"lists"`
}

// Margins are in millimeters.
type Margins struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

// List is a named list of records of the document (lists.items):
// order_items where order = {{record.id}}.
type List struct {
	Name       string `json:"name"`
	Collection string `json:"collection"`
	Filter     string `json:"filter"`
	Sort       string `json:"sort"`
	Limit      int    `json:"limit"`
}

// Block is a part of the document. Its settings depend on its type.
type Block struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	ShowIf string `json:"show_if,omitempty"` // a formula: the block is left out when it's empty, false or 0

	// heading, text
	Text   string  `json:"text,omitempty"`
	Level  int     `json:"level,omitempty"` // heading: 1 to 3
	Align  string  `json:"align,omitempty"` // left | center | right
	Size   float64 `json:"size,omitempty"`  // points
	Color  string  `json:"color,omitempty"`
	Bold   bool    `json:"bold,omitempty"`
	Italic bool    `json:"italic,omitempty"`
	Mono   bool    `json:"mono,omitempty"`

	// fields: label/value rows; table: the totals under it
	Rows       []Row   `json:"rows,omitempty"`
	LabelWidth float64 `json:"label_width,omitempty"` // percent

	// columns
	Columns []Column `json:"columns,omitempty"`

	// table: the rows come from a list (a formula: {{lists.items}})
	Items      string     `json:"items,omitempty"`
	Cells      []TableCol `json:"cells,omitempty"`
	Totals     []Row      `json:"totals,omitempty"`
	Zebra      bool       `json:"zebra,omitempty"`
	HideHeader bool       `json:"hide_header,omitempty"`
	Empty      string     `json:"empty,omitempty"` // shown when the list is empty

	// image: a file of the template (asset), of the record (field) or an
	// address (url, a formula: {{record.photo_url}}/large.webp)
	Asset string  `json:"asset,omitempty"`
	Field string  `json:"field,omitempty"`
	URL   string  `json:"url,omitempty"`
	Width float64 `json:"width,omitempty"` // mm

	// spacer
	Height float64 `json:"height,omitempty"` // mm
}

// Row is a label and its value (formulas).
type Row struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Column is a column of a "columns" block.
type Column struct {
	Width  float64 `json:"width"` // percent
	Blocks []Block `json:"blocks"`
}

// TableCol is a column of a table: {{row.x}} is the item, {{index}} its position.
type TableCol struct {
	Header string  `json:"header"`
	Value  string  `json:"value"`
	Width  float64 `json:"width"` // percent
	Align  string  `json:"align"`
}

var (
	blockTypes = map[string]bool{"heading": true, "text": true, "fields": true, "columns": true, "table": true, "image": true, "divider": true, "spacer": true, "page_break": true}
	formats    = map[string][2]float64{
		"a3": {297, 420}, "a4": {210, 297}, "a5": {148, 210}, "letter": {215.9, 279.4}, "legal": {215.9, 355.6},
	}
	listNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,40}$`)
	colorRegex    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// pageSize returns the page size in millimeters.
func (s *Spec) pageSize() (w, h float64) {
	if f, ok := formats[s.Format]; ok {
		w, h = f[0], f[1]
	} else {
		w, h = s.Width, s.Height
		if w < 50 || h < 50 || w > 1200 || h > 1200 {
			w, h = 210, 297
		}
	}
	if s.Orientation == "landscape" {
		w, h = h, w
	}
	return w, h
}

// normalize fills the defaults.
func (s *Spec) normalize() {
	if s.Format == "" {
		s.Format = "a4"
	}
	if s.FontSize < 6 || s.FontSize > 24 {
		s.FontSize = 10
	}
	m := &s.Margins
	for _, v := range []*float64{&m.Top, &m.Right, &m.Bottom, &m.Left} {
		if *v <= 0 || *v > 80 {
			*v = 18
		}
	}
	if !colorRegex.MatchString(s.Accent) {
		s.Accent = "#2563eb"
	}
}

// specOf reads the spec of a template record.
func specOf(rec *core.Record) (*Spec, error) {
	s := &Spec{
		Format:      rec.GetString("format"),
		Orientation: rec.GetString("orientation"),
		Width:       rec.GetFloat("width_mm"),
		Height:      rec.GetFloat("height_mm"),
		FontSize:    rec.GetFloat("font_size"),
		Accent:      rec.GetString("accent_color"),
		Filename:    rec.GetString("filename"),
	}
	for field, dst := range map[string]any{"margins": &s.Margins, "header": &s.Header, "body": &s.Body, "footer": &s.Footer, "lists": &s.Lists} {
		raw := rec.GetString(field)
		if raw == "" || raw == "null" {
			continue
		}
		if err := json.Unmarshal([]byte(raw), dst); err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
	}
	s.normalize()
	return s, nil
}

// walk calls fn for every block, the blocks of the columns included.
func walk(blocks []Block, fn func(b *Block)) {
	for i := range blocks {
		fn(&blocks[i])
		for j := range blocks[i].Columns {
			walk(blocks[i].Columns[j].Blocks, fn)
		}
	}
}

// formulas returns the formulas of a block (for the checks).
func (b *Block) formulas() []string {
	out := []string{b.ShowIf, b.Text, b.Items, b.Empty, b.URL}
	for _, r := range append(append([]Row{}, b.Rows...), b.Totals...) {
		out = append(out, r.Label, r.Value)
	}
	for _, c := range b.Cells {
		out = append(out, c.Header, c.Value)
	}
	return out
}

// label is the name of a block in the messages.
func (b *Block) label() string {
	name := strings.ReplaceAll(b.Type, "_", " ")
	if t := strings.TrimSpace(b.Text); t != "" && (b.Type == "heading" || b.Type == "text") {
		if len([]rune(t)) > 30 {
			t = string([]rune(t)[:30]) + "…"
		}
		return fmt.Sprintf("%s “%s”", name, t)
	}
	return name
}
