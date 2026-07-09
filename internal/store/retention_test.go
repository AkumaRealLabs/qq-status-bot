package store

import (
	"path/filepath"
	"testing"
	"time"

	"ai-upstream-monitor/internal/monitor"
)

func TestCleanupExpiredData(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "retention.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}

	old := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := s.exec(t.Context(), `INSERT INTO probe_runs (id, upstream_id, card_id, checked_at, model, input, status, output, http_status, latency_ms, success, error)
		VALUES ('p1','u1','c1',?,'m','','operational','',0,1,1,'')`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(t.Context(), `INSERT INTO balance_snapshots (id, upstream_id, checked_at, balance, used, remain, requests, error, latency_ms)
		VALUES ('b1','u1',?,1,0,1,0,'',0)`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(t.Context(), `INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES ('s1','u1','hash',?,?)`, old, old); err != nil {
		t.Fatal(err)
	}
	// fresh probe should survive
	if _, err := s.SaveProbe(t.Context(), "u1", "c1", monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}

	stats, err := s.CleanupExpiredData(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stats.ProbeRuns < 1 || stats.BalanceSnapshots < 1 || stats.ExpiredSessions < 1 {
		t.Fatalf("expected deletions, got %+v", stats)
	}
	var n int
	if err := s.row(t.Context(), `SELECT COUNT(*) FROM probe_runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fresh probe should remain, count=%d", n)
	}
}
