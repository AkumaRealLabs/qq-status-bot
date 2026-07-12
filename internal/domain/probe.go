package domain

import (
	"strings"
	"time"
)

// 调度恢复策略：已关渠足够久 + 最近连续成功。
const (
	SchedulerRestoreMinDuration  = 15 * time.Minute
	SchedulerRestoreSuccessCount = 3
)

// EffectiveFailureThreshold 返回告警失败阈值，未配置时回退默认值。
func EffectiveFailureThreshold(rules NotificationRules) int {
	if rules.FailureThreshold <= 0 {
		return DefaultNotificationRules().FailureThreshold
	}
	return rules.FailureThreshold
}

// EffectiveMuteThreshold 返回静音/自动关渠阈值，未配置时回退默认值。
func EffectiveMuteThreshold(rules NotificationRules) int {
	if rules.MuteFailureThreshold <= 0 {
		return DefaultNotificationRules().MuteFailureThreshold
	}
	return rules.MuteFailureThreshold
}

// EffectiveInternalRetry 返回探测本地错误的内部重试次数与间隔。
func EffectiveInternalRetry(rules NotificationRules) (retries int, interval time.Duration) {
	n := NormalizeNotificationRules(rules)
	return n.InternalRetryCount, time.Duration(n.InternalRetryIntervalMs) * time.Millisecond
}

func normalizeMuteAt(muteAt int) int {
	if muteAt <= 0 {
		return DefaultNotificationRules().MuteFailureThreshold
	}
	return muteAt
}

// ProbeMuted 表示卡片是否应显示为静音（失败连击或已自动关渠）。
func ProbeMuted(failureCount, muteAt int, autoDisabled bool) bool {
	muteAt = normalizeMuteAt(muteAt)
	return failureCount >= muteAt || autoDisabled
}

// SuppressProbeAlert 为 true 时不应发出失败告警：
// 已自动关渠，或本次失败次数不是静音边界那一次。
func SuppressProbeAlert(autoDisabled bool, failures, muteAt int) bool {
	muteAt = normalizeMuteAt(muteAt)
	return autoDisabled || failures != muteAt
}

// UpstreamAlerting 为 true 表示连续失败已达告警阈值。
func UpstreamAlerting(failures, threshold int) bool {
	if threshold <= 0 {
		threshold = DefaultNotificationRules().FailureThreshold
	}
	return failures >= threshold
}

// ShouldAutoDisableScheduler 判断是否应关闭绑定的调度渠道。
func ShouldAutoDisableScheduler(poolEnabled bool, channelID string, success bool, failures, muteAt int, alreadyDisabled bool) bool {
	if !poolEnabled || strings.TrimSpace(channelID) == "" || success || alreadyDisabled {
		return false
	}
	muteAt = normalizeMuteAt(muteAt)
	return failures == muteAt
}

// NeedsSchedulerRestoreTimestamp 为 true 表示已自动关渠但尚未记录 disabled-at。
func NeedsSchedulerRestoreTimestamp(disabledAt *time.Time) bool {
	return disabledAt == nil
}

// SchedulerRestoreReady 是纯恢复资格判断（关渠时长 + 最近 N 次成功）。
// recent 应为最新在前；只看前 SchedulerRestoreSuccessCount 条。
func SchedulerRestoreReady(disabledAt *time.Time, recent []ProbeRun, now time.Time) bool {
	if disabledAt == nil {
		return false
	}
	if now.Sub(*disabledAt) < SchedulerRestoreMinDuration {
		return false
	}
	if len(recent) < SchedulerRestoreSuccessCount {
		return false
	}
	for i := 0; i < SchedulerRestoreSuccessCount; i++ {
		if !recent[i].Success {
			return false
		}
	}
	return true
}

// IsQuotaProbeError 将探测错误文案归类为额度/余额耗尽。
func IsQuotaProbeError(errText string) bool {
	lower := strings.ToLower(errText)
	for _, needle := range []string{
		"余额不足",
		"额度不足",
		"预扣费不足",
		"预扣费额度失败",
		"insufficient_quota",
		"insufficient account balance",
		"not enough quota",
		"not enough balance",
		"pre-deduct",
		"prepaid balance",
		"quota exceeded",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
