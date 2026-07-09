package store

import (
	"context"
	"encoding/json"
	"strings"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) Settings(ctx context.Context) (domain.Settings, error) {
	var cfg domain.Settings
	var rules string
	err := s.row(ctx, `SELECT check_interval_minutes, telegram_bot_token, telegram_chat_id, probe_model, site_name, site_icon, epay_base_url, epay_pid, epay_key, notification_rules FROM settings WHERE id='default'`).
		Scan(&cfg.CheckIntervalMinutes, &cfg.TelegramBotToken, &cfg.TelegramChatID, &cfg.ProbeModel, &cfg.SiteName, &cfg.SiteIcon, &cfg.EpayBaseURL, &cfg.EpayPID, &cfg.EpayKey, &rules)
	if err != nil {
		return cfg, err
	}
	if rules == "" {
		cfg.NotificationRules = domain.DefaultNotificationRules()
	} else {
		_ = json.Unmarshal([]byte(rules), &cfg.NotificationRules)
	}
	cfg.NotificationRules = domain.NormalizeNotificationRules(cfg.NotificationRules)
	cfg.CheckIntervalMinutes = domain.NormalizeCheckInterval(cfg.CheckIntervalMinutes)
	cfg.ProbeModel = domain.ProbeModel
	if cfg.SiteName == "" {
		cfg.SiteName = "AI 上游监控"
	}
	return cfg, nil
}

func (s *Store) UpdateSettings(ctx context.Context, cfg domain.Settings) (domain.Settings, error) {
	old, err := s.Settings(ctx)
	if err != nil {
		return cfg, err
	}
	cfg.CheckIntervalMinutes = domain.NormalizeCheckInterval(cfg.CheckIntervalMinutes)
	cfg.ProbeModel = domain.ProbeModel
	if cfg.SiteName == "" {
		cfg.SiteName = "AI 上游监控"
	}
	cfg.TelegramBotToken = domain.KeepSecret(cfg.TelegramBotToken, old.TelegramBotToken)
	cfg.TelegramChatID = strings.TrimSpace(cfg.TelegramChatID)
	cfg.EpayBaseURL = strings.TrimRight(strings.TrimSpace(cfg.EpayBaseURL), "/")
	cfg.EpayPID = strings.TrimSpace(cfg.EpayPID)
	cfg.EpayKey = domain.KeepSecret(cfg.EpayKey, old.EpayKey)
	if cfg.NotificationRules.EventTypes == nil && cfg.NotificationRules.FailureThreshold == 0 {
		current, err := s.NotificationRules(ctx)
		if err != nil {
			return cfg, err
		}
		cfg.NotificationRules = current
	} else {
		cfg.NotificationRules = domain.NormalizeNotificationRules(cfg.NotificationRules)
	}
	rules, err := json.Marshal(cfg.NotificationRules)
	if err != nil {
		return cfg, err
	}
	_, err = s.exec(ctx, `UPDATE settings SET check_interval_minutes=?, telegram_bot_token=?, telegram_chat_id=?, probe_model=?, site_name=?, site_icon=?, epay_base_url=?, epay_pid=?, epay_key=?, notification_rules=? WHERE id='default'`,
		cfg.CheckIntervalMinutes, cfg.TelegramBotToken, cfg.TelegramChatID, cfg.ProbeModel, cfg.SiteName, cfg.SiteIcon, cfg.EpayBaseURL, cfg.EpayPID, cfg.EpayKey, string(rules))
	return cfg, err
}
