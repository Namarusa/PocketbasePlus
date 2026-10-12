package flows

import (
	"encoding/json"
	"strconv"
)

// The test data of the flow editor: a run started from the editor keeps the
// trigger data, the first output of each step and the list of each loop,
// without the secrets and shortened (lists cut to 50 items or less to stay
// small, long texts cut), so the variable picker can show the fields and
// their values.

const maxSampleBytes = 128 << 10

// sampleState collects the values during a test run.
type sampleState struct {
	steps map[string]any
	loops map[string]any
}

func newSampleState() *sampleState {
	return &sampleState{steps: map[string]any{}, loops: map[string]any{}}
}

// step keeps the first output of a step (a loop runs its steps many times).
func (s *sampleState) step(key string, output any) {
	if s == nil || key == "" {
		return
	}
	if _, seen := s.steps[key]; !seen {
		s.steps[key] = output
	}
}

func (s *sampleState) loop(key string, items []any) {
	if s == nil || key == "" {
		return
	}
	if _, seen := s.loops[key]; !seen {
		s.loops[key] = items
	}
}

// build returns the stored sample: {data: {trigger, steps, loops}, counts:
// {path: length of each shortened list}}.
func (s *sampleState) build(trigger any) map[string]any {
	data := redact(map[string]any{"trigger": trigger, "steps": s.steps, "loops": s.loops})
	// as many items as possible (the previews count and filter the lists)
	for _, l := range []struct{ items, text int }{{50, 300}, {10, 200}, {3, 120}, {1, 60}} {
		counts := map[string]any{}
		out := pruneSample(data, "", l.items, l.text, 0, counts)
		b, err := json.Marshal(out)
		if err == nil && len(b) <= maxSampleBytes {
			return map[string]any{"data": out, "counts": counts}
		}
	}
	return map[string]any{"data": map[string]any{}, "counts": map[string]any{}, "tooBig": true}
}

// pruneSample shortens a value, recording the real length of the cut lists.
func pruneSample(v any, path string, maxItems, maxText, depth int, counts map[string]any) any {
	if depth > 20 {
		return nil
	}
	join := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		n := 0
		for k, item := range val {
			if n >= 200 {
				break
			}
			out[k] = pruneSample(item, join(k), maxItems, maxText, depth+1, counts)
			n++
		}
		return out
	case []any:
		if len(val) > maxItems {
			counts[path] = len(val)
		}
		out := make([]any, 0, min(len(val), maxItems))
		for i, item := range val {
			if i >= maxItems {
				break
			}
			out = append(out, pruneSample(item, join(strconv.Itoa(i)), maxItems, maxText, depth+1, counts))
		}
		return out
	case string:
		if len(val) > maxText {
			return val[:maxText] + "…"
		}
		return val
	default:
		return val
	}
}
