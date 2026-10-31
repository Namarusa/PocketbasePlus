package documents

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"unicode/utf8"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/qr"
)

// Code blocks: a QR code, a Data Matrix or a barcode of a value (a formula:
// {{record.number}}, a payment link...), drawn as vector squares and bars:
// sharp at any size, on screen and printed.

var codeKinds = map[string]bool{"qr": true, "datamatrix": true, "code128": true, "code39": true, "ean": true}

const maxCodeValue = 2000 // characters

// twoD tells the square codes from the barcodes.
func (b *Block) twoD() bool { return b.Code == "" || b.Code == "qr" || b.Code == "datamatrix" }

// code returns the encoded value of a block (nil when its value is empty or
// can't be encoded: the error goes on the block).
func (e *engine) code(b *Block) barcode.Barcode {
	value := strings.TrimSpace(e.text(b, b.Text))
	if value == "" {
		return nil
	}
	key := b.Code + "|" + b.ECC + "|" + value
	if c, ok := e.codes[key]; ok {
		return c
	}
	c, err := encodeCode(b.Code, b.ECC, value)
	if err != nil {
		e.fail(b, err)
	}
	e.codes[key] = c
	return c
}

func encodeCode(kind, ecc, value string) (barcode.Barcode, error) {
	if utf8.RuneCountInString(value) > maxCodeValue {
		return nil, fmt.Errorf("the value is too long for a code (%d characters at most)", maxCodeValue)
	}
	var (
		c   barcode.Barcode
		err error
	)
	switch kind {
	case "", "qr":
		level := map[string]qr.ErrorCorrectionLevel{"L": qr.L, "Q": qr.Q, "H": qr.H}[ecc]
		if ecc == "" || ecc == "M" {
			level = qr.M
		}
		c, err = qr.Encode(value, level, qr.Auto)
	case "datamatrix":
		c, err = datamatrix.Encode(value)
	case "code128":
		c, err = code128.Encode(value)
	case "code39":
		c, err = code39.Encode(strings.ToUpper(value), false, true)
	case "ean":
		for _, r := range value {
			if r < '0' || r > '9' {
				return nil, errors.New("an EAN barcode is made of digits only (7, 8, 12 or 13 of them)")
			}
		}
		switch len(value) {
		case 7, 8, 12, 13:
		default:
			return nil, errors.New("an EAN barcode has 7, 8, 12 or 13 digits")
		}
		c, err = ean.Encode(value)
	default:
		return nil, fmt.Errorf("unknown code %q", kind)
	}
	if err != nil {
		return nil, fmt.Errorf("the value can't be encoded: %w", err)
	}
	return c, nil
}

// codeSize returns the size of the bars or squares of a code block, and the
// height of its caption.
func (e *engine) codeSize(b *Block, w float64) (cw, ch float64) {
	c := e.code(b)
	if c == nil {
		return 0, 0
	}
	cw = b.Width
	if b.twoD() {
		if cw <= 0 {
			cw = 30
		}
		cw = min(max(cw, 8), w)
		r := c.Bounds()
		return cw, cw * float64(r.Dy()) / float64(r.Dx())
	}
	if cw <= 0 {
		cw = 60
	}
	cw = min(max(cw, 15), w)
	ch = b.Height
	if ch <= 0 {
		ch = 15
	}
	ch = min(max(ch, 4), 100)
	if b.Caption {
		ch += e.captionStyle().lh + 0.5
	}
	return cw, ch
}

func (e *engine) captionStyle() textStyle {
	size := max(e.spec.FontSize*0.85, 6)
	return textStyle{size: size, lh: size * ptToMM * lineFactor, mono: true, color: e.ink}
}

func (e *engine) drawCode(b *Block, x, w float64) {
	cw, ch := e.codeSize(b, w)
	if cw == 0 {
		return
	}
	e.ensure(ch)
	e.inked()
	cx := x
	switch b.Align {
	case "center":
		cx = x + (w-cw)/2
	case "right":
		cx = x + w - cw
	}
	if n := len(e.open); n > 0 {
		e.open[n-1].x, e.open[n-1].w = cx, cw
	}
	c := e.code(b)
	y := e.pdf.GetY()
	ink := [3]int{0, 0, 0} // black: what the scanners read best
	if colorRegex.MatchString(b.Color) {
		ink = rgb(b.Color)
	}
	e.pdf.SetFillColor(ink[0], ink[1], ink[2])
	r := c.Bounds()
	mw := cw / float64(r.Dx())
	barsH := ch
	if !b.twoD() && b.Caption {
		barsH = ch - e.captionStyle().lh - 0.5
	}
	mh := barsH / float64(r.Dy())
	for row := r.Min.Y; row < r.Max.Y; row++ {
		// the dark modules of a row, drawn as runs
		start := -1
		for col := r.Min.X; col <= r.Max.X; col++ {
			dark := col < r.Max.X && isDark(c.At(col, row))
			if dark && start < 0 {
				start = col
			} else if !dark && start >= 0 {
				// a hair taller than the row: no gaps between the rows
				e.pdf.Rect(cx+float64(start-r.Min.X)*mw, y+float64(row-r.Min.Y)*mh, float64(col-start)*mw, mh+0.01, "F")
				start = -1
			}
		}
	}
	if !b.twoD() && b.Caption {
		st := e.captionStyle()
		e.setFont(st, false, false)
		e.pdf.SetTextColor(ink[0], ink[1], ink[2])
		text := c.Content()
		tw := e.pdf.GetStringWidth(text)
		e.pdf.Text(cx+(cw-tw)/2, y+ch-st.lh*0.3, text)
	}
	e.pdf.SetY(y + ch + 2)
}

func isDark(c color.Color) bool {
	return color.GrayModel.Convert(c).(color.Gray).Y < 128
}
