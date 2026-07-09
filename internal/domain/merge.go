package domain

import "strings"

// KeepText：请求字段为空时保留库中非密钥字段。
func KeepText(in, old string) string {
	if strings.TrimSpace(in) == "" {
		return old
	}
	return strings.TrimSpace(in)
}

// DefaultSiteName：site_name 为空时的默认站点名。
const DefaultSiteName = "AI 上游监控"

// DefaultCLIProxyName：cliproxy 名称为空时的默认名。
const DefaultCLIProxyName = "CLIProxyAPI"

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
	return out
}

// MergeUpdate：密钥保留、空来源保留、运行时字段保留，以及绑定变更→清除自动关渠规则。
// 在拼好请求字段后调用；规范化/校验仍在 app 层单独做。
func (in ModelCard) MergeUpdate(old ModelCard) ModelCard {
	out := in
	out.APIKey = KeepSecret(in.APIKey, old.APIKey)
	if strings.TrimSpace(out.BaseURL) == "" && strings.TrimSpace(out.UpstreamID) == "" && strings.TrimSpace(out.KeyID) == "" {
		out.BaseURL, out.UpstreamID, out.KeyID = old.BaseURL, old.UpstreamID, old.KeyID
		if strings.TrimSpace(out.APIKey) == "" {
			out.APIKey = old.APIKey
		}
	}
	out.ID = old.ID
	out.LastError = old.LastError
	out.FailureCount = old.FailureCount
	out.SortOrder = old.SortOrder
	out.CreatedAt = old.CreatedAt
	out.SchedulerAutoDisabledAt = old.SchedulerAutoDisabledAt

	// 变更调度绑定时清除自动关渠（渠道仍可由调用方设置）。
	if schedulerBindingChanged(old, out) {
		out.SchedulerAutoDisabled = false
	}
	return out
}

func schedulerBindingChanged(old, next ModelCard) bool {
	if strings.TrimSpace(next.SchedulerChannelID) != strings.TrimSpace(old.SchedulerChannelID) {
		return true
	}
	if strings.TrimSpace(next.SchedulerGroup) != strings.TrimSpace(old.SchedulerGroup) {
		return true
	}
	if !next.PoolEnabled && old.PoolEnabled {
		return true
	}
	return false
}

// ApplySchedulerGroupPatch：PATCH /cards 的可选分组变更语义：
// 分组为空或变更时清除渠道 id/名称与自动关渠。
func ApplySchedulerGroupPatch(oldGroup, oldChannelID, oldChannelName string, oldAutoDisabled bool, newGroup string) (group, channelID, channelName string, autoDisabled bool) {
	group = newGroup
	channelID, channelName, autoDisabled = oldChannelID, oldChannelName, oldAutoDisabled
	if strings.TrimSpace(group) == "" || strings.TrimSpace(group) != oldGroup {
		channelID, channelName, autoDisabled = "", "", false
	}
	return group, channelID, channelName, autoDisabled
}

// ApplySchedulerChannelPatch：渠道清空或变更时清除自动关渠。
func ApplySchedulerChannelPatch(oldChannelID string, oldAutoDisabled bool, newChannelID string) (channelID string, autoDisabled bool) {
	channelID = newChannelID
	autoDisabled = oldAutoDisabled
	if strings.TrimSpace(channelID) == "" || channelID != oldChannelID {
		autoDisabled = false
	}
	return channelID, autoDisabled
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
	out.ProbeModel = ProbeModel
	out.SiteName = strings.TrimSpace(in.SiteName)
	if out.SiteName == "" {
		out.SiteName = DefaultSiteName
	}
	out.SiteIcon = strings.TrimSpace(in.SiteIcon)
	out.TelegramBotToken = KeepSecret(in.TelegramBotToken, old.TelegramBotToken)
	out.TelegramChatID = strings.TrimSpace(in.TelegramChatID)
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
	out.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	out.UserID = strings.TrimSpace(in.UserID)
	out.AccessToken = KeepSecret(in.AccessToken, old.AccessToken)
	out.UnassignedGroup = strings.TrimSpace(in.UnassignedGroup)
	out.Tiers = NormalizeSchedulerTiers(in.Tiers)
	return out
}

// MergeUpdate：密钥保留及名称/URL 默认值。
func (in CLIProxyConfig) MergeUpdate(old CLIProxyConfig) CLIProxyConfig {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	if out.Name == "" {
		out.Name = DefaultCLIProxyName
	}
	out.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	out.ManagementKey = KeepSecret(in.ManagementKey, old.ManagementKey)
	return out
}
