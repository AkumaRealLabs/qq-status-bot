package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeSendsFixedModelPayload(t *testing.T) {
	old := newChallenge
	newChallenge = func() challenge { return challenge{Prompt: "Which fruit? banana or car", ExpectedAnswer: "banana"} }
	defer func() { newChallenge = old }()
	var saw bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body struct {
			Model string `json:"model"`
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
			MaxOutputTokens int  `json:"max_output_tokens"`
			Stream          bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		saw = body.Model == "gpt-5.5" &&
			len(body.Input) == 1 &&
			body.Input[0].Role == "user" &&
			len(body.Input[0].Content) == 1 &&
			body.Input[0].Content[0].Type == "input_text" &&
			body.Input[0].Content[0].Text == "Which fruit? banana or car" &&
			body.MaxOutputTokens == 16 &&
			!body.Stream
		_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "banana"})
	}))
	defer s.Close()

	got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
	if !saw || !got.Success || got.Status != StatusOperational || got.Input == "ping" {
		t.Fatalf("saw=%v got=%+v", saw, got)
	}
}

func TestProbeExtractsNestedResponseText(t *testing.T) {
	old := newChallenge
	newChallenge = func() challenge { return challenge{Prompt: "Where? lake", ExpectedAnswer: "lake"} }
	defer func() { newChallenge = old }()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{
				"content": []any{map[string]any{"text": "Lake!"}},
			}},
		})
	}))
	defer s.Close()

	got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
	if got.Status != StatusOperational || got.Output != "Lake!" || !got.Success {
		t.Fatalf("got=%+v", got)
	}
}

func TestProbeClassifiesFailures(t *testing.T) {
	oldChallenge := newChallenge
	oldDegraded := degradedAfter
	newChallenge = func() challenge { return challenge{Prompt: "Which fruit? banana or car", ExpectedAnswer: "banana"} }
	defer func() {
		newChallenge = oldChallenge
		degradedAfter = oldDegraded
	}()

	t.Run("validation failed", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "blue"})
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusValidationFailed || got.Success || got.Error == "" {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("empty reply", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": ""})
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusFailed || got.Success {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("http error keeps body", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad key", http.StatusUnauthorized)
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusFailed || got.HTTPStatus != http.StatusUnauthorized || got.Error == "" {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("degraded", func(t *testing.T) {
		degradedAfter = -time.Nanosecond
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "banana"})
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusDegraded || !got.Success {
			t.Fatalf("got=%+v", got)
		}
	})
}
