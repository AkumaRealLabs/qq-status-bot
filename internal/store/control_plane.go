package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) SchedulerChannelLifecycle(ctx context.Context, channelID string) (domain.SchedulerChannelLifecycle, bool, error) {
	row, err := scanSchedulerChannelLifecycle(s.row(ctx, controlPlaneLifecycleSelect+` WHERE channel_id=?`, channelID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SchedulerChannelLifecycle{}, false, nil
	}
	return row, err == nil, err
}

func (s *Store) SchedulerChannelLifecycles(ctx context.Context) ([]domain.SchedulerChannelLifecycle, error) {
	rows, err := s.query(ctx, controlPlaneLifecycleSelect+` ORDER BY channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SchedulerChannelLifecycle{}
	for rows.Next() {
		row, err := scanSchedulerChannelLifecycleRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) SaveSchedulerChannelLifecycle(ctx context.Context, row domain.SchedulerChannelLifecycle) error {
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_channel_lifecycle
		(channel_id, channel_name, remote_status, remote_priority, remote_weight, owner, external_takeover, aum_disabled,
		last_aum_status, last_aum_write_at, last_source, last_reason, traffic_since, affinity_cleanup_pending,
		affinity_cleanup_retry_at, affinity_cleanup_retries, affinity_cleanup_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET channel_name=?, remote_status=?, remote_priority=?, remote_weight=?, owner=?, external_takeover=?,
		aum_disabled=?, last_aum_status=?, last_aum_write_at=?, last_source=?, last_reason=?, traffic_since=?, affinity_cleanup_pending=?,
		affinity_cleanup_retry_at=?, affinity_cleanup_retries=?, affinity_cleanup_error=?, updated_at=?`,
		row.ChannelID, row.ChannelName, row.RemoteStatus, row.RemotePriority, row.RemoteWeight, row.Owner, boolInt(row.ExternalTakeover), boolInt(row.AUMDisabled),
		row.LastAUMStatus, timeTextValue(row.LastAUMWriteAt), row.LastSource, row.LastReason, timeTextValue(row.TrafficSince), boolInt(row.AffinityCleanupPending),
		timeTextValue(row.AffinityCleanupRetryAt), row.AffinityCleanupRetries, row.AffinityCleanupError, row.UpdatedAt.UTC().Format(time.RFC3339Nano),
		row.ChannelName, row.RemoteStatus, row.RemotePriority, row.RemoteWeight, row.Owner, boolInt(row.ExternalTakeover), boolInt(row.AUMDisabled),
		row.LastAUMStatus, timeTextValue(row.LastAUMWriteAt), row.LastSource, row.LastReason, timeTextValue(row.TrafficSince), boolInt(row.AffinityCleanupPending),
		timeTextValue(row.AffinityCleanupRetryAt), row.AffinityCleanupRetries, row.AffinityCleanupError, row.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

const controlPlaneLifecycleSelect = `SELECT channel_id, channel_name, remote_status, remote_priority, remote_weight, owner, external_takeover,
	aum_disabled, last_aum_status, last_aum_write_at, last_source, last_reason, traffic_since, affinity_cleanup_pending,
	affinity_cleanup_retry_at, affinity_cleanup_retries, affinity_cleanup_error, updated_at FROM scheduler_channel_lifecycle`

type lifecycleScanner interface {
	Scan(dest ...any) error
}

func scanSchedulerChannelLifecycle(scanner lifecycleScanner) (domain.SchedulerChannelLifecycle, error) {
	var out domain.SchedulerChannelLifecycle
	var remoteWeight int64
	var externalTakeover, aumDisabled, cleanupPending int
	var lastWrite, trafficSince, cleanupRetry, updated string
	err := scanner.Scan(&out.ChannelID, &out.ChannelName, &out.RemoteStatus, &out.RemotePriority, &remoteWeight, &out.Owner, &externalTakeover,
		&aumDisabled, &out.LastAUMStatus, &lastWrite, &out.LastSource, &out.LastReason, &trafficSince, &cleanupPending,
		&cleanupRetry, &out.AffinityCleanupRetries, &out.AffinityCleanupError, &updated)
	if err != nil {
		return out, err
	}
	out.RemoteWeight = uint(remoteWeight)
	out.ExternalTakeover = boolFromInt(externalTakeover)
	out.AUMDisabled = boolFromInt(aumDisabled)
	out.AffinityCleanupPending = boolFromInt(cleanupPending)
	out.LastAUMWriteAt = parseTime(lastWrite)
	out.TrafficSince = parseTime(trafficSince)
	out.AffinityCleanupRetryAt = parseTime(cleanupRetry)
	out.UpdatedAt = parseTime(updated)
	return out, nil
}

func scanSchedulerChannelLifecycleRows(rows *sql.Rows) (domain.SchedulerChannelLifecycle, error) {
	return scanSchedulerChannelLifecycle(rows)
}
