package media

import (
	"bytes"
	"encoding/xml"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	_ "golang.org/x/image/webp"
)

// MaxBytes is the largest file of the library.
const MaxBytes = 100 << 20

// the kinds of files (filters of the library, pickers)
var kinds = []string{"image", "video", "audio", "pdf", "font", "document"}

// mimeTypes are the files the library takes: no HTML or scripts (served by
// the app, they would run in its pages).
var mimeTypes = []string{
	"image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/svg+xml", "image/x-icon", "image/vnd.microsoft.icon", "image/bmp", "image/tiff",
	"video/mp4", "video/webm", "video/quicktime", "video/ogg",
	"audio/mpeg", "audio/ogg", "audio/wav", "audio/x-wav", "audio/webm", "audio/mp4", "audio/x-m4a", "audio/aac", "audio/flac", "audio/x-flac",
	"application/pdf",
	"font/woff2", "font/woff", "font/ttf", "font/otf", "font/collection", "application/vnd.ms-fontobject",
	"text/plain", "text/csv", "application/json", "application/zip",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"application/vnd.oasis.opendocument.text", "application/vnd.oasis.opendocument.spreadsheet", "application/vnd.oasis.opendocument.presentation",
	"application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint", "application/rtf", "text/rtf",
}

type meta struct {
	mime          string
	kind          string
	size          int64
	width, height int
}

// fileMeta reads the type and, for images, the size of an uploaded file.
func fileMeta(f *filesystem.File) meta {
	out := meta{size: f.Size, kind: "document"}
	r, err := f.Reader.Open()
	if err != nil {
		return out
	}
	defer r.Close()
	head, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	mt := mimetype.Detect(head)
	out.mime, _, _ = strings.Cut(mt.String(), ";")
	out.kind = kindOf(out.mime, f.OriginalName)
	switch {
	case out.mime == "image/svg+xml":
		out.width, out.height = svgSize(head)
	case out.kind == "image":
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(head)); err == nil {
			out.width, out.height = cfg.Width, cfg.Height
		} else if _, err := r.Seek(0, io.SeekStart); err == nil {
			// a large image: its header is further
			if cfg, _, err := image.DecodeConfig(r); err == nil {
				out.width, out.height = cfg.Width, cfg.Height
			}
		}
	}
	return out
}

func kindOf(mime, name string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case mime == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mime, "font/") || mime == "application/vnd.ms-fontobject":
		return "font"
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".woff", ".woff2", ".ttf", ".otf":
		return "font"
	}
	return "document"
}

var svgLength = regexp.MustCompile(`^\s*([0-9.]+)\s*(px)?\s*$`)

// svgSize reads the width and height of an SVG (its attributes, else its viewBox).
func svgSize(head []byte) (int, int) {
	d := xml.NewDecoder(bytes.NewReader(head))
	for {
		tok, err := d.Token()
		if err != nil {
			return 0, 0
		}
		el, ok := tok.(xml.StartElement)
		if !ok || el.Name.Local != "svg" {
			continue
		}
		var w, h float64
		var box []string
		for _, a := range el.Attr {
			switch a.Name.Local {
			case "width":
				w = svgNumber(a.Value)
			case "height":
				h = svgNumber(a.Value)
			case "viewBox":
				box = strings.Fields(strings.ReplaceAll(a.Value, ",", " "))
			}
		}
		if (w == 0 || h == 0) && len(box) == 4 {
			bw, _ := strconv.ParseFloat(box[2], 64)
			bh, _ := strconv.ParseFloat(box[3], 64)
			if w == 0 && h == 0 {
				w, h = bw, bh
			} else if w == 0 && bh > 0 {
				w = h * bw / bh
			} else if bw > 0 {
				h = w * bh / bw
			}
		}
		return int(math.Round(w)), int(math.Round(h))
	}
}

func svgNumber(s string) float64 {
	m := svgLength.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// baseName is a file name without its extension and PocketBase's suffix.
func baseName(name string) string {
	name = strings.TrimSuffix(name, path.Ext(name))
	return strings.TrimSpace(name)
}

var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

// linkName is the name in the address of a file: its name, and the
// extension of its file (logo-dark.png).
func linkName(name, file string) string {
	s := slugRegex.ReplaceAllString(foldAccents(strings.ToLower(name)), "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-")
	}
	if s == "" {
		s = "file"
	}
	return s + strings.ToLower(path.Ext(file))
}

var accents = strings.NewReplacer("à", "a", "á", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "ç", "c", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ñ", "n", "ò", "o", "ó", "o", "ô", "o", "ö", "o", "õ", "o", "ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y",
	"œ", "oe", "æ", "ae", "ß", "ss")

func foldAccents(s string) string { return accents.Replace(s) }
