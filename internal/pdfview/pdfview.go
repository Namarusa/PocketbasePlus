// Package pdfview holds what the modules showing PDFs share: the files of
// PDF.js (Mozilla, Apache-2.0: pdfjs/LICENSE), served by the Documents editor
// and by the PDF viewers of the pages, and the signed links of the documents
// shown in the pages (a page signs the template and the record it shows, the
// Documents module checks the link and draws the PDF).
package pdfview

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Version of PDF.js: the files are served under it (cached for a year).
const Version = "4.10.38"

// The files are gzipped (pdf.min.mjs.gz): sent as they are to the browsers
// accepting gzip (all of them), unzipped for the others.
//
//go:embed pdfjs
var files embed.FS

// Exists reports whether a file of PDF.js is embedded ("pdf.min.mjs",
// "cmaps/UniJIS-UCS2-H.bcmap", "standard_fonts/FoxitSans.pfb").
func Exists(name string) bool {
	_, ok := lookup(name)
	return ok
}

func lookup(name string) (string, bool) {
	clean := path.Clean("/" + name)[1:]
	if clean != name || name == "" || strings.Contains(name, "..") || strings.HasSuffix(name, ".gz") {
		return "", false
	}
	for _, p := range []string{"pdfjs/" + name + ".gz", "pdfjs/" + name} {
		if st, err := fs.Stat(files, p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// Serve answers with a file of PDF.js. version is the one of the request
// path: another version is not found (the address of an older or newer
// PDF.js, cached by the browser).
func Serve(e *core.RequestEvent, version, name string) error {
	p, ok := lookup(name)
	if version != Version || !ok {
		return e.NotFoundError("", nil)
	}
	data, err := files.ReadFile(p)
	if err != nil {
		return e.NotFoundError("", err)
	}
	h := e.Response.Header()
	h.Set("Content-Type", contentType(name))
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Vary", "Accept-Encoding")
	// the worker and the fonts are read by scripts of the pages: same origin only
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if strings.HasSuffix(p, ".gz") {
		if strings.Contains(e.Request.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
		} else {
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return e.InternalServerError("", err)
			}
			defer zr.Close()
			if data, err = io.ReadAll(zr); err != nil {
				return e.InternalServerError("", err)
			}
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	e.Response.WriteHeader(http.StatusOK)
	if e.Request.Method != http.MethodHead {
		_, err = e.Response.Write(data)
	}
	return err
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".mjs", ".js":
		return "text/javascript; charset=utf-8"
	case ".ttf":
		return "font/ttf"
	case "":
		return "text/plain; charset=utf-8" // the licenses
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// --- documents shown in the pages ---------------------------------------------------------

// ViewPath is the address of the Documents module drawing a template for a
// record (record "-": none), followed by ?exp=...&sig=... (ViewLink).
const ViewPath = "/api/documents/view/"

// ViewTTL is how long a link of a page shows its document.
const ViewTTL = 6 * time.Hour

// ViewLink returns the signed address of a document shown by a page: the
// template (id or slug) drawn for a record of a collection (empty: none).
func ViewLink(app core.App, template, collection, record string) string {
	if record == "" {
		record = "-"
	}
	exp := strconv.FormatInt(time.Now().Add(ViewTTL).Unix(), 10)
	return ViewPath + escape(template) + "/" + escape(collection+":"+record) + "?exp=" + exp + "&sig=" + viewSig(app, template, collection, record, exp)
}

// CheckView checks the signature of a link made by ViewLink, and returns the
// collection and record it shows (empty for "-").
func CheckView(app core.App, template, target, exp, sig string) (collection, record string, ok bool) {
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > ts {
		return "", "", false
	}
	collection, record, found := strings.Cut(target, ":")
	if !found || !hmac.Equal([]byte(sig), []byte(viewSig(app, template, collection, record, exp))) {
		return "", "", false
	}
	if record == "-" {
		record = ""
	}
	return collection, record, true
}

func viewSig(app core.App, template, collection, record, exp string) string {
	base := ""
	if c, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers); err == nil {
		base = c.AuthToken.Secret
	}
	k := hmac.New(sha256.New, []byte(base))
	k.Write([]byte("pocketbaseplus-document-view"))
	m := hmac.New(sha256.New, k.Sum(nil))
	m.Write([]byte(template + "\x00" + collection + "\x00" + record + "\x00" + exp))
	return hex.EncodeToString(m.Sum(nil))
}

// escape keeps a path segment readable (ids and slugs need no escaping).
func escape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.:~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}
