package flows

import "time"

// SetScriptMaxHeapGrowth changes the script memory limit and returns a
// function restoring the previous one.
func SetScriptMaxHeapGrowth(n uint64) (restore func()) {
	prev := scriptMaxHeapGrowth
	scriptMaxHeapGrowth = n
	return func() { scriptMaxHeapGrowth = prev }
}

// ResetSignInLimiter forgets the failed sign ins.
func ResetSignInLimiter() {
	signInLimiter.Lock()
	signInLimiter.failures = map[string][]time.Time{}
	signInLimiter.Unlock()
}

// SetActionHTTPLimit changes the number of requests a visitor may make and
// returns a function restoring it.
func SetActionHTTPLimit(n int) (restore func()) {
	prev := actionHTTP.max
	actionHTTP.max = n
	actionHTTP.reset()
	return func() { actionHTTP.max = prev; actionHTTP.reset() }
}
