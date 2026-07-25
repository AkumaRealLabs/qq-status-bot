package axonhub

import (
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
	rows, err := (Client{BaseURL: server.URL, Token: "control-key"}).Channels(t.Context())
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
	_, err := (Client{BaseURL: server.URL, Token: "key"}).Channels(t.Context())
	if err == nil || !strings.Contains(err.Error(), "GraphQL") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientHandlesTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer slow.Close()
	_, err := (Client{BaseURL: slow.URL, Token: "key", HTTP: &http.Client{Timeout: 5 * time.Millisecond}}).Channels(t.Context())
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("timeout err=%v", err)
	}
}

func TestClientSignInUsesAdminCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/auth/signin" || r.Method != http.MethodPost {
			t.Fatalf("path=%s method=%s", r.URL.Path, r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["email"] != "admin@example.com" || body["password"] != "password" {
			t.Fatalf("body=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "token"})
	}))
	defer server.Close()
	token, expiry, err := (Client{BaseURL: server.URL}).SignIn(t.Context(), "admin@example.com", "password")
	if err != nil || token != "token" || !expiry.After(time.Now()) {
		t.Fatalf("token=%q expiry=%s err=%v", token, expiry, err)
	}
}

func TestClientUpdateFieldsNeverUsesStatusMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body.Query, "updateChannelStatus") || !strings.Contains(body.Query, "updateChannel") {
			t.Fatalf("unexpected mutation: %s", body.Query)
		}
		input, _ := body.Variables["input"].(map[string]any)
		if len(input) != 2 || input["orderingWeight"] != float64(90) {
			t.Fatalf("input=%v", input)
		}
		if _, ok := input["status"]; ok {
			t.Fatalf("status must not be written: %v", input)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"updateChannel": map[string]any{
			"id": "9", "name": "GPT", "type": "openai", "status": "disabled", "orderingWeight": 90, "tags": []string{"manual", "payg_low"}, "allModelEntries": []any{},
		}}})
	}))
	defer server.Close()
	row, err := (Client{BaseURL: server.URL, Token: "key"}).UpdateFields(t.Context(), "9", []string{"manual", "payg_low"}, 90)
	if err != nil || row.Status != "disabled" || row.OrderingWeight != 90 {
		t.Fatalf("row=%+v err=%v", row, err)
	}
}
