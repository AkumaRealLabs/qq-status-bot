package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

func TestSetupOnlyOnce(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.Setup(t.Context(), "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Setup(t.Context(), "admin2", "secret"); err == nil {
		t.Fatal("second setup succeeded")
	}
}

func TestUpstreamRowsReturnsEmptyKeysArray(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.SaveUpstream(t.Context(), "", domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.UpstreamRows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "null" || !json.Valid(body) {
		t.Fatalf("bad json: %s", body)
	}
	if !strings.Contains(string(body), `"keys":[]`) {
		t.Fatalf("keys not encoded as empty array: %s", body)
	}
}

func TestEffectiveRatioUsesBalanceRate(t *testing.T) {
	if got := effectiveRatio("0.045", 2); got != "0.09" {
		t.Fatalf("ratio = %q, want 0.09", got)
	}
	if got := effectiveRatio("custom", 2); got != "custom" {
		t.Fatalf("non-numeric ratio = %q, want custom", got)
	}
}

func TestMonitorStatusCountsProbeStatuses(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://example.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "A", UpstreamID: u.ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{monitor.StatusOperational, monitor.StatusDegraded, monitor.StatusValidationFailed} {
		if _, err := st.SaveProbe(t.Context(), u.ID, card.ID, monitor.ProbeResult{Status: status, Latency: time.Millisecond}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := New(st).MonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	if out["success"] != 2 || out["failed"] != 1 {
		t.Fatalf("status = %#v", out)
	}
}

func TestSaveCardSupportsCustomAndUpstreamKey(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	custom, err := svc.SaveCard(t.Context(), "", domain.ModelCard{
		Name: "自定义", BaseURL: " https://api.example.test/ ", APIKey: " sk-test ", Enabled: true, PublicEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.BaseURL != "https://api.example.test" || custom.APIKey != "sk-test" || !custom.PublicEnabled {
		t.Fatalf("custom = %+v", custom)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://upstream.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "k1", Name: "主 Key", Key: "sk-up"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	card, err := svc.SaveCard(t.Context(), "", domain.ModelCard{UpstreamID: u.ID, KeyID: keys[0].ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "A · 主 Key" || card.UpstreamID != u.ID || card.KeyID != keys[0].ID {
		t.Fatalf("card = %+v", card)
	}
}

func TestCheckCustomCardUsesOwnURLKeyAndFixedModel(t *testing.T) {
	var auth, model string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		model, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "banana"})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	card, err := svc.SaveCard(t.Context(), "", domain.ModelCard{Name: "自定义", BaseURL: ts.URL, APIKey: "sk-custom", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-custom" || model != domain.ProbeModel {
		t.Fatalf("auth=%q model=%q", auth, model)
	}
}

func TestSortCardsRejectsDuplicateIDs(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "A", BaseURL: "https://a.example.test", APIKey: "sk-a", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(st).SortCards(t.Context(), []string{card.ID, card.ID}); err == nil {
		t.Fatal("duplicate ids should fail")
	}
}

func TestRedeemBalanceAuditsWithoutPlainCode(t *testing.T) {
	var sawCode bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/topup" {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		sawCode = body["key"] == "secret-code"
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": 10})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: ts.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.RedeemBalance(t.Context(), u.ID, "secret-code"); err != nil {
		t.Fatal(err)
	}
	if !sawCode {
		t.Fatal("redeem code was not sent upstream")
	}
	logs, err := st.BalanceRechargeLogs(t.Context(), u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Status != "success" || strings.Contains(logs[0].PaymentType+logs[0].Message, "secret-code") {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestPublicMonitorStatusFiltersAndRedacts(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	public, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "公开", BaseURL: "https://api.example.test", APIKey: "sk-public", Enabled: true, PublicEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	private, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "私有", BaseURL: "https://private.example.test", APIKey: "sk-private", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "暂停", BaseURL: "https://paused.example.test", APIKey: "sk-paused", PublicEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", public.ID, monitor.ProbeResult{
		Status: monitor.StatusValidationFailed, Input: "secret challenge", Output: "secret answer", Error: `回复验证失败: 期望 "banana", 实际: "secret"`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", private.ID, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", paused.ID, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	out, err := New(st).PublicMonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"name":"公开"`) || !strings.Contains(text, `"name":"暂停"`) || strings.Contains(text, "私有") || strings.Contains(text, "sk-public") || strings.Contains(text, "secret") {
		t.Fatalf("public body = %s", body)
	}
	for _, hidden := range []string{`"id"`, "api_key", "input", "output", "model", "enabled", "public_enabled", "sort_order", "failure_count", "created_at", "updated_at"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("public body leaked %s: %s", hidden, body)
		}
	}
	var parsed struct {
		Rows []struct {
			History []map[string]any `json:"history"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Rows[0].History[0]["success"]; ok {
		t.Fatalf("public history leaked success: %s", body)
	}
	if !strings.Contains(text, "验证失败") || strings.Contains(text, monitor.StatusValidationFailed) {
		t.Fatalf("bad public status summary: %s", body)
	}
}
