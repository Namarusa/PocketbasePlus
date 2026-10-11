package forms

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// IssueToken returns a valid submit token of a form (tests).
func IssueToken(r *core.Record) string {
	return issueToken(r, time.Now())
}

// OldToken returns an expired submit token of a form (tests).
func OldToken(r *core.Record) string {
	return issueToken(r, time.Now().Add(-3*time.Hour))
}

// UseLimit records n submissions of an IP to a form (tests).
func UseLimit(formId, ip string, n int) {
	for i := 0; i < n; i++ {
		submissions.allow(formId+"|"+ip, n, time.Now())
	}
}
