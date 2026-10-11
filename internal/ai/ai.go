// Package ai holds the AI providers and models configured in Settings → AI,
// and a provider-neutral "decision" API used by the flows "AI decision" step.
//
// A decision evaluates typed questions against a state (record data, text...):
//
//   - noul: a yes/no question, answered with the probability of yes
//   - choice: one option of a set, with a probability per option
//   - score: a rating on ordered levels, with a probability per level
//
// System One models (TypeSafe Jev) answer these natively with calibrated
// probabilities. Standard models (OpenRouter, Anthropic, OpenAI, Gemini,
// DeepSeek, Kimi, GLM) answer them through a JSON schema, with self-reported
// (uncalibrated) probabilities.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

const (
	ProvidersCollection = "ai_providers"
	ModelsCollection    = "ai_models"

	KindTypeSafe   = "typesafe"
	KindOpenRouter = "openrouter"
	KindAnthropic  = "anthropic"
	KindOpenAI     = "openai"
	KindGemini     = "gemini"
	KindDeepSeek   = "deepseek"
	KindKimi       = "kimi"
	KindGLM        = "glm"

	TypeSystemOne = "system_one"
	TypeStandard  = "standard"

	QuestionNoul   = "noul"
	QuestionChoice = "choice"
	QuestionScore  = "score"

	MaxOptions   = 255 // per choice (TypeSafe limit)
	MaxLevels    = 10  // per score (TypeSafe limit)
	MaxQuestions = 100
)

// ModelType returns the model type served by a provider kind.
func ModelType(kind string) string {
	switch kind {
	case KindTypeSafe:
		return TypeSystemOne
	case KindOpenRouter, KindAnthropic, KindOpenAI, KindGemini, KindDeepSeek, KindKimi, KindGLM:
		return TypeStandard
	}
	return ""
}

var KeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Option is one option of a choice question.
type Option struct {
	Key         string `json:"key"`
	Label       string `json:"label,omitempty"` // readable name (record label...), defaults to the key
	Description any    `json:"description,omitempty"`
}

// Question is a typed question. Instructions and descriptions can be text or
// structured data (objects/lists).
type Question struct {
	Key          string   `json:"key"`
	Type         string   `json:"type"`
	Instructions any      `json:"instructions"`
	Yes          any      `json:"yes,omitempty"`     // noul: what a yes means
	No           any      `json:"no,omitempty"`      // noul: what a no means
	Options      []Option `json:"options,omitempty"` // choice
	Levels       []any    `json:"levels,omitempty"`  // score, from lowest to highest
}

// Request is a decision request.
type Request struct {
	State     any        `json:"state"`
	Questions []Question `json:"questions"`
	// Explain asks standard models for a short reason per answer (ignored by System One models).
	Explain bool `json:"explain,omitempty"`
}

