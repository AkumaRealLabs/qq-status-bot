package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) CreateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if !c.PoolEnabledSet {
		c.PoolEnabled = true
	}
	c.PoolEnabledSet = true
	c.Model = domain.NormalizeProbeModel(c.Model)
	if c.SortOrder <= 0 {
		next, err := s.nextCardSortOrder(ctx)
		if err != nil {
			return c, err
		}
		c.SortOrder = next
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO model_cards
		(id, name, base_url, api_key, upstream_id, key_id, model, display_group, pool_enabled, manual_cost_ratio, scheduler_group, scheduler_channel_id, scheduler_channel_name, axonhub_channel_id, axonhub_channel_name, scheduler_auto_disabled, scheduler_auto_disabled_at, enabled, public_enabled, sort_order, last_error, failure_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.BaseURL, c.APIKey, c.UpstreamID, c.KeyID, c.Model, c.DisplayGroup, boolInt(c.PoolEnabled), c.ManualCostRatio, c.SchedulerGroup, c.SchedulerChannelID, c.SchedulerChannelName, c.AxonHubChannelID, c.AxonHubChannelName, boolInt(c.SchedulerAutoDisabled), cardAutoDisabledAt(c), boolInt(c.Enabled), boolInt(c.PublicEnabled),
		c.SortOrder, c.LastError, c.FailureCount, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) nextCardSortOrder(ctx context.Context) (int, error) {
	var maxOrder int
	err := s.row(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM model_cards`).Scan(&maxOrder)
	return maxOrder + 1, err
}

func (s *Store) UpdateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error) {
	if !c.PoolEnabledSet {
		c.PoolEnabled = true
	}
	c.PoolEnabledSet = true
	c.Model = domain.NormalizeProbeModel(c.Model)
	c.UpdatedAt = time.Now().UTC()
	_, err := s.exec(ctx, `UPDATE model_cards SET name=?, base_url=?, api_key=?, upstream_id=?, key_id=?, model=?, display_group=?, pool_enabled=?, manual_cost_ratio=?, scheduler_group=?, scheduler_channel_id=?, scheduler_channel_name=?, axonhub_channel_id=?, axonhub_channel_name=?, scheduler_auto_disabled=?, scheduler_auto_disabled_at=?, enabled=?,
		public_enabled=?, sort_order=?, last_error=?, failure_count=?, updated_at=? WHERE id=?`,
		c.Name, c.BaseURL, c.APIKey, c.UpstreamID, c.KeyID, c.Model, c.DisplayGroup, boolInt(c.PoolEnabled), c.ManualCostRatio, c.SchedulerGroup, c.SchedulerChannelID, c.SchedulerChannelName, c.AxonHubChannelID, c.AxonHubChannelName, boolInt(c.SchedulerAutoDisabled), cardAutoDisabledAt(c), boolInt(c.Enabled), boolInt(c.PublicEnabled),
		c.SortOrder, c.LastError, c.FailureCount, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	return c, err
}

func (s *Store) DeleteCard(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM model_cards WHERE id=?`, id)
	return err
}

func (s *Store) Card(ctx context.Context, id string) (domain.ModelCard, error) {
	return s.scanCard(s.row(ctx, cardSelect+` WHERE id=?`, id))
}

func (s *Store) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	rows, err := s.query(ctx, cardSelect+` ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelCard
	for rows.Next() {
		c, err := scanCardRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) scanCard(row *sql.Row) (domain.ModelCard, error) {
	var c domain.ModelCard
	var poolEnabled, autoDisabled, enabled, publicEnabled int
	var autoDisabledAt, created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.BaseURL, &c.APIKey, &c.UpstreamID, &c.KeyID, &c.Model, &c.DisplayGroup, &poolEnabled, &c.ManualCostRatio, &c.SchedulerGroup, &c.SchedulerChannelID, &c.SchedulerChannelName, &c.AxonHubChannelID, &c.AxonHubChannelName, &autoDisabled, &autoDisabledAt, &enabled, &publicEnabled, &c.SortOrder, &c.LastError, &c.FailureCount, &created, &updated)
	c.PoolEnabled = boolFromInt(poolEnabled)
	c.PoolEnabledSet = true
	c.SchedulerAutoDisabled = boolFromInt(autoDisabled)
	c.SchedulerAutoDisabledAt = parseOptionalTime(autoDisabledAt)
	c.Enabled = boolFromInt(enabled)
	c.PublicEnabled = boolFromInt(publicEnabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func scanCardRows(rows *sql.Rows) (domain.ModelCard, error) {
	var c domain.ModelCard
	var poolEnabled, autoDisabled, enabled, publicEnabled int
	var autoDisabledAt, created, updated string
	err := rows.Scan(&c.ID, &c.Name, &c.BaseURL, &c.APIKey, &c.UpstreamID, &c.KeyID, &c.Model, &c.DisplayGroup, &poolEnabled, &c.ManualCostRatio, &c.SchedulerGroup, &c.SchedulerChannelID, &c.SchedulerChannelName, &c.AxonHubChannelID, &c.AxonHubChannelName, &autoDisabled, &autoDisabledAt, &enabled, &publicEnabled, &c.SortOrder, &c.LastError, &c.FailureCount, &created, &updated)
	c.PoolEnabled = boolFromInt(poolEnabled)
	c.PoolEnabledSet = true
	c.SchedulerAutoDisabled = boolFromInt(autoDisabled)
	c.SchedulerAutoDisabledAt = parseOptionalTime(autoDisabledAt)
	c.Enabled = boolFromInt(enabled)
	c.PublicEnabled = boolFromInt(publicEnabled)
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

const cardSelect = `SELECT id, name, base_url, api_key, upstream_id, key_id, model, display_group, pool_enabled, manual_cost_ratio,
	scheduler_group, scheduler_channel_id, scheduler_channel_name, axonhub_channel_id, axonhub_channel_name,
	scheduler_auto_disabled, scheduler_auto_disabled_at, enabled, public_enabled, sort_order, last_error, failure_count, created_at, updated_at FROM model_cards`

func (s *Store) UpdateCardOrder(ctx context.Context, ids []string) error {
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
		res, err := tx.ExecContext(ctx, s.rebind(`UPDATE model_cards SET sort_order=?, updated_at=? WHERE id=?`), i+1, nowText(), id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("card not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	done = true
	return nil
}
