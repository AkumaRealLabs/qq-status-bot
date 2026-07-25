package domain

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var oneBotGroupIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// NormalizeOneBotGroupIDs 去除空白和重复项，并保持管理员输入顺序。
func NormalizeOneBotGroupIDs(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// ValidateOneBotSettings 仅在启用时要求完整配置，避免阻断尚未使用的安装。
func ValidateOneBotSettings(cfg Settings) error {
	if !cfg.OneBotEnabled {
		return nil
	}
	parsed, err := url.Parse(cfg.OneBotBaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("请配置有效的 OneBot HTTP 地址")
	}
	if strings.TrimSpace(cfg.OneBotHTTPToken) == "" {
		return errors.New("请配置 OneBot HTTP API Token")
	}
	if strings.TrimSpace(cfg.OneBotWebhookToken) == "" {
		return errors.New("请配置 OneBot Webhook Token")
	}
	if len(cfg.OneBotGroupIDs) == 0 {
		return errors.New("请至少配置一个 QQ 群号")
	}
	for _, groupID := range cfg.OneBotGroupIDs {
		if !oneBotGroupIDPattern.MatchString(groupID) {
			return errors.New("QQ 群号必须是正整数")
		}
		if _, err := strconv.ParseUint(groupID, 10, 64); err != nil {
			return errors.New("QQ 群号必须是正整数")
		}
	}
	return nil
}

// KeepText：请求字段为空时保留库中非密钥字段。
func KeepText(in, old string) string {
	if strings.TrimSpace(in) == "" {
		return old
	}
	return strings.TrimSpace(in)
}

// FirstNonEmpty 返回第一个非空白字符串。
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// DefaultSiteName：site_name 为空时的默认站点名。
const DefaultSiteName = "AI 上游监控"

// MergeUpdate：空密钥保留，并保留旧实体的运行时/身份字段。
func (in Upstream) MergeUpdate(old Upstream) Upstream {
	out := in
	out.ID = old.ID
	out.AccessToken = KeepSecret(in.AccessToken, old.AccessToken)
	out.Password = KeepSecret(in.Password, old.Password)
	out.Sub2APIAccessToken = KeepSecret(in.Sub2APIAccessToken, old.Sub2APIAccessToken)
	out.Sub2APIRefreshToken = KeepSecret(in.Sub2APIRefreshToken, old.Sub2APIRefreshToken)
	out.LastError = old.LastError
	out.FailureCount = old.FailureCount
	out.CreatedAt = old.CreatedAt
	out.RunwayWarningHours = old.RunwayWarningHours
	return out
}

// MergeUpdate：密钥/文本保留，并保留身份字段。
func (in RevenueCard) MergeUpdate(old RevenueCard) RevenueCard {
	out := in
	out.AccessToken = KeepSecret(in.AccessToken, old.AccessToken)
	out.AdminAPIKey = KeepSecret(in.AdminAPIKey, old.AdminAPIKey)
	out.EpayKey = KeepSecret(in.EpayKey, old.EpayKey)
	out.BaseURL = KeepText(in.BaseURL, old.BaseURL)
	out.UserID = KeepText(in.UserID, old.UserID)
	out.EpayPID = KeepText(in.EpayPID, old.EpayPID)
	if strings.TrimSpace(in.UpstreamID) == "" && strings.TrimSpace(in.SourceType) == old.SourceType {
		out.UpstreamID = old.UpstreamID
	}
	out.ID = old.ID
	out.SortOrder = old.SortOrder
	out.CreatedAt = old.CreatedAt
	return out
}

// MergeUpdate：密钥保留、默认值，以及通知规则的部分保留。
func (in Settings) MergeUpdate(old Settings) Settings {
	out := in
	out.CheckIntervalMinutes = NormalizeCheckInterval(in.CheckIntervalMinutes)
	out.SiteName = strings.TrimSpace(in.SiteName)
	if out.SiteName == "" {
		out.SiteName = DefaultSiteName
	}
	out.SiteIcon = strings.TrimSpace(in.SiteIcon)
	out.TelegramBotToken = KeepSecret(in.TelegramBotToken, old.TelegramBotToken)
	out.TelegramChatID = strings.TrimSpace(in.TelegramChatID)
	out.OneBotBaseURL = strings.TrimRight(strings.TrimSpace(in.OneBotBaseURL), "/")
	out.OneBotHTTPToken = KeepSecret(in.OneBotHTTPToken, old.OneBotHTTPToken)
	out.OneBotWebhookToken = KeepSecret(in.OneBotWebhookToken, old.OneBotWebhookToken)
	out.OneBotGroupIDs = NormalizeOneBotGroupIDs(in.OneBotGroupIDs)
	out.EpayBaseURL = strings.TrimRight(strings.TrimSpace(in.EpayBaseURL), "/")
	out.EpayPID = strings.TrimSpace(in.EpayPID)
	out.EpayKey = KeepSecret(in.EpayKey, old.EpayKey)
	// 通知规则补丁为零值表示「规则不变」。
	if in.NotificationRules.EventTypes == nil && in.NotificationRules.FailureThreshold == 0 {
		out.NotificationRules = old.NotificationRules
	} else {
		out.NotificationRules = NormalizeNotificationRules(in.NotificationRules)
	}
	return out
}

// MergeUpdate：密钥保留并规范化档位/URL/未分配分组字段。
func (in SchedulerConfig) MergeUpdate(old SchedulerConfig) SchedulerConfig {
	out := in
	if strings.TrimSpace(in.Provider) == "" {
		out.Provider = NormalizeSchedulerProvider(old.Provider)
	} else {
		out.Provider = NormalizeSchedulerProvider(in.Provider)
	}
	out.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	out.UserID = strings.TrimSpace(in.UserID)
	out.AccessToken = KeepSecret(in.AccessToken, old.AccessToken)
	out.UnassignedGroup = strings.TrimSpace(in.UnassignedGroup)
	out.Tiers = NormalizeSchedulerTiers(in.Tiers)
	return out
}

func (in AxonHubConfig) MergeUpdate(old AxonHubConfig) AxonHubConfig {
	out := in
	out.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	out.AdminEmail = strings.TrimSpace(in.AdminEmail)
	out.AdminPassword = KeepSecret(in.AdminPassword, old.AdminPassword)
	if strings.TrimSpace(in.ControlMode) == "" {
		out.ControlMode = NormalizeAxonHubControlMode(old.ControlMode)
	} else {
		out.ControlMode = NormalizeAxonHubControlMode(in.ControlMode)
	}
	return out
}
