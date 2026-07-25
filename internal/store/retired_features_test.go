package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

var retiredFeatureTables = []string{
	"scheduler_channel_cost_snapshots",
	"scheduler_group_sale_snapshots",
	"tg_session",
	"tg_channels",
	"tg_messages",
	"cliproxy_quota_snapshots",
}

func TestRetireProfitMessagesAndPoolsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-features.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE migration_records (source TEXT PRIMARY KEY, migrated_at TEXT NOT NULL);
		CREATE TABLE settings (
			id TEXT PRIMARY KEY, check_interval_minutes INTEGER NOT NULL,
			telegram_bot_token TEXT NOT NULL DEFAULT '', telegram_chat_id TEXT NOT NULL DEFAULT '',
			site_name TEXT NOT NULL DEFAULT '', scheduler_tiers TEXT NOT NULL DEFAULT '', notification_rules TEXT NOT NULL DEFAULT '',
			cliproxy_name TEXT NOT NULL DEFAULT '', cliproxy_base_url TEXT NOT NULL DEFAULT '',
			cliproxy_management_key TEXT NOT NULL DEFAULT '', cliproxy_enabled INTEGER NOT NULL DEFAULT 1
		);
		INSERT INTO settings VALUES (
			'default', 5, 'bot-secret', 'chat-id', '保留站点',
			'[{"tag":"low","group":"gpt_low","price_min":0,"price_max":0.1,"sale_price":0.2}]',
			'{"enabled":true,"event_types":{"balance_low":true,"cliproxy_error":true},"failure_threshold":2,"recovery":true}',
			'CPA', 'http://cliproxy', 'management-secret', 1
		);
		CREATE TABLE ops_events (id TEXT PRIMARY KEY,type TEXT NOT NULL,severity TEXT NOT NULL DEFAULT 'info',title TEXT NOT NULL DEFAULT '',message TEXT NOT NULL DEFAULT '',target_type TEXT NOT NULL DEFAULT '',target_id TEXT NOT NULL DEFAULT '',actions TEXT NOT NULL DEFAULT '[]',read INTEGER NOT NULL DEFAULT 0,acked INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
		INSERT INTO ops_events (id,type,created_at,updated_at) VALUES ('clip','cliproxy_error','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('keep','balance_low','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
		CREATE TABLE audit_logs (id TEXT PRIMARY KEY,actor TEXT NOT NULL DEFAULT '',action TEXT NOT NULL,target_type TEXT NOT NULL DEFAULT '',target_id TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL DEFAULT '',fields TEXT NOT NULL DEFAULT '[]',created_at TEXT NOT NULL);
		INSERT INTO audit_logs (id,action,created_at) VALUES ('audit-keep','PATCH /api/settings','2026-01-01T00:00:00Z');
		CREATE TABLE scheduler_channel_cost_snapshots (id TEXT PRIMARY KEY);
		CREATE TABLE scheduler_group_sale_snapshots (id TEXT PRIMARY KEY);
		CREATE TABLE tg_session (id TEXT PRIMARY KEY);
		CREATE TABLE tg_channels (id TEXT PRIMARY KEY);
		CREATE TABLE tg_messages (id TEXT PRIMARY KEY);
		CREATE TABLE cliproxy_quota_snapshots (id TEXT PRIMARY KEY);
		INSERT INTO scheduler_channel_cost_snapshots VALUES ('cost');
		INSERT INTO scheduler_group_sale_snapshots VALUES ('sale');
		INSERT INTO tg_session VALUES ('session');
		INSERT INTO tg_channels VALUES ('channel');
		INSERT INTO tg_messages VALUES ('message');
		INSERT INTO cliproxy_quota_snapshots VALUES ('quota');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertRetiredFeatureSchemaAbsent(t, st)

	var tiers, rules, siteName, botToken string
	if err := st.row(t.Context(), `SELECT scheduler_tiers, notification_rules, site_name, telegram_bot_token FROM settings WHERE id='default'`).Scan(&tiers, &rules, &siteName, &botToken); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tiers, "sale_price") || strings.Contains(rules, "cliproxy_error") {
		t.Fatalf("退休 JSON 未清理 tiers=%s rules=%s", tiers, rules)
	}
	if siteName != "保留站点" || botToken != "bot-secret" {
		t.Fatalf("无关设置被修改 site=%q bot=%q", siteName, botToken)
	}
	for table, want := range map[string]int{"ops_events": 1, "audit_logs": 1} {
		var count int
		if err := st.row(t.Context(), `SELECT COUNT(*) FROM `+quoteIdent(table)).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	assertRetiredFeatureSchemaAbsent(t, st)
}

func TestFreshDatabaseDoesNotCreateRetiredFeatureSchema(t *testing.T) {
	st := testStore(t)
	assertRetiredFeatureSchemaAbsent(t, st)
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatalf("重复启动迁移失败: %v", err)
	}
}

