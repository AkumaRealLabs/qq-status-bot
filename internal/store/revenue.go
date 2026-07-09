package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) CreateRevenueCard(ctx context.Context, c domain.RevenueCard) (domain.RevenueCard, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.SortOrder <= 0 {
		next, err := s.nextRevenueCardSortOrder(ctx)
		if err != nil {
			return c, err
		}
		c.SortOrder = next
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO revenue_cards
		(id, name, source_type, upstream_id, base_url, user_id, access_token, admin_api_key, epay_pid, epay_key, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.SourceType, c.UpstreamID, c.BaseURL, c.UserID, c.AccessToken, c.AdminAPIKey, c.EpayPID, c.EpayKey,
		boolInt(c.Enabled), c.SortOrder, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) nextRevenueCardSortOrder(ctx context.Context) (int, error) {
	var maxOrder int
	err := s.row(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM revenue_cards`).Scan(&maxOrder)
	return maxOrder + 1, err
}

func (s *Store) UpdateRevenueCard(ctx context.Context, c domain.RevenueCard) (domain.RevenueCard, error) {
	c.UpdatedAt = time.Now().UTC()
	_, err := s.exec(ctx, `UPDATE revenue_cards SET name=?, source_type=?, upstream_id=?, base_url=?, user_id=?, access_token=?,
		admin_api_key=?, epay_pid=?, epay_key=?, enabled=?, sort_order=?, updated_at=? WHERE id=?`,
		c.Name, c.SourceType, c.UpstreamID, c.BaseURL, c.UserID, c.AccessToken, c.AdminAPIKey, c.EpayPID, c.EpayKey,
		boolInt(c.Enabled), c.SortOrder, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	return c, err
}

func (s *Store) DeleteRevenueCard(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM revenue_cards WHERE id=?`, id)
	return err
}

func (s *Store) RevenueCard(ctx context.Context, id string) (domain.RevenueCard, error) {
	return s.scanRevenueCard(s.row(ctx, revenueCardSelectSQL()+` WHERE id=?`, id))
}

func (s *Store) ListRevenueCards(ctx context.Context) ([]domain.RevenueCard, error) {
	rows, err := s.query(ctx, revenueCardSelectSQL()+` ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RevenueCard{}
	for rows.Next() {
		c, err := scanRevenueCardRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) scanRevenueCard(row *sql.Row) (domain.RevenueCard, error) {
	var c domain.RevenueCard
	var enabled int
	var created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.SourceType, &c.UpstreamID, &c.BaseURL, &c.UserID, &c.AccessToken,
		&c.AdminAPIKey, &c.EpayPID, &c.EpayKey, &enabled, &c.SortOrder, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func scanRevenueCardRows(rows *sql.Rows) (domain.RevenueCard, error) {
	var c domain.RevenueCard
	var enabled int
	var created, updated string
	err := rows.Scan(&c.ID, &c.Name, &c.SourceType, &c.UpstreamID, &c.BaseURL, &c.UserID, &c.AccessToken,
		&c.AdminAPIKey, &c.EpayPID, &c.EpayKey, &enabled, &c.SortOrder, &created, &updated)
	c.Enabled = boolFromInt(enabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func revenueCardSelectSQL() string {
	return `SELECT id, name, source_type, upstream_id, base_url, user_id, access_token, admin_api_key, epay_pid, epay_key, enabled, sort_order, created_at, updated_at FROM revenue_cards`
}

func (s *Store) UpdateRevenueCardOrder(ctx context.Context, ids []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, s.rebind(`UPDATE revenue_cards SET sort_order=?, updated_at=? WHERE id=?`), i+1, nowText(), id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("revenue card not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	done = true
	return nil
}