// Answer is the typed answer of a question.
type Answer struct {
	Type string `json:"type"`
	// noul: probability of yes (0-1)
	Noul float64 `json:"noul,omitempty"`
	// choice: the most likely option key
	Choice string `json:"choice,omitempty"`
	// score: probability-weighted level index (0 = first level)
	Score float64 `json:"score,omitempty"`
	// choice: option key -> probability; score: level index ("0", "1"...) -> probability
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence"`
	Reason        string             `json:"reason,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response holds one answer per question key.
type Response struct {
	Model      string            `json:"model"`
	Answers    map[string]Answer `json:"answers"`
	Usage      Usage             `json:"usage"`
	Calibrated bool              `json:"calibrated"`
}

// ModelInfo is a model offered by a provider.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Context     int    `json:"context,omitempty"` // context window (tokens), when known
	// prices in USD per million tokens, when known
	InputPrice  float64 `json:"inputPrice,omitempty"`
	OutputPrice float64 `json:"outputPrice,omitempty"`
}

// Provider is an AI provider client.
type Provider interface {
	Decide(ctx context.Context, model string, req Request) (*Response, error)
	Models(ctx context.Context) ([]ModelInfo, error)
}

// Validate checks a request against the provider-neutral limits.
func (req Request) Validate() error {
	if len(req.Questions) == 0 {
		return errors.New("add at least one question")
	}
	if len(req.Questions) > MaxQuestions {
		return fmt.Errorf("at most %d questions per step", MaxQuestions)
	}
	keys := map[string]bool{}
	for _, q := range req.Questions {
		if !KeyRegex.MatchString(q.Key) {
			return fmt.Errorf("question key %q: use only letters, digits and _", q.Key)
		}
		if keys[q.Key] {
			return fmt.Errorf("question key %q is used more than once", q.Key)
		}
		keys[q.Key] = true
		if isBlank(q.Instructions) {
			return fmt.Errorf("question %q: the question text is empty", q.Key)
		}
		switch q.Type {
		case QuestionNoul:
		case QuestionChoice:
			if len(q.Options) < 2 {
				return fmt.Errorf("question %q: a choice needs at least 2 options (got %d)", q.Key, len(q.Options))
			}
			if len(q.Options) > MaxOptions {
				return fmt.Errorf("question %q: a choice accepts at most %d options (got %d)", q.Key, MaxOptions, len(q.Options))
			}
			seen := map[string]bool{}
			for _, o := range q.Options {
				if strings.TrimSpace(o.Key) == "" {
					return fmt.Errorf("question %q: an option is empty", q.Key)
				}
				if seen[o.Key] {
					return fmt.Errorf("question %q: the option %q is listed more than once", q.Key, o.Key)
				}
				seen[o.Key] = true
			}
		case QuestionScore:
			if len(q.Levels) < 2 || len(q.Levels) > MaxLevels {
				return fmt.Errorf("question %q: a score needs 2 to %d levels (got %d)", q.Key, MaxLevels, len(q.Levels))
			}
			for _, l := range q.Levels {
				if isBlank(l) {
					return fmt.Errorf("question %q: a level is empty", q.Key)
				}
			}
		default:
			return fmt.Errorf("question %q: unknown type %q", q.Key, q.Type)
		}
	}
	return nil
}

func isBlank(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case map[string]any:
		return len(val) == 0
	case []any:
		return len(val) == 0
	}
	return false
}

// --- confidence (same formulas as TypeSafe, see docs.typesafe.ai/confidence) ---

// NoulConfidence is the distance of p from an even 0.5, from 0 to 1.
func NoulConfidence(p float64) float64 {
	return clamp01(math.Abs(2*p - 1))
}

// ChoiceConfidence compares the top probability with an even split.
func ChoiceConfidence(probs []float64) float64 {
	n := float64(len(probs))
	if n < 2 {
		return 1
	}
	return clamp01((maxOf(probs) - 1/n) / (1 - 1/n))
}

// ScoreConfidence compares the spread around the most likely level with the
// spread of an even distribution (neighboring levels lower it less).
func ScoreConfidence(probs []float64) float64 {
	n := len(probs)
	if n < 2 {
		return 1
	}
	m := argmax(probs)
	spread, even := 0.0, 0.0
	for i, p := range probs {
		spread += p * math.Abs(float64(i-m))
		even += math.Abs(float64(i) - float64(n-1)/2)
	}
	even /= float64(n)
	return clamp01(1 - spread/even)
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

func maxOf(v []float64) float64 {
	m := math.Inf(-1)
	for _, x := range v {
		m = math.Max(m, x)
	}
	return m
}

// argmax returns the index of the highest value (the first one on ties).
func argmax(v []float64) int {
	best := 0
	for i, x := range v {
		if x > v[best] {
			best = i
		}
	}
	return best
}

// normalize turns raw weights into probabilities summing to 1 (even when all are 0).
func normalize(v []float64) []float64 {
	out := make([]float64, len(v))
	sum := 0.0
	for i, x := range v {
		if math.IsNaN(x) || x < 0 {
			x = 0
		}
		out[i] = x
		sum += x
	}
	for i := range out {
		if sum > 0 {
			out[i] /= sum
		} else {
			out[i] = 1 / float64(len(out))
		}
	}
	return out
}

// --- ordered JSON ----------------------------------------------------------------

// ordered is a JSON object keeping its key order (Go maps are sorted when
// marshaled, and the order of choice options can influence the answers).
type ordered []kv

type kv struct {
	Key   string
	Value any
}

func (o ordered) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(e.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(e.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// IsEmpty reports whether a value is nil, a blank string or an empty map/list.
func IsEmpty(v any) bool {
	return isBlank(v)
}
