package ai

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// DefaultTypeSafeURL is the TypeSafe API (System One models such as Jev).
const DefaultTypeSafeURL = "https://api.typesafe.ai"

// TypeSafe calls the TypeSafe System One API (docs.typesafe.ai/api).
type TypeSafe struct {
	BaseURL string
	APIKey  string
}

func (p *TypeSafe) url(path string) string {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = DefaultTypeSafeURL
	}
	return base + path
}

func (p *TypeSafe) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + p.APIKey}
}

// systemOneRequest builds the request body: questions and choice options
// keep their order.
func systemOneRequest(model string, req Request) ordered {
	questions := ordered{}
	for _, q := range req.Questions {
		question := ordered{{"type", q.Type}, {"instructions", q.Instructions}}
		switch q.Type {
		case QuestionNoul:
			criteria := ordered{}
			if !isBlank(q.Yes) {
				criteria = append(criteria, kv{"true", q.Yes})
			}
			if !isBlank(q.No) {
				criteria = append(criteria, kv{"false", q.No})
			}
			if len(criteria) > 0 {
				question = append(question, kv{"criteria", criteria})
			}
		case QuestionChoice:
			criteria := ordered{}
			for _, o := range q.Options {
				criteria = append(criteria, kv{o.Key, optionDescription(o)})
			}
			question = append(question, kv{"criteria", criteria})
		case QuestionScore:
			question = append(question, kv{"criteria", q.Levels})
		}
		questions = append(questions, kv{q.Key, question})
	}
	return ordered{{"state", req.State}, {"model", model}, {"questions", questions}}
}

// optionDescription is what the model reads about an option: its
// description, its label when it differs from the key (record ids), or null.
func optionDescription(o Option) any {
	if !isBlank(o.Description) {
		if o.Label != "" && o.Label != o.Key {
			return map[string]any{"name": o.Label, "description": o.Description}
		}
		return o.Description
	}
	if o.Label != "" && o.Label != o.Key {
		return o.Label
	}
	return nil
}

type systemOneResponse struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Type          string             `json:"type"`
		Noul          *float64           `json:"noul"`
		Choice        string             `json:"choice"`
		Score         *float64           `json:"score"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    *float64           `json:"confidence"`
	} `json:"answers"`
	Usage Usage `json:"usage"`
}

func (p *TypeSafe) Decide(ctx context.Context, model string, req Request) (*Response, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if model == "" {
		model = "jev-latest"
	}
	var res systemOneResponse
	if err := doJSON(ctx, http.MethodPost, p.url("/v1/systemone"), p.headers(), systemOneRequest(model, req), &res); err != nil {
		return nil, err
	}

	out := &Response{Model: res.Model, Usage: res.Usage, Calibrated: true, Answers: map[string]Answer{}}
	for _, q := range req.Questions {
		a, ok := res.Answers[q.Key]
		if !ok {
			return nil, fmt.Errorf("the AI provider didn't answer the question %q", q.Key)
		}
		answer := Answer{Type: q.Type, Probabilities: a.Probabilities}
		switch q.Type {
		case QuestionNoul:
			if a.Noul == nil {
				return nil, fmt.Errorf("the AI provider returned no value for the question %q", q.Key)
			}
			answer.Noul = clamp01(*a.Noul)
			answer.Confidence = NoulConfidence(answer.Noul)
		case QuestionChoice:
			answer.Choice = a.Choice
			probs := make([]float64, len(q.Options))
			for i, o := range q.Options {
				probs[i] = a.Probabilities[o.Key]
			}
			if answer.Choice == "" {
				answer.Choice = q.Options[argmax(probs)].Key
			}
			if a.Confidence != nil {
				answer.Confidence = clamp01(*a.Confidence)
			} else {
				answer.Confidence = ChoiceConfidence(normalize(probs))
			}
		case QuestionScore:
			probs := make([]float64, len(q.Levels))
			for i := range q.Levels {
				probs[i] = a.Probabilities[strconv.Itoa(i)]
			}
			if a.Score != nil {
				answer.Score = *a.Score
			} else {
				answer.Score = expectation(normalize(probs))
			}
			if a.Confidence != nil {
				answer.Confidence = clamp01(*a.Confidence)
			} else {
				answer.Confidence = ScoreConfidence(normalize(probs))
			}
		}
		out.Answers[q.Key] = answer
	}
	return out, nil
}

func (p *TypeSafe) Models(ctx context.Context) ([]ModelInfo, error) {
	var res struct {
		Models []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			ReleaseDate string `json:"release_date"`
		} `json:"models"`
	}
	if err := doJSON(ctx, http.MethodGet, p.url("/v1/models"), p.headers(), nil, &res); err != nil {
		return nil, err
	}
	list := make([]ModelInfo, 0, len(res.Models))
	for _, m := range res.Models {
		list = append(list, ModelInfo{ID: m.Name, Name: m.Name, Description: m.Description})
	}
	return list, nil
}

// expectation is the probability-weighted level index.
func expectation(probs []float64) float64 {
	s := 0.0
	for i, p := range probs {
		s += float64(i) * p
	}
	return s
}
