package store

import (
	"context"
	"time"

	"ai-upstream-monitor/internal/domain"
)

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
