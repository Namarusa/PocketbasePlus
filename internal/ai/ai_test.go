package ai

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sampleRequest() Request {
	return Request{
		State: map[string]any{"subject": "Payouts failing", "body": "Help! My payouts have been failing for 3 days."},
		Questions: []Question{
			{Key: "urgent", Type: QuestionNoul, Instructions: "Is it urgent?", Yes: "Blocked now"},
			// options out of alphabetical order: the order must be kept
			{Key: "team", Type: QuestionChoice, Instructions: "Which team?", Options: []Option{
				{Key: "technical", Description: "Bugs"},
				{Key: "billing", Description: "Payments"},
				{Key: "rec123", Label: "Sales team"},
			}},
			{Key: "mood", Type: QuestionScore, Instructions: "How frustrated?", Levels: []any{"Calm", "Annoyed", "Angry"}},
		},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.001 }

func TestConfidenceFormulas(t *testing.T) {
	// examples from docs.typesafe.ai/confidence
	if c := ScoreConfidence([]float64{0, 0.57, 0.43}); !near(c, 0.355) {
		t.Errorf("score confidence: got %v", c)
	}
	if c := ScoreConfidence([]float64{0.5, 0, 0.5}); !near(c, 0) {
		t.Errorf("score confidence (opposite ends): got %v", c)
	}
	if c := ScoreConfidence([]float64{0, 0.5, 0.5}); !near(c, 0.25) {
		t.Errorf("score confidence (neighbors): got %v", c)
	}
	if c := ChoiceConfidence([]float64{0.6, 0.3, 0.1}); !near(c, 0.4) {
		t.Errorf("choice confidence: got %v", c)
	}
	if c := NoulConfidence(0.95); !near(c, 0.9) {
		t.Errorf("noul confidence: got %v", c)
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]Request{
		"no questions":     {State: "x"},
		"bad key":          {State: "x", Questions: []Question{{Key: "a-b", Type: QuestionNoul, Instructions: "?"}}},
		"empty question":   {State: "x", Questions: []Question{{Key: "a", Type: QuestionNoul, Instructions: " "}}},
		"one option":       {State: "x", Questions: []Question{{Key: "a", Type: QuestionChoice, Instructions: "?", Options: []Option{{Key: "x"}}}}},
		"duplicate option": {State: "x", Questions: []Question{{Key: "a", Type: QuestionChoice, Instructions: "?", Options: []Option{{Key: "x"}, {Key: "x"}}}}},
		"too many levels":  {State: "x", Questions: []Question{{Key: "a", Type: QuestionScore, Instructions: "?", Levels: []any{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}}}},
		"duplicate key":    {State: "x", Questions: []Question{{Key: "a", Type: QuestionNoul, Instructions: "?"}, {Key: "a", Type: QuestionNoul, Instructions: "?"}}},
	}
	for name, req := range cases {
		if err := req.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if err := sampleRequest().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTypeSafeDecide(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer ts-key" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{
			"model": "jev-1.13.0",
			"answers": {
				"urgent": {"type": "noul", "noul": 0.95},
				"team": {"type": "choice", "choice": "billing", "probabilities": {"technical": 0.1, "billing": 0.88, "rec123": 0.02}, "confidence": 0.81},
				"mood": {"type": "score", "score": 1.05, "legend": {"0": "Calm"}, "probabilities": {"0": 0, "1": 0.95, "2": 0.05}, "confidence": 0.92}
			},
			"usage": {"input_tokens": 300, "output_tokens": 20}
		}`))
	}))
	defer srv.Close()

	p := &TypeSafe{BaseURL: srv.URL, APIKey: "ts-key"}
	res, err := p.Decide(context.Background(), "jev-latest", sampleRequest())
	if err != nil {
		t.Fatal(err)
	}

	// questions and options keep their order, criteria are sent as documented
	for _, want := range []string{
		`"model":"jev-latest"`,
		`"criteria":{"technical":"Bugs","billing":"Payments","rec123":"Sales team"}`,
		`"criteria":{"true":"Blocked now"}`,
		`"criteria":["Calm","Annoyed","Angry"]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("request body misses %s:\n%s", want, body)
		}
	}
	if strings.Index(body, `"urgent"`) > strings.Index(body, `"team"`) || strings.Index(body, `"team"`) > strings.Index(body, `"mood"`) {
		t.Errorf("questions order not kept: %s", body)
	}

	if !res.Calibrated || res.Model != "jev-1.13.0" || res.Usage.InputTokens != 300 {
		t.Errorf("unexpected response: %+v", res)
	}
	if a := res.Answers["urgent"]; !near(a.Noul, 0.95) || !near(a.Confidence, 0.9) {
		t.Errorf("urgent: %+v", a)
	}
	if a := res.Answers["team"]; a.Choice != "billing" || !near(a.Confidence, 0.81) {
		t.Errorf("team: %+v", a)
	}
	if a := res.Answers["mood"]; !near(a.Score, 1.05) || !near(a.Confidence, 0.92) {
		t.Errorf("mood: %+v", a)
	}
}

