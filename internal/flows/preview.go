package flows

import (
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/secrets"
)

// The live result of the editor's inputs: an expression evaluated with the
// test data of the flow (the last run started from the editor).

type previewBody struct {
	Flow string `json:"flow"`
	// the loops around the step, outermost first: their keys (the item and
	// index are the first item of their list in the test data)
	Loops []string `json:"loops"`
	// test data the editor knows without a run (e.g. the saved test response
	// of an API call), used for the steps the last run didn't reach
	Steps map[string]any `json:"steps"`
	Expr  string         `json:"expr"`
	// several expressions at once (the inputs of the editor), by id
	Items []previewItem `json:"items"`
}

type previewItem struct {
	ID    string   `json:"id"`
	Expr  string   `json:"expr"`
	Loops []string `json:"loops"`
	// the item of the innermost loop when the test run didn't reach it (the
	// editor finds it in the saved test response of an API call)
	Item any `json:"item"`
}

// latestSample returns the test data of the flow's last test run.
func latestSample(app core.App, flowID string) (map[string]any, *core.Record) {
	if flowID == "" {
		return nil, nil
	}
	runs, err := app.FindRecordsByFilter(runsCollection, "flow = {:f} && sample != null && sample != ''", "-created", 1, 0, dbx.Params{"f": flowID})
	if err != nil || len(runs) == 0 {
		return nil, nil
	}
	var s map[string]any
	_ = json.Unmarshal([]byte(runs[0].GetString("sample")), &s)
	return s, runs[0]
}

// previewData builds the run data of the expressions from the test data.
func previewData(sample map[string]any, flow *core.Record, body previewBody) map[string]any {
	data := asMap(sample["data"])
	trigger := data["trigger"]
	if trigger == nil {
		trigger = map[string]any{}
	}
	steps := map[string]any{}
	for k, v := range body.Steps {
		steps[k] = toPlain(v)
	}
	for k, v := range asMap(data["steps"]) {
		steps[k] = v // the run's values win over the saved samples
	}
	out := map[string]any{
		"trigger": trigger,
		"steps":   steps,
		"last":    nil,
		"now":     time.Now().UTC().Format(time.RFC3339),
	}
	if flow != nil {
		out["flow"] = map[string]any{"id": flow.Id, "name": flow.GetString("name")}
	}
	loops := asMap(data["loops"])
	for _, key := range body.Loops {
		if list, ok := loops[key].([]any); ok && len(list) > 0 {
			out["item"], out["index"] = list[0], 0
		} else {
			out["item"], out["index"] = nil, 0
		}
	}
	return out
}

func (e *Engine) preview(re *core.RequestEvent) error {
	var body previewBody
	if err := re.BindBody(&body); err != nil {
		return re.BadRequestError("Invalid request body.", err)
	}
	if len(body.Items) > 300 {
		return re.BadRequestError("Too many expressions.", nil)
	}
	var flow *core.Record
	if body.Flow != "" {
		flow, _ = re.App.FindRecordById(flowsCollection, body.Flow)
	}
	sample, _ := latestSample(re.App, body.Flow)

	evaluate := func(expr string, loops []string, item any) map[string]any {
		if len(expr) > 20000 {
			return map[string]any{"error": "The expression is too long."}
		}
		b := body
		b.Loops = loops
		data := previewData(sample, flow, b)
		if item != nil && data["item"] == nil && len(loops) > 0 {
			data["item"] = toPlain(item)
		}
		// the secrets show their name, never their value
		if ph := secrets.Placeholders(expr); ph != nil {
			data["secret"] = ph
		}
		out := map[string]any{}
		v, err := resolveValue(expr, data)
		if err != nil {
			out["error"] = cleanTemplateError(err)
			return out
		}
		v = toPlain(v)
		out["type"] = valueType(v)
		out["value"] = pruneSample(redact(v), "", 20, 500, 0, map[string]any{})
		if list, ok := v.([]any); ok {
			out["count"] = len(list) // the value shows 20 items at most
		}
		if s, ok := v.(string); ok {
			out["text"] = s
		}
		if path, ok := singleVar(expr); ok && v == nil {
			out["missing"] = path
		}
		if usesShortened(expr, sample) {
			out["shortened"] = true // computed on the first items of a long list
		}
		return out
	}

	if body.Items != nil {
		results := map[string]any{}
		for _, item := range body.Items {
			results[item.ID] = evaluate(item.Expr, item.Loops, item.Item)
		}
		return re.JSON(http.StatusOK, map[string]any{"hasSample": sample != nil, "results": results})
	}
	out := evaluate(body.Expr, body.Loops, nil)
	out["hasSample"] = sample != nil
	return re.JSON(http.StatusOK, out)
}

func valueType(v any) string {
	if v == nil {
		return "empty"
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return "empty"
		}
		return "text"
	case bool:
		return "bool"
	case float64, float32, int, int64, int32:
		return "number"
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array:
		return "list"
	case reflect.Map:
		return "object"
	}
	return "text"
}

var templatePosRegex = regexp.MustCompile(`^template: [^:]*:\d+(:\d+)?: `)

// cleanTemplateError keeps the useful part of a template error:
// `template: v:1:9: executing "v" at <divide 1 0>: error calling divide:
// division by zero` is "divide: division by zero".
func cleanTemplateError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "error calling "); i >= 0 {
		return msg[i+len("error calling "):]
	}
	msg = templatePosRegex.ReplaceAllString(msg, "")
	msg = strings.ReplaceAll(msg, "$.", "")
	if strings.HasPrefix(msg, "executing ") {
		if i := strings.Index(msg, ">: "); i >= 0 {
			msg = msg[i+3:]
		}
	}
	return msg
}

// sampleOf returns the test data of a flow for the editor: the last test
// run's data, the lengths of its shortened lists and its date.
func sampleOf(re *core.RequestEvent) error {
	sample, run := latestSample(re.App, re.Request.PathValue("id"))
	if sample == nil {
		return re.JSON(http.StatusOK, map[string]any{})
	}
	sample["run"] = run.Id
	sample["created"] = run.GetString("created")
	sample["status"] = run.GetString("status")
	return re.JSON(http.StatusOK, sample)
}

// usesShortened reports whether an expression reads a list the test data
// keeps only the first items of (its count or filters may differ in a run).
func usesShortened(expr string, sample map[string]any) bool {
	src := normalize(expr)
	for path := range asMap(sample["counts"]) {
		if strings.HasPrefix(path, "loops.") {
			continue
		}
		if strings.Contains(src, "."+path) {
			return true
		}
	}
	return false
}
