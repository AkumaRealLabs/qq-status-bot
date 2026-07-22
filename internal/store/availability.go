package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) AvailabilityPolicy(ctx context.Context, upstreamID string) (domain.AvailabilityPolicy, error) {
	var policy domain.AvailabilityPolicy
	err := s.row(ctx, `SELECT low_balance_threshold, balance_guard_mode, balance_close_threshold, balance_recover_threshold, runway_warning_hours FROM upstreams WHERE id=?`, upstreamID).
		Scan(&policy.LowBalanceThreshold, &policy.BalanceGuardMode, &policy.BalanceCloseThreshold, &policy.BalanceRecoverThreshold, &policy.RunwayWarningHours)
	return policy.Normalized(), err
}

func (s *Store) UpdateAvailabilityPolicy(ctx context.Context, upstreamID string, policy domain.AvailabilityPolicy) (domain.AvailabilityPolicy, error) {
	policy = policy.Normalized()
	_, err := s.exec(ctx, `UPDATE upstreams SET low_balance_threshold=?, balance_guard_mode=?, balance_close_threshold=?, balance_recover_threshold=?, runway_warning_hours=?, updated_at=? WHERE id=?`,
		policy.LowBalanceThreshold, policy.BalanceGuardMode, policy.BalanceCloseThreshold, policy.BalanceRecoverThreshold, policy.RunwayWarningHours, nowText(), upstreamID)
	return policy, err
}

