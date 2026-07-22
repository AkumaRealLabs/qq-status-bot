package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) AxonHubConfig(ctx context.Context) (domain.AxonHubConfig, error) {
	var cfg domain.AxonHubConfig
	err := s.row(ctx, `SELECT axonhub_base_url, axonhub_api_key, axonhub_control_mode FROM settings WHERE id='default'`).
		Scan(&cfg.BaseURL, &cfg.APIKey, &cfg.ControlMode)
	cfg.ControlMode = domain.NormalizeAxonHubControlMode(cfg.ControlMode)
	return cfg, err
}

func (s *Store) UpdateAxonHubConfig(ctx context.Context, cfg domain.AxonHubConfig) (domain.AxonHubConfig, error) {
	old, err := s.AxonHubConfig(ctx)
	if err != nil {
		return cfg, err
	}
	cfg = cfg.MergeUpdate(old)
	_, err = s.exec(ctx, `UPDATE settings SET axonhub_base_url=?, axonhub_api_key=?, axonhub_control_mode=? WHERE id='default'`,
		cfg.BaseURL, cfg.APIKey, cfg.ControlMode)
	return cfg, err
}

func (s *Store) UpdateSchedulerProvider(ctx context.Context, provider string) error {
	_, err := s.exec(ctx, `UPDATE settings SET scheduler_provider=? WHERE id='default'`, domain.NormalizeSchedulerProvider(provider))
	return err
}

func (s *Store) AxonHubChannelLifecycle(ctx context.Context, channelID string) (domain.AxonHubChannelLifecycle, bool, error) {
	row, err := scanAxonHubLifecycle(s.row(ctx, axonHubLifecycleSelect+` WHERE channel_id=?`, channelID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AxonHubChannelLifecycle{}, false, nil
	}
	return row, err == nil, err
}

func (s *Store) AxonHubChannelLifecycles(ctx context.Context) ([]domain.AxonHubChannelLifecycle, error) {
	rows, err := s.query(ctx, axonHubLifecycleSelect+` ORDER BY channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AxonHubChannelLifecycle{}
	for rows.Next() {
		row, err := scanAxonHubLifecycle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) SaveAxonHubChannelLifecycle(ctx context.Context, row domain.AxonHubChannelLifecycle) error {
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}
	tags, err := json.Marshal(row.RemoteTags)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `INSERT INTO scheduler_axonhub_channel_lifecycle
		(channel_id, channel_name, remote_status, remote_tags, remote_managed_tag, remote_weight, desired_tag, desired_weight,
		owner, external_takeover, aum_disabled, aum_disabled_at, last_aum_status, last_aum_tag, last_aum_weight,
		last_aum_write_at, last_source, last_reason, pending_action, pending_status, pending_tag, pending_weight,
		retry_at, retry_count, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET channel_name=?, remote_status=?, remote_tags=?, remote_managed_tag=?, remote_weight=?,
		desired_tag=?, desired_weight=?, owner=?, external_takeover=?, aum_disabled=?, aum_disabled_at=?, last_aum_status=?,
		last_aum_tag=?, last_aum_weight=?, last_aum_write_at=?, last_source=?, last_reason=?, pending_action=?, pending_status=?,
		pending_tag=?, pending_weight=?, retry_at=?, retry_count=?, last_error=?, updated_at=?`,
		row.ChannelID, row.ChannelName, row.RemoteStatus, string(tags), row.RemoteManagedTag, row.RemoteWeight, row.DesiredTag, row.DesiredWeight,
		row.Owner, boolInt(row.ExternalTakeover), boolInt(row.AUMDisabled), timeTextValue(row.AUMDisabledAt), row.LastAUMStatus,
		row.LastAUMTag, row.LastAUMWeight, timeTextValue(row.LastAUMWriteAt), row.LastSource, row.LastReason, row.PendingAction,
		row.PendingStatus, row.PendingTag, row.PendingWeight, timeTextValue(row.RetryAt), row.RetryCount, row.LastError, row.UpdatedAt.UTC().Format(time.RFC3339Nano),
		row.ChannelName, row.RemoteStatus, string(tags), row.RemoteManagedTag, row.RemoteWeight, row.DesiredTag, row.DesiredWeight,
		row.Owner, boolInt(row.ExternalTakeover), boolInt(row.AUMDisabled), timeTextValue(row.AUMDisabledAt), row.LastAUMStatus,
		row.LastAUMTag, row.LastAUMWeight, timeTextValue(row.LastAUMWriteAt), row.LastSource, row.LastReason, row.PendingAction,
		row.PendingStatus, row.PendingTag, row.PendingWeight, timeTextValue(row.RetryAt), row.RetryCount, row.LastError, row.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

const axonHubLifecycleSelect = `SELECT channel_id, channel_name, remote_status, remote_tags, remote_managed_tag, remote_weight,
	desired_tag, desired_weight, owner, external_takeover, aum_disabled, aum_disabled_at, last_aum_status, last_aum_tag,
	last_aum_weight, last_aum_write_at, last_source, last_reason, pending_action, pending_status, pending_tag, pending_weight,
	retry_at, retry_count, last_error, updated_at FROM scheduler_axonhub_channel_lifecycle`

func scanAxonHubLifecycle(scanner lifecycleScanner) (domain.AxonHubChannelLifecycle, error) {
	var out domain.AxonHubChannelLifecycle
	var remoteTags, disabledAt, lastWrite, retryAt, updated string
	var external, disabled int
	err := scanner.Scan(&out.ChannelID, &out.ChannelName, &out.RemoteStatus, &remoteTags, &out.RemoteManagedTag, &out.RemoteWeight,
		&out.DesiredTag, &out.DesiredWeight, &out.Owner, &external, &disabled, &disabledAt, &out.LastAUMStatus,
		&out.LastAUMTag, &out.LastAUMWeight, &lastWrite, &out.LastSource, &out.LastReason, &out.PendingAction,
		&out.PendingStatus, &out.PendingTag, &out.PendingWeight, &retryAt, &out.RetryCount, &out.LastError, &updated)
	if err != nil {
		return out, err
	}
	_ = json.Unmarshal([]byte(remoteTags), &out.RemoteTags)
	out.ExternalTakeover = boolFromInt(external)
	out.AUMDisabled = boolFromInt(disabled)
	out.AUMDisabledAt = parseTime(disabledAt)
	out.LastAUMWriteAt = parseTime(lastWrite)
	out.RetryAt = parseTime(retryAt)
	out.UpdatedAt = parseTime(updated)
	return out, nil
}
