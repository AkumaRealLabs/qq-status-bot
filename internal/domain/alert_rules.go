package domain

// EffectiveFailureThreshold 返回余额/凭据查询的连续失败告警阈值。
func EffectiveFailureThreshold(rules NotificationRules) int {
	if rules.FailureThreshold <= 0 {
		return DefaultNotificationRules().FailureThreshold
	}
	return rules.FailureThreshold
}

func UpstreamAlerting(failures, threshold int) bool {
	if threshold <= 0 {
		threshold = DefaultNotificationRules().FailureThreshold
	}
	return failures >= threshold
}