func TestLegacyExportImportIgnoresRetiredFeatureData(t *testing.T) {
	st := testStore(t)
	in := ExportData{Version: "1", Tables: map[string][]RowMap{
		"settings": {{
			"id": "default", "check_interval_minutes": 5, "telegram_bot_token": "bot", "telegram_chat_id": "chat",
			"scheduler_tiers":    `[{"tag":"low","group":"gpt_low","price_min":0,"price_max":0.1,"sale_price":0.2}]`,
			"notification_rules": `{"enabled":true,"event_types":{"balance_low":true,"cliproxy_error":true},"failure_threshold":2,"recovery":true}`,
			"cliproxy_name":      "CPA", "cliproxy_base_url": "http://cliproxy", "cliproxy_management_key": "secret", "cliproxy_enabled": 1,
		}},
		"audit_logs":                       {{"id": "audit", "action": "legacy", "created_at": "2026-01-01T00:00:00Z"}},
		"scheduler_channel_cost_snapshots": {{"id": "cost"}},
		"scheduler_group_sale_snapshots":   {{"id": "sale", "sale_price": 1}},
		"tg_session":                       {{"id": "session"}},
		"tg_channels":                      {{"id": "channel"}},
		"tg_messages":                      {{"id": "message"}},
		"cliproxy_quota_snapshots":         {{"id": "quota"}},
	}}
	if err := st.ImportData(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	assertRetiredFeatureSchemaAbsent(t, st)
	var tiers, rules string
	if err := st.row(t.Context(), `SELECT scheduler_tiers, notification_rules FROM settings WHERE id='default'`).Scan(&tiers, &rules); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tiers, "sale_price") || strings.Contains(rules, "cliproxy_error") {
		t.Fatalf("旧备份恢复了退休字段 tiers=%s rules=%s", tiers, rules)
	}
	out, err := st.ExportData(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range retiredFeatureTables {
		if _, ok := out.Tables[table]; ok {
			t.Fatalf("新备份仍包含退休表 %s", table)
		}
	}
	if len(out.Tables["audit_logs"]) != 1 {
		t.Fatalf("审计记录未保留: %+v", out.Tables["audit_logs"])
	}
}

func assertRetiredFeatureSchemaAbsent(t *testing.T, st *Store) {
	t.Helper()
	for _, table := range retiredFeatureTables {
		var count int
		if err := st.row(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("退休表仍存在 %s count=%d err=%v", table, count, err)
		}
	}
	columns, err := st.columns(t.Context(), "settings")
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"cliproxy_name", "cliproxy_base_url", "cliproxy_management_key", "cliproxy_enabled"} {
		if columns[column] {
			t.Fatalf("退休列仍存在 settings.%s", column)
		}
	}
	done, err := st.MigrationDone(t.Context(), retireProfitMessagesAndPoolsMigration)
	if err != nil || !done {
		t.Fatalf("迁移标记 done=%v err=%v", done, err)
	}
}
