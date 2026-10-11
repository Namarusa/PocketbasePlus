package httpcall

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestXMLToMap(t *testing.T) {
	doc := `<?xml version="1.0" encoding="ISO-8859-1"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>News &amp; more</title>
    <atom:link href="https://x.test/feed" rel="self"/>
    <item><title>One</title><guid isPermaLink="false">a1</guid></item>
    <item><title><![CDATA[Two <b>bold</b>]]></title><empty/></item>
    <note lang="fr">Caf` + "\xe9" + `</note>
  </channel>
</rss>`
	got, err := XMLToMap([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"rss": map[string]any{
		"@version": "2.0",
		"channel": map[string]any{
			"title": "News & more",
			"link":  map[string]any{"@href": "https://x.test/feed", "@rel": "self"},
			"item": []any{
				map[string]any{"title": "One", "guid": map[string]any{"@isPermaLink": "false", "#text": "a1"}},
				map[string]any{"title": "Two <b>bold</b>", "empty": ""},
			},
			"note": map[string]any{"@lang": "fr", "#text": "Café"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}

	// three repeated elements stay one list
	got, _ = XMLToMap([]byte(`<a><b>1</b><b>2</b><b>3</b></a>`))
	if l, _ := got["a"].(map[string]any)["b"].([]any); len(l) != 3 {
		t.Fatalf("expected 3 items, got %#v", got)
	}

	for _, bad := range []string{"", "text", "<a><b></a>", "<a/><b/>", strings.Repeat("<a>", maxXMLDepth+1) + strings.Repeat("</a>", maxXMLDepth+1)} {
		if _, err := XMLToMap([]byte(bad)); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}

	// external entities are never loaded
	xxe := `<?xml version="1.0"?><!DOCTYPE a [<!ENTITY x SYSTEM "file:///etc/passwd">]><a>&x;</a>`
	if v, err := XMLToMap([]byte(xxe)); err == nil && strings.Contains(v["a"].(string), "root") {
		t.Fatal("the external entity was loaded")
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		raw, ctype, format, wantFormat string
		wantErr                        bool
	}{
		{`{"a":1}`, "application/json", "auto", "json", false},
		{`{"a":1}`, "text/plain", "auto", "json", false},
		{`<a>1</a>`, "application/atom+xml", "auto", "xml", false},
		{`<a>1</a>`, "", "auto", "xml", false},
		{`<html><body>x</body></html>`, "text/html", "auto", "text", false},
		{`hello`, "application/json", "auto", "text", false},
		{`hello`, "", "text", "text", false},
		{`<a>1</a>`, "", "", "text", false}, // flows saved before the formats: never XML
		{`[1,2]`, "", "", "json", false},
		{`hello`, "", "json", "text", true},
		{`hello`, "", "xml", "text", true},
		{``, "", "json", "json", false},
	}
	for _, c := range cases {
		f, _, err := Parse([]byte(c.raw), c.ctype, c.format)
		if f != c.wantFormat || (err != nil) != c.wantErr {
			t.Errorf("Parse(%q, %q, %q) = %q, %v", c.raw, c.ctype, c.format, f, err)
		}
	}
	if _, _, err := Parse(nil, "", "yaml"); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func TestDoRetries(t *testing.T) {
	t.Setenv(AllowPrivateEnv, "1")
	prev := retryBase
	retryBase = time.Millisecond
	defer func() { retryBase = prev }()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<r><q>` + html.EscapeString(r.URL.Query().Get("q")) + `</q></r>`))
	}))
	defer srv.Close()

	res, err := Do(context.Background(), Request{URL: srv.URL + "/x?a=1", Query: map[string][]string{"q": {"a b&c"}}, Retries: 3, Format: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || res.Attempts != 3 || res.Format != "xml" {
		t.Fatalf("got %d after %d attempts (%s)", res.Status, res.Attempts, res.Format)
	}
	if q := res.Body.(map[string]any)["r"].(map[string]any)["q"]; q != "a b&c" {
		t.Fatalf("query value: %v", q)
	}

	// POST isn't retried unless asked
	hits.Store(0)
	res, err = Do(context.Background(), Request{Method: "POST", URL: srv.URL, Body: []byte("x"), Retries: 3})
	if err != nil || res.Status != 503 || res.Attempts != 1 {
		t.Fatalf("expected one attempt, got %v %v", res, err)
	}
	hits.Store(0)
	res, _ = Do(context.Background(), Request{Method: "POST", URL: srv.URL, Body: []byte("x"), Retries: 3, RetryUnsafe: true})
	if res.Status != 200 {
		t.Fatalf("expected the retried POST to succeed, got %d", res.Status)
	}
}

func TestDoBlocksPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	_, err := Do(context.Background(), Request{URL: srv.URL, Retries: 3})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected the loopback address to be blocked, got %v", err)
	}
	if _, err := Do(context.Background(), Request{URL: "ftp://x.test/"}); err == nil {
		t.Fatal("expected an error for a non http url")
	}
}
