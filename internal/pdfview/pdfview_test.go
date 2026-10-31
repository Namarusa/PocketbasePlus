package pdfview

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestFiles(t *testing.T) {
	for _, name := range []string{"pdf.min.mjs", "pdf.worker.min.mjs", "LICENSE", "cmaps/UniJIS-UCS2-H.bcmap", "cmaps/LICENSE", "standard_fonts/FoxitSerif.pfb", "standard_fonts/LiberationSans-Regular.ttf"} {
		if !Exists(name) {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"", "../pdfview.go", "cmaps/../pdf.min.mjs", "/pdf.min.mjs", "cmaps", "nope.js", "pdf.min.mjs.gz"} {
		if Exists(name) {
			t.Errorf("%q should not be served", name)
		}
	}

	app := testutil.NewApp(t)
	serve := func(version, name string, gz bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		if gz {
			req.Header.Set("Accept-Encoding", "gzip, br")
		}
		rec := httptest.NewRecorder()
		e := &core.RequestEvent{App: app}
		e.Request, e.Response = req, rec
		if err := Serve(e, version, name); err != nil {
			var apiErr *router.ApiError
			if errors.As(err, &apiErr) {
				rec.Code = apiErr.Status
			}
		}
		return rec
	}
	plain := serve(Version, "pdf.min.mjs", false)
	if plain.Code != 200 || plain.Header().Get("Content-Encoding") != "" || !strings.Contains(plain.Body.String(), "GlobalWorkerOptions") ||
		plain.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("plain: %d %v", plain.Code, plain.Header())
	}
	zipped := serve(Version, "pdf.min.mjs", true)
	zr, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, _ := io.ReadAll(zr)
	if zipped.Header().Get("Content-Encoding") != "gzip" || !bytes.Equal(unzipped, plain.Body.Bytes()) {
		t.Error("gzip: not the same file")
	}
	if font := serve(Version, "standard_fonts/LiberationSans-Regular.ttf", false); font.Header().Get("Content-Type") != "font/ttf" || font.Body.Len() < 10_000 {
		t.Errorf("font: %v %d", font.Header(), font.Body.Len())
	}
	if res := serve("1.0.0", "pdf.min.mjs", false); res.Code != 404 {
		t.Errorf("another version: %d", res.Code)
	}
	if res := serve(Version, "../pdfview.go", false); res.Code != 404 {
		t.Errorf("outside: %d", res.Code)
	}
}

func TestViewLinks(t *testing.T) {
	app := testutil.NewApp(t)
	link := ViewLink(app, "invoice", "orders", "abc123")
	u, err := url.Parse(link)
	if err != nil || !strings.HasPrefix(u.Path, ViewPath+"invoice/orders:abc123") {
		t.Fatalf("link: %s", link)
	}
	q := u.Query()
	col, rec, ok := CheckView(app, "invoice", "orders:abc123", q.Get("exp"), q.Get("sig"))
	if !ok || col != "orders" || rec != "abc123" {
		t.Errorf("check: %v %q %q", ok, col, rec)
	}
	for _, c := range [][4]string{
		{"other", "orders:abc123", q.Get("exp"), q.Get("sig")},
		{"invoice", "orders:abc124", q.Get("exp"), q.Get("sig")},
		{"invoice", "users:abc123", q.Get("exp"), q.Get("sig")},
		{"invoice", "orders:abc123", "9999999999", q.Get("sig")},
		{"invoice", "orders:abc123", "1", q.Get("sig")},
		{"invoice", "orders-abc123", q.Get("exp"), q.Get("sig")},
	} {
		if _, _, ok := CheckView(app, c[0], c[1], c[2], c[3]); ok {
			t.Errorf("accepted %v", c)
		}
	}
	// no record
	u, _ = url.Parse(ViewLink(app, "a b", "", ""))
	if !strings.HasSuffix(u.EscapedPath(), "/a%20b/:-") {
		t.Errorf("no record: %s", u.EscapedPath())
	}
	if _, rec, ok := CheckView(app, "a b", ":-", u.Query().Get("exp"), u.Query().Get("sig")); !ok || rec != "" {
		t.Errorf("no record: %v %q", ok, rec)
	}
}
