package flows

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"

	"github.com/Namarusa/PocketbasePlus/internal/ai"
	"github.com/Namarusa/PocketbasePlus/internal/labels"
)

// "AI decision" step: typed questions (yes/no, choice, score) answered by a
// model of Settings → AI. Config:
//
//	model           ai_models record id
//	state           {mode: "values", values: {name: expr}} or {mode: "expression", expression: expr}
//	maxChars        truncates the texts of the state (0 = no limit)
//	questions       [{key, type, question, context: {name: expr}, yes, no, threshold,
//	                  minConfidence, options: optionsSource, levels: levelsSource}]
//	explain         standard models also return a short reason per answer
//	branches        runs "then" when every answer is confident, "else" otherwise
//	timeoutSeconds  default 30
//
// Output: one entry per question key, plus confident, model, provider,
// calibrated and usage.

const (
	maxAIRecordOptions = ai.MaxOptions
	defaultAITimeout   = 30
)

// output keys that can't be used as question keys
var aiReservedKeys = map[string]bool{"confident": true, "model": true, "provider": true, "calibrated": true, "usage": true}

var aiQuestionTypes = map[string]bool{ai.QuestionNoul: true, ai.QuestionChoice: true, ai.QuestionScore: true}

// validateAIStep checks the static part of the config when the flow is saved.
func validateAIStep(s *Step) error {
	if asString(s.Config, "model") == "" {
		return errors.New("select an AI model")
	}
	questions, _ := s.Config["questions"].([]any)
	if len(questions) == 0 {
		return errors.New("add at least one question")
	}
	if len(questions) > ai.MaxQuestions {
		return fmt.Errorf("at most %d questions", ai.MaxQuestions)
	}
	keys := map[string]bool{}
	for _, raw := range questions {
		q := asMap(raw)
		key := asString(q, "key")
		if !keyRegex.MatchString(key) {
			return fmt.Errorf("the question key %q must contain only letters, digits and _", key)
		}
		if aiReservedKeys[key] {
			return fmt.Errorf("the question key %q is reserved, choose another one", key)
		}
		if keys[key] {
			return fmt.Errorf("the question key %q is used more than once", key)
		}
		keys[key] = true
		if !aiQuestionTypes[asString(q, "type")] {
			return fmt.Errorf("question %q: unknown type %q", key, asString(q, "type"))
		}
	}
	return nil
}

// aiQuestion is a resolved question with the settings used to build its output.
type aiQuestion struct {
	ai.Question
	threshold     float64
	minConfidence float64
	levelLabels   []string
}

