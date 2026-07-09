package domain

import "strings"

// ShouldNotify 判断某事件类型是否应触发通知渠道。
func ShouldNotify(rules NotificationRules, eventType string, recover bool) bool {
	if !rules.Enabled {
		return false
	}
	if recover && !rules.Recovery {
		return false
	}
	if rules.EventTypes == nil {
		return false
	}
	return rules.EventTypes[eventType]
}

// AlertEventType 将探测/上游告警 kind 映射为运维事件元数据。
// kind 示例："ping:<cardID>"、"quota:<cardID>"、"balance"、"credential"。
func AlertEventType(kind string, recover bool) (eventType, targetType, targetID string) {
	if strings.HasPrefix(kind, "ping:") {
		return "probe_failed", "card", strings.TrimPrefix(kind, "ping:")
	}
	if strings.HasPrefix(kind, "quota:") {
		return "quota_exhausted", "card", strings.TrimPrefix(kind, "quota:")
	}
	if strings.HasPrefix(kind, "internal:") {
		return "probe_internal_error", "card", strings.TrimPrefix(kind, "internal:")
	}
	switch kind {
	case "balance":
		return "balance_low", "upstream", ""
	case "credential":
		return "credential_invalid", "upstream", ""
	case "balance_query":
		return "balance_query_failed", "upstream", ""
	default:
		if recover {
			return "system_recovered", "upstream", ""
		}
		return "system_warning", "upstream", ""
	}
}

// AlertOpsTitle 是运维事件类型的可读标题。
func AlertOpsTitle(eventType string, recover bool) string {
	if recover {
		return "已恢复"
	}
	switch eventType {
	case "probe_failed":
		return "探测失败"
	case "quota_exhausted":
		return "余额不足/成本池不可用"
	case "probe_internal_error":
		return "本地探测错误"
	case "balance_low":
		return "余额低"
	case "credential_invalid":
		return "凭据失效"
	case "balance_query_failed":
		return "额度查询失败"
	default:
		return "系统事件"
	}
}

// AlertOpsActions 列出某事件类型建议的运维动作。
func AlertOpsActions(eventType string) []string {
	switch eventType {
	case "probe_failed", "quota_exhausted", "probe_internal_error":
		return []string{"check_card"}
	case "credential_invalid", "balance_query_failed":
		return []string{"check_upstream", "sync_keys"}
	case "balance_low":
		return []string{"check_upstream"}
	case "cliproxy_error":
		return []string{"refresh_cliproxy_accounts"}
	default:
		return nil
	}
}

// ProbeAlertKind 构造探测失败的告警 kind 与消息。
func ProbeAlertKind(cardName, cardID, errText string) (kind, message string) {
	if IsQuotaProbeError(errText) {
		return "quota:" + cardID, cardName + " 余额不足/成本池不可用: " + errText
	}
	return "ping:" + cardID, cardName + " 探测失败: " + errText
}