func (s *Store) ChannelAvailability(ctx context.Context, channelID string) (domain.ChannelAvailability, bool, error) {
	row, err := s.scanChannelAvailability(s.row(ctx, availabilitySelect+` WHERE channel_id=?`, channelID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ChannelAvailability{}, false, nil
	}
	return row, err == nil, err
}

func (s *Store) ChannelAvailabilities(ctx context.Context, upstreamID, state string) ([]domain.ChannelAvailability, error) {
	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(upstreamID) != "" {
		where = append(where, "upstream_id=?")
		args = append(args, strings.TrimSpace(upstreamID))
	}
	rows, err := s.query(ctx, availabilitySelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ChannelAvailability{}
	for rows.Next() {
		row, err := scanChannelAvailabilityRows(rows)
		if err != nil {
			return nil, err
		}
		if state != "" {
			// 展示状态由 domain 推导；此处先保留持久行，app 层会套策略筛选。
			_ = state
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SaveChannelAvailabilityCAS 使用乐观锁串行化同一渠道的策略、人工操作和后台重放。
// expectedVersion=0 代表仅在尚不存在记录时创建。
func (s *Store) SaveChannelAvailabilityCAS(ctx context.Context, row domain.ChannelAvailability, expectedVersion int64) (bool, error) {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	if row.ChannelID == "" {
		return false, errors.New("channel_id is required")
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}
	row.Blockers = domain.NormalizeBlockers(row.Blockers)
	blockers, err := json.Marshal(row.Blockers)
	if err != nil {
		return false, err
	}
	if expectedVersion == 0 {
		row.Version = 1
		result, err := s.exec(ctx, `INSERT INTO channel_availability
			(channel_id, channel_name, card_id, card_name, upstream_id, upstream_name, managed, blockers, desired_status, actual_status,
			disabled_at, recovery_success_count, override_kind, override_until, pending_action, pending_status, retry_at, retry_count, last_error, version, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(channel_id) DO NOTHING`,
			row.ChannelID, row.ChannelName, row.CardID, row.CardName, row.UpstreamID, row.UpstreamName, boolInt(row.Managed), string(blockers), row.DesiredStatus, row.ActualStatus,
			timeText(row.DisabledAt), row.RecoverySuccess, row.Override, timeText(row.OverrideUntil), row.PendingAction, row.PendingStatus, timeText(row.RetryAt), row.RetryCount, row.LastError, row.Version, row.UpdatedAt.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		return n == 1, err
	}
	row.Version = expectedVersion + 1
	result, err := s.exec(ctx, `UPDATE channel_availability SET
		channel_name=?, card_id=?, card_name=?, upstream_id=?, upstream_name=?, managed=?, blockers=?, desired_status=?, actual_status=?,
		disabled_at=?, recovery_success_count=?, override_kind=?, override_until=?, pending_action=?, pending_status=?, retry_at=?, retry_count=?, last_error=?, version=?, updated_at=?
		WHERE channel_id=? AND version=?`,
		row.ChannelName, row.CardID, row.CardName, row.UpstreamID, row.UpstreamName, boolInt(row.Managed), string(blockers), row.DesiredStatus, row.ActualStatus,
		timeText(row.DisabledAt), row.RecoverySuccess, row.Override, timeText(row.OverrideUntil), row.PendingAction, row.PendingStatus, timeText(row.RetryAt), row.RetryCount, row.LastError, row.Version, row.UpdatedAt.UTC().Format(time.RFC3339Nano), row.ChannelID, expectedVersion)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) DeleteChannelAvailabilityForUpstream(ctx context.Context, upstreamID string) error {
	_, err := s.exec(ctx, `DELETE FROM channel_availability WHERE upstream_id=?`, upstreamID)
	return err
}

func (s *Store) DeleteChannelAvailabilityBinding(ctx context.Context, channelID, cardID string) error {
	_, err := s.exec(ctx, `DELETE FROM channel_availability WHERE channel_id=? AND card_id=?`, strings.TrimSpace(channelID), strings.TrimSpace(cardID))
	return err
}

const availabilitySelect = `SELECT channel_id, channel_name, card_id, card_name, upstream_id, upstream_name, managed, blockers, desired_status, actual_status,
	disabled_at, recovery_success_count, override_kind, override_until, pending_action, pending_status, retry_at, retry_count, last_error, version, updated_at FROM channel_availability`

func (s *Store) scanChannelAvailability(row *sql.Row) (domain.ChannelAvailability, error) {
	var out domain.ChannelAvailability
	var managed int
	var blockers, disabledAt, overrideUntil, retryAt, updated string
	err := row.Scan(&out.ChannelID, &out.ChannelName, &out.CardID, &out.CardName, &out.UpstreamID, &out.UpstreamName, &managed, &blockers, &out.DesiredStatus, &out.ActualStatus,
		&disabledAt, &out.RecoverySuccess, &out.Override, &overrideUntil, &out.PendingAction, &out.PendingStatus, &retryAt, &out.RetryCount, &out.LastError, &out.Version, &updated)
	if err != nil {
		return out, err
	}
	return hydrateChannelAvailability(out, managed, blockers, disabledAt, overrideUntil, retryAt, updated), nil
}

func scanChannelAvailabilityRows(rows *sql.Rows) (domain.ChannelAvailability, error) {
	var out domain.ChannelAvailability
	var managed int
	var blockers, disabledAt, overrideUntil, retryAt, updated string
	err := rows.Scan(&out.ChannelID, &out.ChannelName, &out.CardID, &out.CardName, &out.UpstreamID, &out.UpstreamName, &managed, &blockers, &out.DesiredStatus, &out.ActualStatus,
		&disabledAt, &out.RecoverySuccess, &out.Override, &overrideUntil, &out.PendingAction, &out.PendingStatus, &retryAt, &out.RetryCount, &out.LastError, &out.Version, &updated)
	if err != nil {
		return out, err
	}
	return hydrateChannelAvailability(out, managed, blockers, disabledAt, overrideUntil, retryAt, updated), nil
}

func hydrateChannelAvailability(out domain.ChannelAvailability, managed int, blockers, disabledAt, overrideUntil, retryAt, updated string) domain.ChannelAvailability {
	out.Managed = boolFromInt(managed)
	_ = json.Unmarshal([]byte(blockers), &out.Blockers)
	out.Blockers = domain.NormalizeBlockers(out.Blockers)
	out.DisabledAt = timePtr(disabledAt)
	out.OverrideUntil = timePtr(overrideUntil)
	out.RetryAt = timePtr(retryAt)
	out.UpdatedAt = parseTime(updated)
	return out
}

func timeText(v *time.Time) string {
	if v == nil || v.IsZero() {
		return ""
	}
	return v.UTC().Format(time.RFC3339Nano)
}

func timePtr(text string) *time.Time {
	parsed := parseTime(text)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}
