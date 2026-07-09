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

func (s *Store) NotificationRules(ctx context.Context) (domain.NotificationRules, error) {
	var raw string
	err := s.row(ctx, `SELECT notification_rules FROM settings WHERE id='default'`).Scan(&raw)
	if err != nil {
		return domain.DefaultNotificationRules(), err
	}
	var rules domain.NotificationRules
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &rules)
	} else {
		rules = domain.DefaultNotificationRules()
	}
	return domain.NormalizeNotificationRules(rules), nil
}

func (s *Store) UpdateNotificationRules(ctx context.Context, rules domain.NotificationRules) (domain.NotificationRules, error) {
	rules = domain.NormalizeNotificationRules(rules)
	b, err := json.Marshal(rules)
	if err != nil {
		return rules, err
	}
	_, err = s.exec(ctx, `UPDATE settings SET notification_rules=? WHERE id='default'`, string(b))
	return rules, err
}

func (s *Store) CreateOpsEvent(ctx context.Context, event domain.OpsEvent) (domain.OpsEvent, error) {
	if event.ID == "" {
		event.ID = NewID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.UpdatedAt = event.CreatedAt
	if event.Severity == "" {
		event.Severity = "info"
	}
	actions, err := json.Marshal(event.Actions)
	if err != nil {
		return event, err
	}
	_, err = s.exec(ctx, `INSERT INTO ops_events
		(id, type, severity, title, message, target_type, target_id, actions, read, acked, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Type, event.Severity, event.Title, event.Message, event.TargetType, event.TargetID, string(actions),
		boolInt(event.Read), boolInt(event.Acked), event.CreatedAt.Format(time.RFC3339Nano), event.UpdatedAt.Format(time.RFC3339Nano))
	return event, err
}

func (s *Store) OpsEvents(ctx context.Context, filter domain.OpsEventFilter) ([]domain.OpsEvent, error) {
	limit := opsEventLimit(filter.Limit)
	where, args := opsEventWhere(filter)
	args = append(args, limit)
	rows, err := s.query(ctx, `SELECT id, type, severity, title, message, target_type, target_id, actions, read, acked, created_at, updated_at
		FROM ops_events WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.OpsEvent{}
	for rows.Next() {
		event, err := scanOpsEventRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) OpsEventGroups(ctx context.Context, filter domain.OpsEventFilter) ([]domain.OpsEventGroup, error) {
	limit := opsEventLimit(filter.Limit)
	where, args := opsEventWhere(filter)
	args = append(args, limit)
	rows, err := s.query(ctx, `SELECT e.id, e.type, e.severity, e.title, e.message, e.target_type, e.target_id, e.actions, e.read, e.acked, e.created_at, e.updated_at,
		g.count, g.unread_count, g.unacked_count
		FROM (
			SELECT type, target_type, target_id, COUNT(*) count, SUM(CASE WHEN read=0 THEN 1 ELSE 0 END) unread_count, SUM(CASE WHEN acked=0 THEN 1 ELSE 0 END) unacked_count, MAX(created_at) latest_at
			FROM ops_events WHERE `+strings.Join(where, " AND ")+`
			GROUP BY type, target_type, target_id
			ORDER BY latest_at DESC LIMIT ?
		) g
		JOIN ops_events e ON e.id = (
			SELECT id FROM ops_events
			WHERE type=g.type AND target_type=g.target_type AND target_id=g.target_id
				AND created_at=g.latest_at
			ORDER BY id DESC LIMIT 1
		)
		ORDER BY e.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.OpsEventGroup{}
	for rows.Next() {
		var group domain.OpsEventGroup
		event, err := scanOpsEventGroupRows(rows, &group)
		if err != nil {
			return nil, err
		}
		group.Type, group.TargetType, group.TargetID, group.Latest = event.Type, event.TargetType, event.TargetID, event
		out = append(out, group)
	}
	return out, rows.Err()
}

func opsEventLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func opsEventWhere(filter domain.OpsEventFilter) ([]string, []any) {
	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filter.Type) != "" {
		where = append(where, "type=?")
		args = append(args, strings.TrimSpace(filter.Type))
	}
	if strings.TrimSpace(filter.TargetType) != "" {
		where = append(where, "target_type=?")
		args = append(args, strings.TrimSpace(filter.TargetType))
	}
	if strings.TrimSpace(filter.TargetID) != "" {
		where = append(where, "target_id=?")
		args = append(args, strings.TrimSpace(filter.TargetID))
	}
	switch strings.TrimSpace(filter.State) {
	case "unread":
		where = append(where, "read=0")
	case "unacked":
		where = append(where, "acked=0")
	case "acked":
		where = append(where, "acked=1")
	}
	return where, args
}

func scanOpsEventRows(rows *sql.Rows) (domain.OpsEvent, error) {
	var event domain.OpsEvent
	var actions, created, updated string
	var read, acked int
	err := rows.Scan(&event.ID, &event.Type, &event.Severity, &event.Title, &event.Message, &event.TargetType, &event.TargetID, &actions, &read, &acked, &created, &updated)
	_ = json.Unmarshal([]byte(actions), &event.Actions)
	event.Read = boolFromInt(read)
	event.Acked = boolFromInt(acked)
	event.CreatedAt, event.UpdatedAt = parseTime(created), parseTime(updated)
	return event, err
}

func scanOpsEventGroupRows(rows *sql.Rows, group *domain.OpsEventGroup) (domain.OpsEvent, error) {
	var event domain.OpsEvent
	var actions, created, updated string
	var read, acked int
	err := rows.Scan(&event.ID, &event.Type, &event.Severity, &event.Title, &event.Message, &event.TargetType, &event.TargetID, &actions, &read, &acked, &created, &updated, &group.Count, &group.UnreadCount, &group.UnackedCount)
	_ = json.Unmarshal([]byte(actions), &event.Actions)
	event.Read = boolFromInt(read)
	event.Acked = boolFromInt(acked)
	event.CreatedAt, event.UpdatedAt = parseTime(created), parseTime(updated)
	return event, err
}

func (s *Store) MarkOpsEventRead(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `UPDATE ops_events SET read=1, updated_at=? WHERE id=?`, nowText(), id)
	return err
}

func (s *Store) AckOpsEvent(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `UPDATE ops_events SET read=1, acked=1, updated_at=? WHERE id=?`, nowText(), id)
	return err
}

func (s *Store) MarkOpsEventsRead(ctx context.Context, filter domain.OpsEventFilter) error {
	where, args := opsEventWhere(filter)
	args = append([]any{nowText()}, args...)
	_, err := s.exec(ctx, `UPDATE ops_events SET read=1, updated_at=? WHERE `+strings.Join(where, " AND "), args...)
	return err
}

func (s *Store) AckOpsEvents(ctx context.Context, filter domain.OpsEventFilter) error {
	where, args := opsEventWhere(filter)
	args = append([]any{nowText()}, args...)
	_, err := s.exec(ctx, `UPDATE ops_events SET read=1, acked=1, updated_at=? WHERE `+strings.Join(where, " AND "), args...)
	return err
}

func (s *Store) CreateAudit(ctx context.Context, log domain.AuditLog) error {
	if log.ID == "" {
		log.ID = NewID()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	if log.Fields == nil {
		log.Fields = []string{}
	}
	fields, err := json.Marshal(log.Fields)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `INSERT INTO audit_logs (id, actor, action, target_type, target_id, summary, fields, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, log.ID, log.Actor, log.Action, log.TargetType, log.TargetID, log.Summary, string(fields), log.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) AuditLogs(ctx context.Context, action, target string, limit int) ([]domain.AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(action) != "" {
		where = append(where, "action LIKE ?")
		args = append(args, "%"+strings.TrimSpace(action)+"%")
	}
	if strings.TrimSpace(target) != "" {
		where = append(where, "(target_type LIKE ? OR target_id LIKE ?)")
		args = append(args, "%"+strings.TrimSpace(target)+"%", "%"+strings.TrimSpace(target)+"%")
	}
	args = append(args, limit)
	rows, err := s.query(ctx, `SELECT id, actor, action, target_type, target_id, summary, fields, created_at
		FROM audit_logs WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AuditLog{}
	for rows.Next() {
		var log domain.AuditLog
		var fields, created string
		if err := rows.Scan(&log.ID, &log.Actor, &log.Action, &log.TargetType, &log.TargetID, &log.Summary, &fields, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(fields), &log.Fields)
		if log.Fields == nil {
			log.Fields = []string{}
		}
		log.CreatedAt = parseTime(created)
		out = append(out, log)
	}
	return out, rows.Err()
}

func (s *Store) SaveRevenueSnapshot(ctx context.Context, snap domain.RevenueSnapshot) error {
	if snap.ID == "" {
		snap.ID = NewID()
	}
	if snap.CheckedAt.IsZero() {
		snap.CheckedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO revenue_snapshots (id, source_id, source_name, source_type, checked_at, revenue, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, snap.ID, snap.SourceID, snap.SourceName, snap.SourceType, snap.CheckedAt.Format(time.RFC3339Nano), snap.Revenue, snap.Error)
	return err
}

func (s *Store) RevenueSnapshotsSince(ctx context.Context, since time.Time) ([]domain.RevenueSnapshot, error) {
	rows, err := s.query(ctx, `SELECT id, source_id, source_name, source_type, checked_at, revenue, error FROM revenue_snapshots WHERE `+timeWhere(s.Driver, "checked_at")+` ORDER BY checked_at`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RevenueSnapshot{}
	for rows.Next() {
		var snap domain.RevenueSnapshot
		var checked string
		if err := rows.Scan(&snap.ID, &snap.SourceID, &snap.SourceName, &snap.SourceType, &checked, &snap.Revenue, &snap.Error); err != nil {
			return nil, err
		}
		snap.CheckedAt = parseTime(checked)
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *Store) SaveCLIProxyQuotaSnapshot(ctx context.Context, snap domain.CLIProxyQuotaSnapshot) error {
	if snap.ID == "" {
		snap.ID = NewID()
	}
	if snap.CheckedAt.IsZero() {
		snap.CheckedAt = time.Now().UTC()
	}
	_, err := s.exec(ctx, `INSERT INTO cliproxy_quota_snapshots (id, account_name, auth_index, checked_at, ok, plan_type, summary, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, snap.ID, snap.AccountName, snap.AuthIndex, snap.CheckedAt.Format(time.RFC3339Nano), boolInt(snap.OK), snap.PlanType, snap.Summary, snap.Error)
	return err
}

func (s *Store) BalanceSnapshotsSince(ctx context.Context, upstreamID string, since time.Time) ([]domain.BalanceSnapshot, error) {
	where := []string{timeWhere(s.Driver, "checked_at")}
	args := []any{since.UTC().Format(time.RFC3339Nano)}
	if upstreamID != "" {
		where = append(where, "upstream_id=?")
		args = append(args, upstreamID)
	}
	rows, err := s.query(ctx, `SELECT id, upstream_id, checked_at, balance, used, remain, requests, error, latency_ms FROM balance_snapshots WHERE `+strings.Join(where, " AND ")+` ORDER BY checked_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.BalanceSnapshot{}
	for rows.Next() {
		var snap domain.BalanceSnapshot
		var checked string
		if err := rows.Scan(&snap.ID, &snap.UpstreamID, &checked, &snap.Balance, &snap.Used, &snap.Remain, &snap.Requests, &snap.Error, &snap.LatencyMS); err != nil {
			return nil, err
		}
		snap.CheckedAt = parseTime(checked)
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *Store) CheckWritable(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`CREATE TEMP TABLE IF NOT EXISTS ops_self_check (id TEXT)`)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, s.rebind(`INSERT INTO ops_self_check (id) VALUES (?)`), NewID())
	return err
}

func timeWhere(driver, col string) string {
	if driver == "sqlite" {
		return "unixepoch(" + col + ")>=unixepoch(?)"
	}
	return col + ">=?"
}

func ignoreNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
