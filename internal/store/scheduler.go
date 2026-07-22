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

func (s *Store) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	var cfg domain.SchedulerConfig
	var tiers string
	err := s.row(ctx, `SELECT scheduler_provider, scheduler_base_url, scheduler_user_id, scheduler_access_token, scheduler_unassigned_group, scheduler_tiers,
		scheduler_traffic_mode, scheduler_traffic_profile, scheduler_log_poll_seconds FROM settings WHERE id='default'`).
		Scan(&cfg.Provider, &cfg.BaseURL, &cfg.UserID, &cfg.AccessToken, &cfg.UnassignedGroup, &tiers, &cfg.TrafficMode, &cfg.TrafficProfile, &cfg.TrafficPollSecs)
	cfg.Provider = domain.NormalizeSchedulerProvider(cfg.Provider)
	cfg.Tiers = schedulerTiers(tiers)
	cfg.TrafficMode = domain.NormalizeTrafficMode(cfg.TrafficMode)
	cfg.TrafficProfile = domain.NormalizeTrafficProfile(cfg.TrafficProfile)
	cfg.TrafficPollSecs = domain.NormalizeTrafficPollSeconds(cfg.TrafficPollSecs)
	return cfg, err
}

// UpdateSchedulerConfig 持久化调度配置。调用方应先 MergeUpdate。
// 对直连 store 的调用方保留防御性 MergeUpdate。
func (s *Store) UpdateSchedulerConfig(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerConfig, error) {
	old, err := s.SchedulerConfig(ctx)
	if err != nil {
		return cfg, err
	}
	cfg = cfg.MergeUpdate(old)
	b, err := json.Marshal(cfg.Tiers)
	if err != nil {
		return cfg, err
	}
	_, err = s.exec(ctx, `UPDATE settings SET scheduler_provider=?, scheduler_base_url=?, scheduler_user_id=?, scheduler_access_token=?, scheduler_unassigned_group=?, scheduler_tiers=?,
		scheduler_traffic_mode=?, scheduler_traffic_profile=?, scheduler_log_poll_seconds=? WHERE id='default'`,
		cfg.Provider, cfg.BaseURL, cfg.UserID, cfg.AccessToken, cfg.UnassignedGroup, string(b), cfg.TrafficMode, cfg.TrafficProfile, cfg.TrafficPollSecs)
	return cfg, err
}

func schedulerTiers(raw string) []domain.SchedulerTier {
	var tiers []domain.SchedulerTier
	_ = json.Unmarshal([]byte(raw), &tiers)
	return domain.NormalizeSchedulerTiers(tiers)
}