func stepAIDecision(r *run, s Step) (any, any, error) {
	model, err := ai.LoadModel(r.app, asString(s.Config, "model"))
	if err != nil {
		return nil, nil, err
	}

	state, err := r.aiState(s.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("state: %w", err)
	}

	var questions []aiQuestion
	for _, raw := range asList(s.Config["questions"]) {
		q, err := r.aiQuestion(asMap(raw))
		if err != nil {
			return nil, nil, err
		}
		questions = append(questions, q)
	}

	req := ai.Request{State: state, Explain: asBool(s.Config, "explain")}
	for _, q := range questions {
		req.Questions = append(req.Questions, q.Question)
	}
	input := map[string]any{
		"model":     model.Name,
		"provider":  model.ProviderName,
		"modelId":   model.Model,
		"state":     state,
		"questions": req.Questions,
	}
	if err := req.Validate(); err != nil {
		return input, nil, err
	}

	timeout := asInt(s.Config, "timeoutSeconds", defaultAITimeout)
	if timeout <= 0 || timeout > 120 {
		timeout = defaultAITimeout
	}
	ctx, cancel := context.WithTimeout(r.ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	res, err := model.Decide(ctx, req)
	if err != nil {
		return input, nil, err
	}

	output := map[string]any{
		"model":      res.Model,
		"provider":   model.ProviderName,
		"calibrated": res.Calibrated,
		"usage":      map[string]any{"input_tokens": res.Usage.InputTokens, "output_tokens": res.Usage.OutputTokens},
	}
	allConfident := true
	for _, q := range questions {
		answer := aiAnswerOutput(q, res.Answers[q.Key])
		if !answer["confident"].(bool) {
			allConfident = false
		}
		output[q.Key] = answer
	}
	output["confident"] = allConfident

	if asBool(s.Config, "branches") {
		// the branches use the answers
		plain := toPlain(output)
		r.data["steps"].(map[string]any)[s.Key] = plain
		r.data["last"] = plain
		branch := s.Else
		if allConfident {
			branch = s.Then
		}
		if err := r.runNested(branch); err != nil {
			return input, output, err
		}
	}
	return input, output, nil
}

// aiAnswerOutput is the step output of one answer: the raw values plus
// ready-to-use fields for conditions (yes, label, level, confident).
func aiAnswerOutput(q aiQuestion, a ai.Answer) map[string]any {
	confidence := round4(a.Confidence)
	out := map[string]any{
		"type":       q.Type,
		"confidence": confidence,
		"confident":  confidence >= q.minConfidence,
	}
	if a.Reason != "" {
		out["reason"] = a.Reason
	}
	switch q.Type {
	case ai.QuestionNoul:
		out["value"] = round4(a.Noul)
		out["yes"] = a.Noul >= q.threshold
	case ai.QuestionChoice:
		out["choice"] = a.Choice
		out["label"] = a.Choice
		for _, o := range q.Options {
			if o.Key == a.Choice && o.Label != "" {
				out["label"] = o.Label
			}
		}
		out["probabilities"] = roundMap(a.Probabilities)
	case ai.QuestionScore:
		level := int(math.Round(a.Score))
		level = max(0, min(level, len(q.levelLabels)-1))
		out["score"] = round4(a.Score)
		out["level"] = level
		if level < len(q.levelLabels) {
			out["label"] = q.levelLabels[level]
		}
		probs := map[string]any{}
		for i, l := range q.levelLabels {
			probs[l] = round4(a.Probabilities[fmt.Sprint(i)])
		}
		out["probabilities"] = probs
	}
	return out
}

// aiState resolves the data the model evaluates.
func (r *run) aiState(cfg map[string]any) (any, error) {
	st := asMap(cfg["state"])
	var state any
	if asString(st, "mode") == "expression" {
		v, err := resolveValue(st["expression"], r.data)
		if err != nil {
			return nil, err
		}
		state = toPlain(v)
	} else {
		v, err := resolveValue(st["values"], r.data)
		if err != nil {
			return nil, err
		}
		values := asMap(toPlain(v))
		if len(values) == 0 {
			return nil, errors.New("add at least one value to evaluate")
		}
		state = values
	}
	if n := asInt(cfg, "maxChars", 0); n > 0 {
		state = truncateTexts(state, n)
	}
	if ai.IsEmpty(state) {
		return nil, errors.New("the data to evaluate is empty")
	}
	return state, nil
}

func (r *run) aiQuestion(q map[string]any) (aiQuestion, error) {
	key := asString(q, "key")
	out := aiQuestion{Question: ai.Question{Key: key, Type: asString(q, "type")}}
	fail := func(err error) (aiQuestion, error) {
		return out, fmt.Errorf("question %q: %w", key, err)
	}

	text, err := resolveString(q["question"], r.data)
	if err != nil {
		return fail(err)
	}
	out.Instructions = strings.TrimSpace(text)
	// structured instructions: {"question": ..., "candidate": {...}}, the
	// question refers to the context values by name
	if ctxValues := asMap(q["context"]); len(ctxValues) > 0 {
		resolved, err := resolveValue(ctxValues, r.data)
		if err != nil {
			return fail(err)
		}
		instructions := map[string]any{}
		for k, v := range asMap(toPlain(resolved)) {
			instructions[k] = v
		}
		instructions["question"] = out.Instructions
		out.Instructions = instructions
	}

	out.minConfidence = asFloat(q, "minConfidence", 0)
	switch out.Type {
	case ai.QuestionNoul:
		out.threshold = asFloat(q, "threshold", 0.5)
		if out.Yes, err = resolveString(q["yes"], r.data); err != nil {
			return fail(err)
		}
		if out.No, err = resolveString(q["no"], r.data); err != nil {
			return fail(err)
		}
	case ai.QuestionChoice:
		if out.Options, err = r.aiOptions(asMap(q["options"])); err != nil {
			return fail(err)
		}
	case ai.QuestionScore:
		levels, labels, err := r.aiLevels(asMap(q["levels"]))
		if err != nil {
			return fail(err)
		}
		out.Levels, out.levelLabels = levels, labels
	}
	return out, nil
}

// aiOptions resolves the options of a choice:
//
//	static      items: [{key, description}] (both accept dynamic content)
//	select      the values of a select field (collection, field), with optional descriptions: {value: text}
//	records     records of a collection (collection, filter, sort, limit, describe: [fields]):
//	            key = record id, label = record name
//	expression  a list (expression): strings, or objects read with keyPath/labelPath/descriptionPath
func (r *run) aiOptions(src map[string]any) ([]ai.Option, error) {
	var options []ai.Option
	switch asString(src, "source") {
	case "", "static":
		for _, raw := range asList(src["items"]) {
			item := asMap(raw)
			key, err := resolveString(item["key"], r.data)
			if err != nil {
				return nil, err
			}
			desc, err := resolveString(item["description"], r.data)
			if err != nil {
				return nil, err
			}
			if key = strings.TrimSpace(key); key != "" {
				options = append(options, ai.Option{Key: key, Description: emptyToNil(desc)})
			}
		}
	case "select":
		values, err := r.selectValues(src)
		if err != nil {
			return nil, err
		}
		descriptions := asMap(src["descriptions"])
		for _, v := range values {
			desc, err := resolveString(descriptions[v], r.data)
			if err != nil {
				return nil, err
			}
			options = append(options, ai.Option{Key: v, Description: emptyToNil(desc)})
		}
	case "records":
		col, err := r.collection(asString(src, "collection"))
		if err != nil {
			return nil, err
		}
		filter, params, err := bindFilter(asString(src, "filter"), r.data)
		if err != nil {
			return nil, err
		}
		limit := asInt(src, "limit", 100)
		if limit <= 0 || limit > maxAIRecordOptions {
			limit = maxAIRecordOptions
		}
		// one more to report lists that don't fit instead of cutting them silently
		records, err := r.app.FindRecordsByFilter(col, filter, asString(src, "sort"), limit+1, 0, params)
		if err != nil {
			return nil, fmt.Errorf("options filter: %w", err)
		}
		if len(records) > limit {
			return nil, fmt.Errorf("more than %d records match the options filter, narrow it down", limit)
		}
		labelFields := labels.Fields(col, false)
		describe := asStrings(src["describe"])
		for _, rec := range records {
			options = append(options, ai.Option{Key: rec.Id, Label: labels.Of(rec, labelFields), Description: describeRecord(rec, describe)})
		}
	case "expression":
		list, err := r.aiList(src)
		if err != nil {
			return nil, err
		}
		for _, item := range list {
			key, label, desc := pickItem(item, asString(src, "keyPath"), asString(src, "labelPath"), asString(src, "descriptionPath"))
			if key == "" {
				return nil, errors.New("an option has no key (set the key path, e.g. id)")
			}
			options = append(options, ai.Option{Key: key, Label: label, Description: desc})
		}
	default:
		return nil, fmt.Errorf("unknown options source %q", asString(src, "source"))
	}
	return dedupeOptions(options), nil
}

// aiLevels resolves the ordered levels of a score (lowest first) and their labels.
func (r *run) aiLevels(src map[string]any) ([]any, []string, error) {
	var levels []any
	var names []string
	switch asString(src, "source") {
	case "", "static":
		for _, raw := range asList(src["items"]) {
			v, err := resolveString(raw, r.data)
			if err != nil {
				return nil, nil, err
			}
			if v = strings.TrimSpace(v); v != "" {
				levels = append(levels, v)
				names = append(names, v)
			}
		}
	case "select":
		values, err := r.selectValues(src)
		if err != nil {
			return nil, nil, err
		}
		descriptions := asMap(src["descriptions"])
		for _, v := range values {
			desc, err := resolveString(descriptions[v], r.data)
			if err != nil {
				return nil, nil, err
			}
			if desc = strings.TrimSpace(desc); desc != "" {
				levels = append(levels, map[string]any{"name": v, "description": desc})
			} else {
				levels = append(levels, v)
			}
			names = append(names, v)
		}
	case "expression":
		list, err := r.aiList(src)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range list {
			key, label, desc := pickItem(item, asString(src, "labelPath"), "", asString(src, "descriptionPath"))
			if label == "" {
				label = key
			}
			if desc != nil {
				levels = append(levels, map[string]any{"name": label, "description": desc})
			} else {
				levels = append(levels, label)
			}
			names = append(names, label)
		}
	default:
		return nil, nil, fmt.Errorf("unknown levels source %q", asString(src, "source"))
	}
	return levels, names, nil
}

// selectValues returns the values of a select field.
func (r *run) selectValues(src map[string]any) ([]string, error) {
	col, err := r.collection(asString(src, "collection"))
	if err != nil {
		return nil, err
	}
	f, ok := col.Fields.GetByName(asString(src, "field")).(*core.SelectField)
	if !ok {
		return nil, fmt.Errorf("%q is not a select field of %s", asString(src, "field"), col.Name)
	}
	return f.Values, nil
}

func (r *run) aiList(src map[string]any) ([]any, error) {
	v, err := resolveValue(src["expression"], r.data)
	if err != nil {
		return nil, err
	}
	switch list := toPlain(v).(type) {
	case []any:
		return list, nil
	case nil:
		return nil, errors.New("the list is empty")
	default:
		return nil, fmt.Errorf("expected a list, got %T", list)
	}
}

// pickItem reads the key, label and description of a list item (plain
// values are their own key).
func pickItem(item any, keyPath, labelPath, descriptionPath string) (string, string, any) {
	m, ok := item.(map[string]any)
	if !ok {
		return strings.TrimSpace(toString(item)), "", nil
	}
	if keyPath == "" {
		keyPath = "id"
	}
	key := strings.TrimSpace(toString(lookupPath(m, keyPath)))
	label := ""
	if labelPath != "" {
		label = toString(lookupPath(m, labelPath))
	}
	var desc any
	if descriptionPath != "" {
		desc = lookupPath(m, descriptionPath)
		if ai.IsEmpty(desc) {
			desc = nil
		}
	}
	return key, label, desc
}

// describeRecord is the description of a record option: one field value, or
// an object of the chosen fields (none = no description).
func describeRecord(rec *core.Record, fields []string) any {
	if len(fields) == 0 {
		return nil
	}
	data := recordData(rec)
	if len(fields) == 1 {
		if ai.IsEmpty(data[fields[0]]) {
			return nil
		}
		return data[fields[0]]
	}
	desc := map[string]any{}
	for _, f := range fields {
		if v, ok := data[f]; ok && !ai.IsEmpty(v) {
			desc[f] = v
		}
	}
	if len(desc) == 0 {
		return nil
	}
	return desc
}

func dedupeOptions(options []ai.Option) []ai.Option {
	seen := map[string]bool{}
	out := options[:0]
	for _, o := range options {
		if !seen[o.Key] {
			seen[o.Key] = true
			out = append(out, o)
		}
	}
	return out
}

// truncateTexts shortens every string of a value to n characters.
func truncateTexts(v any, n int) any {
	switch val := v.(type) {
	case string:
		if utf8.RuneCountInString(val) > n {
			return string([]rune(val)[:n]) + "…"
		}
		return val
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = truncateTexts(item, n)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = truncateTexts(item, n)
		}
		return out
	}
	return v
}

