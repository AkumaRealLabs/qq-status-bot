package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

const costBindingSelect = `SELECT b.id, b.name, b.upstream_id, COALESCE(u.name, ''), b.key_id, COALESCE(k.name, ''),
	COALESCE(k.group_name, ''), COALESCE(k.group_ratio, ''), COALESCE(u.balance_rate, 0), b.manual_cost_ratio,
	b.scheduler_channel_id, b.scheduler_channel_name, b.axonhub_channel_id, b.axonhub_channel_name,
	b.enabled, b.created_at, b.updated_at
	FROM scheduler_cost_bindings b
	LEFT JOIN upstreams u ON u.id=b.upstream_id
	LEFT JOIN api_keys k ON k.id=b.key_id`

func (s *Store) CreateCostBinding(ctx context.Context, binding domain.SchedulerCostBinding) (domain.SchedulerCostBinding, error) {
	binding = domain.NormalizeCostBinding(binding)
	if binding.ID == "" {
		binding.ID = NewID()
	}
	now := time.Now().UTC()
	binding.CreatedAt, binding.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO scheduler_cost_bindings
		(id, name, upstream_id, key_id, manual_cost_ratio, scheduler_channel_id, scheduler_channel_name,
		 axonhub_channel_id, axonhub_channel_name, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		binding.ID, binding.Name, binding.UpstreamID, binding.KeyID, binding.ManualCostRatio,
		binding.SchedulerChannelID, binding.SchedulerChannelName, binding.AxonHubChannelID, binding.AxonHubChannelName,
		boolInt(binding.Enabled), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return binding, err
	}
	return s.CostBinding(ctx, binding.ID)
}

func (s *Store) UpdateCostBinding(ctx context.Context, binding domain.SchedulerCostBinding) (domain.SchedulerCostBinding, error) {
	binding = domain.NormalizeCostBinding(binding)
	binding.UpdatedAt = time.Now().UTC()
	_, err := s.exec(ctx, `UPDATE scheduler_cost_bindings SET name=?, upstream_id=?, key_id=?, manual_cost_ratio=?,
		scheduler_channel_id=?, scheduler_channel_name=?, axonhub_channel_id=?, axonhub_channel_name=?, enabled=?, updated_at=? WHERE id=?`,
		binding.Name, binding.UpstreamID, binding.KeyID, binding.ManualCostRatio, binding.SchedulerChannelID,
		binding.SchedulerChannelName, binding.AxonHubChannelID, binding.AxonHubChannelName, boolInt(binding.Enabled),
		binding.UpdatedAt.Format(time.RFC3339Nano), binding.ID)
	if err != nil {
		return binding, err
	}
	return s.CostBinding(ctx, binding.ID)
}

