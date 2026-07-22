package axonhub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientChannelsUsesBearerAndParsesSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/graphql" || r.Header.Get("Authorization") != "Bearer control-key" {
			t.Fatalf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"allChannelSummarys": []map[string]any{{
					"id": "9", "name": "GPT", "type": "openai", "status": "enabled", "orderingWeight": 88, "tags": []string{"manual"},
					"allModelEntries": []map[string]string{{"requestModel": "gpt-5.6-sol"}},
				}},
			},
		})
	}))
	defer server.Close()
	rows, err := (Client{BaseURL: server.URL, APIKey: "control-key"}).Channels(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].ID != "9" || rows[0].Status != "enabled" || rows[0].OrderingWeight != 88 || len(rows[0].Models) != 1 {
		t.Fatalf("row=%+v", rows[0])
	}
}

func TestClientTreatsGraphQLErrorsAsFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}, "errors": []map[string]string{{"message": "permission denied"}}})
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL, APIKey: "key"}).Channels(t.Context())
	if err == nil || !strings.Contains(err.Error(), "GraphQL") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientWritesLowercaseStatusAndHandlesTimeout(t *testing.T) {
	status := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if value, ok := body.Variables["status"].(string); ok {
			status = value
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"updateChannelStatus": map[string]any{
					"id": "9", "name": "GPT", "type": "openai", "status": "disabled", "orderingWeight": 1, "tags": []string{}, "allModelEntries": []any{},
				},
			},
		})
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL, APIKey: "key"}).UpdateStatus(t.Context(), "9", "DISABLED")
	if err != nil || status != "disabled" {
		t.Fatalf("status=%q err=%v", status, err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer slow.Close()
	_, err = (Client{BaseURL: slow.URL, APIKey: "key", HTTP: &http.Client{Timeout: 5 * time.Millisecond}}).Channels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("timeout err=%v", err)
	}
}