func emptyToNil(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.TrimSpace(s)
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asStrings(v any) []string {
	var out []string
	for _, item := range asList(v) {
		if s := toString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func asFloat(m map[string]any, key string, def float64) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return def
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func roundMap(m map[string]float64) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = round4(v)
	}
	return out
}

// "AI generate" step: a standard model writes a text, or named fields
// (strict JSON schema). Config:
//
//	model           ai_models record id (standard models)
//	system          instructions (role, tone, rules)
//	prompt          the request, with dynamic content
//	output          "text" (default) or "fields"
//	fields          [{name, type: text|number|boolean|list, description}]
//	temperature     optional (0-2)
//	maxTokens       optional
//	timeoutSeconds  default 60
//
// Output: {text} or the fields, plus model and usage.
func stepAIGenerate(r *run, s Step) (any, any, error) {
	model, err := ai.LoadModel(r.app, asString(s.Config, "model"))
	if err != nil {
		return nil, nil, err
	}
	cfg, err := r.resolved(s, "system", "prompt")
	if err != nil {
		return nil, nil, err
	}
	req := ai.GenerateRequest{
		System:    toString(cfg["system"]),
		Prompt:    toString(cfg["prompt"]),
		MaxTokens: asInt(s.Config, "maxTokens", 0),
	}
	if t, ok := s.Config["temperature"].(float64); ok {
		req.Temperature = &t
	}
	if asString(s.Config, "output") == "fields" {
		for _, raw := range asList(s.Config["fields"]) {
			f := asMap(raw)
			req.Fields = append(req.Fields, ai.Field{Name: asString(f, "name"), Type: asString(f, "type"), Description: asString(f, "description")})
		}
		if len(req.Fields) == 0 {
			return nil, nil, errors.New("add at least one output field")
		}
	}
	input := map[string]any{"model": model.Name, "modelId": model.Model, "system": req.System, "prompt": req.Prompt}
	if len(req.Fields) > 0 {
		input["fields"] = req.Fields
	}

	timeout := asInt(s.Config, "timeoutSeconds", 60)
	if timeout <= 0 || timeout > 300 {
		timeout = 60
	}
	ctx, cancel := context.WithTimeout(r.ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	res, err := model.Generate(ctx, req)
	if err != nil {
		return input, nil, err
	}
	output := map[string]any{
		"model": res.Model,
		"usage": map[string]any{"input_tokens": res.Usage.InputTokens, "output_tokens": res.Usage.OutputTokens},
	}
	if len(req.Fields) > 0 {
		for k, v := range res.Fields {
			output[k] = v
		}
	} else {
		output["text"] = res.Text
	}
	return input, output, nil
}

var aiGenerateReserved = map[string]bool{"model": true, "usage": true, "text": true}

func validateAIGenerateStep(s *Step) error {
	if asString(s.Config, "model") == "" {
		return errors.New("select an AI model")
	}
	if strings.TrimSpace(asString(s.Config, "prompt")) == "" {
		return errors.New("the prompt is empty")
	}
	if asString(s.Config, "output") != "fields" {
		return nil
	}
	seen := map[string]bool{}
	for _, raw := range asList(s.Config["fields"]) {
		name := asString(asMap(raw), "name")
		if !keyRegex.MatchString(name) {
			return fmt.Errorf("the output field %q must contain only letters, digits and _", name)
		}
		if aiGenerateReserved[name] || seen[name] {
			return fmt.Errorf("the output field %q is reserved or used twice", name)
		}
		seen[name] = true
	}
	if len(seen) == 0 {
		return errors.New("add at least one output field")
	}
	return nil
}
