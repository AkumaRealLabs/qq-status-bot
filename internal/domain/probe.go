package domain

import (
	"strings"
	"time"
)

// Scheduler restore policy: disabled long enough + recent consecutive successes.
const (
	SchedulerRestoreMinDuration  = 15 * time.Minute
	SchedulerRestoreSuccessCount = 3
)

// EffectiveFailureThreshold returns the alert failure threshold, falling back to defaults.
func EffectiveFailureThreshold(rules NotificationRules) int {
	if rules.FailureThreshold <= 0 {
		return DefaultNotificationRules().FailureThreshold
	}
	return rules.FailureThreshold
}

// EffectiveMuteThreshold returns the mute / auto-disable threshold, falling back to defaults.
func EffectiveMuteThreshold(rules NotificationRules) int {
	if rules.MuteFailureThreshold <= 0 {
		return DefaultNotificationRules().MuteFailureThreshold
	}
	return rules.MuteFailureThreshold
}

// EffectiveInternalRetry returns probe internal-retry count and interval.
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

// ProbeMuted reports whether a card should show as muted (failure streak or auto-disabled).
func ProbeMuted(failureCount, muteAt int, autoDisabled bool) bool {
	muteAt = normalizeMuteAt(muteAt)
	return failureCount >= muteAt || autoDisabled
}

// SuppressProbeAlert is true when a failure alert should not fire:
// already auto-disabled, or this failure is not exactly the mute boundary.
func SuppressProbeAlert(autoDisabled bool, failures, muteAt int) bool {
	muteAt = normalizeMuteAt(muteAt)
	return autoDisabled || failures != muteAt
}

// UpstreamAlerting is true when consecutive failures reach the alert threshold.
func UpstreamAlerting(failures, threshold int) bool {
	if threshold <= 0 {
		threshold = DefaultNotificationRules().FailureThreshold
	}
	return failures >= threshold
}

// ShouldAutoDisableScheduler decides whether to close the bound scheduler channel.
func ShouldAutoDisableScheduler(poolEnabled bool, channelID string, success bool, failures, muteAt int, alreadyDisabled bool) bool {
	if !poolEnabled || strings.TrimSpace(channelID) == "" || success || alreadyDisabled {
		return false
	}
	muteAt = normalizeMuteAt(muteAt)
	return failures == muteAt
}

// NeedsSchedulerRestoreTimestamp is true when auto-disabled is set but disabled-at was never recorded.
func NeedsSchedulerRestoreTimestamp(disabledAt *time.Time) bool {
	return disabledAt == nil
}

// SchedulerRestoreReady is pure restore eligibility (duration + last N successes).
// recent should be newest-first; only the first SchedulerRestoreSuccessCount entries are considered.
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

// IsQuotaProbeError classifies probe error text as quota/balance exhaustion.
func IsQuotaProbeError(errText string) bool {
	lower := strings.ToLower(errText)
	for _, needle := range []string{
		"余额不足",
		"额度不足",
		"预扣费不足",
		"insufficient_quota",
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
