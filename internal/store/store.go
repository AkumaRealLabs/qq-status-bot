package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB     *sql.DB
	Driver string
}

const InitialUserID = "initial-user"

var ErrInitialUserExists = errors.New("initial user already exists")

func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		dsn = "/app/data/monitor.sqlite"
	}
	driver := "sqlite"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		driver = "postgres"
	} else if !strings.HasPrefix(dsn, "file:") {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, Driver: driver}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
			return nil, err
		}
		if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=10000"); err != nil {
			return nil, err
		}
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

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
			scheduler_tiers TEXT NOT NULL DEFAULT ''
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
			display_group TEXT NOT NULL DEFAULT '', scheduler_group TEXT NOT NULL DEFAULT '', scheduler_channel_id TEXT NOT NULL DEFAULT '', scheduler_channel_name TEXT NOT NULL DEFAULT '',
			scheduler_auto_disabled INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1, public_enabled INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
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
			expected_answer TEXT NOT NULL DEFAULT '', output TEXT NOT NULL DEFAULT '', http_status INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS alert_events (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, type TEXT NOT NULL, recover INTEGER NOT NULL DEFAULT 0,
			sent INTEGER NOT NULL DEFAULT 0, message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
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
		`CREATE INDEX IF NOT EXISTS idx_recharge_upstream_time ON balance_recharge_logs(upstream_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_logs_time ON scheduler_logs(created_at)`,
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
	if err := s.addColumnIfMissing(ctx, "probe_runs", "status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "probe_runs", "output", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "probe_runs", "expected_answer", "TEXT NOT NULL DEFAULT ''"); err != nil {
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

func (s *Store) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.DB.ExecContext(ctx, s.rebind(q), args...)
}

func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.DB.QueryContext(ctx, s.rebind(q), args...)
}

func (s *Store) row(ctx context.Context, q string, args ...any) *sql.Row {
	return s.DB.QueryRowContext(ctx, s.rebind(q), args...)
}

