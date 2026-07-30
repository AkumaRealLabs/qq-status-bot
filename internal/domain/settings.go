package domain

import (
	"errors"
	"net/url"
	"strings"
)

type Settings struct {
	QQBotAppID        string   `json:"qqbot_app_id"`
	QQBotAppSecret    string   `json:"qqbot_app_secret,omitempty"`
	QQBotAppSecretSet bool     `json:"qqbot_app_secret_set,omitempty"`
	AllowedGroups     []string `json:"qqbot_allowed_groups"`
	Commands          []string `json:"status_commands"`
	StatusURL         string   `json:"status_url"`
	StatusPageID      string   `json:"status_page_id"`
	StatusPeriod      string   `json:"status_period"`
	ScreenshotTimeout int      `json:"screenshot_timeout_seconds"`
	QueueSize         int      `json:"screenshot_queue_size"`
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
