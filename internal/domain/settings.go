package domain

import (
	"errors"
	"fmt"
	"net/mail"
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
	GGAPIBalanceEnabled  bool     `json:"ggapi_balance_enabled"`
	GGAPIBaseURL         string   `json:"ggapi_base_url"`
	GGAPIAdminToken      string   `json:"ggapi_admin_token,omitempty"`
	GGAPIAdminTokenSet   bool     `json:"ggapi_admin_token_set,omitempty"`
	GGAPISmtpHost        string   `json:"ggapi_smtp_host"`
	GGAPISmtpPort        int      `json:"ggapi_smtp_port"`
	GGAPISmtpUsername    string   `json:"ggapi_smtp_username"`
	GGAPISmtpPassword    string   `json:"ggapi_smtp_password,omitempty"`
	GGAPISmtpPasswordSet bool     `json:"ggapi_smtp_password_set,omitempty"`
	GGAPISmtpFrom        string   `json:"ggapi_smtp_from"`
	GGAPISmtpFromName    string   `json:"ggapi_smtp_from_name"`
	GGAPISmtpTLSMode     string   `json:"ggapi_smtp_tls_mode"`
}

// AccountBinding 是成员与 GGAPI 用户之间的持久绑定。Email 和 MemberOpenID
// 只在服务端内部使用，管理接口通过 PublicView 返回脱敏数据。
type AccountBinding struct {
	ID               string `json:"id"`
	MemberOpenID     string `json:"member_openid"`
	Email            string `json:"email"`
	GGAPIUserID      string `json:"ggapi_user_id"`
	Username         string `json:"username"`
	FirstGroupOpenID string `json:"first_group_openid"`
	BoundAt          string `json:"bound_at"`
}

type AccountBindingView struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	GGAPIUserID      string `json:"ggapi_user_id"`
	Username         string `json:"username"`
	FirstGroupOpenID string `json:"first_group_openid"`
	BoundAt          string `json:"bound_at"`
}

func (b AccountBinding) PublicView() AccountBindingView {
	return AccountBindingView{ID: b.ID, Email: MaskEmail(b.Email), GGAPIUserID: b.GGAPIUserID,
		Username: b.Username, FirstGroupOpenID: b.FirstGroupOpenID, BoundAt: b.BoundAt}
}

func MaskEmail(raw string) string {
	email := strings.TrimSpace(strings.ToLower(raw))
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return "***"
	}
	local, host := email[:at], email[at+1:]
	return local[:1] + "***@" + host
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
	out.GGAPIAdminTokenSet = strings.TrimSpace(s.GGAPIAdminToken) != ""
	out.GGAPIAdminToken = ""
	out.GGAPISmtpPasswordSet = strings.TrimSpace(s.GGAPISmtpPassword) != ""
	out.GGAPISmtpPassword = ""
	out.AllowedGroups = append([]string{}, s.AllowedGroups...)
	out.Commands = append([]string{}, s.Commands...)
	out.AlertGroups = append([]string{}, s.AlertGroups...)
	if out.AlertFailureSamples <= 0 {
		out.AlertFailureSamples = 2
	}
	if out.AlertRecoverySamples <= 0 {
		out.AlertRecoverySamples = 2
	}
	if out.GGAPISmtpTLSMode == "" {
		out.GGAPISmtpTLSMode = "starttls"
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
	if strings.TrimSpace(s.GGAPIAdminToken) == "" {
		out.GGAPIAdminToken = old.GGAPIAdminToken
	} else {
		out.GGAPIAdminToken = strings.TrimSpace(s.GGAPIAdminToken)
	}
	if strings.TrimSpace(s.GGAPISmtpPassword) == "" {
		out.GGAPISmtpPassword = old.GGAPISmtpPassword
	} else {
		out.GGAPISmtpPassword = strings.TrimSpace(s.GGAPISmtpPassword)
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
	if strings.TrimSpace(s.GGAPIBaseURL) == "" {
		out.GGAPIBaseURL = old.GGAPIBaseURL
	} else {
		out.GGAPIBaseURL = strings.TrimRight(strings.TrimSpace(s.GGAPIBaseURL), "/")
	}
	if strings.TrimSpace(s.GGAPISmtpHost) == "" {
		out.GGAPISmtpHost = old.GGAPISmtpHost
	} else {
		out.GGAPISmtpHost = strings.TrimSpace(s.GGAPISmtpHost)
	}
	if strings.TrimSpace(s.GGAPISmtpUsername) == "" {
		out.GGAPISmtpUsername = old.GGAPISmtpUsername
	} else {
		out.GGAPISmtpUsername = strings.TrimSpace(s.GGAPISmtpUsername)
	}
	if strings.TrimSpace(s.GGAPISmtpFrom) == "" {
		out.GGAPISmtpFrom = old.GGAPISmtpFrom
	} else {
		out.GGAPISmtpFrom = strings.TrimSpace(s.GGAPISmtpFrom)
	}
	if strings.TrimSpace(s.GGAPISmtpFromName) == "" {
		out.GGAPISmtpFromName = old.GGAPISmtpFromName
	} else {
		out.GGAPISmtpFromName = strings.TrimSpace(s.GGAPISmtpFromName)
	}
	if strings.TrimSpace(s.GGAPISmtpTLSMode) == "" {
		out.GGAPISmtpTLSMode = old.GGAPISmtpTLSMode
	} else {
		out.GGAPISmtpTLSMode = strings.ToLower(strings.TrimSpace(s.GGAPISmtpTLSMode))
	}
	if s.GGAPISmtpPort <= 0 {
		out.GGAPISmtpPort = old.GGAPISmtpPort
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
	out.QQBotAppSecretSet = strings.TrimSpace(out.QQBotAppSecret) != ""
	out.GGAPIAdminTokenSet = strings.TrimSpace(out.GGAPIAdminToken) != ""
	out.GGAPISmtpPasswordSet = strings.TrimSpace(out.GGAPISmtpPassword) != ""
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
	if s.GGAPIBalanceEnabled {
		base, err := url.Parse(strings.TrimSpace(s.GGAPIBaseURL))
		if err != nil || base.Host == "" || base.Scheme != "https" || base.User != nil {
			return errors.New("启用 GGAPI 余额时，GGAPI 地址必须是 HTTPS URL")
		}
		if strings.TrimSpace(s.GGAPIAdminToken) == "" {
			return errors.New("启用 GGAPI 余额时必须配置管理令牌")
		}
		if strings.TrimSpace(s.GGAPISmtpHost) == "" {
			return errors.New("启用 GGAPI 余额时必须配置 SMTP 主机")
		}
		if s.GGAPISmtpPort < 1 || s.GGAPISmtpPort > 65535 {
			return errors.New("SMTP 端口必须在 1 到 65535 之间")
		}
		if strings.TrimSpace(s.GGAPISmtpUsername) == "" || strings.TrimSpace(s.GGAPISmtpPassword) == "" {
			return errors.New("启用 GGAPI 余额时必须配置 SMTP 凭证")
		}
		from := strings.TrimSpace(s.GGAPISmtpFrom)
		if from == "" {
			return errors.New("SMTP 发件人不能为空")
		}
		if _, err := mail.ParseAddress(from); err != nil {
			return fmt.Errorf("SMTP 发件人格式无效: %w", err)
		}
		mode := strings.ToLower(strings.TrimSpace(s.GGAPISmtpTLSMode))
		if mode == "" {
			mode = "starttls"
		}
		switch mode {
		case "implicit_tls", "implicit-tls", "tls", "starttls":
		default:
			return errors.New("SMTP TLS 模式只支持 implicit_tls 或 starttls")
		}
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
