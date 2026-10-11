package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// httpClient calls the providers. Their URLs are set by superusers (not by
// flow data), so the private address protection of the HTTP request step
// doesn't apply; timeouts come from the request contexts.
var httpClient = &http.Client{Timeout: 2 * time.Minute}

const maxAttempts = 3

// retryable statuses: rate limited, overloaded (TypeSafe 529) and gateway errors
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == 529 ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// retryBase is the first backoff delay (doubled on each retry).
var retryBase = 500 * time.Millisecond

// doJSON sends a JSON request (body may be nil) and decodes a JSON response
// into out, retrying rate limits and overloads with backoff.
func doJSON(ctx context.Context, method, url string, headers map[string]string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	for attempt := 1; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reader)
		if err != nil {
			return err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		res, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("the AI provider didn't answer in time: %w", ctx.Err())
			}
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
		res.Body.Close()
		if err != nil {
			return err
		}

		if res.StatusCode < 300 {
			if out == nil {
				return nil
			}
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("invalid response from the AI provider: %w", err)
			}
			return nil
		}

		apiErr := &APIError{Status: res.StatusCode, Message: errorMessage(raw)}
		if !retryable(res.StatusCode) || attempt >= maxAttempts {
			return apiErr
		}
		wait := retryBase << (attempt - 1)
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		wait = min(wait, 10*time.Second)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			return apiErr
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return apiErr
		}
	}
}

// APIError is an error status returned by a provider.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	hint := ""
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		hint = " (check the API key in Settings → AI providers)"
	case http.StatusTooManyRequests:
		hint = " (rate limited)"
	case http.StatusPaymentRequired:
		hint = " (no credits left on the provider account)"
	}
	if e.Message == "" {
		return fmt.Sprintf("the AI provider returned status %d%s", e.Status, hint)
	}
	return fmt.Sprintf("the AI provider returned status %d%s: %s", e.Status, hint, e.Message)
}

// IsAPIStatus reports whether err is an APIError with the status.
func IsAPIStatus(err error, status int) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == status
}

// errorMessage extracts a readable message from an error body
// ({"error": {"message"}}, {"error": "..."}, {"detail": ...}, {"message"}).
func errorMessage(raw []byte) string {
	var body map[string]any
	if json.Unmarshal(raw, &body) == nil {
		for _, k := range []string{"error", "detail", "message"} {
			switch v := body[k].(type) {
			case string:
				return limit(v, 500)
			case map[string]any:
				if m, ok := v["message"].(string); ok {
					return limit(m, 500)
				}
			case nil:
			default:
				b, _ := json.Marshal(v)
				return limit(string(b), 500)
			}
		}
	}
	return limit(strings.TrimSpace(string(raw)), 300)
}

func limit(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
