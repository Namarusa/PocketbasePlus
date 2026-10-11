package flows

import (
	"errors"
	"fmt"
	"runtime/metrics"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const (
	scriptTimeout     = 5 * time.Second
	scriptMaxStack    = 10000 // max call depth (runaway recursion)
	scriptMemoryCheck = 10 * time.Millisecond
)

// scriptMaxHeapGrowth is the process heap growth that interrupts a script.
var scriptMaxHeapGrowth uint64 = 256 << 20

// heapBytes returns the live heap size (cheap, no stop the world).
func heapBytes() uint64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

// stepScript runs sandboxed JavaScript: the code only receives a copy of the
// flow data (`data.trigger`, `data.steps`, `data.last`, `data.item`...) and a
// captured `console.log`; it has no access to the app, files or network.
// The returned value becomes the step output.
func stepScript(r *run, s Step) (any, any, error) {
	code := asString(s.Config, "code")
	logs := []string{}

	vm := goja.New()
	vm.SetMaxCallStackSize(scriptMaxStack)
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	if err := vm.Set("data", toPlain(scriptData(r))); err != nil {
		return nil, nil, err
	}
	logFn := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			if obj, ok := a.Export().(map[string]any); ok {
				parts[i] = toString(obj)
			} else if list, ok := a.Export().([]any); ok {
				parts[i] = toString(list)
			} else {
				parts[i] = a.String()
			}
		}
		if len(logs) < 200 {
			logs = append(logs, strings.Join(parts, " "))
		}
		return goja.Undefined()
	}
	console := vm.NewObject()
	_ = console.Set("log", logFn)
	_ = console.Set("error", logFn)
	_ = console.Set("warn", logFn)
	_ = vm.Set("console", console)

	timer := time.AfterFunc(scriptTimeout, func() {
		vm.Interrupt("script timeout (" + scriptTimeout.String() + ")")
	})
	defer timer.Stop()

	// goja has no memory limit: interrupt the script when the process heap grows
	// too much while it runs. It's a safeguard against runaway allocations
	// (e.g. an endless array push), not an exact per script quota.
	stop := make(chan struct{})
	defer close(stop)
	maxGrowth := scriptMaxHeapGrowth
	go func() {
		base := heapBytes()
		ticker := time.NewTicker(scriptMemoryCheck)
		defer ticker.Stop()
		for {
			select {
			case <-r.ctx.Done():
				vm.Interrupt("flow cancelled")
				return
			case <-ticker.C:
				if heapBytes() > base+maxGrowth {
					vm.Interrupt("script memory limit exceeded")
					return
				}
			case <-stop:
				return
			}
		}
	}()

	// wrapped in a function so that the code can use `return`
	value, err := vm.RunString("(function(data){\n" + code + "\n})(data)")
	input := map[string]any{"console": logs}
	if err != nil {
		var exc *goja.Exception
		var interrupted *goja.InterruptedError
		switch {
		case errors.As(err, &interrupted):
			return input, nil, fmt.Errorf("%v", interrupted.Value())
		case errors.As(err, &exc):
			return input, nil, errors.New(exc.Value().String())
		}
		return input, nil, err
	}

	var output any
	if value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
		output = toPlain(value.Export())
	}
	if len(logs) > 0 {
		input["console"] = logs
	}
	return input, output, nil
}

func scriptData(r *run) map[string]any {
	return map[string]any{
		"trigger": r.data["trigger"],
		"steps":   r.data["steps"],
		"last":    r.data["last"],
		"item":    r.data["item"],
		"index":   r.data["index"],
		"flow":    r.data["flow"],
		"now":     r.data["now"],
	}
}
