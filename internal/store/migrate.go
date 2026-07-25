package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.preflightCostBindingMigration(ctx); err != nil {
		return err
	}
	monitoringRetired, err := s.migrationDoneIfTableExists(ctx, retireMonitoringMigration)
	if err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS migration_records (source TEXT PRIMARY KEY, migrated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			id TEXT PRIMARY KEY, check_interval_minutes INTEGER NOT NULL, telegram_bot_token TEXT NOT NULL DEFAULT '',
			telegram_chat_id TEXT NOT NULL DEFAULT '',
			onebot_enabled INTEGER NOT NULL DEFAULT 0, onebot_base_url TEXT NOT NULL DEFAULT '',
			onebot_http_token TEXT NOT NULL DEFAULT '', onebot_webhook_token TEXT NOT NULL DEFAULT '', onebot_group_ids TEXT NOT NULL DEFAULT '[]',
			site_name TEXT NOT NULL DEFAULT 'AI 上游监控', site_icon TEXT NOT NULL DEFAULT '',
			epay_base_url TEXT NOT NULL DEFAULT '', epay_pid TEXT NOT NULL DEFAULT '', epay_key TEXT NOT NULL DEFAULT '',
			scheduler_provider TEXT NOT NULL DEFAULT 'ggapi',
			scheduler_base_url TEXT NOT NULL DEFAULT '', scheduler_user_id TEXT NOT NULL DEFAULT '', scheduler_access_token TEXT NOT NULL DEFAULT '',
			scheduler_unassigned_group TEXT NOT NULL DEFAULT '',
			scheduler_tiers TEXT NOT NULL DEFAULT '',
			axonhub_base_url TEXT NOT NULL DEFAULT '', axonhub_admin_email TEXT NOT NULL DEFAULT '', axonhub_admin_password TEXT NOT NULL DEFAULT '', axonhub_control_mode TEXT NOT NULL DEFAULT 'off',
			notification_rules TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS upstreams (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, type TEXT NOT NULL, base_url TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
			user_id TEXT NOT NULL DEFAULT '', access_token TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '', sub2api_access_token TEXT NOT NULL DEFAULT '', sub2api_refresh_token TEXT NOT NULL DEFAULT '',
			balance_rate REAL NOT NULL DEFAULT 1, low_balance_threshold REAL NOT NULL DEFAULT 0,
			runway_warning_hours REAL NOT NULL DEFAULT 24,
			last_error TEXT NOT NULL DEFAULT '', failure_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, remote_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
			key TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
			group_name TEXT NOT NULL DEFAULT '', group_ratio TEXT NOT NULL DEFAULT '', quota REAL NOT NULL DEFAULT 0,
			used_quota REAL NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			UNIQUE(upstream_id, remote_id)
		)`,
		`CREATE TABLE IF NOT EXISTS model_cards (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, base_url TEXT NOT NULL DEFAULT '', api_key TEXT NOT NULL DEFAULT '',
			upstream_id TEXT NOT NULL DEFAULT '', key_id TEXT NOT NULL DEFAULT '', model TEXT NOT NULL,
			display_group TEXT NOT NULL DEFAULT '', pool_enabled INTEGER NOT NULL DEFAULT 1, manual_cost_ratio TEXT NOT NULL DEFAULT '',
			scheduler_group TEXT NOT NULL DEFAULT '', scheduler_channel_id TEXT NOT NULL DEFAULT '', scheduler_channel_name TEXT NOT NULL DEFAULT '',
			axonhub_channel_id TEXT NOT NULL DEFAULT '', axonhub_channel_name TEXT NOT NULL DEFAULT '',
			scheduler_auto_disabled INTEGER NOT NULL DEFAULT 0, scheduler_auto_disabled_at TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, public_enabled INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
			failure_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_cost_bindings (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, upstream_id TEXT NOT NULL DEFAULT '', key_id TEXT NOT NULL DEFAULT '',
			manual_cost_ratio TEXT NOT NULL DEFAULT '', scheduler_channel_id TEXT NOT NULL DEFAULT '', scheduler_channel_name TEXT NOT NULL DEFAULT '',
			axonhub_channel_id TEXT NOT NULL DEFAULT '', axonhub_channel_name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_cost_field_ownership (
			provider TEXT NOT NULL, channel_id TEXT NOT NULL, channel_name TEXT NOT NULL DEFAULT '', remote_groups TEXT NOT NULL DEFAULT '',
			remote_priority INTEGER NOT NULL DEFAULT 0, remote_weight INTEGER NOT NULL DEFAULT 0,
			managed INTEGER NOT NULL DEFAULT 0, external_takeover INTEGER NOT NULL DEFAULT 0,
			last_reason TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, PRIMARY KEY(provider, channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS balance_snapshots (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, checked_at TEXT NOT NULL, balance REAL NOT NULL DEFAULT 0,
			used REAL NOT NULL DEFAULT 0, remain REAL NOT NULL DEFAULT 0, requests INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '', latency_ms INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS probe_runs (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL,
			model TEXT NOT NULL, input TEXT NOT NULL DEFAULT 'ping', status TEXT NOT NULL DEFAULT '',
			output TEXT NOT NULL DEFAULT '', http_status INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
			purpose TEXT NOT NULL DEFAULT 'regular'
		)`,
		`CREATE TABLE IF NOT EXISTS alert_events (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, type TEXT NOT NULL, recover INTEGER NOT NULL DEFAULT 0,
			sent INTEGER NOT NULL DEFAULT 0, message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ops_events (
			id TEXT PRIMARY KEY, type TEXT NOT NULL, severity TEXT NOT NULL DEFAULT 'info', title TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '', target_type TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL DEFAULT '',
			actions TEXT NOT NULL DEFAULT '[]', read INTEGER NOT NULL DEFAULT 0, acked INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY, actor TEXT NOT NULL DEFAULT '', action TEXT NOT NULL, target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', fields TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS revenue_snapshots (
			id TEXT PRIMARY KEY, source_id TEXT NOT NULL DEFAULT '', source_name TEXT NOT NULL DEFAULT '',
			source_type TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL, revenue REAL NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS balance_recharge_logs (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, method TEXT NOT NULL, amount REAL NOT NULL DEFAULT 0,
			payment_type TEXT NOT NULL DEFAULT '', remote_order_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '', raw_status TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_logs (
			id TEXT PRIMARY KEY, card_id TEXT NOT NULL DEFAULT '', card_name TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '', channel_name TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT 'ggapi', created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channel_availability (
			channel_id TEXT PRIMARY KEY, channel_name TEXT NOT NULL DEFAULT '', card_id TEXT NOT NULL DEFAULT '', card_name TEXT NOT NULL DEFAULT '',
			upstream_id TEXT NOT NULL DEFAULT '', upstream_name TEXT NOT NULL DEFAULT '', managed INTEGER NOT NULL DEFAULT 1,
			blockers TEXT NOT NULL DEFAULT '[]', desired_status INTEGER NOT NULL DEFAULT 1, actual_status INTEGER NOT NULL DEFAULT 0,
			disabled_at TEXT NOT NULL DEFAULT '', recovery_success_count INTEGER NOT NULL DEFAULT 0,
			override_kind TEXT NOT NULL DEFAULT '', override_until TEXT NOT NULL DEFAULT '',
			pending_action TEXT NOT NULL DEFAULT '', pending_status INTEGER NOT NULL DEFAULT 0,
			retry_at TEXT NOT NULL DEFAULT '', retry_count INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_traffic_events (
			id TEXT PRIMARY KEY, dedupe_key TEXT NOT NULL UNIQUE, source TEXT NOT NULL DEFAULT '', occurred_at TEXT NOT NULL,
			channel_id TEXT NOT NULL DEFAULT '', channel_name TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', group_name TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '', upstream_request_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', http_status INTEGER NOT NULL DEFAULT 0,
			error_type TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '', duration_ms INTEGER NOT NULL DEFAULT 0, ttft_ms INTEGER NOT NULL DEFAULT 0,
			stream_ended INTEGER NOT NULL DEFAULT 0, tokens INTEGER NOT NULL DEFAULT 0, retry_count INTEGER NOT NULL DEFAULT 0,
			retry_succeeded INTEGER NOT NULL DEFAULT 0, affinity_rule TEXT NOT NULL DEFAULT '', affinity_group TEXT NOT NULL DEFAULT '',
			affinity_hit INTEGER NOT NULL DEFAULT 0, session_scoped INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_traffic_10s (
			id TEXT PRIMARY KEY, channel_id TEXT NOT NULL, channel_name TEXT NOT NULL DEFAULT '', model TEXT NOT NULL, group_name TEXT NOT NULL DEFAULT '',
			window_start TEXT NOT NULL, window_end TEXT NOT NULL, requests INTEGER NOT NULL DEFAULT 0, successes INTEGER NOT NULL DEFAULT 0,
			soft_failures INTEGER NOT NULL DEFAULT 0, hard_failures INTEGER NOT NULL DEFAULT 0, user_errors INTEGER NOT NULL DEFAULT 0,
			p95_ttft_ms INTEGER NOT NULL DEFAULT 0, avg_ttft_ms INTEGER NOT NULL DEFAULT 0, failure_rate REAL NOT NULL DEFAULT 0,
			last_success_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, UNIQUE(channel_id, model, window_start)
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_traffic_1m (
			id TEXT PRIMARY KEY, channel_id TEXT NOT NULL, channel_name TEXT NOT NULL DEFAULT '', model TEXT NOT NULL, group_name TEXT NOT NULL DEFAULT '',
			window_start TEXT NOT NULL, window_end TEXT NOT NULL, requests INTEGER NOT NULL DEFAULT 0, successes INTEGER NOT NULL DEFAULT 0,
			soft_failures INTEGER NOT NULL DEFAULT 0, hard_failures INTEGER NOT NULL DEFAULT 0, user_errors INTEGER NOT NULL DEFAULT 0,
			p95_ttft_ms INTEGER NOT NULL DEFAULT 0, avg_ttft_ms INTEGER NOT NULL DEFAULT 0, failure_rate REAL NOT NULL DEFAULT 0,
			last_success_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, UNIQUE(channel_id, model, window_start)
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_traffic_cursors (
			source TEXT PRIMARY KEY, cursor_at TEXT NOT NULL DEFAULT '', scan_start_at TEXT NOT NULL DEFAULT '', scan_end_at TEXT NOT NULL DEFAULT '',
			next_page INTEGER NOT NULL DEFAULT 0, last_poll_at TEXT NOT NULL DEFAULT '', last_event_at TEXT NOT NULL DEFAULT '',
			backlog_pages INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_traffic_control (
			channel_id TEXT PRIMARY KEY, base_priority INTEGER NOT NULL DEFAULT 0, base_weight INTEGER NOT NULL DEFAULT 0,
			desired_priority INTEGER NOT NULL DEFAULT 0, desired_weight INTEGER NOT NULL DEFAULT 0,
			actual_priority INTEGER NOT NULL DEFAULT 0, actual_weight INTEGER NOT NULL DEFAULT 0, desired_status INTEGER NOT NULL DEFAULT 1,
			actual_status INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'healthy', reason TEXT NOT NULL DEFAULT '',
			failure_windows INTEGER NOT NULL DEFAULT 0, recovery_stage INTEGER NOT NULL DEFAULT 0, cooldown_until TEXT NOT NULL DEFAULT '',
			last_probe_at TEXT NOT NULL DEFAULT '', recovery_successes INTEGER NOT NULL DEFAULT 0, stage_changed_at TEXT NOT NULL DEFAULT '',
			retry_at TEXT NOT NULL DEFAULT '', retry_count INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_channel_lifecycle (
			channel_id TEXT PRIMARY KEY, channel_name TEXT NOT NULL DEFAULT '', remote_status INTEGER NOT NULL DEFAULT 0,
			remote_priority INTEGER NOT NULL DEFAULT 0, remote_weight INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT 'aum',
			external_takeover INTEGER NOT NULL DEFAULT 0, aum_disabled INTEGER NOT NULL DEFAULT 0, last_aum_status INTEGER NOT NULL DEFAULT 0,
			last_aum_write_at TEXT NOT NULL DEFAULT '', last_source TEXT NOT NULL DEFAULT '', last_reason TEXT NOT NULL DEFAULT '',
			traffic_since TEXT NOT NULL DEFAULT '', affinity_cleanup_pending INTEGER NOT NULL DEFAULT 0,
			affinity_cleanup_retry_at TEXT NOT NULL DEFAULT '', affinity_cleanup_retries INTEGER NOT NULL DEFAULT 0,
			affinity_cleanup_error TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_axonhub_channel_lifecycle (
			channel_id TEXT PRIMARY KEY, channel_name TEXT NOT NULL DEFAULT '', remote_status TEXT NOT NULL DEFAULT '',
			remote_tags TEXT NOT NULL DEFAULT '[]', remote_managed_tag TEXT NOT NULL DEFAULT '', remote_weight INTEGER NOT NULL DEFAULT 0,
			desired_tag TEXT NOT NULL DEFAULT '', desired_weight INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT 'observed',
			external_takeover INTEGER NOT NULL DEFAULT 0, aum_disabled INTEGER NOT NULL DEFAULT 0, aum_disabled_at TEXT NOT NULL DEFAULT '',
			health_state TEXT NOT NULL DEFAULT 'healthy', auto_disabled_at TEXT NOT NULL DEFAULT '', auto_disable_status_code INTEGER NOT NULL DEFAULT 0,
			auto_disable_model TEXT NOT NULL DEFAULT '', recovery_not_before TEXT NOT NULL DEFAULT '', recovery_successes INTEGER NOT NULL DEFAULT 0,
			last_recovery_probe_at TEXT NOT NULL DEFAULT '', recovery_error TEXT NOT NULL DEFAULT '',
			last_aum_status TEXT NOT NULL DEFAULT '', last_aum_tag TEXT NOT NULL DEFAULT '', last_aum_weight INTEGER NOT NULL DEFAULT 0,
			last_aum_write_at TEXT NOT NULL DEFAULT '', last_source TEXT NOT NULL DEFAULT '', last_reason TEXT NOT NULL DEFAULT '',
			pending_action TEXT NOT NULL DEFAULT '', pending_status TEXT NOT NULL DEFAULT '', pending_tag TEXT NOT NULL DEFAULT '',
			pending_weight INTEGER NOT NULL DEFAULT 0, retry_at TEXT NOT NULL DEFAULT '', retry_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS revenue_cards (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, source_type TEXT NOT NULL, upstream_id TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '', access_token TEXT NOT NULL DEFAULT '',
			admin_api_key TEXT NOT NULL DEFAULT '', epay_pid TEXT NOT NULL DEFAULT '', epay_key TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_upstream ON api_keys(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cards_upstream ON model_cards(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cost_bindings_upstream ON scheduler_cost_bindings(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_probe_card_time ON probe_runs(card_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_balance_upstream_time ON balance_snapshots(upstream_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state ON alert_events(upstream_id, type, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_events_time ON ops_events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_time ON audit_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_revenue_snapshots_time ON revenue_snapshots(checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_recharge_upstream_time ON balance_recharge_logs(upstream_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_recharge_pending ON balance_recharge_logs(method, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_logs_time ON scheduler_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_availability_upstream ON channel_availability(upstream_id, managed)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_availability_retry ON channel_availability(pending_action, retry_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_traffic_events_time ON scheduler_traffic_events(occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_traffic_events_channel_model ON scheduler_traffic_events(channel_id, model, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_traffic_10s_time ON scheduler_traffic_10s(window_start)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_traffic_1m_time ON scheduler_traffic_1m(window_start)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_channel_lifecycle_cleanup ON scheduler_channel_lifecycle(affinity_cleanup_pending, affinity_cleanup_retry_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_axonhub_lifecycle_retry ON scheduler_axonhub_channel_lifecycle(pending_action, retry_at)`,
		`CREATE INDEX IF NOT EXISTS idx_revenue_cards_upstream ON revenue_cards(upstream_id)`,
	}
	if monitoringRetired {
		stmts = withoutRetiredMonitoringStatements(stmts)
	}
	for _, stmt := range stmts {
		if _, err := s.DB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing(ctx, "settings", "site_name", "TEXT NOT NULL DEFAULT 'AI 上游监控'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "site_icon", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []struct{ name, def string }{
		{"onebot_enabled", "INTEGER NOT NULL DEFAULT 0"},
		{"onebot_base_url", "TEXT NOT NULL DEFAULT ''"},
		{"onebot_http_token", "TEXT NOT NULL DEFAULT ''"},
		{"onebot_webhook_token", "TEXT NOT NULL DEFAULT ''"},
		{"onebot_group_ids", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if err := s.addColumnIfMissing(ctx, "settings", col.name, col.def); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing(ctx, "settings", "epay_base_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "epay_pid", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "epay_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []string{"scheduler_base_url", "scheduler_user_id", "scheduler_access_token", "axonhub_base_url", "axonhub_admin_email", "axonhub_admin_password"} {
		if err := s.addColumnIfMissing(ctx, "settings", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	for _, col := range []struct{ name, def string }{
		{"scheduler_provider", "TEXT NOT NULL DEFAULT 'ggapi'"},
		{"axonhub_control_mode", "TEXT NOT NULL DEFAULT 'off'"},
	} {
		if err := s.addColumnIfMissing(ctx, "settings", col.name, col.def); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing(ctx, "settings", "scheduler_tiers", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "scheduler_unassigned_group", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if !monitoringRetired {
		for _, col := range []struct{ name, def string }{
			{"scan_start_at", "TEXT NOT NULL DEFAULT ''"}, {"scan_end_at", "TEXT NOT NULL DEFAULT ''"}, {"next_page", "INTEGER NOT NULL DEFAULT 0"},
		} {
			if err := s.addColumnIfMissing(ctx, "scheduler_traffic_cursors", col.name, col.def); err != nil {
				return err
			}
		}
		for _, col := range []struct{ name, def string }{
			{"affinity_rule", "TEXT NOT NULL DEFAULT ''"}, {"affinity_group", "TEXT NOT NULL DEFAULT ''"},
			{"affinity_hit", "INTEGER NOT NULL DEFAULT 0"}, {"session_scoped", "INTEGER NOT NULL DEFAULT 0"},
		} {
			if err := s.addColumnIfMissing(ctx, "scheduler_traffic_events", col.name, col.def); err != nil {
				return err
			}
		}
		for _, col := range []struct{ name, def string }{
			{"desired_priority", "INTEGER NOT NULL DEFAULT 0"}, {"desired_weight", "INTEGER NOT NULL DEFAULT 0"},
			{"last_probe_at", "TEXT NOT NULL DEFAULT ''"}, {"recovery_successes", "INTEGER NOT NULL DEFAULT 0"}, {"stage_changed_at", "TEXT NOT NULL DEFAULT ''"},
		} {
			if err := s.addColumnIfMissing(ctx, "scheduler_traffic_control", col.name, col.def); err != nil {
				return err
			}
		}
		for _, col := range []struct{ name, def string }{
			{"health_state", "TEXT NOT NULL DEFAULT 'healthy'"},
			{"auto_disabled_at", "TEXT NOT NULL DEFAULT ''"},
			{"auto_disable_status_code", "INTEGER NOT NULL DEFAULT 0"},
			{"auto_disable_model", "TEXT NOT NULL DEFAULT ''"},
			{"recovery_not_before", "TEXT NOT NULL DEFAULT ''"},
			{"recovery_successes", "INTEGER NOT NULL DEFAULT 0"},
			{"last_recovery_probe_at", "TEXT NOT NULL DEFAULT ''"},
			{"recovery_error", "TEXT NOT NULL DEFAULT ''"},
		} {
			if err := s.addColumnIfMissing(ctx, "scheduler_axonhub_channel_lifecycle", col.name, col.def); err != nil {
				return err
			}
		}
	}
	for _, col := range []struct{ name, def string }{
		{"notification_rules", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing(ctx, "settings", col.name, col.def); err != nil {
			return err
		}
	}
	if !monitoringRetired {
		if err := s.prepareLegacyMonitoringSchema(ctx); err != nil {
			return err
		}
	}
	for _, col := range []struct{ name, def string }{
		{"runway_warning_hours", "REAL NOT NULL DEFAULT 24"},
	} {
		if err := s.addColumnIfMissing(ctx, "upstreams", col.name, col.def); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing(ctx, "scheduler_logs", "reason", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "scheduler_logs", "provider", "TEXT NOT NULL DEFAULT 'ggapi'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "balance_recharge_logs", "raw_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []struct{ name, def string }{
		{"base_url", "TEXT NOT NULL DEFAULT ''"},
		{"user_id", "TEXT NOT NULL DEFAULT ''"},
		{"access_token", "TEXT NOT NULL DEFAULT ''"},
		{"admin_api_key", "TEXT NOT NULL DEFAULT ''"},
		{"epay_pid", "TEXT NOT NULL DEFAULT ''"},
		{"epay_key", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing(ctx, "revenue_cards", col.name, col.def); err != nil {
			return err
		}
	}
	if _, err := s.exec(ctx, `INSERT INTO settings (id, check_interval_minutes) VALUES ('default', 5) ON CONFLICT(id) DO NOTHING`); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `UPDATE settings SET scheduler_provider='ggapi' WHERE TRIM(scheduler_provider)=''`); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `UPDATE settings SET axonhub_control_mode='off' WHERE LOWER(TRIM(axonhub_control_mode))<>'active'`); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `UPDATE scheduler_logs SET provider='ggapi' WHERE TRIM(provider)=''`); err != nil {
		return err
	}
	if !monitoringRetired {
		// 兼容旧自动关闭：只接管状态，不自动远端恢复。
		if _, err := s.exec(ctx, `INSERT INTO channel_availability
		(channel_id, channel_name, card_id, card_name, upstream_id, managed, blockers, desired_status, actual_status, disabled_at, recovery_success_count, version, updated_at)
		SELECT scheduler_channel_id, scheduler_channel_name, id, name, upstream_id, 1, '[{"kind":"probe_failed","message":"从旧自动关闭状态迁移"}]', 2, 2,
			CASE WHEN scheduler_auto_disabled_at<>'' THEN scheduler_auto_disabled_at ELSE ? END, 0, 1, ?
		FROM model_cards
		WHERE pool_enabled=1 AND scheduler_auto_disabled=1 AND TRIM(scheduler_channel_id)<>''
		ON CONFLICT(channel_id) DO NOTHING`, nowText(), nowText()); err != nil {
			return err
		}
		// 旧运行态里明确由 AUM 关闭的渠道可以继续恢复；其余远端状态在首轮读取时只建立基线。
		if _, err := s.exec(ctx, `INSERT INTO scheduler_channel_lifecycle
		(channel_id, channel_name, remote_status, owner, aum_disabled, last_aum_status, last_source, last_reason, traffic_since, updated_at)
		SELECT channel_id, channel_name, actual_status, 'aum', 1, 2, 'migration', '从既有可用性运行态迁移', ?, ?
		FROM channel_availability WHERE actual_status=2 AND desired_status=2 AND disabled_at<>''
		ON CONFLICT(channel_id) DO NOTHING`, nowText(), nowText()); err != nil {
			return err
		}
	}
	if err := s.retireMonitoringSchema(ctx); err != nil {
		return err
	}
	if err := s.retireProfitMessagesAndPools(ctx); err != nil {
		return err
	}
	return s.ensureDefaultRevenueCard(ctx)
}

const retireMonitoringMigration = "retire-monitoring-v1"
const retireProfitMessagesAndPoolsMigration = "retire-profit-messages-pools-v1"

func (s *Store) migrationDoneIfTableExists(ctx context.Context, source string) (bool, error) {
	var one int
	err := s.row(ctx, `SELECT 1 FROM migration_records WHERE source=?`, source).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "no such table") || strings.Contains(strings.ToLower(err.Error()), "does not exist")) {
		return false, nil
	}
	return err == nil, err
}

func withoutRetiredMonitoringStatements(stmts []string) []string {
	retired := []string{
		"model_cards", "probe_runs", "channel_availability", "scheduler_traffic_events", "scheduler_traffic_10s",
		"scheduler_traffic_1m", "scheduler_traffic_cursors", "scheduler_traffic_control",
		"scheduler_channel_lifecycle", "scheduler_axonhub_channel_lifecycle",
	}
	out := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		lower := strings.ToLower(stmt)
		skip := false
		for _, table := range retired {
			if strings.Contains(lower, table) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, stmt)
		}
	}
	return out
}

func (s *Store) prepareLegacyMonitoringSchema(ctx context.Context) error {
	for _, col := range []struct{ table, name, def string }{
		{"probe_runs", "status", "TEXT NOT NULL DEFAULT ''"},
		{"probe_runs", "output", "TEXT NOT NULL DEFAULT ''"},
		{"probe_runs", "purpose", "TEXT NOT NULL DEFAULT 'regular'"},
		{"model_cards", "base_url", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "api_key", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "public_enabled", "INTEGER NOT NULL DEFAULT 0"},
		{"model_cards", "display_group", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "pool_enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"model_cards", "manual_cost_ratio", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "scheduler_group", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "sort_order", "INTEGER NOT NULL DEFAULT 0"},
		{"model_cards", "scheduler_channel_id", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "scheduler_channel_name", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "axonhub_channel_id", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "axonhub_channel_name", "TEXT NOT NULL DEFAULT ''"},
		{"model_cards", "scheduler_auto_disabled", "INTEGER NOT NULL DEFAULT 0"},
		{"model_cards", "scheduler_auto_disabled_at", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing(ctx, col.table, col.name, col.def); err != nil {
			return err
		}
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_probe_card_purpose_time ON probe_runs(card_id, purpose, checked_at)`); err != nil {
		return err
	}
	return s.dropColumnIfExists(ctx, "probe_runs", "expected_answer")
}

func (s *Store) preflightCostBindingMigration(ctx context.Context) error {
	var marker int
	err := s.row(ctx, `SELECT 1 FROM migration_records WHERE source=?`, retireMonitoringMigration).Scan(&marker)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		// migration_records 还不存在表示全新数据库，由后续建表流程处理。
		if strings.Contains(strings.ToLower(err.Error()), "no such table") || strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			return nil
		}
		return err
	}
	cols, err := s.columns(ctx, "model_cards")
	if err != nil {
		return err
	}
	if len(cols) == 0 || !cols["pool_enabled"] || !cols["scheduler_channel_id"] || !cols["axonhub_channel_id"] {
		return nil
	}
	for _, column := range []string{"scheduler_channel_id", "axonhub_channel_id"} {
		query := `SELECT ` + quoteIdent(column) + ` FROM model_cards WHERE pool_enabled=1 AND TRIM(` + quoteIdent(column) + `)<>'' GROUP BY ` + quoteIdent(column) + ` HAVING COUNT(*)>1 LIMIT 1`
		var duplicate string
		err := s.row(ctx, query).Scan(&duplicate)
		if err == nil {
			return fmt.Errorf("成本绑定迁移失败：渠道 %s 存在重复绑定", duplicate)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

// retireMonitoringSchema 在一个事务中迁移成本绑定并删除不再使用的健康运行态。
func (s *Store) retireMonitoringSchema(ctx context.Context) error {
	done, err := s.MigrationDone(ctx, retireMonitoringMigration)
	if err != nil || done {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, column := range []string{"scheduler_channel_id", "axonhub_channel_id"} {
		query := `SELECT ` + quoteIdent(column) + ` FROM model_cards WHERE pool_enabled=1 AND TRIM(` + quoteIdent(column) + `)<>'' GROUP BY ` + quoteIdent(column) + ` HAVING COUNT(*)>1 LIMIT 1`
		var duplicate string
		err := tx.QueryRowContext(ctx, query).Scan(&duplicate)
		if err == nil {
			return fmt.Errorf("成本绑定迁移失败：渠道 %s 存在重复绑定", duplicate)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	insert := `INSERT INTO scheduler_cost_bindings
		(id, name, upstream_id, key_id, manual_cost_ratio, scheduler_channel_id, scheduler_channel_name,
		 axonhub_channel_id, axonhub_channel_name, enabled, created_at, updated_at)
		SELECT id, name, upstream_id, key_id, manual_cost_ratio, scheduler_channel_id, scheduler_channel_name,
		 axonhub_channel_id, axonhub_channel_name, enabled, created_at, updated_at
		FROM model_cards WHERE pool_enabled=1
		ON CONFLICT(id) DO NOTHING`
	if _, err := tx.ExecContext(ctx, s.rebind(insert)); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM alert_events WHERE type LIKE 'ping:%' OR type LIKE 'internal:%' OR type LIKE 'quota:%'`,
		`DELETE FROM ops_events WHERE type IN (
			'probe_failed','probe_internal_error','quota_exhausted','scheduler_changed',
			'availability_changed','availability_action_failed','channel_availability',
			'scheduler_control_owner_changed','axonhub_control_plane','scheduler_traffic'
		)`,
		`DELETE FROM scheduler_logs WHERE action<>'group_sync'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, table := range []string{
		"probe_runs", "model_cards", "channel_availability", "scheduler_traffic_events", "scheduler_traffic_10s",
		"scheduler_traffic_1m", "scheduler_traffic_cursors", "scheduler_traffic_control",
		"scheduler_channel_lifecycle", "scheduler_axonhub_channel_lifecycle",
	} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdent(table)); err != nil {
			return err
		}
	}
	for table, columns := range map[string][]string{
		"settings":  {"probe_model", "scheduler_traffic_mode", "scheduler_traffic_profile", "scheduler_log_poll_seconds", "axonhub_api_key"},
		"upstreams": {"balance_guard_mode", "balance_close_threshold", "balance_recover_threshold"},
	} {
		for _, column := range columns {
			if err := dropColumnInTx(ctx, tx, s.Driver, table, column); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO migration_records (source, migrated_at) VALUES (?, ?) ON CONFLICT(source) DO NOTHING`), retireMonitoringMigration, nowText()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func dropColumnInTx(ctx context.Context, tx *sql.Tx, driver, table, column string) error {
	query := `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`
	if driver != "postgres" {
		query = `SELECT name FROM pragma_table_info(?) WHERE name=?`
	}
	var found string
	if err := tx.QueryRowContext(ctx, query, table, column).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `ALTER TABLE `+quoteIdent(table)+` DROP COLUMN `+quoteIdent(column))
	return err
}

// retireProfitMessagesAndPools 在同一事务内不可逆删除已退休功能的数据结构。
func (s *Store) retireProfitMessagesAndPools(ctx context.Context) error {
	done, err := s.MigrationDone(ctx, retireProfitMessagesAndPoolsMigration)
	if err != nil || done {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, table := range []string{
		"scheduler_channel_cost_snapshots", "scheduler_group_sale_snapshots",
		"tg_session", "tg_channels", "tg_messages", "cliproxy_quota_snapshots",
	} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdent(table)); err != nil {
			return err
		}
	}
	for _, column := range []string{"cliproxy_name", "cliproxy_base_url", "cliproxy_management_key", "cliproxy_enabled"} {
		if err := dropColumnInTx(ctx, tx, s.Driver, "settings", column); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ops_events WHERE type='cliproxy_error'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_events WHERE type='cliproxy_error'`); err != nil {
		return err
	}
	if err := s.normalizeRetiredSettingsTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO migration_records (source, migrated_at) VALUES (?, ?) ON CONFLICT(source) DO NOTHING`), retireProfitMessagesAndPoolsMigration, nowText()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) normalizeRetiredSettingsTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, scheduler_tiers, notification_rules FROM settings`)
	if err != nil {
		return err
	}
	type row struct{ id, tiers, rules string }
	var settings []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.tiers, &item.rules); err != nil {
			rows.Close()
			return err
		}
		settings = append(settings, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range settings {
		tiers := removeJSONKeyFromArray(item.tiers, "sale_price")
		rules := removeNestedJSONKey(item.rules, "event_types", "cliproxy_error")
		if _, err := tx.ExecContext(ctx, s.rebind(`UPDATE settings SET scheduler_tiers=?, notification_rules=? WHERE id=?`), tiers, rules, item.id); err != nil {
			return err
		}
	}
	return nil
}

func removeJSONKeyFromArray(raw, key string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var rows []map[string]any
	if json.Unmarshal([]byte(raw), &rows) != nil {
		return raw
	}
	for _, row := range rows {
		delete(row, key)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return raw
	}
	return string(out)
}

func removeNestedJSONKey(raw, parent, key string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var value map[string]any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return raw
	}
	if nested, ok := value[parent].(map[string]any); ok {
		delete(nested, key)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return string(out)
}

func (s *Store) ensureDefaultRevenueCard(ctx context.Context) error {
	var n int
	if err := s.row(ctx, `SELECT COUNT(*) FROM revenue_cards`).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return nil
	}
	now := nowText()
	_, err := s.exec(ctx, `INSERT INTO revenue_cards (id, name, source_type, enabled, sort_order, created_at, updated_at) VALUES (?, '今日收入', 'epay_total', 1, 1, ?, ?)`, NewID(), now, now)
	return err
}

func (s *Store) addColumnIfMissing(ctx context.Context, table, column, def string) error {
	cols, err := s.columns(ctx, table)
	if err != nil {
		return err
	}
	if cols[column] {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE `+quoteIdent(table)+` ADD COLUMN `+quoteIdent(column)+` `+def)
	return err
}

func (s *Store) dropColumnIfExists(ctx context.Context, table, column string) error {
	cols, err := s.columns(ctx, table)
	if err != nil {
		return err
	}
	if !cols[column] {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE `+quoteIdent(table)+` DROP COLUMN `+quoteIdent(column))
	return err
}

func (s *Store) columns(ctx context.Context, table string) (map[string]bool, error) {
	if s.Driver != "postgres" {
		return tableColumns(ctx, s.DB, table)
	}
	rows, err := s.query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=?`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
