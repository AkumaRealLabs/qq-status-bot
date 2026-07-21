package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) SaveTrafficEvent(ctx context.Context, event domain.TrafficEvent) (bool, error) {
	if event.ID == "" {
		event.ID = NewID()
	}
	if event.DedupeKey == "" {
		event.DedupeKey = domain.TrafficDedupeKey(event)
	}
	result, err := s.exec(ctx, `INSERT INTO scheduler_traffic_events
		(id, dedupe_key, source, occurred_at, channel_id, channel_name, model, group_name, request_id, upstream_request_id, kind,
		http_status, error_type, error_code, duration_ms, ttft_ms, stream_ended, tokens, retry_count, retry_succeeded, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(dedupe_key) DO NOTHING`,
		event.ID, event.DedupeKey, event.Source, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.ChannelID, event.ChannelName,
		event.Model, event.Group, event.RequestID, event.UpstreamRequestID, event.Kind, event.HTTPStatus, event.ErrorType, event.ErrorCode,
		event.DurationMS, event.TTFTMS, boolInt(event.StreamEnded), event.Tokens, event.RetryCount, boolInt(event.RetrySucceeded), nowText())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) TrafficEventsSince(ctx context.Context, since time.Time) ([]domain.TrafficEvent, error) {
	rows, err := s.query(ctx, `SELECT id, dedupe_key, source, occurred_at, channel_id, channel_name, model, group_name, request_id,
		upstream_request_id, kind, http_status, error_type, error_code, duration_ms, ttft_ms, stream_ended, tokens, retry_count, retry_succeeded
		FROM scheduler_traffic_events WHERE occurred_at>=? ORDER BY occurred_at`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TrafficEvent{}
	for rows.Next() {
		var event domain.TrafficEvent
		var occurred string
		var streamEnded, retrySucceeded int
		if err := rows.Scan(&event.ID, &event.DedupeKey, &event.Source, &occurred, &event.ChannelID, &event.ChannelName, &event.Model,
			&event.Group, &event.RequestID, &event.UpstreamRequestID, &event.Kind, &event.HTTPStatus, &event.ErrorType, &event.ErrorCode,
			&event.DurationMS, &event.TTFTMS, &streamEnded, &event.Tokens, &event.RetryCount, &retrySucceeded); err != nil {
			return nil, err
		}
		event.OccurredAt = parseTime(occurred)
		event.StreamEnded, event.RetrySucceeded = boolFromInt(streamEnded), boolFromInt(retrySucceeded)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) SaveTrafficWindow(ctx context.Context, table string, row domain.TrafficWindow) error {
	if table != "scheduler_traffic_10s" && table != "scheduler_traffic_1m" {
		return errors.New("invalid traffic summary table")
	}
	_, err := s.exec(ctx, `INSERT INTO `+quoteIdent(table)+`
		(id, channel_id, channel_name, model, group_name, window_start, window_end, requests, successes, soft_failures, hard_failures,
		user_errors, p95_ttft_ms, avg_ttft_ms, failure_rate, last_success_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, model, window_start) DO UPDATE SET channel_name=?, group_name=?, window_end=?, requests=?, successes=?,
		soft_failures=?, hard_failures=?, user_errors=?, p95_ttft_ms=?, avg_ttft_ms=?, failure_rate=?, last_success_at=?, updated_at=?`,
		NewID(), row.ChannelID, row.ChannelName, row.Model, row.Group, row.WindowStart.UTC().Format(time.RFC3339Nano), row.WindowEnd.UTC().Format(time.RFC3339Nano),
		row.Requests, row.Successes, row.SoftFailures, row.HardFailures, row.UserErrors, row.P95TTFTMS, row.AvgTTFTMS, row.FailureRate,
		timeTextValue(row.LastSuccessAt), nowText(), row.ChannelName, row.Group, row.WindowEnd.UTC().Format(time.RFC3339Nano), row.Requests, row.Successes,
		row.SoftFailures, row.HardFailures, row.UserErrors, row.P95TTFTMS, row.AvgTTFTMS, row.FailureRate, timeTextValue(row.LastSuccessAt), nowText())
	return err
}

func (s *Store) TrafficCursor(ctx context.Context, source string) (domain.TrafficCursor, bool, error) {
	var out domain.TrafficCursor
	var cursorAt, scanStartAt, scanEndAt, lastPollAt, lastEventAt, updated string
	err := s.row(ctx, `SELECT source, cursor_at, scan_start_at, scan_end_at, next_page, last_poll_at, last_event_at, backlog_pages, last_error, updated_at
		FROM scheduler_traffic_cursors WHERE source=?`, source).Scan(&out.Source, &cursorAt, &scanStartAt, &scanEndAt, &out.NextPage, &lastPollAt, &lastEventAt, &out.BacklogPages, &out.LastError, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	out.CursorAt, out.ScanStartAt, out.ScanEndAt = parseTime(cursorAt), parseTime(scanStartAt), parseTime(scanEndAt)
	out.LastPollAt, out.LastEventAt, out.UpdatedAt = parseTime(lastPollAt), parseTime(lastEventAt), parseTime(updated)
	return out, err == nil, err
}

func (s *Store) SaveTrafficCursor(ctx context.Context, cursor domain.TrafficCursor) error {
	if cursor.UpdatedAt.IsZero() {
		cursor.UpdatedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_traffic_cursors
		(source, cursor_at, scan_start_at, scan_end_at, next_page, last_poll_at, last_event_at, backlog_pages, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source) DO UPDATE SET cursor_at=?, scan_start_at=?, scan_end_at=?, next_page=?, last_poll_at=?, last_event_at=?, backlog_pages=?, last_error=?, updated_at=?`,
		cursor.Source, timeTextValue(cursor.CursorAt), timeTextValue(cursor.ScanStartAt), timeTextValue(cursor.ScanEndAt), cursor.NextPage,
		timeTextValue(cursor.LastPollAt), timeTextValue(cursor.LastEventAt), cursor.BacklogPages, cursor.LastError, cursor.UpdatedAt.UTC().Format(time.RFC3339Nano),
		timeTextValue(cursor.CursorAt), timeTextValue(cursor.ScanStartAt), timeTextValue(cursor.ScanEndAt), cursor.NextPage,
		timeTextValue(cursor.LastPollAt), timeTextValue(cursor.LastEventAt), cursor.BacklogPages, cursor.LastError, cursor.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) TrafficCursors(ctx context.Context) ([]domain.TrafficCursor, error) {
	rows, err := s.query(ctx, `SELECT source, cursor_at, scan_start_at, scan_end_at, next_page, last_poll_at, last_event_at, backlog_pages, last_error, updated_at FROM scheduler_traffic_cursors ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TrafficCursor{}
	for rows.Next() {
		var row domain.TrafficCursor
		var cursorAt, scanStartAt, scanEndAt, lastPollAt, lastEventAt, updated string
		if err := rows.Scan(&row.Source, &cursorAt, &scanStartAt, &scanEndAt, &row.NextPage, &lastPollAt, &lastEventAt, &row.BacklogPages, &row.LastError, &updated); err != nil {
			return nil, err
		}
		row.CursorAt, row.ScanStartAt, row.ScanEndAt = parseTime(cursorAt), parseTime(scanStartAt), parseTime(scanEndAt)
		row.LastPollAt, row.LastEventAt, row.UpdatedAt = parseTime(lastPollAt), parseTime(lastEventAt), parseTime(updated)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) TrafficControl(ctx context.Context, channelID string) (domain.TrafficControlState, bool, error) {
	var out domain.TrafficControlState
	var baseWeight, desiredWeight, actualWeight int64
	var cooldown, lastProbeAt, stageChangedAt, retryAt, updated string
	err := s.row(ctx, `SELECT channel_id, base_priority, base_weight, desired_priority, desired_weight, actual_priority, actual_weight, desired_status, actual_status, state,
		reason, failure_windows, recovery_stage, cooldown_until, last_probe_at, recovery_successes, stage_changed_at,
		retry_at, retry_count, updated_at FROM scheduler_traffic_control WHERE channel_id=?`, channelID).
		Scan(&out.ChannelID, &out.BasePriority, &baseWeight, &out.DesiredPriority, &desiredWeight, &out.ActualPriority, &actualWeight, &out.DesiredStatus, &out.ActualStatus, &out.State,
			&out.Reason, &out.FailureWindows, &out.RecoveryStage, &cooldown, &lastProbeAt, &out.RecoverySuccesses, &stageChangedAt, &retryAt, &out.RetryCount, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	out.BaseWeight, out.DesiredWeight, out.ActualWeight = uint(baseWeight), uint(desiredWeight), uint(actualWeight)
	out.CooldownUntil, out.LastProbeAt, out.StageChangedAt = parseTime(cooldown), parseTime(lastProbeAt), parseTime(stageChangedAt)
	out.RetryAt, out.UpdatedAt = parseTime(retryAt), parseTime(updated)
	return out, err == nil, err
}

func (s *Store) SaveTrafficControl(ctx context.Context, row domain.TrafficControlState) error {
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_traffic_control
		(channel_id, base_priority, base_weight, desired_priority, desired_weight, actual_priority, actual_weight, desired_status, actual_status, state, reason,
		failure_windows, recovery_stage, cooldown_until, last_probe_at, recovery_successes, stage_changed_at, retry_at, retry_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET base_priority=?, base_weight=?, desired_priority=?, desired_weight=?, actual_priority=?, actual_weight=?, desired_status=?, actual_status=?,
		state=?, reason=?, failure_windows=?, recovery_stage=?, cooldown_until=?, last_probe_at=?, recovery_successes=?, stage_changed_at=?, retry_at=?, retry_count=?, updated_at=?`,
		row.ChannelID, row.BasePriority, row.BaseWeight, row.DesiredPriority, row.DesiredWeight, row.ActualPriority, row.ActualWeight, row.DesiredStatus, row.ActualStatus, row.State,
		row.Reason, row.FailureWindows, row.RecoveryStage, timeTextValue(row.CooldownUntil), timeTextValue(row.LastProbeAt), row.RecoverySuccesses,
		timeTextValue(row.StageChangedAt), timeTextValue(row.RetryAt), row.RetryCount, row.UpdatedAt.UTC().Format(time.RFC3339Nano),
		row.BasePriority, row.BaseWeight, row.DesiredPriority, row.DesiredWeight, row.ActualPriority, row.ActualWeight, row.DesiredStatus, row.ActualStatus, row.State, row.Reason,
		row.FailureWindows, row.RecoveryStage, timeTextValue(row.CooldownUntil), timeTextValue(row.LastProbeAt), row.RecoverySuccesses,
		timeTextValue(row.StageChangedAt), timeTextValue(row.RetryAt), row.RetryCount, row.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) TrafficControls(ctx context.Context) ([]domain.TrafficControlState, error) {
	rows, err := s.query(ctx, `SELECT channel_id, base_priority, base_weight, desired_priority, desired_weight, actual_priority, actual_weight, desired_status, actual_status, state,
		reason, failure_windows, recovery_stage, cooldown_until, last_probe_at, recovery_successes, stage_changed_at,
		retry_at, retry_count, updated_at FROM scheduler_traffic_control ORDER BY channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TrafficControlState{}
	for rows.Next() {
		var row domain.TrafficControlState
		var baseWeight, desiredWeight, actualWeight int64
		var cooldown, lastProbeAt, stageChangedAt, retryAt, updated string
		if err := rows.Scan(&row.ChannelID, &row.BasePriority, &baseWeight, &row.DesiredPriority, &desiredWeight, &row.ActualPriority, &actualWeight, &row.DesiredStatus, &row.ActualStatus,
			&row.State, &row.Reason, &row.FailureWindows, &row.RecoveryStage, &cooldown, &lastProbeAt, &row.RecoverySuccesses, &stageChangedAt,
			&retryAt, &row.RetryCount, &updated); err != nil {
			return nil, err
		}
		row.BaseWeight, row.DesiredWeight, row.ActualWeight = uint(baseWeight), uint(desiredWeight), uint(actualWeight)
		row.CooldownUntil, row.LastProbeAt, row.StageChangedAt = parseTime(cooldown), parseTime(lastProbeAt), parseTime(stageChangedAt)
		row.RetryAt, row.UpdatedAt = parseTime(retryAt), parseTime(updated)
		out = append(out, row)
	}
	return out, rows.Err()
}

func timeTextValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
