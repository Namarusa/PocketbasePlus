// Package flows implements a generic automation engine (in the spirit of
// Directus Flows / Budibase automations): a flow has one trigger and a list
// of steps; conditions and loops hold nested step lists.
package flows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/actor"
	"github.com/Namarusa/PocketbasePlus/internal/mail"
)

const (
	flowsCollection = "flows"
	runsCollection  = "flow_runs"

	TriggerRecord   = "record"
	TriggerManual   = "manual"
	TriggerWebhook  = "webhook"
	TriggerSchedule = "schedule"
	TriggerFlow     = "flow"

	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusRejected = "rejected"

	maxDepth         = 5    // max nested flow triggering (flow -> record change -> flow ...)
	maxConcurrent    = 10   // max background runs at once
	maxQueued        = 1000 // max background runs waiting for a slot
	maxLogValueBytes = 8000 // per step input/output stored in the run log
	syncTimeout      = 15 * time.Second
	asyncTimeout     = 15 * time.Minute
)

// Step is one flow step. Condition steps use Then/Else, loops use Steps.
type Step struct {
	ID              string         `json:"id"`
	Key             string         `json:"key"`
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Config          map[string]any `json:"config"`
	Disabled        bool           `json:"disabled,omitempty"`
	ContinueOnError bool           `json:"continueOnError,omitempty"`
	Then            []Step         `json:"then,omitempty"`
	Else            []Step         `json:"else,omitempty"`
	Steps           []Step         `json:"steps,omitempty"`
}

