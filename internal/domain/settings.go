package domain

import (
	"errors"
	"net/url"
	"strings"
)

type Settings struct {
	QQBotAppID           string   `json:"qqbot_app_id"`
	QQBotAppSecret       string   `json:"qqbot_app_secret,omitempty"`
	QQBotAppSecretSet    bool     `json:"qqbot_app_secret_set,omitempty"`
	AllowedGroups        []string `json:"qqbot_allowed_groups"`
	Commands             []string `json:"status_commands"`
	StatusURL            string   `json:"status_url"`
	StatusPageID         string   `json:"status_page_id"`
	StatusPeriod         string   `json:"status_period"`
	ScreenshotTimeout    int      `json:"screenshot_timeout_seconds"`
	QueueSize            int      `json:"screenshot_queue_size"`
	AlertsEnabled        bool     `json:"alerts_enabled"`
	AlertGroups          []string `json:"alert_groups"`
	AlertFailureSamples  int      `json:"alert_failure_samples"`
	AlertRecoverySamples int      `json:"alert_recovery_samples"`
}

// AlertState 保存告警状态，避免进程或容器重启后重复通知。
type AlertState struct {
	Enabled    bool                      `json:"enabled"`
	SourceKey  string                    `json:"source_key"`
	PollFailed bool                      `json:"poll_failed"`
	Nodes      map[string]AlertNodeState `json:"nodes"`
}

type AlertNodeState struct {
	LastHeartbeat    string         `json:"last_heartbeat,omitempty"`
	LastStatus       int            `json:"last_status,omitempty"`
	OfflineSamples   int            `json:"offline_samples,omitempty"`
	OnlineSamples    int            `json:"online_samples,omitempty"`
	IncidentStarted  string         `json:"incident_started,omitempty"`
	ConfirmedOffline bool           `json:"confirmed_offline,omitempty"`
	OfflineAttempts  map[string]int `json:"offline_attempts,omitempty"`
	NotifiedGroups   []string       `json:"notified_groups,omitempty"`
	RecoveryTime     string         `json:"recovery_time,omitempty"`
	RecoveryAttempts map[string]int `json:"recovery_attempts,omitempty"`
}

type EventLog struct {
	ID          string `json:"id"`
	Direction   string `json:"direction"`
	EventType   string `json:"event_type"`
	GroupOpenID string `json:"group_openid,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func (s Settings) Public() Settings {
	out := s
	out.QQBotAppSecretSet = strings.TrimSpace(s.QQBotAppSecret) != ""
	out.QQBotAppSecret = ""
	out.AllowedGroups = append([]string{}, s.AllowedGroups...)
	out.Commands = append([]string{}, s.Commands...)
	out.AlertGroups = append([]string{}, s.AlertGroups...)
	if out.AlertFailureSamples <= 0 {
		out.AlertFailureSamples = 2
	}
	if out.AlertRecoverySamples <= 0 {
		out.AlertRecoverySamples = 2
	}
	return out
}

func (s Settings) MergeUpdate(old Settings) Settings {
	out := s
	out.QQBotAppID = strings.TrimSpace(s.QQBotAppID)
	if out.QQBotAppID == "" {
		out.QQBotAppID = old.QQBotAppID
	}
	if strings.TrimSpace(s.QQBotAppSecret) == "" {
		out.QQBotAppSecret = old.QQBotAppSecret
	} else {
		out.QQBotAppSecret = strings.TrimSpace(s.QQBotAppSecret)
	}
	out.AllowedGroups = normalizeList(s.AllowedGroups)
	out.AlertGroups = normalizeList(s.AlertGroups)
	out.Commands = normalizeList(s.Commands)
	if len(out.Commands) == 0 {
		out.Commands = old.Commands
	}
	if strings.TrimSpace(s.StatusURL) == "" {
		out.StatusURL = old.StatusURL
	} else {
		out.StatusURL = strings.TrimRight(strings.TrimSpace(s.StatusURL), "/")
	}
	if strings.TrimSpace(s.StatusPageID) == "" {
		out.StatusPageID = old.StatusPageID
	} else {
		out.StatusPageID = strings.TrimSpace(s.StatusPageID)
	}
	if strings.TrimSpace(s.StatusPeriod) == "" {
		out.StatusPeriod = old.StatusPeriod
	} else {
		out.StatusPeriod = strings.TrimSpace(s.StatusPeriod)
	}
	if s.ScreenshotTimeout <= 0 {
		out.ScreenshotTimeout = old.ScreenshotTimeout
	}
	if s.QueueSize <= 0 {
		out.QueueSize = old.QueueSize
	}
	if out.AlertFailureSamples <= 0 {
		out.AlertFailureSamples = old.AlertFailureSamples
	}
	if out.AlertRecoverySamples <= 0 {
		out.AlertRecoverySamples = old.AlertRecoverySamples
	}
	if out.AlertFailureSamples <= 0 {
		out.AlertFailureSamples = 2
	}
	if out.AlertRecoverySamples <= 0 {
		out.AlertRecoverySamples = 2
	}
	return out
}

func (s Settings) Validate() error {
	parsed, err := url.Parse(strings.TrimSpace(s.StatusURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("状态图数据源必须是完整的 HTTP/HTTPS URL")
	}
	if strings.TrimSpace(s.StatusPageID) == "" {
		return errors.New("Page ID 不能为空")
	}
	if strings.TrimSpace(s.StatusPeriod) == "" {
		return errors.New("统计周期不能为空")
	}
	if s.ScreenshotTimeout < 15 || s.ScreenshotTimeout > 240 {
		return errors.New("状态图超时必须在 15 到 240 秒之间")
	}
	if s.QueueSize < 1 || s.QueueSize > 20 {
		return errors.New("队列长度必须在 1 到 20 之间")
	}
	if s.AlertFailureSamples > 20 {
		return errors.New("故障连续样本阈值必须在 1 到 20 之间")
	}
	if s.AlertRecoverySamples > 20 {
		return errors.New("恢复连续样本阈值必须在 1 到 20 之间")
	}
	if s.AlertsEnabled && len(normalizeList(s.AlertGroups)) == 0 {
		return errors.New("启用故障通知时至少配置一个告警群")
	}
	return nil
}

func normalizeList(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
