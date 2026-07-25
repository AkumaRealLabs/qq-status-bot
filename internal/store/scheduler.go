package store

import (
	"context"
	"encoding/json"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	var cfg domain.SchedulerConfig
	var tiers string
	err := s.row(ctx, `SELECT scheduler_provider, scheduler_base_url, scheduler_user_id, scheduler_access_token, scheduler_unassigned_group, scheduler_tiers FROM settings WHERE id='default'`).
		Scan(&cfg.Provider, &cfg.BaseURL, &cfg.UserID, &cfg.AccessToken, &cfg.UnassignedGroup, &tiers)
	cfg.Provider = domain.NormalizeSchedulerProvider(cfg.Provider)
	cfg.Tiers = schedulerTiers(tiers)
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
	_, err = s.exec(ctx, `UPDATE settings SET scheduler_provider=?, scheduler_base_url=?, scheduler_user_id=?, scheduler_access_token=?, scheduler_unassigned_group=?, scheduler_tiers=? WHERE id='default'`,
		cfg.Provider, cfg.BaseURL, cfg.UserID, cfg.AccessToken, cfg.UnassignedGroup, string(b))
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