func TestTypeSafeErrorsAndRetries(t *testing.T) {
	prev := retryBase
	retryBase = time.Millisecond
	defer func() { retryBase = prev }()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch {
		case r.Header.Get("Authorization") == "Bearer bad":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail": "Invalid API key"}`))
		case n == 1:
			w.WriteHeader(529)
		default:
			_, _ = w.Write([]byte(`{"model": "jev-1.13.0", "answers": {"ok": {"type": "noul", "noul": 0.2}}}`))
		}
	}))
	defer srv.Close()

	req := Request{State: "x", Questions: []Question{{Key: "ok", Type: QuestionNoul, Instructions: "?"}}}
	res, err := (&TypeSafe{BaseURL: srv.URL, APIKey: "good"}).Decide(context.Background(), "", req)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !near(res.Answers["ok"].Noul, 0.2) {
		t.Errorf("expected a retry after 529, calls=%d res=%+v", calls.Load(), res)
	}

	_, err = (&TypeSafe{BaseURL: srv.URL, APIKey: "bad"}).Decide(context.Background(), "", req)
	if !IsAPIStatus(err, http.StatusUnauthorized) || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("expected a 401 error with the message, got %v", err)
	}
}

func TestOpenRouterDecide(t *testing.T) {
	var calls atomic.Int32
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer or-key" || r.Header.Get("X-Title") != "Acme" {
			t.Errorf("unexpected request %s %v", r.URL.Path, r.Header)
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		content := "not json"
		if calls.Add(1) > 1 {
			// unnormalized probabilities, wrapped in a code fence
			content = "```json\n" + `{
				"urgent": {"reason": "Payouts failing for days", "p_yes": 0.9},
				"team": {"reason": "Payouts", "probabilities": {"technical": 1, "billing": 3, "rec123": 0}},
				"mood": {"reason": "Exclamation", "probabilities": {"0": 0, "1": 0.5, "2": 0.5}}
			}` + "\n```"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "openai/gpt-5-mini",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
			"usage":   map[string]any{"prompt_tokens": 500, "completion_tokens": 80},
		})
	}))
	defer srv.Close()

	req := sampleRequest()
	req.Explain = true
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "or-key", AppName: "Acme"}
	res, err := p.Decide(context.Background(), "openai/gpt-5-mini", req)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected one retry after an invalid answer, got %d calls", calls.Load())
	}

	// strict schema with one object per question, routed to compatible providers only
	format := sent["response_format"].(map[string]any)
	schema := format["json_schema"].(map[string]any)
	if format["type"] != "json_schema" || schema["strict"] != true {
		t.Errorf("unexpected response_format: %v", format)
	}
	if sent["provider"].(map[string]any)["require_parameters"] != true || sent["temperature"] != 0.0 {
		t.Errorf("unexpected routing/temperature: %v %v", sent["provider"], sent["temperature"])
	}
	team := schema["schema"].(map[string]any)["properties"].(map[string]any)["team"].(map[string]any)
	probs := team["properties"].(map[string]any)["probabilities"].(map[string]any)
	if req := probs["required"].([]any); len(req) != 3 || req[0] != "technical" || req[2] != "rec123" {
		t.Errorf("unexpected option keys: %v", req)
	}
	user := sent["messages"].([]any)[1].(map[string]any)["content"].(string)
	if !strings.Contains(user, `"options":{"technical":"Bugs","billing":"Payments","rec123":"Sales team"}`) || !strings.Contains(user, `"yes_means":"Blocked now"`) {
		t.Errorf("unexpected prompt: %s", user)
	}

	if res.Calibrated || res.Usage.InputTokens != 500 {
		t.Errorf("unexpected response: %+v", res)
	}
	if a := res.Answers["urgent"]; !near(a.Noul, 0.9) || a.Reason == "" {
		t.Errorf("urgent: %+v", a)
	}
	if a := res.Answers["team"]; a.Choice != "billing" || !near(a.Probabilities["billing"], 0.75) || !near(a.Confidence, (0.75-1.0/3)/(2.0/3)) {
		t.Errorf("team: %+v", a)
	}
	if a := res.Answers["mood"]; !near(a.Score, 1.5) || !near(a.Confidence, 0.25) {
		t.Errorf("mood: %+v", a)
	}
}

func TestOpenRouterErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error": {"message": "Insufficient credits", "code": 402}}`))
	}))
	defer srv.Close()
	req := Request{State: "x", Questions: []Question{{Key: "ok", Type: QuestionNoul, Instructions: "?"}}}
	_, err := (&OpenRouter{BaseURL: srv.URL, APIKey: "k"}).Decide(context.Background(), "m", req)
	if err == nil || !strings.Contains(err.Error(), "Insufficient credits") || !strings.Contains(err.Error(), "no credits left") {
		t.Errorf("unexpected error: %v", err)
	}
}
