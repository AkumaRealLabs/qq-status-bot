package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

func (s *Store) SaveProbe(ctx context.Context, upstreamID, cardID, model string, p monitor.ProbeResult) (domain.ProbeRun, error) {
	return s.SaveProbeWithPurpose(ctx, upstreamID, cardID, model, "regular", p)
}

func (s *Store) SaveProbeWithPurpose(ctx context.Context, upstreamID, cardID, model, purpose string, p monitor.ProbeResult) (domain.ProbeRun, error) {
	status := p.Status
	if status == "" {
		status = legacyProbeStatus(p.Success)
	}
	success := status == monitor.StatusOperational || status == monitor.StatusDegraded
	run := domain.ProbeRun{
		ID:         NewID(),
		UpstreamID: upstreamID,
		CardID:     cardID,
		CheckedAt:  time.Now().UTC(),
		Model:      domain.NormalizeProbeModel(model),
		Input:      p.Input,
		Status:     status,
		Output:     p.Output,
		HTTPStatus: p.HTTPStatus,
		LatencyMS:  int(p.Latency.Milliseconds()),
		Success:    success,
		Error:      p.Error,
		Purpose:    normalizeProbePurpose(purpose),
	}
	_, err := s.exec(ctx, `INSERT INTO probe_runs (id, upstream_id, card_id, checked_at, model, input, status, output, http_status, latency_ms, success, error, purpose) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.UpstreamID, run.CardID, run.CheckedAt.Format(time.RFC3339Nano), run.Model, run.Input, run.Status, run.Output, run.HTTPStatus, run.LatencyMS, boolInt(run.Success), run.Error, run.Purpose)
	return run, err
}

func (s *Store) UpdateCardProbeState(ctx context.Context, id, lastError string, failureCount int) error {
	_, err := s.exec(ctx, `UPDATE model_cards SET last_error=?, failure_count=?, updated_at=? WHERE id=?`, lastError, failureCount, nowText(), id)
	return err
}

func (s *Store) UpdateCardSchedulerAutoDisabled(ctx context.Context, id string, disabled bool) error {
	disabledAt := ""
	if disabled {
		disabledAt = nowText()
	}
	_, err := s.exec(ctx, `UPDATE model_cards SET scheduler_auto_disabled=?, scheduler_auto_disabled_at=?, updated_at=? WHERE id=?`, boolInt(disabled), disabledAt, nowText(), id)
	return err
}

func (s *Store) RecentProbesForCard(ctx context.Context, cardID string, limit int) ([]domain.ProbeRun, error) {
	rows, err := s.query(ctx, `SELECT id, upstream_id, card_id, checked_at, model, input, status, output, http_status, latency_ms, success, error, purpose
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
	query := `SELECT id, upstream_id, card_id, checked_at, model, input, status, output, http_status, latency_ms, success, error, purpose
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
	err := rows.Scan(&p.ID, &p.UpstreamID, &p.CardID, &checked, &p.Model, &p.Input, &p.Status, &p.Output, &p.HTTPStatus, &p.LatencyMS, &success, &p.Error, &p.Purpose)
	p.CheckedAt = parseTime(checked)
	if p.Status == "" {
		p.Status = legacyProbeStatus(boolFromInt(success))
	}
	p.Success = p.Status == monitor.StatusOperational || p.Status == monitor.StatusDegraded
	return p, err
}

func normalizeProbePurpose(purpose string) string {
	switch purpose {
	case "confirm", "recovery":
		return purpose
	default:
		return "regular"
	}
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