func (s *Store) DeleteCostBinding(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM scheduler_cost_bindings WHERE id=?`, id)
	return err
}

func (s *Store) CostBinding(ctx context.Context, id string) (domain.SchedulerCostBinding, error) {
	return s.scanCostBinding(s.row(ctx, costBindingSelect+` WHERE b.id=?`, id))
}

func (s *Store) ListCostBindings(ctx context.Context) ([]domain.SchedulerCostBinding, error) {
	rows, err := s.query(ctx, costBindingSelect+` ORDER BY b.created_at, b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SchedulerCostBinding{}
	for rows.Next() {
		binding, err := scanCostBindingValues(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (s *Store) scanCostBinding(row *sql.Row) (domain.SchedulerCostBinding, error) {
	return scanCostBindingValues(row)
}

type costBindingScanner interface {
	Scan(...any) error
}

func scanCostBindingValues(scanner costBindingScanner) (domain.SchedulerCostBinding, error) {
	var out domain.SchedulerCostBinding
	var enabled int
	var created, updated string
	err := scanner.Scan(&out.ID, &out.Name, &out.UpstreamID, &out.UpstreamName, &out.KeyID, &out.KeyName,
		&out.KeyGroup, &out.KeyRatio, &out.BalanceRate, &out.ManualCostRatio,
		&out.SchedulerChannelID, &out.SchedulerChannelName, &out.AxonHubChannelID, &out.AxonHubChannelName,
		&enabled, &created, &updated)
	if err != nil {
		return out, err
	}
	out.Enabled = boolFromInt(enabled)
	out.CreatedAt, out.UpdatedAt = parseTime(created), parseTime(updated)
	out.SourceType = domain.CostSourceManual
	if out.UpstreamID != "" || out.KeyID != "" {
		out.SourceType = domain.CostSourceUpstreamKey
	}
	return out, nil
}

func (s *Store) SaveCostFieldOwnership(ctx context.Context, row domain.CostFieldOwnership) error {
	row.Provider = domain.NormalizeSchedulerProvider(row.Provider)
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}
	groups := domain.JoinGroups(row.RemoteGroups)
	_, err := s.exec(ctx, `INSERT INTO scheduler_cost_field_ownership
		(provider, channel_id, channel_name, remote_groups, remote_priority, remote_weight, managed, external_takeover, last_reason, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, channel_id) DO UPDATE SET channel_name=?, remote_groups=?, remote_priority=?, remote_weight=?,
		managed=?, external_takeover=?, last_reason=?, updated_at=?`,
		row.Provider, row.ChannelID, row.ChannelName, groups, row.RemotePriority, row.RemoteWeight,
		boolInt(row.Managed), boolInt(row.ExternalTakeover), row.LastReason, row.UpdatedAt.Format(time.RFC3339Nano),
		row.ChannelName, groups, row.RemotePriority, row.RemoteWeight, boolInt(row.Managed), boolInt(row.ExternalTakeover),
		row.LastReason, row.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) CostFieldOwnership(ctx context.Context, provider, channelID string) (domain.CostFieldOwnership, bool, error) {
	var out domain.CostFieldOwnership
	var groups, updated string
	var managed, takeover int
	err := s.row(ctx, `SELECT provider, channel_id, channel_name, remote_groups, remote_priority, remote_weight,
		managed, external_takeover, last_reason, updated_at FROM scheduler_cost_field_ownership WHERE provider=? AND channel_id=?`,
		domain.NormalizeSchedulerProvider(provider), strings.TrimSpace(channelID)).
		Scan(&out.Provider, &out.ChannelID, &out.ChannelName, &groups, &out.RemotePriority, &out.RemoteWeight,
			&managed, &takeover, &out.LastReason, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.RemoteGroups = domain.SplitGroups(groups)
	out.Managed, out.ExternalTakeover = boolFromInt(managed), boolFromInt(takeover)
	out.UpdatedAt = parseTime(updated)
	return out, true, nil
}

func (s *Store) ListCostFieldOwnership(ctx context.Context, provider string) ([]domain.CostFieldOwnership, error) {
	rows, err := s.query(ctx, `SELECT provider, channel_id, channel_name, remote_groups, remote_priority, remote_weight,
		managed, external_takeover, last_reason, updated_at FROM scheduler_cost_field_ownership WHERE provider=? ORDER BY channel_id`, domain.NormalizeSchedulerProvider(provider))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CostFieldOwnership{}
	for rows.Next() {
		var row domain.CostFieldOwnership
		var groups, updated string
		var managed, takeover int
		if err := rows.Scan(&row.Provider, &row.ChannelID, &row.ChannelName, &groups, &row.RemotePriority, &row.RemoteWeight,
			&managed, &takeover, &row.LastReason, &updated); err != nil {
			return nil, err
		}
		row.RemoteGroups = domain.SplitGroups(groups)
		row.Managed, row.ExternalTakeover = boolFromInt(managed), boolFromInt(takeover)
		row.UpdatedAt = parseTime(updated)
		out = append(out, row)
	}
	return out, rows.Err()
}
