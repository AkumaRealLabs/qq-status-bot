package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCostBindingCRUDAndProjectionData(t *testing.T) {
	st := testStore(t)
	now := nowText()
	if _, err := st.exec(t.Context(), `INSERT INTO upstreams (id,name,type,base_url,enabled,balance_rate,runway_warning_hours,created_at,updated_at) VALUES ('u1','上游','newapi','https://example.test',1,0.5,24,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.exec(t.Context(), `INSERT INTO api_keys (id,upstream_id,remote_id,name,group_name,group_ratio,created_at,updated_at) VALUES ('k1','u1','r1','Key','g','0.2',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateCostBinding(t.Context(), domain.SchedulerCostBinding{Name: "成本", UpstreamID: "u1", KeyID: "k1", SchedulerChannelID: "9", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.UpstreamName != "上游" || created.KeyName != "Key" || created.KeyRatio != "0.2" || created.BalanceRate != 0.5 {
		t.Fatalf("binding=%+v", created)
	}
	created.ManualCostRatio = "0.1"
	created.UpstreamID, created.KeyID = "", ""
	updated, err := st.UpdateCostBinding(t.Context(), created)
	if err != nil || updated.ManualCostRatio != "0.1" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if err := st.DeleteCostBinding(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CostBinding(t.Context(), created.ID); err != sql.ErrNoRows {
		t.Fatalf("delete err=%v", err)
	}
}

func TestRetireMonitoringMigrationMovesOnlyPoolBindingsAndDropsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`CREATE TABLE migration_records (source TEXT PRIMARY KEY, migrated_at TEXT NOT NULL);
		CREATE TABLE model_cards (id TEXT PRIMARY KEY,name TEXT NOT NULL,base_url TEXT NOT NULL DEFAULT '',api_key TEXT NOT NULL DEFAULT '',upstream_id TEXT NOT NULL DEFAULT '',key_id TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',display_group TEXT NOT NULL DEFAULT '',pool_enabled INTEGER NOT NULL DEFAULT 1,manual_cost_ratio TEXT NOT NULL DEFAULT '',scheduler_group TEXT NOT NULL DEFAULT '',scheduler_channel_id TEXT NOT NULL DEFAULT '',scheduler_channel_name TEXT NOT NULL DEFAULT '',axonhub_channel_id TEXT NOT NULL DEFAULT '',axonhub_channel_name TEXT NOT NULL DEFAULT '',scheduler_auto_disabled INTEGER NOT NULL DEFAULT 0,scheduler_auto_disabled_at TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,public_enabled INTEGER NOT NULL DEFAULT 0,sort_order INTEGER NOT NULL DEFAULT 0,last_error TEXT NOT NULL DEFAULT '',failure_count INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
		CREATE TABLE alert_events (id TEXT PRIMARY KEY,upstream_id TEXT NOT NULL,type TEXT NOT NULL,recover INTEGER NOT NULL DEFAULT 0,sent INTEGER NOT NULL DEFAULT 0,message TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL);
		CREATE TABLE ops_events (id TEXT PRIMARY KEY,type TEXT NOT NULL,severity TEXT NOT NULL DEFAULT 'info',title TEXT NOT NULL DEFAULT '',message TEXT NOT NULL DEFAULT '',target_type TEXT NOT NULL DEFAULT '',target_id TEXT NOT NULL DEFAULT '',actions TEXT NOT NULL DEFAULT '[]',read INTEGER NOT NULL DEFAULT 0,acked INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
		INSERT INTO model_cards VALUES ('upstream','上游成本','','secret-upstream','u1','k1','gpt','公开组',1,'','old','9','GGAPI','a9','Axon',0,'',1,1,1,'',0,?,?);
		INSERT INTO model_cards VALUES ('manual','手动成本','https://api','secret-manual','','','gpt','公开组',1,'0.14','old','10','GGAPI2','','',0,'',0,1,2,'',0,?,?);
		INSERT INTO model_cards VALUES ('monitor','纯监控','https://monitor','secret-monitor','','','gpt','公开组',0,'','','','','','',0,'',1,1,3,'',0,?,?);
		INSERT INTO alert_events (id,upstream_id,type,created_at) VALUES ('probe-alert','','ping:upstream',?),('balance-alert','','balance',?);
		INSERT INTO ops_events (id,type,created_at,updated_at) VALUES
			('probe-event','probe_failed',?,?),('scheduler-event','scheduler_changed',?,?),
			('availability-event','availability_changed',?,?),('availability-error','availability_action_failed',?,?),
			('balance-event','balance_low',?,?)`,
		now, now, now, now, now, now,
		now, now, now, now, now, now, now, now, now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListCostBindings(t.Context())
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.ID == "monitor" {
			t.Fatal("纯监控卡片不应迁移")
		}
	}
	var ddl string
	if err := st.row(t.Context(), `SELECT sql FROM sqlite_master WHERE name='scheduler_cost_bindings'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(ddl), "api_key") || strings.Contains(strings.ToLower(ddl), "base_url") || strings.Contains(strings.ToLower(ddl), "model") {
		t.Fatalf("new table contains retired secret/probe columns: %s", ddl)
	}
	for _, table := range []string{"model_cards", "probe_runs", "channel_availability", "scheduler_traffic_control"} {
		var count int
		_ = st.row(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
		if count != 0 {
			t.Fatalf("retired table still exists: %s", table)
		}
	}
	var eventTypes string
	if err := st.row(t.Context(), `SELECT GROUP_CONCAT(type, ',') FROM ops_events`).Scan(&eventTypes); err != nil || eventTypes != "balance_low" {
		t.Fatalf("ops events=%q err=%v", eventTypes, err)
	}
	if err := st.row(t.Context(), `SELECT GROUP_CONCAT(type, ',') FROM alert_events`).Scan(&eventTypes); err != nil || eventTypes != "balance" {
		t.Fatalf("alert events=%q err=%v", eventTypes, err)
	}
	for table, retiredColumns := range map[string][]string{
		"settings":  {"probe_model", "scheduler_traffic_mode", "scheduler_traffic_profile", "scheduler_log_poll_seconds", "axonhub_api_key"},
		"upstreams": {"balance_guard_mode", "balance_close_threshold", "balance_recover_threshold"},
	} {
		columns, err := st.columns(t.Context(), table)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range retiredColumns {
			if columns[column] {
				t.Fatalf("retired column still exists: %s.%s", table, column)
			}
		}
	}
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// 早期 model_cards 缺少成本绑定列时，prepareLegacyCostBindingSource 必须先补齐，
// 否则 retireMonitoringSchema 的 SELECT 会在老库上直接失败。
func TestRetireMonitoringMigrationBackfillsAncientModelCards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ancient.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// 只有最早期就存在的列：没有 pool_enabled / key_id / manual_cost_ratio /
	// scheduler_channel_* / axonhub_channel_*。
	_, err = db.Exec(`CREATE TABLE migration_records (source TEXT PRIMARY KEY, migrated_at TEXT NOT NULL);
		CREATE TABLE model_cards (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, model TEXT NOT NULL DEFAULT '',
			upstream_id TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
		INSERT INTO model_cards (id,name,model,upstream_id,enabled,created_at,updated_at)
			VALUES ('ancient','远古卡片','gpt','u1',1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatalf("老库迁移失败: %v", err)
	}
	rows, err := st.ListCostBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "ancient" {
		t.Fatalf("远古卡片应迁移成一条成本绑定，实际 %+v", rows)
	}
	var count int
	_ = st.row(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='model_cards'`).Scan(&count)
	if count != 0 {
		t.Fatal("迁移后 model_cards 应被删除")
	}
}

func TestRetireMonitoringMigrationDuplicateBindingRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE migration_records (source TEXT PRIMARY KEY, migrated_at TEXT NOT NULL);
		CREATE TABLE model_cards (id TEXT PRIMARY KEY,name TEXT NOT NULL,pool_enabled INTEGER NOT NULL DEFAULT 1,scheduler_channel_id TEXT NOT NULL DEFAULT '',axonhub_channel_id TEXT NOT NULL DEFAULT '');
		INSERT INTO model_cards VALUES ('a','A',1,'9',''); INSERT INTO model_cards VALUES ('b','B',1,'9','')`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	err = st.Migrate(t.Context())
	if err == nil || !strings.Contains(err.Error(), "重复绑定") {
		t.Fatalf("err=%v", err)
	}
	var count int
	if err := st.row(t.Context(), `SELECT COUNT(*) FROM model_cards`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("legacy rows changed count=%d err=%v", count, err)
	}
}

func TestCostFieldOwnershipRoundTrip(t *testing.T) {
	st := testStore(t)
	want := domain.CostFieldOwnership{Provider: "axonhub", ChannelID: "9", RemoteGroups: []string{"manual", "payg_low"}, RemoteWeight: 90, Managed: true}
	if err := st.SaveCostFieldOwnership(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, found, err := st.CostFieldOwnership(t.Context(), "axonhub", "9")
	if err != nil || !found || !got.Managed || got.RemoteWeight != 90 || len(got.RemoteGroups) != 2 {
		t.Fatalf("got=%+v found=%v err=%v", got, found, err)
	}
}

func TestPocketBaseMigrationImportsOnlyCostBindingsAfterMonitoringRetired(t *testing.T) {
	st := testStore(t)
	pbPath := filepath.Join(t.TempDir(), "pocketbase.sqlite")
	pb, err := sql.Open("sqlite", pbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := nowText()
	_, err = pb.Exec(`CREATE TABLE model_cards (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, upstream TEXT NOT NULL DEFAULT '', key TEXT NOT NULL DEFAULT '',
		pool_enabled INTEGER NOT NULL DEFAULT 1, manual_cost_ratio TEXT NOT NULL DEFAULT '',
		scheduler_channel_id TEXT NOT NULL DEFAULT '', scheduler_channel_name TEXT NOT NULL DEFAULT '',
		axonhub_channel_id TEXT NOT NULL DEFAULT '', axonhub_channel_name TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	INSERT INTO model_cards VALUES ('cost','成本卡','u1','k1',1,'','9','GGAPI','a9','AxonHub',1,?,?);
	INSERT INTO model_cards VALUES ('monitor','纯监控卡','','',0,'','','','','',1,?,?)`, now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := st.MigratePocketBase(t.Context(), pbPath); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListCostBindings(t.Context())
	if err != nil || len(rows) != 1 || rows[0].ID != "cost" || rows[0].SchedulerChannelID != "9" || rows[0].AxonHubChannelID != "a9" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestMigratePostgresSQLUsesReboundPlaceholders(t *testing.T) {
	st := &Store{Driver: "postgres"}
	if got := st.rebind(`SELECT ? FROM x WHERE a=?`); got != `SELECT $1 FROM x WHERE a=$2` {
		t.Fatalf("rebind=%q", got)
	}
}

var _ = context.Background