func (s *Store) CreateSchedulerLog(ctx context.Context, log domain.SchedulerLog) error {
	if log.ID == "" {
		log.ID = NewID()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	if log.Provider == "" {
		log.Provider = domain.SchedulerProviderGGAPI
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_logs (id, card_id, card_name, channel_id, channel_name, action, status, message, reason, provider, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, log.ID, log.CardID, log.CardName, log.ChannelID, log.ChannelName, log.Action, log.Status, log.Message, log.Reason, log.Provider, log.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) SchedulerLogs(ctx context.Context, limit int) ([]domain.SchedulerLog, error) {
	return s.schedulerLogs(ctx, "", limit)
}

func (s *Store) SchedulerLogsForProvider(ctx context.Context, provider string, limit int) ([]domain.SchedulerLog, error) {
	return s.schedulerLogs(ctx, domain.NormalizeSchedulerProvider(provider), limit)
}

func (s *Store) schedulerLogs(ctx context.Context, provider string, limit int) ([]domain.SchedulerLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id, card_id, card_name, channel_id, channel_name, action, status, message, reason, provider, created_at FROM scheduler_logs`
	args := []any{}
	if provider != "" {
		query += ` WHERE provider=?`
		args = append(args, provider)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SchedulerLog{}
	for rows.Next() {
		var log domain.SchedulerLog
		var created string
		if err := rows.Scan(&log.ID, &log.CardID, &log.CardName, &log.ChannelID, &log.ChannelName, &log.Action, &log.Status, &log.Message, &log.Reason, &log.Provider, &created); err != nil {
			return nil, err
		}
		log.CreatedAt = parseTime(created)
		out = append(out, log)
	}
	return out, rows.Err()
}

func (s *Store) SaveSchedulerChannelCostSnapshot(ctx context.Context, snap domain.SchedulerChannelCostSnapshot) (domain.SchedulerChannelCostSnapshot, error) {
	snap.ChannelID = strings.TrimSpace(snap.ChannelID)
	if snap.ChannelID == "" {
		return snap, nil
	}
	if snap.ID == "" {
		snap.ID = NewID()
	}
	if snap.EffectiveAt.IsZero() {
		snap.EffectiveAt = time.Now().UTC()
	}
	if snap.Provider == "" {
		snap.Provider = domain.SchedulerProviderGGAPI
	}
	if latest, ok, err := s.latestSchedulerChannelCostSnapshotForProvider(ctx, snap.Provider, snap.ChannelID); err != nil {
		return snap, err
	} else if ok && sameChannelCostSnapshot(latest, snap) {
		return latest, nil
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_channel_cost_snapshots
		(id, channel_id, channel_name, card_id, card_name, source_type, upstream_id, upstream_name, key_id, key_name, cost_per_unit, active, missing_reason, provider, effective_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID, snap.ChannelID, snap.ChannelName, snap.CardID, snap.CardName, snap.SourceType, snap.UpstreamID, snap.UpstreamName,
		snap.KeyID, snap.KeyName, snap.CostPerUnit, boolInt(snap.Active), snap.MissingReason, snap.Provider, snap.EffectiveAt.UTC().Format(time.RFC3339Nano))
	return snap, err
}

func (s *Store) LatestSchedulerChannelCostSnapshot(ctx context.Context, channelID string) (domain.SchedulerChannelCostSnapshot, bool, error) {
	return s.latestSchedulerChannelCostSnapshotForProvider(ctx, domain.SchedulerProviderGGAPI, channelID)
}

func (s *Store) latestSchedulerChannelCostSnapshotForProvider(ctx context.Context, provider, channelID string) (domain.SchedulerChannelCostSnapshot, bool, error) {
	return s.schedulerChannelCostSnapshot(ctx, `provider=? AND channel_id=?`, provider, channelID)
}

func (s *Store) LatestSchedulerChannelCostSnapshotForProvider(ctx context.Context, provider, channelID string) (domain.SchedulerChannelCostSnapshot, bool, error) {
	return s.latestSchedulerChannelCostSnapshotForProvider(ctx, provider, channelID)
}

func (s *Store) SchedulerChannelCostSnapshotAt(ctx context.Context, channelID string, at time.Time) (domain.SchedulerChannelCostSnapshot, bool, error) {
	return s.schedulerChannelCostSnapshot(ctx, `provider=? AND channel_id=? AND effective_at<=?`, domain.SchedulerProviderGGAPI, channelID, at.UTC().Format(time.RFC3339Nano))
}

func (s *Store) FirstSchedulerChannelCostSnapshot(ctx context.Context, channelID string) (domain.SchedulerChannelCostSnapshot, bool, error) {
	return s.schedulerChannelCostSnapshotOrder(ctx, `provider=? AND channel_id=?`, `effective_at ASC`, domain.SchedulerProviderGGAPI, channelID)
}

func (s *Store) schedulerChannelCostSnapshot(ctx context.Context, where string, args ...any) (domain.SchedulerChannelCostSnapshot, bool, error) {
	return s.schedulerChannelCostSnapshotOrder(ctx, where, `effective_at DESC`, args...)
}

func (s *Store) schedulerChannelCostSnapshotOrder(ctx context.Context, where, order string, args ...any) (domain.SchedulerChannelCostSnapshot, bool, error) {
	var snap domain.SchedulerChannelCostSnapshot
	var active int
	var effective string
	err := s.row(ctx, `SELECT id, channel_id, channel_name, card_id, card_name, source_type, upstream_id, upstream_name, key_id, key_name, cost_per_unit, active, missing_reason, provider, effective_at
		FROM scheduler_channel_cost_snapshots WHERE `+where+` ORDER BY `+order+` LIMIT 1`, args...).
		Scan(&snap.ID, &snap.ChannelID, &snap.ChannelName, &snap.CardID, &snap.CardName, &snap.SourceType, &snap.UpstreamID, &snap.UpstreamName,
			&snap.KeyID, &snap.KeyName, &snap.CostPerUnit, &active, &snap.MissingReason, &snap.Provider, &effective)
	if errors.Is(err, sql.ErrNoRows) {
		return snap, false, nil
	}
	snap.Active = boolFromInt(active)
	snap.EffectiveAt = parseTime(effective)
	return snap, err == nil, err
}

func sameChannelCostSnapshot(a, b domain.SchedulerChannelCostSnapshot) bool {
	return a.ChannelName == b.ChannelName && a.CardID == b.CardID && a.CardName == b.CardName &&
		a.SourceType == b.SourceType && a.UpstreamID == b.UpstreamID && a.UpstreamName == b.UpstreamName &&
		a.KeyID == b.KeyID && a.KeyName == b.KeyName && a.CostPerUnit == b.CostPerUnit &&
		a.Active == b.Active && a.MissingReason == b.MissingReason && a.Provider == b.Provider
}

func (s *Store) SaveSchedulerGroupSaleSnapshot(ctx context.Context, snap domain.SchedulerGroupSaleSnapshot) (domain.SchedulerGroupSaleSnapshot, error) {
	snap.Group = strings.TrimSpace(snap.Group)
	if snap.Group == "" {
		return snap, nil
	}
	if snap.ID == "" {
		snap.ID = NewID()
	}
	if snap.EffectiveAt.IsZero() {
		snap.EffectiveAt = time.Now().UTC()
	}
	if latest, ok, err := s.LatestSchedulerGroupSaleSnapshot(ctx, snap.Group); err != nil {
		return snap, err
	} else if ok && latest.Tag == snap.Tag && latest.SalePrice == snap.SalePrice && latest.Active == snap.Active {
		return latest, nil
	}
	_, err := s.exec(ctx, `INSERT INTO scheduler_group_sale_snapshots (id, group_name, tag, sale_price, active, effective_at)
		VALUES (?, ?, ?, ?, ?, ?)`, snap.ID, snap.Group, snap.Tag, snap.SalePrice, boolInt(snap.Active), snap.EffectiveAt.UTC().Format(time.RFC3339Nano))
	return snap, err
}

func (s *Store) LatestSchedulerGroupSaleSnapshot(ctx context.Context, group string) (domain.SchedulerGroupSaleSnapshot, bool, error) {
	return s.schedulerGroupSaleSnapshot(ctx, `group_name=?`, group)
}

func (s *Store) SchedulerGroupSaleSnapshotAt(ctx context.Context, group string, at time.Time) (domain.SchedulerGroupSaleSnapshot, bool, error) {
	return s.schedulerGroupSaleSnapshot(ctx, `group_name=? AND effective_at<=?`, group, at.UTC().Format(time.RFC3339Nano))
}

func (s *Store) FirstSchedulerGroupSaleSnapshot(ctx context.Context, group string) (domain.SchedulerGroupSaleSnapshot, bool, error) {
	return s.schedulerGroupSaleSnapshotOrder(ctx, `group_name=?`, `effective_at ASC`, group)
}

func (s *Store) schedulerGroupSaleSnapshot(ctx context.Context, where string, args ...any) (domain.SchedulerGroupSaleSnapshot, bool, error) {
	return s.schedulerGroupSaleSnapshotOrder(ctx, where, `effective_at DESC`, args...)
}

func (s *Store) schedulerGroupSaleSnapshotOrder(ctx context.Context, where, order string, args ...any) (domain.SchedulerGroupSaleSnapshot, bool, error) {
	var snap domain.SchedulerGroupSaleSnapshot
	var active int
	var effective string
	err := s.row(ctx, `SELECT id, group_name, tag, sale_price, active, effective_at FROM scheduler_group_sale_snapshots WHERE `+where+` ORDER BY `+order+` LIMIT 1`, args...).
		Scan(&snap.ID, &snap.Group, &snap.Tag, &snap.SalePrice, &active, &effective)
	if errors.Is(err, sql.ErrNoRows) {
		return snap, false, nil
	}
	snap.Active = boolFromInt(active)
	snap.EffectiveAt = parseTime(effective)
	return snap, err == nil, err
}

func (s *Store) SchedulerSaleSnapshotGroups(ctx context.Context, end time.Time) ([]string, error) {
	rows, err := s.query(ctx, `SELECT DISTINCT group_name FROM scheduler_group_sale_snapshots WHERE group_name<>'' AND effective_at<=? ORDER BY group_name`, end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	return out, rows.Err()
}
