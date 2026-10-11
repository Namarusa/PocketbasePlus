// Package httpcall sends the HTTP requests of the flows (the "HTTP request"
// step and the saved API calls): private addresses are blocked, failed
// requests can be retried, and the response is read as JSON, XML or text.
package httpcall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// AllowPrivateEnv set to "1" or "true" lets the requests reach loopback,
// private and link-local addresses (e.g. services on the same network).
const AllowPrivateEnv = "POCKETBASEPLUS_FLOWS_ALLOW_PRIVATE_HTTP"

// ErrBlockedAddress is returned for a request to an internal address.
var ErrBlockedAddress = errors.New("the address is private or local (set " + AllowPrivateEnv + "=1 to allow it)")

// cgnat is the shared address space (100.64.0.0/10), not covered by netip.Addr.IsPrivate.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isBlockedIP reports whether a dialed address is internal. URLs can be built
// from trigger data (e.g. a webhook body), so they must not reach the server
// itself, its network or the cloud metadata endpoints.
func isBlockedIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() || cgnat.Contains(addr) || (addr.Is4() && addr.As4()[0] == 0)
}

func allowPrivate() bool {
	v, _ := strconv.ParseBool(os.Getenv(AllowPrivateEnv))
	return v
}

// the check runs on the resolved address of every connection (redirects
// included), so DNS names pointing to internal addresses are blocked too
var dialer = &net.Dialer{
	Timeout: 30 * time.Second,
	Control: func(network, address string, _ syscall.RawConn) error {
		if allowPrivate() {
			return nil
		}
		ap, err := netip.ParseAddrPort(address)
		if err != nil || isBlockedIP(ap.Addr()) {
			return ErrBlockedAddress
		}
		return nil
	},
}

var client = &http.Client{Transport: &http.Transport{
	Proxy:                 nil, // a proxy would hide the destination address from the check
	DialContext:           dialer.DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}}

const (
	// MaxBody is the largest response read.
	MaxBody = 5 << 20
	// MaxRaw is the largest raw text kept in the output.
	MaxRaw = 1 << 20
	// MaxRetries is the highest number of retries.
	MaxRetries = 3
	// MaxTimeout is the longest timeout of a request (all attempts).
	MaxTimeout = 2 * time.Minute
)

// Request is a request to send.
type Request struct {
	Method      string
	URL         string
	Query       url.Values // added to the query of the URL
	Headers     map[string]string
	Body        []byte
	ContentType string // used when the headers have none and there is a body
	Timeout     time.Duration
	Retries     int  // retries on network errors, 429 and 5xx (0-3)
	RetryUnsafe bool // also retry POST and PATCH (not idempotent)
	Format      string
}

// Response is the received response.
type Response struct {
	Status   int
	Headers  map[string]any
	Format   string // json, xml or text
	Body     any    // parsed
	Raw      string // the text (MaxRaw at most)
	Attempts int
	Duration time.Duration
}

// OK reports a status below 400.
func (r *Response) OK() bool { return r.Status < 400 }

// Output is the response as plain data for the flows.
func (r *Response) Output() map[string]any {
	return map[string]any{
		"status":  r.Status,
		"ok":      r.OK(),
		"headers": r.Headers,
		"format":  r.Format,
		"body":    r.Body,
		"raw":     r.Raw,
	}
}

// retryBase is the first backoff delay (doubled on each retry).
var retryBase = 500 * time.Millisecond

const maxRetryAfter = 5 * time.Second

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

// Do sends the request (and its retries) and reads the response.
func Do(ctx context.Context, req Request) (*Response, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		return nil, errors.New("the url must start with http:// or https://")
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if len(req.Query) > 0 {
		q := u.Query()
		for k, list := range req.Query {
			for _, v := range list {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}
	format, err := normalizeFormat(req.Format)
	if err != nil {
		return nil, err
	}
	timeout := req.Timeout
	if timeout <= 0 || timeout > MaxTimeout {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	retries := min(max(req.Retries, 0), MaxRetries)
	if !idempotent(method) && !req.RetryUnsafe {
		retries = 0
	}
	sendBody := len(req.Body) > 0 && method != http.MethodGet && method != http.MethodHead

	start := time.Now()
	var res *http.Response
	attempts := 0
	for {
		attempts++
		var body io.Reader
		if sendBody {
			body = bytes.NewReader(req.Body)
		}
		hreq, err := http.NewRequestWithContext(ctx, method, u.String(), body)
		if err != nil {
			return nil, err
		}
		for k, v := range req.Headers {
			hreq.Header.Set(k, v)
		}
		if sendBody && hreq.Header.Get("Content-Type") == "" && req.ContentType != "" {
			hreq.Header.Set("Content-Type", req.ContentType)
		}
		res, err = client.Do(hreq)
		blocked := err != nil && errors.Is(err, ErrBlockedAddress)
		if attempts > retries || blocked || ctx.Err() != nil || (err == nil && !retryableStatus(res.StatusCode)) {
			if err != nil {
				return nil, err
			}
			break
		}
		wait := retryBase << (attempts - 1)
		if err == nil {
			if s, perr := strconv.Atoi(res.Header.Get("Retry-After")); perr == nil && s >= 0 {
				wait = min(time.Duration(s)*time.Second, maxRetryAfter)
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return nil, err
			}
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxBody))
	if err != nil {
		return nil, err
	}

	out := &Response{Status: res.StatusCode, Headers: map[string]any{}, Attempts: attempts}
	for k := range res.Header {
		out.Headers[k] = res.Header.Get(k)
	}
	out.Format, out.Body, err = Parse(raw, res.Header.Get("Content-Type"), format)
	out.Raw = string(raw)
	if len(out.Raw) > MaxRaw {
		out.Raw = out.Raw[:MaxRaw]
	}
	out.Duration = time.Since(start)
	return out, err
}
