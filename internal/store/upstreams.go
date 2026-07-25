package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

func (s *Store) CreateUpstream(ctx context.Context, u domain.Upstream) (domain.Upstream, error) {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.BalanceRate <= 0 {
		u.BalanceRate = 1
	}
	if u.RunwayWarningHours <= 0 {
		u.RunwayWarningHours = 24
	}
	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO upstreams
		(id, name, type, base_url, enabled, user_id, access_token, email, password, sub2api_access_token, sub2api_refresh_token,
		balance_rate, low_balance_threshold, runway_warning_hours,
		last_error, failure_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Name, u.Type, u.BaseURL, boolInt(u.Enabled), u.UserID, u.AccessToken, u.Email, u.Password,
		u.Sub2APIAccessToken, u.Sub2APIRefreshToken, u.BalanceRate, u.LowBalanceThreshold,
		u.RunwayWarningHours, u.LastError, u.FailureCount,
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano))
	return u, err
}

func (s *Store) UpdateUpstream(ctx context.Context, u domain.Upstream) (domain.Upstream, error) {
	u.UpdatedAt = time.Now().UTC()
	if u.BalanceRate <= 0 {
		u.BalanceRate = 1
	}
	if u.RunwayWarningHours <= 0 {
		u.RunwayWarningHours = 24
	}
	_, err := s.exec(ctx, `UPDATE upstreams SET name=?, type=?, base_url=?, enabled=?, user_id=?, access_token=?, email=?, password=?,
		sub2api_access_token=?, sub2api_refresh_token=?, balance_rate=?, low_balance_threshold=?,
		runway_warning_hours=?, last_error=?, failure_count=?, updated_at=? WHERE id=?`,
		u.Name, u.Type, u.BaseURL, boolInt(u.Enabled), u.UserID, u.AccessToken, u.Email, u.Password, u.Sub2APIAccessToken,
		u.Sub2APIRefreshToken, u.BalanceRate, u.LowBalanceThreshold,
		u.RunwayWarningHours, u.LastError, u.FailureCount, u.UpdatedAt.Format(time.RFC3339Nano), u.ID)
	return u, err
}

func (s *Store) DeleteUpstream(ctx context.Context, id string) error {
	for _, stmt := range []string{
		`DELETE FROM scheduler_cost_bindings WHERE upstream_id=?`,
		`DELETE FROM api_keys WHERE upstream_id=?`,
		`DELETE FROM balance_snapshots WHERE upstream_id=?`,
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
		sub2api_access_token, sub2api_refresh_token, balance_rate, low_balance_threshold,
		runway_warning_hours, last_error, failure_count, created_at, updated_at
		FROM upstreams WHERE id=?`, id))
}

func (s *Store) ListUpstreams(ctx context.Context) ([]domain.Upstream, error) {
	rows, err := s.query(ctx, `SELECT id, name, type, base_url, enabled, user_id, access_token, email, password,
		sub2api_access_token, sub2api_refresh_token, balance_rate, low_balance_threshold,
		runway_warning_hours, last_error, failure_count, created_at, updated_at
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
		&u.Sub2APIAccessToken, &u.Sub2APIRefreshToken, &u.BalanceRate, &u.LowBalanceThreshold,
		&u.RunwayWarningHours, &u.LastError, &u.FailureCount, &created, &updated)
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
		&u.Sub2APIAccessToken, &u.Sub2APIRefreshToken, &u.BalanceRate, &u.LowBalanceThreshold,
		&u.RunwayWarningHours, &u.LastError, &u.FailureCount, &created, &updated)
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
