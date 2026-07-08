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

func TestSaveProbeStoresPingFields(t *testing.T) {
	s := testStore(t)
	run, err := s.SaveProbe(t.Context(), "u1", "c1", monitor.ProbeResult{
		Status:     monitor.StatusOperational,
		Input:      "ping",
		Output:     "pong",
		HTTPStatus: 200,
		Latency:    123 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ProbesForCardSince(t.Context(), "c1", time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Success || len(rows) != 1 || rows[0].Status != monitor.StatusOperational || rows[0].Input != "ping" || rows[0].Output != "pong" {
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

func TestMigrateDropsProbeExpectedAnswer(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.exec(t.Context(), `CREATE TABLE probe_runs (
		id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL,
		model TEXT NOT NULL, input TEXT NOT NULL DEFAULT 'ping', status TEXT NOT NULL DEFAULT '',
		expected_answer TEXT NOT NULL DEFAULT '', output TEXT NOT NULL DEFAULT '', http_status INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cols, err := s.columns(t.Context(), "probe_runs")
	if err != nil {
		t.Fatal(err)
	}
	if cols["expected_answer"] {
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
	if !cols["base_url"] || !cols["api_key"] || !cols["public_enabled"] || !cols["display_group"] || !cols["scheduler_group"] || !cols["sort_order"] {
		t.Fatalf("columns = %#v", cols)
	}
	card, err := s.Card(t.Context(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if card.PublicEnabled || card.BaseURL != "" || card.APIKey != "" || card.DisplayGroup != "" || card.SchedulerGroup != "" {
		t.Fatalf("card = %+v", card)
	}
}

func TestMigrateAddsEpaySettingsColumns(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.exec(t.Context(), `CREATE TABLE settings (
		id TEXT PRIMARY KEY, check_interval_minutes INTEGER NOT NULL, telegram_bot_token TEXT NOT NULL DEFAULT '',
		telegram_chat_id TEXT NOT NULL DEFAULT '', probe_model TEXT NOT NULL DEFAULT 'gpt-5.5'
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(t.Context(), `INSERT INTO settings (id, check_interval_minutes, probe_model) VALUES ('default', 5, ?)`, domain.ProbeModel); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cols, err := s.columns(t.Context(), "settings")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["epay_base_url"] || !cols["epay_pid"] || !cols["epay_key"] {
		t.Fatalf("columns = %#v", cols)
	}
	cfg, err := s.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.EpayBaseURL = " https://pay.example.test/ "
	cfg.EpayPID = " 1000 "
	cfg.EpayKey = " secret "
	if _, err := s.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.EpayBaseURL != "https://pay.example.test" || got.EpayPID != "1000" || got.EpayKey != "secret" {
		t.Fatalf("settings = %+v", got)
	}
}

func TestMigrateCreatesDefaultRevenueCard(t *testing.T) {
	s := testStore(t)
	cards, err := s.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].Name != "今日收入" || cards[0].SourceType != "epay_total" || !cards[0].Enabled {
		t.Fatalf("cards = %+v", cards)
	}
}

func TestOpsStoreCRUD(t *testing.T) {
	s := testStore(t)
	rules, err := s.NotificationRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !rules.Enabled || rules.FailureThreshold != 2 || !rules.Recovery || !rules.EventTypes["probe_failed"] {
		t.Fatalf("default rules = %+v", rules)
	}
	rules.Enabled = false
	if _, err := s.UpdateNotificationRules(t.Context(), rules); err != nil {
		t.Fatal(err)
	}
	if got, err := s.NotificationRules(t.Context()); err != nil || got.Enabled {
		t.Fatalf("rules=%+v err=%v", got, err)
	}
	if _, err := s.CreateOpsEvent(t.Context(), domain.OpsEvent{Type: "probe_failed", Title: "探测失败", Actions: []string{"check_card"}}); err != nil {
		t.Fatal(err)
	}
	events, err := s.OpsEvents(t.Context(), "probe_failed", "unacked", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Actions[0] != "check_card" {
		t.Fatalf("events = %+v", events)
	}
	if err := s.AckOpsEvent(t.Context(), events[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAudit(t.Context(), domain.AuditLog{Actor: "admin", Action: "PATCH /api/settings", Fields: []string{"telegram_bot_token"}}); err != nil {
		t.Fatal(err)
	}
	audit, err := s.AuditLogs(t.Context(), "settings", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Fields[0] != "telegram_bot_token" {
		t.Fatalf("audit = %+v", audit)
	}
	if err := s.CreateAudit(t.Context(), domain.AuditLog{Actor: "admin", Action: "PATCH /api/cards"}); err != nil {
		t.Fatal(err)
	}
	audit, err = s.AuditLogs(t.Context(), "cards", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Fields == nil {
		t.Fatalf("audit = %+v", audit)
	}
	if err := s.CheckWritable(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestTGMigrateAndCRUD(t *testing.T) {
	s := testStore(t)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	sess, err := s.SaveTGSession(t.Context(), domain.TGSession{APIID: 12345, APIHash: "hash", Phone: "+100", CodeHash: "code", SessionBlob: []byte("session")})
	if err != nil {
		t.Fatal(err)
	}
	gotSess, err := s.TGSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != "default" || gotSess.APIID != 12345 || string(gotSess.SessionBlob) != "session" {
		t.Fatalf("session = %+v", gotSess)
	}
	gotSess.Authorized = true
	gotSess.SessionBlob = nil
	if _, err := s.SaveTGSession(t.Context(), gotSess); err != nil {
		t.Fatal(err)
	}
	gotSess, err = s.TGSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(gotSess.SessionBlob) != "session" {
		t.Fatalf("session blob was not preserved: %+v", gotSess)
	}

	ch, err := s.CreateTGChannel(t.Context(), domain.TGChannel{DisplayName: "频道", Identifier: "@demo", Username: "demo", PeerID: 100, AccessHash: 200, AvatarURL: "/api/tg/media/avatar_100.jpg", Enabled: true, MessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ch.Enabled = false
	ch.MessageLimit = 20
	ch.PinnedOnly = true
	if _, err := s.UpdateTGChannel(t.Context(), ch); err != nil {
		t.Fatal(err)
	}
	channels, err := s.ListTGChannels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].Enabled || channels[0].MessageLimit != 20 || !channels[0].PinnedOnly || channels[0].AvatarURL == "" {
		t.Fatalf("channels = %+v", channels)
	}
	if _, err := s.CreateTGChannel(t.Context(), domain.TGChannel{DisplayName: "频道新名", Identifier: "@demo", Username: "demo", PeerID: 100, AccessHash: 300, Enabled: true, MessageLimit: 10}); err != nil {
		t.Fatal(err)
	}
	channels, err = s.ListTGChannels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].Enabled || channels[0].MessageLimit != 20 || !channels[0].PinnedOnly || channels[0].AccessHash != 300 || channels[0].AvatarURL == "" {
		t.Fatalf("upsert should preserve user config and refresh peer data: %+v", channels)
	}
}

func TestTGMessagesSortedByChannelAndTime(t *testing.T) {
	s := testStore(t)
	ch1, err := s.CreateTGChannel(t.Context(), domain.TGChannel{DisplayName: "A", PeerID: 1, AccessHash: 10, Enabled: true, MessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := s.CreateTGChannel(t.Context(), domain.TGChannel{DisplayName: "B", PeerID: 2, AccessHash: 20, Enabled: true, MessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, msg := range []domain.TGMessage{
		{ChannelID: ch1.ID, RemoteID: 1, PublishedAt: now.Add(-time.Hour), Text: "old"},
		{ChannelID: ch1.ID, RemoteID: 2, PublishedAt: now, Text: "new"},
		{ChannelID: ch2.ID, RemoteID: 3, PublishedAt: now.Add(time.Minute), Text: "other"},
	} {
		if _, err := s.SaveTGMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.TGMessages(t.Context(), ch1.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].RemoteID != 2 || rows[1].RemoteID != 1 || rows[0].ChannelName != "A" {
		t.Fatalf("rows = %+v", rows)
	}
	all, err := s.TGMessages(t.Context(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ChannelID != ch2.ID || all[1].RemoteID != 2 {
		t.Fatalf("all = %+v", all)
	}
	ch1.MessageLimit = 1
	if _, err := s.UpdateTGChannel(t.Context(), ch1); err != nil {
		t.Fatal(err)
	}
	rows, err = s.TGMessages(t.Context(), ch1.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RemoteID != 2 {
		t.Fatalf("trimmed rows = %+v", rows)
	}
}

func TestRevenueCardCRUDAndSort(t *testing.T) {
	s := testStore(t)
	defaults, err := s.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateRevenueCard(t.Context(), domain.RevenueCard{Name: "订单", SourceType: "newapi_orders", UpstreamID: "u1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if card.SortOrder != 2 {
		t.Fatalf("sort_order = %d, want 2", card.SortOrder)
	}
	card.Name = "订单收入"
	card.BaseURL = "https://orders.example.test"
	card.UserID = "new-user"
	card.AccessToken = "new-token"
	card.AdminAPIKey = "admin-secret"
	card.EpayPID = "1000"
	card.EpayKey = "pay-secret"
	card.Enabled = false
	if _, err := s.UpdateRevenueCard(t.Context(), card); err != nil {
		t.Fatal(err)
	}
	got, err := s.RevenueCard(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "订单收入" || got.Enabled || got.BaseURL != "https://orders.example.test" || got.UserID != "new-user" || got.AccessToken != "new-token" || got.AdminAPIKey != "admin-secret" || got.EpayPID != "1000" || got.EpayKey != "pay-secret" {
		t.Fatalf("card = %+v", got)
	}
	if err := s.UpdateRevenueCardOrder(t.Context(), []string{card.ID, defaults[0].ID}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ID != card.ID || rows[1].ID != defaults[0].ID {
		t.Fatalf("rows = %+v", rows)
	}
	if err := s.UpdateRevenueCardOrder(t.Context(), []string{"missing"}); err == nil {
		t.Fatal("missing revenue card id should fail")
	}
	if err := s.DeleteRevenueCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
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
	rows[0].Status = "completed"
	rows[0].RawStatus = "COMPLETED"
	if err := s.UpdateBalanceRechargeLog(t.Context(), rows[0]); err != nil {
		t.Fatal(err)
	}
	one, err := s.BalanceRechargeLog(t.Context(), "u1", log.ID)
	if err != nil {
		t.Fatal(err)
	}
	if one.Status != "completed" || one.RawStatus != "COMPLETED" {
		t.Fatalf("log = %+v", one)
	}
	if err := s.DeleteBalanceRechargeLog(t.Context(), "u1", log.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.BalanceRechargeLogs(t.Context(), "u1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	cols, err := s.columns(t.Context(), "balance_recharge_logs")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["payment_type"] || !cols["remote_order_id"] || !cols["raw_status"] {
		t.Fatalf("columns = %#v", cols)
	}
}

func TestCardStoresCustomFields(t *testing.T) {
	s := testStore(t)
	card, err := s.CreateCard(t.Context(), domain.ModelCard{
		Name: "自定义", BaseURL: "https://api.example.test", APIKey: "sk-test", DisplayGroup: "生产", SchedulerGroup: "gpt_low", Enabled: true, PublicEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "自定义" || got.BaseURL != "https://api.example.test" || got.APIKey != "sk-test" || got.DisplayGroup != "生产" || got.SchedulerGroup != "gpt_low" || !got.PublicEnabled {
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

func TestImportOldDataCreatesDefaultRevenueCard(t *testing.T) {
	dst := testStore(t)
	if err := dst.ImportData(t.Context(), ExportData{
		Version: "1",
		Tables: map[string][]RowMap{
			"settings": {{"id": "default", "check_interval_minutes": 5, "probe_model": domain.ProbeModel, "site_name": "AI 上游监控"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	cards, err := dst.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].SourceType != "epay_total" {
		t.Fatalf("cards = %+v", cards)
	}
}
