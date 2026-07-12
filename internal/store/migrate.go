package store

import (
	"context"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) Migrate(ctx context.Context) error {
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
			telegram_chat_id TEXT NOT NULL DEFAULT '', probe_model TEXT NOT NULL DEFAULT 'gpt-5.5',
			site_name TEXT NOT NULL DEFAULT 'AI 上游监控', site_icon TEXT NOT NULL DEFAULT '',
			epay_base_url TEXT NOT NULL DEFAULT '', epay_pid TEXT NOT NULL DEFAULT '', epay_key TEXT NOT NULL DEFAULT '',
			scheduler_base_url TEXT NOT NULL DEFAULT '', scheduler_user_id TEXT NOT NULL DEFAULT '', scheduler_access_token TEXT NOT NULL DEFAULT '',
			scheduler_unassigned_group TEXT NOT NULL DEFAULT '',
			scheduler_tiers TEXT NOT NULL DEFAULT '',
			cliproxy_name TEXT NOT NULL DEFAULT 'CLIProxyAPI', cliproxy_base_url TEXT NOT NULL DEFAULT '',
			cliproxy_management_key TEXT NOT NULL DEFAULT '', cliproxy_enabled INTEGER NOT NULL DEFAULT 1,
			notification_rules TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS upstreams (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, type TEXT NOT NULL, base_url TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
			user_id TEXT NOT NULL DEFAULT '', access_token TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '', sub2api_access_token TEXT NOT NULL DEFAULT '', sub2api_refresh_token TEXT NOT NULL DEFAULT '',
			balance_rate REAL NOT NULL DEFAULT 1, low_balance_threshold REAL NOT NULL DEFAULT 0,
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
			scheduler_auto_disabled INTEGER NOT NULL DEFAULT 0, scheduler_auto_disabled_at TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, public_enabled INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
			failure_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
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
			latency_ms INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
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
		`CREATE TABLE IF NOT EXISTS cliproxy_quota_snapshots (
			id TEXT PRIMARY KEY, account_name TEXT NOT NULL DEFAULT '', auth_index TEXT NOT NULL DEFAULT '',
			checked_at TEXT NOT NULL, ok INTEGER NOT NULL DEFAULT 0, plan_type TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS balance_recharge_logs (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, method TEXT NOT NULL, amount REAL NOT NULL DEFAULT 0,
			payment_type TEXT NOT NULL DEFAULT '', remote_order_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '', raw_status TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_logs (
			id TEXT PRIMARY KEY, card_id TEXT NOT NULL DEFAULT '', card_name TEXT NOT NULL DEFAULT '',
			channel_id TEXT NOT NULL DEFAULT '', channel_name TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_channel_cost_snapshots (
			id TEXT PRIMARY KEY, channel_id TEXT NOT NULL DEFAULT '', channel_name TEXT NOT NULL DEFAULT '',
			card_id TEXT NOT NULL DEFAULT '', card_name TEXT NOT NULL DEFAULT '', source_type TEXT NOT NULL DEFAULT '',
			upstream_id TEXT NOT NULL DEFAULT '', upstream_name TEXT NOT NULL DEFAULT '',
			key_id TEXT NOT NULL DEFAULT '', key_name TEXT NOT NULL DEFAULT '',
			cost_per_unit REAL NOT NULL DEFAULT 0, active INTEGER NOT NULL DEFAULT 0,
			missing_reason TEXT NOT NULL DEFAULT '', effective_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_group_sale_snapshots (
			id TEXT PRIMARY KEY, group_name TEXT NOT NULL DEFAULT '', tag TEXT NOT NULL DEFAULT '',
			sale_price REAL NOT NULL DEFAULT 0, active INTEGER NOT NULL DEFAULT 0, effective_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS revenue_cards (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, source_type TEXT NOT NULL, upstream_id TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '', access_token TEXT NOT NULL DEFAULT '',
			admin_api_key TEXT NOT NULL DEFAULT '', epay_pid TEXT NOT NULL DEFAULT '', epay_key TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tg_session (
			id TEXT PRIMARY KEY, api_id INTEGER NOT NULL DEFAULT 0, api_hash TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '',
			code_hash TEXT NOT NULL DEFAULT '', session_blob BYTEA, authorized INTEGER NOT NULL DEFAULT 0, password_needed INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tg_channels (
				id TEXT PRIMARY KEY, display_name TEXT NOT NULL, identifier TEXT NOT NULL DEFAULT '', username TEXT NOT NULL DEFAULT '',
				peer_id INTEGER NOT NULL DEFAULT 0, access_hash INTEGER NOT NULL DEFAULT 0, avatar_url TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1,
				message_limit INTEGER NOT NULL DEFAULT 10, pinned_only INTEGER NOT NULL DEFAULT 0, last_sync_at TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(peer_id)
			)`,
		`CREATE TABLE IF NOT EXISTS tg_messages (
			id TEXT PRIMARY KEY, channel_id TEXT NOT NULL, remote_id INTEGER NOT NULL, published_at TEXT NOT NULL, text TEXT NOT NULL DEFAULT '',
			media_type TEXT NOT NULL DEFAULT '', media_path TEXT NOT NULL DEFAULT '', media_url TEXT NOT NULL DEFAULT '', media_cached INTEGER NOT NULL DEFAULT 0,
			link TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(channel_id, remote_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_upstream ON api_keys(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cards_upstream ON model_cards(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_probe_card_time ON probe_runs(card_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_balance_upstream_time ON balance_snapshots(upstream_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state ON alert_events(upstream_id, type, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_events_time ON ops_events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_time ON audit_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_revenue_snapshots_time ON revenue_snapshots(checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_cliproxy_quota_snapshots_time ON cliproxy_quota_snapshots(checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_recharge_upstream_time ON balance_recharge_logs(upstream_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_recharge_pending ON balance_recharge_logs(method, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_logs_time ON scheduler_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_cost_snapshots_lookup ON scheduler_channel_cost_snapshots(channel_id, effective_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_sale_snapshots_lookup ON scheduler_group_sale_snapshots(group_name, effective_at)`,
		`CREATE INDEX IF NOT EXISTS idx_revenue_cards_upstream ON revenue_cards(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tg_messages_channel_time ON tg_messages(channel_id, published_at)`,
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
	if err := s.addColumnIfMissing(ctx, "settings", "epay_base_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "epay_pid", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "epay_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []string{"scheduler_base_url", "scheduler_user_id", "scheduler_access_token"} {
		if err := s.addColumnIfMissing(ctx, "settings", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing(ctx, "settings", "scheduler_tiers", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "settings", "scheduler_unassigned_group", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []struct{ name, def string }{
		{"cliproxy_name", "TEXT NOT NULL DEFAULT 'CLIProxyAPI'"},
		{"cliproxy_base_url", "TEXT NOT NULL DEFAULT ''"},
		{"cliproxy_management_key", "TEXT NOT NULL DEFAULT ''"},
		{"cliproxy_enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"notification_rules", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing(ctx, "settings", col.name, col.def); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing(ctx, "probe_runs", "status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "probe_runs", "output", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.dropColumnIfExists(ctx, "probe_runs", "expected_answer"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "base_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "api_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "public_enabled", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "display_group", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "pool_enabled", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "manual_cost_ratio", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "scheduler_group", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "sort_order", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "scheduler_channel_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "scheduler_channel_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "scheduler_auto_disabled", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "model_cards", "scheduler_auto_disabled_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
	if err := s.addColumnIfMissing(ctx, "tg_channels", "pinned_only", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "tg_channels", "avatar_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `INSERT INTO settings (id, check_interval_minutes, probe_model) VALUES ('default', 5, ?) ON CONFLICT(id) DO NOTHING`, domain.ProbeModel); err != nil {
		return err
	}
	return s.ensureDefaultRevenueCard(ctx)
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
