package store

import (
	"context"
	"encoding/json"

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
		cfg.SiteName = domain.DefaultSiteName
	}
	return cfg, nil
}

// UpdateSettings 按入参持久化设置。调用方（app 层）应先 MergeUpdate。
// 对直连 store 的调用方（测试 / 旧路径）保留防御性 KeepSecret。
func (s *Store) UpdateSettings(ctx context.Context, cfg domain.Settings) (domain.Settings, error) {
	old, err := s.Settings(ctx)
	if err != nil {
		return cfg, err
	}
	// 防御：若调用方跳过了 app 合并，仍保留密钥与部分通知规则。
	cfg = cfg.MergeUpdate(old)
	rules, err := json.Marshal(cfg.NotificationRules)
	if err != nil {
		return cfg, err
	}
	_, err = s.exec(ctx, `UPDATE settings SET check_interval_minutes=?, telegram_bot_token=?, telegram_chat_id=?, probe_model=?, site_name=?, site_icon=?, epay_base_url=?, epay_pid=?, epay_key=?, notification_rules=? WHERE id='default'`,
		cfg.CheckIntervalMinutes, cfg.TelegramBotToken, cfg.TelegramChatID, cfg.ProbeModel, cfg.SiteName, cfg.SiteIcon, cfg.EpayBaseURL, cfg.EpayPID, cfg.EpayKey, string(rules))
	return cfg, err
}
