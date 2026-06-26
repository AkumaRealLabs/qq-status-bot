package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ai-upstream-monitor/internal/domain"
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
