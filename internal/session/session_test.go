package session

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/testutil"
)

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"/x?a=1": "/x?a=1", "/": "/", "": "", "//evil.test": "", "https://evil.test": "", "/\\evil": "",
		"javascript:alert(1)": "", "p/x": "", "/x\r\nSet-Cookie: a": "",
	} {
		if got := LocalPath(in); got != want {
			t.Errorf("LocalPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		headers map[string]string
		want    bool
	}{
		{nil, true},
		{map[string]string{"Origin": "http://example.com"}, true},
		{map[string]string{"Origin": "https://evil.test"}, false},
		{map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://example.com"}, true},
		{map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{map[string]string{"Sec-Fetch-Site": "same-site"}, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/api/flows/action/x", nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := SameOrigin(r); got != c.want {
			t.Errorf("%v: got %v", c.headers, got)
		}
	}
}

func TestRecordRef(t *testing.T) {
	app := testutil.NewApp(t)
	ref := SignRecord(app, "flow1", "orders", "abc")
	if col, id, ok := VerifyRecord(app, "flow1", ref); !ok || col != "orders" || id != "abc" {
		t.Errorf("expected a valid reference, got %q %q %v", col, id, ok)
	}
	if _, _, ok := VerifyRecord(app, "flow2", ref); ok {
		t.Error("a reference is bound to its flow")
	}
	rest := ref[len("orders:abc:"):]
	for _, bad := range []string{"", "orders:abc", "orders:abd:" + rest, "users:abc:" + rest, "orders:abc:1:" + ref[len(ref)-32:]} {
		if _, _, ok := VerifyRecord(app, "flow1", bad); ok {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestSecureCookies(t *testing.T) {
	app := testutil.NewApp(t)
	user := testutil.Create(t, app, "users", map[string]any{"email": "a@test.com", "password": "1234567890"})
	event := func(proto string, trusted bool) (*core.RequestEvent, *httptest.ResponseRecorder) {
		if trusted {
			app.Settings().TrustedProxy.Headers = []string{"X-Forwarded-For"}
		} else {
			app.Settings().TrustedProxy.Headers = nil
		}
		r := httptest.NewRequest("GET", "/x", nil)
		if proto != "" {
			r.Header.Set("X-Forwarded-Proto", proto)
		}
		w := httptest.NewRecorder()
		e := &core.RequestEvent{App: app}
		e.Request, e.Response = r, w
		return e, w
	}
	cases := []struct {
		proto   string
		trusted bool
		name    string
		secure  bool
	}{
		{"", false, "gp_session", false},
		{"https", false, "gp_session", false}, // untrusted header
		{"https", true, "__Host-gp_session", true},
	}
	for _, c := range cases {
		e, w := event(c.proto, c.trusted)
		if err := Set(e, user); err != nil {
			t.Fatal(err)
		}
		ck := w.Result().Cookies()[0]
		if ck.Name != c.name || ck.Secure != c.secure || !ck.HttpOnly {
			t.Errorf("%+v: got %s secure=%v", c, ck.Name, ck.Secure)
		}
		// read back with the same scheme; a plain cookie isn't accepted on https
		e2, _ := event(c.proto, c.trusted)
		e2.Request.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
		if Get(e2) == nil {
			t.Errorf("%+v: expected the visitor", c)
		}
		if c.secure {
			e3, _ := event(c.proto, c.trusted)
			e3.Request.AddCookie(&http.Cookie{Name: "gp_session", Value: ck.Value})
			if Get(e3) != nil {
				t.Error("a cookie without the __Host- prefix must be ignored on https")
			}
		}
	}
}
