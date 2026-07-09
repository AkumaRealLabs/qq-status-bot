package domain

import "strings"

// KeepText keeps the stored non-secret value when the request leaves the field empty.
func KeepText(in, old string) string {
	if strings.TrimSpace(in) == "" {
		return old
	}
	return strings.TrimSpace(in)
}

// DefaultSiteName is used when site_name is empty.
const DefaultSiteName = "AI 上游监控"

// DefaultCLIProxyName is used when cliproxy name is empty.
const DefaultCLIProxyName = "CLIProxyAPI"

// MergeUpdate applies empty-secret keep + preserves runtime/identity fields from old.
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

// MergeUpdate applies secret keep, empty-source keep, runtime preserve, and binding→auto-disable rules.
// Call after assembling request fields; normalize/validate separately in app.
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

	// Changing scheduler binding clears auto-disabled (channel may still be set by caller).
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

// ApplySchedulerGroupPatch applies optional group change semantics used by PATCH /cards:
// empty or changed group clears channel id/name and auto-disabled.
func ApplySchedulerGroupPatch(oldGroup, oldChannelID, oldChannelName string, oldAutoDisabled bool, newGroup string) (group, channelID, channelName string, autoDisabled bool) {
	group = newGroup
	channelID, channelName, autoDisabled = oldChannelID, oldChannelName, oldAutoDisabled
	if strings.TrimSpace(group) == "" || strings.TrimSpace(group) != oldGroup {
		channelID, channelName, autoDisabled = "", "", false
	}
	return group, channelID, channelName, autoDisabled
}

// ApplySchedulerChannelPatch clears auto-disabled when channel is cleared or changed.
func ApplySchedulerChannelPatch(oldChannelID string, oldAutoDisabled bool, newChannelID string) (channelID string, autoDisabled bool) {
	channelID = newChannelID
	autoDisabled = oldAutoDisabled
	if strings.TrimSpace(channelID) == "" || channelID != oldChannelID {
		autoDisabled = false
	}
	return channelID, autoDisabled
}

// MergeUpdate applies secret/text keep and preserves identity fields.
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

// MergeUpdate applies secret keep, defaults, and partial notification-rule keep.
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
	// Zero-value notification patch means "leave rules unchanged".
	if in.NotificationRules.EventTypes == nil && in.NotificationRules.FailureThreshold == 0 {
		out.NotificationRules = old.NotificationRules
	} else {
		out.NotificationRules = NormalizeNotificationRules(in.NotificationRules)
	}
	return out
}

// MergeUpdate applies secret keep and normalizes tiers/URL fields.
func (in SchedulerConfig) MergeUpdate(old SchedulerConfig) SchedulerConfig {
	out := in
	out.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	out.UserID = strings.TrimSpace(in.UserID)
	out.AccessToken = KeepSecret(in.AccessToken, old.AccessToken)
	out.Tiers = NormalizeSchedulerTiers(in.Tiers)
	return out
}

// MergeUpdate applies secret keep and name/URL defaults.
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
