package app

import (
	"encoding/json"
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
