package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
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
			telegram_chat_id TEXT NOT NULL DEFAULT '', probe_model TEXT NOT NULL DEFAULT 'gpt-5.5'
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
			id TEXT PRIMARY KEY, name TEXT NOT NULL, upstream_id TEXT NOT NULL, key_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, last_error TEXT NOT NULL DEFAULT '',
			failure_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS balance_snapshots (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, checked_at TEXT NOT NULL, balance REAL NOT NULL DEFAULT 0,
			used REAL NOT NULL DEFAULT 0, remain REAL NOT NULL DEFAULT 0, requests INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '', latency_ms INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS probe_runs (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL,
			model TEXT NOT NULL, input TEXT NOT NULL DEFAULT 'ping', http_status INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS alert_events (
			id TEXT PRIMARY KEY, upstream_id TEXT NOT NULL, type TEXT NOT NULL, recover INTEGER NOT NULL DEFAULT 0,
			sent INTEGER NOT NULL DEFAULT 0, message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_upstream ON api_keys(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cards_upstream ON model_cards(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_probe_card_time ON probe_runs(card_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_balance_upstream_time ON balance_snapshots(upstream_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state ON alert_events(upstream_id, type, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.DB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, err := s.exec(ctx, `INSERT INTO settings (id, check_interval_minutes, probe_model) VALUES ('default', 5, ?) ON CONFLICT(id) DO NOTHING`, domain.ProbeModel)
	return err
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
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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
	err := s.row(ctx, `SELECT check_interval_minutes, telegram_bot_token, telegram_chat_id, probe_model FROM settings WHERE id='default'`).
		Scan(&cfg.CheckIntervalMinutes, &cfg.TelegramBotToken, &cfg.TelegramChatID, &cfg.ProbeModel)
	if err != nil {
		return cfg, err
	}
	cfg.CheckIntervalMinutes = domain.NormalizeCheckInterval(cfg.CheckIntervalMinutes)
	cfg.ProbeModel = domain.ProbeModel
	return cfg, nil
}

func (s *Store) UpdateSettings(ctx context.Context, cfg domain.Settings) (domain.Settings, error) {
	cfg.CheckIntervalMinutes = domain.NormalizeCheckInterval(cfg.CheckIntervalMinutes)
	cfg.ProbeModel = domain.ProbeModel
	_, err := s.exec(ctx, `UPDATE settings SET check_interval_minutes=?, telegram_bot_token=?, telegram_chat_id=?, probe_model=? WHERE id='default'`,
		cfg.CheckIntervalMinutes, cfg.TelegramBotToken, cfg.TelegramChatID, cfg.ProbeModel)
	return cfg, err
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
	var out []domain.APIKey
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
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO model_cards (id, name, upstream_id, key_id, model, enabled, last_error, failure_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.UpstreamID, c.KeyID, c.Model, boolInt(c.Enabled), c.LastError, c.FailureCount, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) UpdateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	c.Model = domain.ProbeModel
	c.UpdatedAt = time.Now().UTC()
	_, err := s.exec(ctx, `UPDATE model_cards SET name=?, upstream_id=?, key_id=?, model=?, enabled=?, last_error=?, failure_count=?, updated_at=? WHERE id=?`,
		c.Name, c.UpstreamID, c.KeyID, c.Model, boolInt(c.Enabled), c.LastError, c.FailureCount, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	return c, err
}

func (s *Store) DeleteCard(ctx context.Context, id string) error {
	if _, err := s.exec(ctx, `DELETE FROM probe_runs WHERE card_id=?`, id); err != nil {
		return err
	}
	_, err := s.exec(ctx, `DELETE FROM model_cards WHERE id=?`, id)
	return err
}

func (s *Store) Card(ctx context.Context, id string) (domain.ModelCard, error) {
	return s.scanCard(s.row(ctx, `SELECT id, name, upstream_id, key_id, model, enabled, last_error, failure_count, created_at, updated_at FROM model_cards WHERE id=?`, id))
}

func (s *Store) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	rows, err := s.query(ctx, `SELECT id, name, upstream_id, key_id, model, enabled, last_error, failure_count, created_at, updated_at FROM model_cards ORDER BY name`)
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
	var enabled int
	var created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.UpstreamID, &c.KeyID, &c.Model, &enabled, &c.LastError, &c.FailureCount, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func scanCardRows(rows *sql.Rows) (domain.ModelCard, error) {
	var c domain.ModelCard
	var enabled int
	var created, updated string
	err := rows.Scan(&c.ID, &c.Name, &c.UpstreamID, &c.KeyID, &c.Model, &enabled, &c.LastError, &c.FailureCount, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func (s *Store) SaveProbe(ctx context.Context, upstreamID, cardID string, p monitor.ProbeResult) (domain.ProbeRun, error) {
	run := domain.ProbeRun{
		ID:         NewID(),
		UpstreamID: upstreamID,
		CardID:     cardID,
		CheckedAt:  time.Now().UTC(),
		Model:      domain.ProbeModel,
		Input:      "ping",
		HTTPStatus: p.HTTPStatus,
		LatencyMS:  int(p.Latency.Milliseconds()),
		Success:    p.Success,
		Error:      p.Error,
	}
	_, err := s.exec(ctx, `INSERT INTO probe_runs (id, upstream_id, card_id, checked_at, model, input, http_status, latency_ms, success, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.UpstreamID, run.CardID, run.CheckedAt.Format(time.RFC3339Nano), run.Model, run.Input, run.HTTPStatus, run.LatencyMS, boolInt(run.Success), run.Error)
	return run, err
}

func (s *Store) UpdateCardProbeState(ctx context.Context, id, lastError string, failureCount int) error {
	_, err := s.exec(ctx, `UPDATE model_cards SET last_error=?, failure_count=?, updated_at=? WHERE id=?`, lastError, failureCount, nowText(), id)
	return err
}

func (s *Store) ProbesForCardSince(ctx context.Context, cardID string, since time.Time, limit int) ([]domain.ProbeRun, error) {
	rows, err := s.query(ctx, `SELECT id, upstream_id, card_id, checked_at, model, input, http_status, latency_ms, success, error
		FROM probe_runs WHERE card_id=? AND checked_at>=? ORDER BY checked_at DESC LIMIT ?`, cardID, since.Format(time.RFC3339Nano), limit)
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
	err := rows.Scan(&p.ID, &p.UpstreamID, &p.CardID, &checked, &p.Model, &p.Input, &p.HTTPStatus, &p.LatencyMS, &success, &p.Error)
	p.CheckedAt = parseTime(checked)
	p.Success = boolFromInt(success)
	return p, err
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

func tableExists(ctx context.Context, db *sql.DB, table string) bool {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	return err == nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
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
	var selectCols, insertCols []string
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
	if len(insertCols) == 0 {
		return nil
	}
	rows, err := oldDB.QueryContext(ctx, `SELECT `+strings.Join(selectCols, ",")+` FROM `+sourceTable)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals := make([]any, len(selectCols))
		ptrs := make([]any, len(selectCols))
		for i := range vals {
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
		q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(id) DO NOTHING`,
			dstTable, strings.Join(insertCols, ","), strings.TrimRight(strings.Repeat("?,", len(insertCols)), ","))
		if _, err := dst.exec(ctx, q, vals...); err != nil {
			return err
		}
	}
	return rows.Err()
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