// StepLog is the run log entry of one executed step.
type StepLog struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Status   string `json:"status"` // success | failed | skipped
	Input    any    `json:"input,omitempty"`
	Output   any    `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
	Depth    int    `json:"depth,omitempty"` // nesting level (branches/loops)
	Index    *int   `json:"index,omitempty"` // loop iteration
}

// Result is the outcome of a run.
type Result struct {
	RunId    string
	Status   string
	Output   any
	Error    string
	Rejected bool
}

// rejectError stops a flow and, for "before" record flows, rejects the change.
type rejectError struct{ message string }

func (e *rejectError) Error() string { return e.message }

// stopError stops a flow successfully.
var errStop = errors.New("flow stopped")

type ctxKey struct{}

// txKey marks contexts of runs executing inside a record save transaction.
type txKey struct{}

// depthFromContext returns the flow nesting depth carried by a context.
func depthFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	d, _ := ctx.Value(ctxKey{}).(int)
	return d
}

// inTxFromContext reports whether a context belongs to a run executing
// inside a record save transaction (a "before" flow or a flow it started).
func inTxFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(txKey{}).(bool)
	return v
}

// run holds the state of one flow execution.
type run struct {
	engine *Engine
	app    core.App // the app or the transaction app (before record flows)
	ctx    context.Context
	flow   *core.Record
	data   map[string]any
	logs   []StepLog
	depth  int // nesting level of the current step list

	// before record flows: the record being saved (can be modified)
	triggerRecord *core.Record
	before        bool

	// page actions: what the steps decide for the response
	action *actionState

	// test runs of the editor: the data kept for the variable picker
	sample *sampleState
}

// Engine runs flows.
type Engine struct {
	app  core.App
	mail *mail.Engine
	sem  chan struct{}

	cacheMu     sync.RWMutex
	cacheLoaded bool
	active      []*core.Record

	pending sync.WaitGroup // background runs and deferred run log writes
	queued  atomic.Int64   // background runs started or waiting for a slot

	// Sync makes background runs synchronous (used by tests).
	Sync bool
}

func newEngine(app core.App, mailEngine *mail.Engine) *Engine {
	return &Engine{app: app, mail: mailEngine, sem: make(chan struct{}, maxConcurrent)}
}

// activeFlows returns the cached active flows.
func (e *Engine) activeFlows() []*core.Record {
	e.cacheMu.RLock()
	if e.cacheLoaded {
		defer e.cacheMu.RUnlock()
		return e.active
	}
	e.cacheMu.RUnlock()

	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if !e.cacheLoaded {
		list, err := e.app.FindRecordsByFilter(flowsCollection, "active = true", "", 0, 0)
		if err != nil {
			e.app.Logger().Error("flows: failed to load flows", "error", err)
			return nil
		}
		e.active = list
		e.cacheLoaded = true
	}
	return e.active
}

func (e *Engine) invalidate() {
	e.cacheMu.Lock()
	e.cacheLoaded = false
	e.active = nil
	e.cacheMu.Unlock()
}

// RunOptions customizes a run.
type RunOptions struct {
	App           core.App        // defaults to the engine app
	Ctx           context.Context // carries the nesting depth
	TriggerRecord *core.Record    // before record flows
	Before        bool
	Timeout       time.Duration
	Sample        bool // keep the test data of the editor (flow_runs.sample)

	action *actionState // page actions
}

// Run executes a flow synchronously and stores the run log.
func (e *Engine) Run(flow *core.Record, trigger map[string]any, opts RunOptions) *Result {
	app := opts.App
	if app == nil {
		app = e.app
	}
	parent := opts.Ctx
	if parent == nil {
		parent = context.Background()
	}
	depth := depthFromContext(parent) + 1
	if depth > maxDepth {
		e.app.Logger().Warn("flows: max nesting depth reached, run skipped", "flow", flow.Id)
		return &Result{Status: StatusFailed, Error: "max flow nesting depth reached"}
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = asyncTimeout
	}
	// runs inside a record save transaction (and the flows they start) must not
	// write with the main app until it ends: SQLite has a single writer
	inTx := opts.Before || inTxFromContext(parent)
	ctx := context.WithValue(context.WithValue(parent, ctxKey{}, depth), txKey{}, inTx)
	// the records changed by the steps are attributed to the flow (revisions)
	ctx = actor.With(ctx, actor.Actor{Type: actor.TypeFlow, Id: flow.Id, Label: "Flow: " + flow.GetString("name")})
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r := &run{
		engine:        e,
		app:           app,
		ctx:           ctx,
		flow:          flow,
		triggerRecord: opts.TriggerRecord,
		before:        opts.Before,
		action:        opts.action,
		data: map[string]any{
			"trigger": toPlain(trigger),
			"steps":   map[string]any{},
			"last":    nil,
			"flow":    map[string]any{"id": flow.Id, "name": flow.GetString("name")},
			"now":     time.Now().UTC().Format(time.RFC3339),
		},
	}
	if opts.Sample {
		r.sample = newSampleState()
	}

	var steps []Step
	if err := json.Unmarshal([]byte(flow.GetString("steps")), &steps); err != nil && flow.GetString("steps") != "" {
		return e.finish(r, time.Now(), fmt.Errorf("invalid steps: %w", err), inTx)
	}

	started := time.Now()
	err := r.runSteps(steps)
	return e.finish(r, started, err, inTx)
}

// finish computes the result and stores the run log.
func (e *Engine) finish(r *run, started time.Time, err error, deferSave bool) *Result {
	res := &Result{Status: StatusSuccess, Output: r.data["last"]}
	var rej *rejectError
	switch {
	case err == nil || errors.Is(err, errStop):
	case errors.As(err, &rej):
		res.Status = StatusRejected
		res.Rejected = true
		res.Error = rej.message
	default:
		res.Status = StatusFailed
		res.Error = err.Error()
	}

	runsCol, cErr := e.app.FindCollectionByNameOrId(runsCollection)
	if cErr != nil {
		return res
	}
	rec := core.NewRecord(runsCol)
	rec.Id = core.GenerateDefaultRandomId() // known before a deferred save
	res.RunId = rec.Id
	rec.Set("flow", r.flow.Id)
	rec.Set("status", res.Status)
	rec.Set("trigger_type", r.flow.GetString("trigger_type"))
	rec.Set("input", truncate(redact(r.data["trigger"])))
	rec.Set("steps", r.logs)
	rec.Set("output", truncate(redact(res.Output)))
	rec.Set("error", limit(res.Error, 5000))
	rec.Set("duration_ms", time.Since(started).Milliseconds())
	if r.sample != nil {
		rec.Set("sample", r.sample.build(r.data["trigger"]))
	}

	save := func() {
		if err := e.app.Save(rec); err != nil {
			e.app.Logger().Error("flows: failed to save run log", "flow", r.flow.Id, "error", err)
		}
	}
	if deferSave {
		// the run is inside a record save transaction: writing with the main app
		// now would wait for that transaction forever, so write once it ends
		e.pending.Add(1)
		go func() {
			defer e.pending.Done()
			save()
		}()
	} else {
		save()
	}
	return res
}

// Wait blocks until the background runs and deferred run log writes are
// done, or the timeout expires. It reports whether everything finished.
func (e *Engine) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		e.pending.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// RunAsync executes a flow in the background (bounded concurrency).
func (e *Engine) RunAsync(flow *core.Record, trigger map[string]any, ctx context.Context) {
	if ctx != nil {
		// keep the depth, not the request lifetime; background runs start after
		// the triggering save is committed so they are not inside its transaction
		ctx = context.WithValue(context.WithoutCancel(ctx), txKey{}, false)
	}
	if e.Sync {
		e.Run(flow, trigger, RunOptions{Ctx: ctx})
		return
	}
	if e.queued.Add(1) > maxQueued {
		e.queued.Add(-1)
		e.app.Logger().Error("flows: too many queued runs, run dropped", "flow", flow.Id)
		run := &run{flow: flow, data: map[string]any{"trigger": toPlain(trigger)}}
		e.finish(run, time.Now(), fmt.Errorf("run dropped: more than %d runs are waiting", maxQueued), false)
		return
	}
	e.pending.Add(1)
	go func() {
		defer e.pending.Done()
		defer e.queued.Add(-1)
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
		defer func() {
			if rec := recover(); rec != nil {
				e.app.Logger().Error("flows: run panic", "flow", flow.Id, "panic", fmt.Sprint(rec))
			}
		}()
		e.Run(flow, trigger, RunOptions{Ctx: ctx})
	}()
}

// runSteps executes a step list in order.
func (r *run) runSteps(steps []Step) error {
	for _, s := range steps {
		if err := r.ctx.Err(); err != nil {
			return fmt.Errorf("flow timeout or cancelled: %w", err)
		}
		if s.Disabled {
			continue
		}
		if err := r.runStep(s); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) runStep(s Step) error {
	started := time.Now()
	entry := StepLog{ID: s.ID, Key: s.Key, Name: s.Name, Type: s.Type, Depth: r.depth}
	if idx, ok := r.data["index"].(int); ok && r.depth > 0 {
		entry.Index = &idx
	}

	handler, ok := stepHandlers[s.Type]
	if !ok {
		entry.Status = StatusFailed
		entry.Error = "unknown step type " + s.Type
		r.logs = append(r.logs, entry)
		return errors.New(entry.Error)
	}

	// placeholder so that nested steps are logged after their parent
	logIndex := len(r.logs)
	r.logs = append(r.logs, entry)

	input, output, err := handler(r, s)

	entry.Duration = time.Since(started).Milliseconds()
	entry.Input = truncate(redact(input))
	entry.Output = truncate(redact(output))

	var rej *rejectError
	switch {
	case err == nil:
		entry.Status = StatusSuccess
	case errors.Is(err, errStop) || errors.As(err, &rej):
		entry.Status = StatusSuccess
		if rej != nil {
			entry.Error = rej.message
		}
	default:
		entry.Status = StatusFailed
		entry.Error = err.Error()
	}
	r.logs[logIndex] = entry

	// expose the output to the next steps
	plain := toPlain(output)
	if s.Key != "" {
		r.data["steps"].(map[string]any)[s.Key] = plain
	}
	r.sample.step(s.Key, plain)
	r.data["last"] = plain

	if err != nil && entry.Status == StatusFailed && s.ContinueOnError {
		return nil
	}
	return err
}

// runNested executes a nested step list (branch or loop body).
func (r *run) runNested(steps []Step) error {
	r.depth++
	defer func() { r.depth-- }()
	return r.runSteps(steps)
}

// secretKeyRegex matches the names of values never written to the run logs
// (passwords typed in page forms, API keys of HTTP headers...).
var secretKeyRegex = regexp.MustCompile(`(?i)(pass(word|wd|code|phrase)?$|^pwd$|secret|token|api[_-]?key|authorization|cookie|^pin$|^otp$|^cv[cv]$|credit.?card|card.?number)`)

const redacted = "[hidden]"

// redact returns a copy of a logged value without its secrets (the run keeps
// the real values).
func redact(v any) any {
	switch val := toPlain(v).(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			if secretKeyRegex.MatchString(k) && item != nil && item != "" {
				out[k] = redacted
			} else {
				out[k] = redact(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = redact(item)
		}
		return out
	default:
		return val
	}
}

// truncate keeps logged values reasonably small.
func truncate(v any) any {
	if v == nil {
		return nil
	}
	plain := toPlain(v)
	b, err := json.Marshal(plain)
	if err != nil || len(b) <= maxLogValueBytes {
		return plain
	}
	return map[string]any{"_truncated": true, "preview": string(b[:maxLogValueBytes])}
}

func limit(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
