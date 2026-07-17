package app

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

type checkSummaryProber struct{}

func (checkSummaryProber) Probe(_ context.Context, baseURL, _, _ string) monitor.ProbeResult {
	if strings.Contains(baseURL, "failed") {
		return monitor.ProbeResult{Status: monitor.StatusError, Error: "Insufficient account balance"}
	}
	return monitor.ProbeResult{Status: monitor.StatusOperational, Success: true}
}

func TestCheckAllLogsProcessingAndProbeCounts(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "check.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, card := range []domain.ModelCard{
		{Name: "正常", BaseURL: "https://ok.example.test", APIKey: "sk-ok", Enabled: true},
		{Name: "余额不足", BaseURL: "https://failed.example.test", APIKey: "sk-failed", Enabled: true},
	} {
		if _, err := st.CreateCard(t.Context(), card); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(st)
	svc.Prober = checkSummaryProber{}
	var logs bytes.Buffer
	oldOutput, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	if err := svc.CheckAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := logs.String()
	for _, want := range []string{
		"cards_processed=2",
		"cards_internal_errors=0",
		"cards_probe_ok=1",
		"cards_probe_failed=1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("日志缺少 %q: %s", want, got)
		}
	}
	if strings.Contains(got, "cards_ok=") || strings.Contains(got, "cards_fail=") {
		t.Fatalf("日志仍包含旧字段: %s", got)
	}
}
