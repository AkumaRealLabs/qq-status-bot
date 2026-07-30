package domain

import "strings"

type Settings struct {
	QQBotAppID         string   `json:"qqbot_app_id"`
	QQBotAppSecret     string   `json:"qqbot_app_secret,omitempty"`
	QQBotAppSecretSet  bool     `json:"qqbot_app_secret_set,omitempty"`
	AllowedGroups      []string `json:"qqbot_allowed_groups"`
	Commands           []string `json:"status_commands"`
	StatusURL          string   `json:"status_url"`
	ScreenshotSelector string   `json:"screenshot_selector"`
	ScreenshotWidth    int      `json:"screenshot_width"`
	ScreenshotHeight   int      `json:"screenshot_height"`
	ScreenshotWait     int      `json:"screenshot_wait_seconds"`
	ScreenshotTimeout  int      `json:"screenshot_timeout_seconds"`
	QueueSize          int      `json:"screenshot_queue_size"`
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
	out.AllowedGroups = append([]string(nil), s.AllowedGroups...)
	out.Commands = append([]string(nil), s.Commands...)
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
	if strings.TrimSpace(s.ScreenshotSelector) == "" {
		out.ScreenshotSelector = old.ScreenshotSelector
	}
	if s.ScreenshotWidth <= 0 {
		out.ScreenshotWidth = old.ScreenshotWidth
	}
	if s.ScreenshotHeight <= 0 {
		out.ScreenshotHeight = old.ScreenshotHeight
	}
	if s.ScreenshotWait <= 0 {
		out.ScreenshotWait = old.ScreenshotWait
	}
	if s.ScreenshotTimeout <= 0 {
		out.ScreenshotTimeout = old.ScreenshotTimeout
	}
	if s.QueueSize <= 0 {
		out.QueueSize = old.QueueSize
	}
	return out
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
