package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeSendsFixedModelPayload(t *testing.T) {
	var saw bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		saw = body["model"] == "gpt-5.5" && body["input"] == "ping" && body["stream"] == false
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ok"})
	}))
	defer s.Close()

	got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
	if !saw || !got.Success {
		t.Fatalf("saw=%v got=%+v", saw, got)
	}
}
