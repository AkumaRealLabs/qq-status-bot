package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMigrateIsRepeatable(t *testing.T) {
	s := testStore(t)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestProbesForCardSinceNormalizesTimeZone(t *testing.T) {
	s := testStore(t)
	if _, err := s.SaveProbe(t.Context(), "u1", "c1", monitor.ProbeResult{Success: true}); err != nil {
		t.Fatal(err)
	}
	since := time.Now().In(time.FixedZone("CST", 8*60*60)).Add(-time.Minute)
	rows, err := s.ProbesForCardSince(t.Context(), "c1", since, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}

func TestProbesForCardSinceCanDisableLimit(t *testing.T) {
	s := testStore(t)
	for range 3 {
		if _, err := s.SaveProbe(t.Context(), "u1", "c1", monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Now().Add(-time.Hour)
	limited, err := s.ProbesForCardSince(t.Context(), "c1", since, 2)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.ProbesForCardSince(t.Context(), "c1", since, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || len(all) != 3 {
		t.Fatalf("limited=%d all=%d", len(limited), len(all))
	}
}

func TestProbesForCardSinceReadsLegacyTimestampFormat(t *testing.T) {
	s := testStore(t)
	if _, err := s.exec(t.Context(), `INSERT INTO probe_runs (id, upstream_id, card_id, checked_at, model, input, http_status, latency_ms, success, error)
		VALUES ('p1', 'u1', 'c1', ?, 'gpt-5.5', 'ping', 200, 12, 1, '')`, time.Now().UTC().Add(-time.Minute).Format("2006-01-02 15:04:05.000Z")); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ProbesForCardSince(t.Context(), "c1", time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Status != monitor.StatusOperational || !rows[0].Success {
		t.Fatalf("legacy probe = %+v", rows[0])
	}
}

func TestSaveProbeStoresChallengeFields(t *testing.T) {
	s := testStore(t)
	run, err := s.SaveProbe(t.Context(), "u1", "c1", monitor.ProbeResult{
		Status:     monitor.StatusValidationFailed,
		Input:      "Which fruit? banana or car",
		Output:     "blue",
		HTTPStatus: 200,
		Latency:    123 * time.Millisecond,
		Error:      "回复验证失败",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ProbesForCardSince(t.Context(), "c1", time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if run.Success || len(rows) != 1 || rows[0].Status != monitor.StatusValidationFailed || rows[0].Input == "ping" || rows[0].Output != "blue" {
		t.Fatalf("run=%+v rows=%+v", run, rows)
	}
}

func TestMigrateAddsProbeStatusColumns(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.exec(t.Context(), `CREATE TABLE probe_runs (
		id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL,
		model TEXT NOT NULL, input TEXT NOT NULL DEFAULT 'ping', http_status INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cols, err := s.columns(t.Context(), "probe_runs")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["status"] || !cols["output"] {
		t.Fatalf("columns = %#v", cols)
	}
}

func TestMigrateAddsCardPublicAndCustomColumns(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.exec(t.Context(), `CREATE TABLE model_cards (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, upstream_id TEXT NOT NULL, key_id TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, last_error TEXT NOT NULL DEFAULT '',
		failure_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(t.Context(), `INSERT INTO model_cards (id, name, upstream_id, model, created_at, updated_at) VALUES ('c1', '旧卡片', 'u1', 'gpt-5.5', ?, ?)`, nowText(), nowText()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cols, err := s.columns(t.Context(), "model_cards")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["base_url"] || !cols["api_key"] || !cols["public_enabled"] || !cols["sort_order"] {
		t.Fatalf("columns = %#v", cols)
	}
	card, err := s.Card(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if card.PublicEnabled || card.BaseURL != "" || card.APIKey != "" {
		t.Fatalf("card = %+v", card)
	}
}

func TestBalanceRechargeLogs(t *testing.T) {
	s := testStore(t)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	log, err := s.SaveBalanceRechargeLog(t.Context(), domain.BalanceRechargeLog{
		UpstreamID: "u1", Method: "order", Amount: 12.5, PaymentType: "stripe", RemoteOrderID: "remote-1", Status: "success", Message: "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.BalanceRechargeLogs(t.Context(), "u1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != log.ID || rows[0].Amount != 12.5 || rows[0].RemoteOrderID != "remote-1" {
		t.Fatalf("rows = %+v", rows)
	}
	cols, err := s.columns(t.Context(), "balance_recharge_logs")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["payment_type"] || !cols["remote_order_id"] {
		t.Fatalf("columns = %#v", cols)
	}
}

func TestCardStoresCustomFields(t *testing.T) {
	s := testStore(t)
	card, err := s.CreateCard(t.Context(), domain.ModelCard{
		Name: "自定义", BaseURL: "https://api.example.test", APIKey: "sk-test", Enabled: true, PublicEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "自定义" || got.BaseURL != "https://api.example.test" || got.APIKey != "sk-test" || !got.PublicEnabled {
		t.Fatalf("card = %+v", got)
	}
}

func TestDeleteCardKeepsProbeRuns(t *testing.T) {
	s := testStore(t)
	card, err := s.CreateCard(t.Context(), domain.ModelCard{Name: "卡片", BaseURL: "https://api.example.test", APIKey: "sk-test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProbe(t.Context(), "", card.ID, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ProbesForCardSince(t.Context(), card.ID, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("probe rows = %d, want 1", len(rows))
	}
}

func TestCardSortOrder(t *testing.T) {
	s := testStore(t)
	a, err := s.CreateCard(t.Context(), domain.ModelCard{Name: "A", BaseURL: "https://a.example.test", APIKey: "sk-a", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateCard(t.Context(), domain.ModelCard{Name: "B", BaseURL: "https://b.example.test", APIKey: "sk-b", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.SortOrder != 1 || b.SortOrder != 2 {
		t.Fatalf("orders: a=%d b=%d", a.SortOrder, b.SortOrder)
	}
	if err := s.UpdateCardOrder(t.Context(), []string{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != b.ID || rows[1].ID != a.ID {
		t.Fatalf("rows = %+v", rows)
	}
	if err := s.UpdateCardOrder(t.Context(), []string{"missing"}); err == nil {
		t.Fatal("missing card id should fail")
	}
}

func TestPocketBaseMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	oldPath := filepath.Join(t.TempDir(), "data.db")
	old, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.ExecContext(ctx, `CREATE TABLE upstreams (id TEXT PRIMARY KEY, name TEXT, type TEXT, base_url TEXT, enabled INTEGER, created_at TEXT, updated_at TEXT);
		INSERT INTO upstreams VALUES ('u1', '上游', 'newapi', 'https://example.test', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()

	s := testStore(t)
	if err := s.MigratePocketBase(ctx, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := s.MigratePocketBase(ctx, oldPath); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListUpstreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "上游" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestPocketBaseMigrationCopiesLegacyColumnsFast(t *testing.T) {
	ctx := context.Background()
	oldPath := filepath.Join(t.TempDir(), "data.db")
	old, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.ExecContext(ctx, `CREATE TABLE upstreams (id TEXT PRIMARY KEY, name TEXT, type TEXT, base_url TEXT, enabled INTEGER);
		CREATE TABLE upstream_keys (id TEXT PRIMARY KEY, upstream TEXT, remote_id TEXT, name TEXT, "group" TEXT);
		CREATE TABLE model_cards (id TEXT PRIMARY KEY, upstream TEXT, key TEXT, name TEXT, model TEXT, enabled INTEGER);
		INSERT INTO upstreams VALUES ('u1', '上游', 'newapi', 'https://example.test', 1);
		INSERT INTO upstream_keys VALUES ('k1', 'u1', 'r1', 'key', '分组');
		INSERT INTO model_cards VALUES ('c1', 'u1', 'k1', '卡片', 'gpt-5.5', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()

	s := testStore(t)
	if err := s.MigratePocketBase(ctx, oldPath); err != nil {
		t.Fatal(err)
	}
	var group string
	if err := s.row(ctx, `SELECT group_name FROM api_keys WHERE id='k1'`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if group != "分组" {
		t.Fatalf("group = %q", group)
	}
}

func TestExportImportData(t *testing.T) {
	ctx := context.Background()
	src := testStore(t)
	if _, err := src.exec(ctx, `INSERT INTO upstreams (id, name, type, base_url, enabled, created_at, updated_at) VALUES ('u1', '上游', 'newapi', 'https://example.test', 1, ?, ?)`, nowText(), nowText()); err != nil {
		t.Fatal(err)
	}
	if _, err := src.exec(ctx, `INSERT INTO api_keys (id, upstream_id, remote_id, name, created_at, updated_at) VALUES ('k1', 'u1', 'r1', 'key', ?, ?)`, nowText(), nowText()); err != nil {
		t.Fatal(err)
	}
	data, err := src.ExportData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dst := testStore(t)
	if err := dst.ImportData(ctx, data); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := dst.row(ctx, `SELECT COUNT(*) FROM api_keys WHERE id='k1' AND upstream_id='u1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("imported keys = %d", n)
	}
}
