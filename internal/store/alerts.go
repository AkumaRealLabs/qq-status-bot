package store

import (
	"context"
	"database/sql"
	"errors"

	"ai-upstream-monitor/internal/domain"
)

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