func (s *Store) rebind(q string) string {
	if s.Driver != "postgres" {
		return q
	}
	var n int
	var b strings.Builder
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func NewID() string {
	return mustRandomHex(16)
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewToken() string {
	return mustRandomHex(32)
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04:05.000Z"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func boolFromInt(v int) bool { return v != 0 }

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	return n, s.row(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (domain.User, error) {
	u := domain.User{ID: NewID(), Username: username, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	_, err := s.exec(ctx, `INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`, u.ID, u.Username, u.PasswordHash, u.CreatedAt.Format(time.RFC3339Nano))
	return u, err
}

func (s *Store) CreateInitialUser(ctx context.Context, username, passwordHash string) (domain.User, error) {
	u := domain.User{ID: InitialUserID, Username: username, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	res, err := s.exec(ctx, `INSERT INTO users (id, username, password_hash, created_at)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`, u.ID, u.Username, u.PasswordHash, u.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return u, err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return u, ErrInitialUserExists
	}
	return u, nil
}

func (s *Store) UserByUsername(ctx context.Context, username string) (domain.User, error) {
	return s.scanUser(s.row(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE username=?`, username))
}

func (s *Store) UserBySessionToken(ctx context.Context, token string) (domain.User, error) {
	hash := HashToken(token)
	return s.scanUser(s.row(ctx, `SELECT u.id, u.username, u.password_hash, u.created_at
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, hash, nowText()))
}

func (s *Store) scanUser(row *sql.Row) (domain.User, error) {
	var u domain.User
	var created string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &created); err != nil {
		return u, err
	}
	u.CreatedAt = parseTime(created)
	return u, nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	token := NewToken()
	_, err := s.exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		NewID(), userID, HashToken(token), time.Now().UTC().Add(ttl).Format(time.RFC3339Nano), nowText())
	return token, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.exec(ctx, `DELETE FROM sessions WHERE token_hash=?`, HashToken(token))
	return err
}

func (s *Store) CleanupSessions(ctx context.Context) error {
	_, err := s.exec(ctx, `DELETE FROM sessions WHERE expires_at<=?`, nowText())
	return err
}

func (s *Store) Settings(ctx context.Context) (domain.Settings, error) {
	var cfg domain.Settings
	err := s.row(ctx, `SELECT check_interval_minutes, telegram_bot_token, telegram_chat_id, probe_model, site_name, site_icon, epay_base_url, epay_pid, epay_key FROM settings WHERE id='default'`).
		Scan(&cfg.CheckIntervalMinutes, &cfg.TelegramBotToken, &cfg.TelegramChatID, &cfg.ProbeModel, &cfg.SiteName, &cfg.SiteIcon, &cfg.EpayBaseURL, &cfg.EpayPID, &cfg.EpayKey)
	if err != nil {
		return cfg, err
	}
	cfg.CheckIntervalMinutes = domain.NormalizeCheckInterval(cfg.CheckIntervalMinutes)
	cfg.ProbeModel = domain.ProbeModel
	if cfg.SiteName == "" {
		cfg.SiteName = "AI 上游监控"
	}
	return cfg, nil
}

func (s *Store) UpdateSettings(ctx context.Context, cfg domain.Settings) (domain.Settings, error) {
	cfg.CheckIntervalMinutes = domain.NormalizeCheckInterval(cfg.CheckIntervalMinutes)
	cfg.ProbeModel = domain.ProbeModel
	if cfg.SiteName == "" {
		cfg.SiteName = "AI 上游监控"
	}
	cfg.EpayBaseURL = strings.TrimRight(strings.TrimSpace(cfg.EpayBaseURL), "/")
	cfg.EpayPID = strings.TrimSpace(cfg.EpayPID)
	cfg.EpayKey = strings.TrimSpace(cfg.EpayKey)
	_, err := s.exec(ctx, `UPDATE settings SET check_interval_minutes=?, telegram_bot_token=?, telegram_chat_id=?, probe_model=?, site_name=?, site_icon=?, epay_base_url=?, epay_pid=?, epay_key=? WHERE id='default'`,
		cfg.CheckIntervalMinutes, cfg.TelegramBotToken, cfg.TelegramChatID, cfg.ProbeModel, cfg.SiteName, cfg.SiteIcon, cfg.EpayBaseURL, cfg.EpayPID, cfg.EpayKey)
	return cfg, err
}

func (s *Store) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	var cfg domain.SchedulerConfig
	var tiers string
	err := s.row(ctx, `SELECT scheduler_base_url, scheduler_user_id, scheduler_access_token, scheduler_tiers FROM settings WHERE id='default'`).
		Scan(&cfg.BaseURL, &cfg.UserID, &cfg.AccessToken, &tiers)
	cfg.Tiers = schedulerTiers(tiers)
	return cfg, err
}

func (s *Store) UpdateSchedulerConfig(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerConfig, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.UserID = strings.TrimSpace(cfg.UserID)
	cfg.AccessToken = strings.TrimSpace(cfg.AccessToken)
	cfg.Tiers = domain.NormalizeSchedulerTiers(cfg.Tiers)
	b, err := json.Marshal(cfg.Tiers)
	if err != nil {
		return cfg, err
	}
	_, err = s.exec(ctx, `UPDATE settings SET scheduler_base_url=?, scheduler_user_id=?, scheduler_access_token=?, scheduler_tiers=? WHERE id='default'`, cfg.BaseURL, cfg.UserID, cfg.AccessToken, string(b))
	return cfg, err
}

func schedulerTiers(raw string) []domain.SchedulerTier {
	var tiers []domain.SchedulerTier
	_ = json.Unmarshal([]byte(raw), &tiers)
	return domain.NormalizeSchedulerTiers(tiers)
}

func (s *Store) CreateSchedulerLog(ctx context.Context, log domain.SchedulerLog) error {
	if log.ID == "" {
		log.ID = NewID()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_logs (id, card_id, card_name, channel_id, channel_name, action, status, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, log.ID, log.CardID, log.CardName, log.ChannelID, log.ChannelName, log.Action, log.Status, log.Message, log.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) SchedulerLogs(ctx context.Context, limit int) ([]domain.SchedulerLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.query(ctx, `SELECT id, card_id, card_name, channel_id, channel_name, action, status, message, created_at FROM scheduler_logs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SchedulerLog{}
	for rows.Next() {
		var log domain.SchedulerLog
		var created string
		if err := rows.Scan(&log.ID, &log.CardID, &log.CardName, &log.ChannelID, &log.ChannelName, &log.Action, &log.Status, &log.Message, &created); err != nil {
			return nil, err
		}
		log.CreatedAt = parseTime(created)
		out = append(out, log)
	}
	return out, rows.Err()
}

func (s *Store) CreateUpstream(ctx context.Context, u domain.Upstream) (domain.Upstream, error) {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.BalanceRate <= 0 {
		u.BalanceRate = 1
	}
	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO upstreams
		(id, name, type, base_url, enabled, user_id, access_token, email, password, sub2api_access_token, sub2api_refresh_token,
		balance_rate, low_balance_threshold, last_error, failure_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Name, u.Type, u.BaseURL, boolInt(u.Enabled), u.UserID, u.AccessToken, u.Email, u.Password,
		u.Sub2APIAccessToken, u.Sub2APIRefreshToken, u.BalanceRate, u.LowBalanceThreshold, u.LastError, u.FailureCount,
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano))
	return u, err
}

func (s *Store) UpdateUpstream(ctx context.Context, u domain.Upstream) (domain.Upstream, error) {
	u.UpdatedAt = time.Now().UTC()
	if u.BalanceRate <= 0 {
		u.BalanceRate = 1
	}
	_, err := s.exec(ctx, `UPDATE upstreams SET name=?, type=?, base_url=?, enabled=?, user_id=?, access_token=?, email=?, password=?,
		sub2api_access_token=?, sub2api_refresh_token=?, balance_rate=?, low_balance_threshold=?, last_error=?, failure_count=?, updated_at=? WHERE id=?`,
		u.Name, u.Type, u.BaseURL, boolInt(u.Enabled), u.UserID, u.AccessToken, u.Email, u.Password, u.Sub2APIAccessToken,
		u.Sub2APIRefreshToken, u.BalanceRate, u.LowBalanceThreshold, u.LastError, u.FailureCount, u.UpdatedAt.Format(time.RFC3339Nano), u.ID)
	return u, err
}

func (s *Store) DeleteUpstream(ctx context.Context, id string) error {
	for _, stmt := range []string{
		`DELETE FROM api_keys WHERE upstream_id=?`,
		`DELETE FROM model_cards WHERE upstream_id=?`,
		`DELETE FROM balance_snapshots WHERE upstream_id=?`,
		`DELETE FROM probe_runs WHERE upstream_id=?`,
		`DELETE FROM alert_events WHERE upstream_id=?`,
		`DELETE FROM balance_recharge_logs WHERE upstream_id=?`,
		`DELETE FROM revenue_cards WHERE upstream_id=?`,
		`DELETE FROM upstreams WHERE id=?`,
	} {
		if _, err := s.exec(ctx, stmt, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Upstream(ctx context.Context, id string) (domain.Upstream, error) {
	return s.scanUpstream(s.row(ctx, `SELECT id, name, type, base_url, enabled, user_id, access_token, email, password,
		sub2api_access_token, sub2api_refresh_token, balance_rate, low_balance_threshold, last_error, failure_count, created_at, updated_at
		FROM upstreams WHERE id=?`, id))
}

func (s *Store) ListUpstreams(ctx context.Context) ([]domain.Upstream, error) {
	rows, err := s.query(ctx, `SELECT id, name, type, base_url, enabled, user_id, access_token, email, password,
		sub2api_access_token, sub2api_refresh_token, balance_rate, low_balance_threshold, last_error, failure_count, created_at, updated_at
		FROM upstreams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Upstream
	for rows.Next() {
		u, err := scanUpstreamRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) scanUpstream(row *sql.Row) (domain.Upstream, error) {
	var u domain.Upstream
	var enabled int
	var created, updated string
	err := row.Scan(&u.ID, &u.Name, &u.Type, &u.BaseURL, &enabled, &u.UserID, &u.AccessToken, &u.Email, &u.Password,
		&u.Sub2APIAccessToken, &u.Sub2APIRefreshToken, &u.BalanceRate, &u.LowBalanceThreshold, &u.LastError, &u.FailureCount, &created, &updated)
	if err != nil {
		return u, err
	}
	u.Enabled = boolFromInt(enabled)
	u.CreatedAt, u.UpdatedAt = parseTime(created), parseTime(updated)
	return u, nil
}

func scanUpstreamRows(rows *sql.Rows) (domain.Upstream, error) {
	var u domain.Upstream
	var enabled int
	var created, updated string
	err := rows.Scan(&u.ID, &u.Name, &u.Type, &u.BaseURL, &enabled, &u.UserID, &u.AccessToken, &u.Email, &u.Password,
		&u.Sub2APIAccessToken, &u.Sub2APIRefreshToken, &u.BalanceRate, &u.LowBalanceThreshold, &u.LastError, &u.FailureCount, &created, &updated)
	u.Enabled = boolFromInt(enabled)
	u.CreatedAt, u.UpdatedAt = parseTime(created), parseTime(updated)
	return u, err
}

func (s *Store) SaveUpstreamTokens(ctx context.Context, id, access, refresh string) error {
	_, err := s.exec(ctx, `UPDATE upstreams SET sub2api_access_token=?, sub2api_refresh_token=?, updated_at=? WHERE id=?`, access, refresh, nowText(), id)
	return err
}

func (s *Store) SaveUpstreamError(ctx context.Context, id, msg string, failureCount int) error {
	_, err := s.exec(ctx, `UPDATE upstreams SET last_error=?, failure_count=?, updated_at=? WHERE id=?`, msg, failureCount, nowText(), id)
	return err
}

func (s *Store) SaveKeys(ctx context.Context, upstreamID string, keys []monitor.APIKey) error {
	for _, k := range keys {
		remoteID := k.RemoteID
		if remoteID == "" {
			remoteID = k.Key
		}
		if remoteID == "" {
			continue
		}
		var id string
		err := s.row(ctx, `SELECT id FROM api_keys WHERE upstream_id=? AND remote_id=?`, upstreamID, remoteID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			id = NewID()
			_, err = s.exec(ctx, `INSERT INTO api_keys
				(id, upstream_id, remote_id, name, key, status, description, group_name, group_ratio, quota, used_quota, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, upstreamID, remoteID, k.Name, k.Key, k.Status, k.Description, k.Group, k.GroupRatio, k.Quota, k.UsedQuota, nowText(), nowText())
		} else if err == nil {
			_, err = s.exec(ctx, `UPDATE api_keys SET name=?, key=?, status=?, description=?, group_name=?, group_ratio=?, quota=?, used_quota=?, updated_at=? WHERE id=?`,
				k.Name, k.Key, k.Status, k.Description, k.Group, k.GroupRatio, k.Quota, k.UsedQuota, nowText(), id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Key(ctx context.Context, id string) (domain.APIKey, error) {
	return s.scanKey(s.row(ctx, `SELECT id, upstream_id, remote_id, name, key, status, description, group_name, group_ratio, quota, used_quota, created_at, updated_at FROM api_keys WHERE id=?`, id))
}

func (s *Store) ListKeys(ctx context.Context, upstreamID string) ([]domain.APIKey, error) {
	rows, err := s.query(ctx, `SELECT id, upstream_id, remote_id, name, key, status, description, group_name, group_ratio, quota, used_quota, created_at, updated_at FROM api_keys WHERE upstream_id=? ORDER BY name`, upstreamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.APIKey{}
	for rows.Next() {
		k, err := scanKeyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) scanKey(row *sql.Row) (domain.APIKey, error) {
	var k domain.APIKey
	var created, updated string
	err := row.Scan(&k.ID, &k.UpstreamID, &k.RemoteID, &k.Name, &k.Key, &k.Status, &k.Description, &k.Group, &k.GroupRatio, &k.Quota, &k.UsedQuota, &created, &updated)
	k.CreatedAt, k.UpdatedAt = parseTime(created), parseTime(updated)
	return k, err
}

func scanKeyRows(rows *sql.Rows) (domain.APIKey, error) {
	var k domain.APIKey
	var created, updated string
	err := rows.Scan(&k.ID, &k.UpstreamID, &k.RemoteID, &k.Name, &k.Key, &k.Status, &k.Description, &k.Group, &k.GroupRatio, &k.Quota, &k.UsedQuota, &created, &updated)
	k.CreatedAt, k.UpdatedAt = parseTime(created), parseTime(updated)
	return k, err
}

func (s *Store) SaveBalance(ctx context.Context, upstreamID string, b monitor.Balance, errText string, latencyMS int) (domain.BalanceSnapshot, error) {
	snap := domain.BalanceSnapshot{ID: NewID(), UpstreamID: upstreamID, CheckedAt: time.Now().UTC(), Balance: b.Balance, Used: b.Used, Remain: b.Remain, Requests: b.Requests, Error: errText, LatencyMS: latencyMS}
	_, err := s.exec(ctx, `INSERT INTO balance_snapshots (id, upstream_id, checked_at, balance, used, remain, requests, error, latency_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID, snap.UpstreamID, snap.CheckedAt.Format(time.RFC3339Nano), snap.Balance, snap.Used, snap.Remain, snap.Requests, snap.Error, snap.LatencyMS)
	return snap, err
}

func (s *Store) LatestBalance(ctx context.Context, upstreamID string) (domain.BalanceSnapshot, error) {
	return s.scanBalance(s.row(ctx, `SELECT id, upstream_id, checked_at, balance, used, remain, requests, error, latency_ms FROM balance_snapshots WHERE upstream_id=? ORDER BY checked_at DESC LIMIT 1`, upstreamID))
}

func (s *Store) scanBalance(row *sql.Row) (domain.BalanceSnapshot, error) {
	var b domain.BalanceSnapshot
	var checked string
	err := row.Scan(&b.ID, &b.UpstreamID, &checked, &b.Balance, &b.Used, &b.Remain, &b.Requests, &b.Error, &b.LatencyMS)
	b.CheckedAt = parseTime(checked)
	return b, err
}

func (s *Store) CreateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	c.Model = domain.ProbeModel
	if c.SortOrder <= 0 {
		next, err := s.nextCardSortOrder(ctx)
		if err != nil {
			return c, err
		}
		c.SortOrder = next
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO model_cards
		(id, name, base_url, api_key, upstream_id, key_id, model, display_group, scheduler_group, scheduler_channel_id, scheduler_channel_name, scheduler_auto_disabled, enabled, public_enabled, sort_order, last_error, failure_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.BaseURL, c.APIKey, c.UpstreamID, c.KeyID, c.Model, c.DisplayGroup, c.SchedulerGroup, c.SchedulerChannelID, c.SchedulerChannelName, boolInt(c.SchedulerAutoDisabled), boolInt(c.Enabled), boolInt(c.PublicEnabled),
		c.SortOrder, c.LastError, c.FailureCount, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) nextCardSortOrder(ctx context.Context) (int, error) {
	var maxOrder int
	err := s.row(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM model_cards`).Scan(&maxOrder)
	return maxOrder + 1, err
}

func (s *Store) UpdateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	c.Model = domain.ProbeModel
	c.UpdatedAt = time.Now().UTC()
	_, err := s.exec(ctx, `UPDATE model_cards SET name=?, base_url=?, api_key=?, upstream_id=?, key_id=?, model=?, display_group=?, scheduler_group=?, scheduler_channel_id=?, scheduler_channel_name=?, scheduler_auto_disabled=?, enabled=?,
		public_enabled=?, sort_order=?, last_error=?, failure_count=?, updated_at=? WHERE id=?`,
		c.Name, c.BaseURL, c.APIKey, c.UpstreamID, c.KeyID, c.Model, c.DisplayGroup, c.SchedulerGroup, c.SchedulerChannelID, c.SchedulerChannelName, boolInt(c.SchedulerAutoDisabled), boolInt(c.Enabled), boolInt(c.PublicEnabled),
		c.SortOrder, c.LastError, c.FailureCount, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	return c, err
}

func (s *Store) DeleteCard(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM model_cards WHERE id=?`, id)
	return err
}

func (s *Store) Card(ctx context.Context, id string) (domain.ModelCard, error) {
	return s.scanCard(s.row(ctx, `SELECT id, name, base_url, api_key, upstream_id, key_id, model, display_group, scheduler_group, scheduler_channel_id, scheduler_channel_name, scheduler_auto_disabled, enabled, public_enabled, sort_order, last_error, failure_count, created_at, updated_at FROM model_cards WHERE id=?`, id))
}

func (s *Store) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	rows, err := s.query(ctx, `SELECT id, name, base_url, api_key, upstream_id, key_id, model, display_group, scheduler_group, scheduler_channel_id, scheduler_channel_name, scheduler_auto_disabled, enabled, public_enabled, sort_order, last_error, failure_count, created_at, updated_at FROM model_cards ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelCard
	for rows.Next() {
		c, err := scanCardRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) scanCard(row *sql.Row) (domain.ModelCard, error) {
	var c domain.ModelCard
	var autoDisabled, enabled, publicEnabled int
	var created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.BaseURL, &c.APIKey, &c.UpstreamID, &c.KeyID, &c.Model, &c.DisplayGroup, &c.SchedulerGroup, &c.SchedulerChannelID, &c.SchedulerChannelName, &autoDisabled, &enabled, &publicEnabled, &c.SortOrder, &c.LastError, &c.FailureCount, &created, &updated)
	c.SchedulerAutoDisabled = boolFromInt(autoDisabled)
	c.Enabled = boolFromInt(enabled)
	c.PublicEnabled = boolFromInt(publicEnabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func scanCardRows(rows *sql.Rows) (domain.ModelCard, error) {
	var c domain.ModelCard
	var autoDisabled, enabled, publicEnabled int
	var created, updated string
	err := rows.Scan(&c.ID, &c.Name, &c.BaseURL, &c.APIKey, &c.UpstreamID, &c.KeyID, &c.Model, &c.DisplayGroup, &c.SchedulerGroup, &c.SchedulerChannelID, &c.SchedulerChannelName, &autoDisabled, &enabled, &publicEnabled, &c.SortOrder, &c.LastError, &c.FailureCount, &created, &updated)
	c.SchedulerAutoDisabled = boolFromInt(autoDisabled)
	c.Enabled = boolFromInt(enabled)
	c.PublicEnabled = boolFromInt(publicEnabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func (s *Store) UpdateCardOrder(ctx context.Context, ids []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, s.rebind(`UPDATE model_cards SET sort_order=?, updated_at=? WHERE id=?`), i+1, nowText(), id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("card not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	done = true
	return nil
}

func (s *Store) CreateRevenueCard(ctx context.Context, c domain.RevenueCard) (domain.RevenueCard, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.SortOrder <= 0 {
		next, err := s.nextRevenueCardSortOrder(ctx)
		if err != nil {
			return c, err
		}
		c.SortOrder = next
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO revenue_cards
		(id, name, source_type, upstream_id, base_url, user_id, access_token, admin_api_key, epay_pid, epay_key, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.SourceType, c.UpstreamID, c.BaseURL, c.UserID, c.AccessToken, c.AdminAPIKey, c.EpayPID, c.EpayKey,
		boolInt(c.Enabled), c.SortOrder, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) nextRevenueCardSortOrder(ctx context.Context) (int, error) {
	var maxOrder int
	err := s.row(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM revenue_cards`).Scan(&maxOrder)
	return maxOrder + 1, err
}

func (s *Store) UpdateRevenueCard(ctx context.Context, c domain.RevenueCard) (domain.RevenueCard, error) {
	c.UpdatedAt = time.Now().UTC()
	_, err := s.exec(ctx, `UPDATE revenue_cards SET name=?, source_type=?, upstream_id=?, base_url=?, user_id=?, access_token=?,
		admin_api_key=?, epay_pid=?, epay_key=?, enabled=?, sort_order=?, updated_at=? WHERE id=?`,
		c.Name, c.SourceType, c.UpstreamID, c.BaseURL, c.UserID, c.AccessToken, c.AdminAPIKey, c.EpayPID, c.EpayKey,
		boolInt(c.Enabled), c.SortOrder, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	return c, err
}

func (s *Store) DeleteRevenueCard(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM revenue_cards WHERE id=?`, id)
	return err
}

func (s *Store) RevenueCard(ctx context.Context, id string) (domain.RevenueCard, error) {
	return s.scanRevenueCard(s.row(ctx, revenueCardSelectSQL()+` WHERE id=?`, id))
}

func (s *Store) ListRevenueCards(ctx context.Context) ([]domain.RevenueCard, error) {
	rows, err := s.query(ctx, revenueCardSelectSQL()+` ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RevenueCard{}
	for rows.Next() {
		c, err := scanRevenueCardRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) scanRevenueCard(row *sql.Row) (domain.RevenueCard, error) {
	var c domain.RevenueCard
	var enabled int
	var created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.SourceType, &c.UpstreamID, &c.BaseURL, &c.UserID, &c.AccessToken,
		&c.AdminAPIKey, &c.EpayPID, &c.EpayKey, &enabled, &c.SortOrder, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func scanRevenueCardRows(rows *sql.Rows) (domain.RevenueCard, error) {
	var c domain.RevenueCard
	var enabled int
	var created, updated string
	err := rows.Scan(&c.ID, &c.Name, &c.SourceType, &c.UpstreamID, &c.BaseURL, &c.UserID, &c.AccessToken,
		&c.AdminAPIKey, &c.EpayPID, &c.EpayKey, &enabled, &c.SortOrder, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func revenueCardSelectSQL() string {
	return `SELECT id, name, source_type, upstream_id, base_url, user_id, access_token, admin_api_key, epay_pid, epay_key, enabled, sort_order, created_at, updated_at FROM revenue_cards`
}

func (s *Store) UpdateRevenueCardOrder(ctx context.Context, ids []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, s.rebind(`UPDATE revenue_cards SET sort_order=?, updated_at=? WHERE id=?`), i+1, nowText(), id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("revenue card not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	done = true
	return nil
}

func (s *Store) TGSession(ctx context.Context) (domain.TGSession, error) {
	var sess domain.TGSession
	var authorized, passwordNeeded int
	var created, updated string
	err := s.row(ctx, `SELECT id, api_id, api_hash, phone, code_hash, session_blob, authorized, password_needed, last_error, created_at, updated_at FROM tg_session WHERE id='default'`).
		Scan(&sess.ID, &sess.APIID, &sess.APIHash, &sess.Phone, &sess.CodeHash, &sess.SessionBlob, &authorized, &passwordNeeded, &sess.LastError, &created, &updated)
	sess.Authorized = boolFromInt(authorized)
	sess.PasswordNeeded = boolFromInt(passwordNeeded)
	sess.CreatedAt, sess.UpdatedAt = parseTime(created), parseTime(updated)
	return sess, err
}

func (s *Store) SaveTGSession(ctx context.Context, sess domain.TGSession) (domain.TGSession, error) {
	if sess.ID == "" {
		sess.ID = "default"
	}
	if sess.SessionBlob == nil {
		var blob []byte
		err := s.row(ctx, `SELECT session_blob FROM tg_session WHERE id=?`, sess.ID).Scan(&blob)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return sess, err
		}
		sess.SessionBlob = blob
	}
	now := nowText()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = parseTime(now)
	}
	sess.UpdatedAt = parseTime(now)
	_, err := s.exec(ctx, `INSERT INTO tg_session
		(id, api_id, api_hash, phone, code_hash, session_blob, authorized, password_needed, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET api_id=excluded.api_id, api_hash=excluded.api_hash, phone=excluded.phone,
			code_hash=excluded.code_hash, session_blob=excluded.session_blob, authorized=excluded.authorized,
			password_needed=excluded.password_needed, last_error=excluded.last_error, updated_at=excluded.updated_at`,
		sess.ID, sess.APIID, sess.APIHash, sess.Phone, sess.CodeHash, sess.SessionBlob, boolInt(sess.Authorized), boolInt(sess.PasswordNeeded), sess.LastError,
		sess.CreatedAt.Format(time.RFC3339Nano), sess.UpdatedAt.Format(time.RFC3339Nano))
	return sess, err
}

func (s *Store) StoreTGSessionBlob(ctx context.Context, data []byte) error {
	_, err := s.exec(ctx, `UPDATE tg_session SET session_blob=?, updated_at=? WHERE id='default'`, data, nowText())
	return err
}

func (s *Store) CreateTGChannel(ctx context.Context, c domain.TGChannel) (domain.TGChannel, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.MessageLimit <= 0 {
		c.MessageLimit = 10
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO tg_channels
		(id, display_name, identifier, username, peer_id, access_hash, avatar_url, enabled, message_limit, pinned_only, last_sync_at, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET display_name=excluded.display_name, identifier=excluded.identifier, username=excluded.username,
			access_hash=excluded.access_hash,
			avatar_url=CASE WHEN excluded.avatar_url <> '' THEN excluded.avatar_url ELSE tg_channels.avatar_url END,
			updated_at=excluded.updated_at`,
		c.ID, c.DisplayName, c.Identifier, c.Username, c.PeerID, c.AccessHash, c.AvatarURL, boolInt(c.Enabled), c.MessageLimit, boolInt(c.PinnedOnly),
		c.LastSyncAt.Format(time.RFC3339Nano), c.LastError, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) UpdateTGChannel(ctx context.Context, c domain.TGChannel) (domain.TGChannel, error) {
	c.UpdatedAt = time.Now().UTC()
	if c.MessageLimit <= 0 {
		c.MessageLimit = 10
	}
	_, err := s.exec(ctx, `UPDATE tg_channels SET display_name=?, identifier=?, username=?, peer_id=?, access_hash=?, avatar_url=?, enabled=?, message_limit=?, pinned_only=?, last_sync_at=?, last_error=?, updated_at=? WHERE id=?`,
		c.DisplayName, c.Identifier, c.Username, c.PeerID, c.AccessHash, c.AvatarURL, boolInt(c.Enabled), c.MessageLimit, boolInt(c.PinnedOnly),
		c.LastSyncAt.Format(time.RFC3339Nano), c.LastError, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	if err != nil {
		return c, err
	}
	return c, s.TrimTGMessages(ctx, c.ID, c.MessageLimit)
}

func (s *Store) DeleteTGChannel(ctx context.Context, id string) error {
	for _, stmt := range []string{`DELETE FROM tg_messages WHERE channel_id=?`, `DELETE FROM tg_channels WHERE id=?`} {
		if _, err := s.exec(ctx, stmt, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteTGMessages(ctx context.Context, channelID string) error {
	_, err := s.exec(ctx, `DELETE FROM tg_messages WHERE channel_id=?`, channelID)
	return err
}

func (s *Store) DeleteAllTGMessages(ctx context.Context) error {
	_, err := s.exec(ctx, `DELETE FROM tg_messages`)
	return err
}

func (s *Store) DeleteTGMessage(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM tg_messages WHERE id=?`, id)
	return err
}

func (s *Store) TrimTGMessages(ctx context.Context, channelID string, limit int) error {
	if limit <= 0 {
		limit = 10
	}
	_, err := s.exec(ctx, `DELETE FROM tg_messages WHERE id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (ORDER BY published_at DESC, remote_id DESC) rn FROM tg_messages WHERE channel_id=?
		) ranked WHERE rn > ?
	)`, channelID, limit)
	return err
}

func (s *Store) TGChannel(ctx context.Context, id string) (domain.TGChannel, error) {
	return s.scanTGChannel(s.row(ctx, tgChannelSelectSQL()+` WHERE id=?`, id))
}

func (s *Store) ListTGChannels(ctx context.Context) ([]domain.TGChannel, error) {
	rows, err := s.query(ctx, tgChannelSelectSQL()+` ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TGChannel{}
	for rows.Next() {
		c, err := scanTGChannelRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) scanTGChannel(row *sql.Row) (domain.TGChannel, error) {
	var c domain.TGChannel
	var enabled, pinnedOnly int
	var lastSync, created, updated string
	err := row.Scan(&c.ID, &c.DisplayName, &c.Identifier, &c.Username, &c.PeerID, &c.AccessHash, &c.AvatarURL, &enabled, &c.MessageLimit, &pinnedOnly, &lastSync, &c.LastError, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.PinnedOnly = boolFromInt(pinnedOnly)
	c.LastSyncAt, c.CreatedAt, c.UpdatedAt = parseTime(lastSync), parseTime(created), parseTime(updated)
	return c, err
}

func scanTGChannelRows(rows *sql.Rows) (domain.TGChannel, error) {
	var c domain.TGChannel
	var enabled, pinnedOnly int
	var lastSync, created, updated string
	err := rows.Scan(&c.ID, &c.DisplayName, &c.Identifier, &c.Username, &c.PeerID, &c.AccessHash, &c.AvatarURL, &enabled, &c.MessageLimit, &pinnedOnly, &lastSync, &c.LastError, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.PinnedOnly = boolFromInt(pinnedOnly)
	c.LastSyncAt, c.CreatedAt, c.UpdatedAt = parseTime(lastSync), parseTime(created), parseTime(updated)
	return c, err
}

func tgChannelSelectSQL() string {
	return `SELECT id, display_name, identifier, username, peer_id, access_hash, avatar_url, enabled, message_limit, pinned_only, last_sync_at, last_error, created_at, updated_at FROM tg_channels`
}

func (s *Store) SaveTGMessage(ctx context.Context, msg domain.TGMessage) (domain.TGMessage, error) {
	if msg.ID == "" {
		msg.ID = NewID()
	}
	now := time.Now().UTC()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	msg.UpdatedAt = now
	_, err := s.exec(ctx, `INSERT INTO tg_messages
		(id, channel_id, remote_id, published_at, text, media_type, media_path, media_url, media_cached, link, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, remote_id) DO UPDATE SET published_at=excluded.published_at, text=excluded.text,
			media_type=excluded.media_type, media_path=excluded.media_path, media_url=excluded.media_url, media_cached=excluded.media_cached,
			link=excluded.link, updated_at=excluded.updated_at`,
		msg.ID, msg.ChannelID, msg.RemoteID, msg.PublishedAt.Format(time.RFC3339Nano), msg.Text, msg.MediaType, msg.MediaPath, msg.MediaURL,
		boolInt(msg.MediaCached), msg.Link, msg.CreatedAt.Format(time.RFC3339Nano), msg.UpdatedAt.Format(time.RFC3339Nano))
	return msg, err
}

func (s *Store) TGMessages(ctx context.Context, channelID string, limit int) ([]domain.TGMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id, channel_id, display_name, remote_id, published_at, text, media_type, media_path, media_url, media_cached, link, created_at, updated_at FROM (
		SELECT m.id, m.channel_id, c.display_name, m.remote_id, m.published_at, m.text, m.media_type, m.media_path, m.media_url, m.media_cached, m.link, m.created_at, m.updated_at,
			ROW_NUMBER() OVER (PARTITION BY m.channel_id ORDER BY m.published_at DESC, m.remote_id DESC) rn, c.message_limit
		FROM tg_messages m JOIN tg_channels c ON c.id=m.channel_id`
	args := []any{}
	if channelID != "" {
		query += ` WHERE m.channel_id=?`
		args = append(args, channelID)
	}
	query += `) ranked WHERE rn <= message_limit ORDER BY published_at DESC, remote_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TGMessage{}
	for rows.Next() {
		var msg domain.TGMessage
		var cached int
		var published, created, updated string
		if err := rows.Scan(&msg.ID, &msg.ChannelID, &msg.ChannelName, &msg.RemoteID, &published, &msg.Text, &msg.MediaType, &msg.MediaPath, &msg.MediaURL, &cached, &msg.Link, &created, &updated); err != nil {
			return nil, err
		}
		msg.PublishedAt, msg.CreatedAt, msg.UpdatedAt = parseTime(published), parseTime(created), parseTime(updated)
		msg.MediaCached = boolFromInt(cached)
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (s *Store) SaveProbe(ctx context.Context, upstreamID, cardID string, p monitor.ProbeResult) (domain.ProbeRun, error) {
	status := p.Status
	if status == "" {
		status = legacyProbeStatus(p.Success)
	}
	success := status == monitor.StatusOperational || status == monitor.StatusDegraded
	run := domain.ProbeRun{
		ID:             NewID(),
		UpstreamID:     upstreamID,
		CardID:         cardID,
		CheckedAt:      time.Now().UTC(),
		Model:          domain.ProbeModel,
		Input:          p.Input,
		ExpectedAnswer: p.ExpectedAnswer,
		Status:         status,
		Output:         p.Output,
		HTTPStatus:     p.HTTPStatus,
		LatencyMS:      int(p.Latency.Milliseconds()),
		Success:        success,
		Error:          p.Error,
	}
	_, err := s.exec(ctx, `INSERT INTO probe_runs (id, upstream_id, card_id, checked_at, model, input, expected_answer, status, output, http_status, latency_ms, success, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.UpstreamID, run.CardID, run.CheckedAt.Format(time.RFC3339Nano), run.Model, run.Input, run.ExpectedAnswer, run.Status, run.Output, run.HTTPStatus, run.LatencyMS, boolInt(run.Success), run.Error)
	return run, err
}

func (s *Store) UpdateCardProbeState(ctx context.Context, id, lastError string, failureCount int) error {
	_, err := s.exec(ctx, `UPDATE model_cards SET last_error=?, failure_count=?, updated_at=? WHERE id=?`, lastError, failureCount, nowText(), id)
	return err
}

func (s *Store) UpdateCardSchedulerAutoDisabled(ctx context.Context, id string, disabled bool) error {
	_, err := s.exec(ctx, `UPDATE model_cards SET scheduler_auto_disabled=?, updated_at=? WHERE id=?`, boolInt(disabled), nowText(), id)
	return err
}

func (s *Store) RecentProbesForCard(ctx context.Context, cardID string, limit int) ([]domain.ProbeRun, error) {
	rows, err := s.query(ctx, `SELECT id, upstream_id, card_id, checked_at, model, input, expected_answer, status, output, http_status, latency_ms, success, error
		FROM probe_runs WHERE card_id=? ORDER BY checked_at DESC LIMIT ?`, cardID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProbeRun
	for rows.Next() {
		p, err := scanProbeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ProbesForCardSince(ctx context.Context, cardID string, since time.Time, limit int) ([]domain.ProbeRun, error) {
	timeFilter := "checked_at>=?"
	if s.Driver == "sqlite" {
		timeFilter = "unixepoch(checked_at)>=unixepoch(?)"
	}
	query := `SELECT id, upstream_id, card_id, checked_at, model, input, expected_answer, status, output, http_status, latency_ms, success, error
		FROM probe_runs WHERE card_id=? AND ` + timeFilter + ` ORDER BY checked_at DESC`
	args := []any{cardID, since.UTC().Format(time.RFC3339Nano)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProbeRun
	for rows.Next() {
		p, err := scanProbeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProbeRows(rows *sql.Rows) (domain.ProbeRun, error) {
	var p domain.ProbeRun
	var checked string
	var success int
	err := rows.Scan(&p.ID, &p.UpstreamID, &p.CardID, &checked, &p.Model, &p.Input, &p.ExpectedAnswer, &p.Status, &p.Output, &p.HTTPStatus, &p.LatencyMS, &success, &p.Error)
	p.CheckedAt = parseTime(checked)
	if p.Status == "" {
		p.Status = legacyProbeStatus(boolFromInt(success))
	}
	p.Success = p.Status == monitor.StatusOperational || p.Status == monitor.StatusDegraded
	return p, err
}

func legacyProbeStatus(success bool) string {
	if success {
		return monitor.StatusOperational
	}
	return monitor.StatusFailed
}

func (s *Store) AlertState(ctx context.Context, upstreamID, kind string) (domain.AlertState, error) {
	var recover int
	var created string
	err := s.row(ctx, `SELECT recover, created_at FROM alert_events WHERE upstream_id=? AND type=? ORDER BY created_at DESC LIMIT 1`, upstreamID, kind).Scan(&recover, &created)
	if errors.Is(err, sql.ErrNoRows) || recover != 0 {
		return domain.AlertState{}, nil
	}
	if err != nil {
		return domain.AlertState{}, err
	}
	return domain.AlertState{Active: true, LastAt: parseTime(created)}, nil
}

func (s *Store) SaveAlert(ctx context.Context, upstreamID string, dec domain.AlertDecision, sent bool) error {
	_, err := s.exec(ctx, `INSERT INTO alert_events (id, upstream_id, type, recover, sent, message, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		NewID(), upstreamID, dec.Type, boolInt(dec.Recover), boolInt(sent), dec.Message, nowText())
	return err
}

func (s *Store) SaveBalanceRechargeLog(ctx context.Context, log domain.BalanceRechargeLog) (domain.BalanceRechargeLog, error) {
	if log.ID == "" {
		log.ID = NewID()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO balance_recharge_logs
		(id, upstream_id, method, amount, payment_type, remote_order_id, status, message, raw_status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.UpstreamID, log.Method, log.Amount, log.PaymentType, log.RemoteOrderID, log.Status, log.Message, log.RawStatus, log.CreatedAt.Format(time.RFC3339Nano))
	return log, err
}

func (s *Store) BalanceRechargeLogs(ctx context.Context, upstreamID string, limit int) ([]domain.BalanceRechargeLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.query(ctx, `SELECT id, upstream_id, method, amount, payment_type, remote_order_id, status, message, raw_status, created_at
		FROM balance_recharge_logs WHERE upstream_id=? ORDER BY created_at DESC LIMIT ?`, upstreamID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.BalanceRechargeLog{}
	for rows.Next() {
		var log domain.BalanceRechargeLog
		var created string
		if err := rows.Scan(&log.ID, &log.UpstreamID, &log.Method, &log.Amount, &log.PaymentType, &log.RemoteOrderID, &log.Status, &log.Message, &log.RawStatus, &created); err != nil {
			return nil, err
		}
		log.CreatedAt = parseTime(created)
		out = append(out, log)
	}
	return out, rows.Err()
}

func (s *Store) BalanceRechargeLog(ctx context.Context, upstreamID, id string) (domain.BalanceRechargeLog, error) {
	var log domain.BalanceRechargeLog
	var created string
	err := s.row(ctx, `SELECT id, upstream_id, method, amount, payment_type, remote_order_id, status, message, raw_status, created_at
		FROM balance_recharge_logs WHERE upstream_id=? AND id=?`, upstreamID, id).
		Scan(&log.ID, &log.UpstreamID, &log.Method, &log.Amount, &log.PaymentType, &log.RemoteOrderID, &log.Status, &log.Message, &log.RawStatus, &created)
	if err != nil {
		return domain.BalanceRechargeLog{}, err
	}
	log.CreatedAt = parseTime(created)
	return log, nil
}

func (s *Store) UpdateBalanceRechargeLog(ctx context.Context, log domain.BalanceRechargeLog) error {
	_, err := s.exec(ctx, `UPDATE balance_recharge_logs SET status=?, message=?, raw_status=? WHERE id=? AND upstream_id=?`,
		log.Status, log.Message, log.RawStatus, log.ID, log.UpstreamID)
	return err
}

func (s *Store) DeleteBalanceRechargeLog(ctx context.Context, upstreamID, id string) error {
	_, err := s.exec(ctx, `DELETE FROM balance_recharge_logs WHERE upstream_id=? AND id=?`, upstreamID, id)
	return err
}

func (s *Store) MigrationDone(ctx context.Context, source string) (bool, error) {
	var one int
	err := s.row(ctx, `SELECT 1 FROM migration_records WHERE source=?`, source).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) MarkMigration(ctx context.Context, source string) error {
	_, err := s.exec(ctx, `INSERT INTO migration_records (source, migrated_at) VALUES (?, ?) ON CONFLICT(source) DO NOTHING`, source, nowText())
	return err
}

func (s *Store) MigratePocketBase(ctx context.Context, oldPath string) error {
	if oldPath == "" {
		oldPath = "/app/pb_data/data.db"
	}
	if _, err := os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	source := "pocketbase:" + oldPath
	done, err := s.MigrationDone(ctx, source)
	if err != nil || done {
		return err
	}
	if s.Driver == "sqlite" {
		return s.migratePocketBaseSQLite(ctx, oldPath, source)
	}
	oldDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(oldPath)+"?mode=ro&cache=shared")
	if err != nil {
		return err
	}
	defer oldDB.Close()
	if err := copyPBTable(ctx, oldDB, s, "upstreams", "upstreams", map[string]string{}); err != nil {
		return err
	}
	if err := copyPBTable(ctx, oldDB, s, "upstream_keys", "api_keys", map[string]string{"upstream": "upstream_id", "group": "group_name"}); err != nil {
		return err
	}
	if err := copyPBTable(ctx, oldDB, s, "model_cards", "model_cards", map[string]string{"upstream": "upstream_id", "key": "key_id"}); err != nil {
		return err
	}
	for _, table := range []string{"balance_snapshots", "probe_runs", "alert_events"} {
		if err := copyPBTable(ctx, oldDB, s, table, table, map[string]string{"upstream": "upstream_id", "card": "card_id"}); err != nil {
			return err
		}
	}
	if err := migratePBSettings(ctx, oldDB, s); err != nil {
		return err
	}
	return s.MarkMigration(ctx, source)
}

func (s *Store) migratePocketBaseSQLite(ctx context.Context, oldPath, source string) error {
	oldDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(oldPath)+"?mode=ro&cache=shared")
	if err != nil {
		return err
	}
	defer oldDB.Close()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=10000"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE `+quoteSQLString("file:"+filepath.ToSlash(oldPath)+"?mode=ro&cache=shared")+` AS pb`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE pb`) //nolint:errcheck
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.Background(), `ROLLBACK`) //nolint:errcheck
		}
	}()
	if err := copyPBTableSQLite(ctx, conn, oldDB, "upstreams", "upstreams", map[string]string{}); err != nil {
		return err
	}
	if err := copyPBTableSQLite(ctx, conn, oldDB, "upstream_keys", "api_keys", map[string]string{"upstream": "upstream_id", "group": "group_name"}); err != nil {
		return err
	}
	if err := copyPBTableSQLite(ctx, conn, oldDB, "model_cards", "model_cards", map[string]string{"upstream": "upstream_id", "key": "key_id"}); err != nil {
		return err
	}
	for _, table := range []string{"balance_snapshots", "probe_runs", "alert_events"} {
		if err := copyPBTableSQLite(ctx, conn, oldDB, table, table, map[string]string{"upstream": "upstream_id", "card": "card_id"}); err != nil {
			return err
		}
	}
	if err := migratePBSettingsSQLite(ctx, conn, oldDB); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO migration_records (source, migrated_at) VALUES (?, ?) ON CONFLICT(source) DO NOTHING`, source, nowText()); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

type tableQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type tableRowQueryer interface {
	tableQueryer
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func tableExists(ctx context.Context, db tableRowQueryer, table string) bool {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	return err == nil
}

func tableColumns(ctx context.Context, db tableQueryer, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+quoteIdent(table)+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteIdents(cols []string) string {
	out := make([]string, len(cols))
	for i, col := range cols {
		out[i] = quoteIdent(col)
	}
	return strings.Join(out, ",")
}

func quoteSQLString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func copyPBTableSQLite(ctx context.Context, conn *sql.Conn, oldDB *sql.DB, sourceTable, dstTable string, aliases map[string]string) error {
	if !tableExists(ctx, oldDB, sourceTable) {
		return nil
	}
	oldCols, err := tableColumns(ctx, oldDB, sourceTable)
	if err != nil {
		return err
	}
	dstCols, err := tableColumns(ctx, conn, dstTable)
	if err != nil {
		return err
	}
	var selectExprs, insertCols []string
	for oldCol := range oldCols {
		newCol := oldCol
		if v := aliases[oldCol]; v != "" {
			newCol = v
		}
		if dstCols[newCol] {
			selectExprs = append(selectExprs, quoteIdent(oldCol))
			insertCols = append(insertCols, newCol)
		}
	}
	for _, col := range []string{"created_at", "updated_at"} {
		if dstCols[col] && !oldCols[col] {
			selectExprs = append(selectExprs, quoteSQLString(nowText()))
			insertCols = append(insertCols, col)
		}
	}
	if len(insertCols) == 0 {
		return nil
	}
	_, err = conn.ExecContext(ctx, fmt.Sprintf(
		`INSERT OR IGNORE INTO %s (%s) SELECT %s FROM pb.%s`,
		quoteIdent(dstTable), quoteIdents(insertCols), strings.Join(selectExprs, ","), quoteIdent(sourceTable),
	))
	return err
}

func copyPBTable(ctx context.Context, oldDB *sql.DB, dst *Store, sourceTable, dstTable string, aliases map[string]string) error {
	if !tableExists(ctx, oldDB, sourceTable) {
		return nil
	}
	oldCols, err := tableColumns(ctx, oldDB, sourceTable)
	if err != nil {
		return err
	}
	dstCols, err := tableColumns(ctx, dst.DB, dstTable)
	if err != nil {
		return err
	}
	var selectCols, insertCols, defaultCols []string
	for oldCol := range oldCols {
		newCol := oldCol
		if v := aliases[oldCol]; v != "" {
			newCol = v
		}
		if dstCols[newCol] {
			selectCols = append(selectCols, oldCol)
			insertCols = append(insertCols, newCol)
		}
	}
	for _, col := range []string{"created_at", "updated_at"} {
		if dstCols[col] && !oldCols[col] {
			insertCols = append(insertCols, col)
			defaultCols = append(defaultCols, col)
		}
	}
	if len(insertCols) == 0 {
		return nil
	}
	var realSelectCols, quotedSelectCols []string
	for _, col := range selectCols {
		realSelectCols = append(realSelectCols, col)
		quotedSelectCols = append(quotedSelectCols, quoteIdent(col))
	}
	rows, err := oldDB.QueryContext(ctx, `SELECT `+strings.Join(quotedSelectCols, ",")+` FROM `+quoteIdent(sourceTable))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals := make([]any, len(realSelectCols))
		ptrs := make([]any, len(realSelectCols))
		for i := range realSelectCols {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		for range defaultCols {
			vals = append(vals, nowText())
		}
		q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(id) DO NOTHING`,
			quoteIdent(dstTable), quoteIdents(insertCols), strings.TrimRight(strings.Repeat("?,", len(insertCols)), ","))
		if _, err := dst.exec(ctx, q, vals...); err != nil {
			return err
		}
	}
	return rows.Err()
}

func migratePBSettingsSQLite(ctx context.Context, conn *sql.Conn, oldDB *sql.DB) error {
	if !tableExists(ctx, oldDB, "settings") {
		return nil
	}
	cols, err := tableColumns(ctx, oldDB, "settings")
	if err != nil {
		return err
	}
	pick := func(name string) string {
		if cols[name] {
			return quoteIdent(name)
		}
		return "''"
	}
	row := oldDB.QueryRowContext(ctx, `SELECT `+pick("telegram_bot_token")+`, `+pick("telegram_chat_id")+`, `+pick("check_interval_minutes")+` FROM settings LIMIT 1`)
	var token, chat string
	var interval any
	if err := row.Scan(&token, &chat, &interval); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	minutes := 5
	switch v := interval.(type) {
	case int64:
		minutes = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			minutes = n
		}
	}
	_, err = conn.ExecContext(ctx, `UPDATE settings SET telegram_bot_token=?, telegram_chat_id=?, check_interval_minutes=?, probe_model=? WHERE id='default'`,
		token, chat, domain.NormalizeCheckInterval(minutes), domain.ProbeModel)
	return err
}

func migratePBSettings(ctx context.Context, oldDB *sql.DB, dst *Store) error {
	if !tableExists(ctx, oldDB, "settings") {
		return nil
	}
	cols, err := tableColumns(ctx, oldDB, "settings")
	if err != nil {
		return err
	}
	pick := func(name string) string {
		if cols[name] {
			return name
		}
		return "''"
	}
	row := oldDB.QueryRowContext(ctx, `SELECT `+pick("telegram_bot_token")+`, `+pick("telegram_chat_id")+`, `+pick("check_interval_minutes")+` FROM settings LIMIT 1`)
	var token, chat string
	var interval any
	if err := row.Scan(&token, &chat, &interval); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	minutes := 5
	switch v := interval.(type) {
	case int64:
		minutes = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			minutes = n
		}
	}
	_, err = dst.exec(ctx, `UPDATE settings SET telegram_bot_token=?, telegram_chat_id=?, check_interval_minutes=?, probe_model=? WHERE id='default'`,
		token, chat, domain.NormalizeCheckInterval(minutes), domain.ProbeModel)
	return err
}
