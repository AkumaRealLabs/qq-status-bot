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

// AlertEventType 将余额与凭据告警映射为运维事件元数据。
func AlertEventType(kind string, recover bool) (eventType, targetType, targetID string) {
	if strings.HasPrefix(kind, "runway:") {
		return "balance_runway_low", "upstream", ""
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
	case "balance_low":
		return "余额低"
	case "credential_invalid":
		return "凭据失效"
	case "balance_query_failed":
		return "额度查询失败"
	case "balance_runway_low":
		return "余额预计耗尽"
	default:
		return "系统事件"
	}
}

// AlertOpsActions 列出某事件类型建议的运维动作。
func AlertOpsActions(eventType string) []string {
	switch eventType {
	case "credential_invalid", "balance_query_failed":
		return []string{"check_upstream", "sync_keys"}
	case "balance_low":
		return []string{"check_upstream"}
	case "balance_runway_low":
		return []string{"check_upstream"}
	case "cliproxy_error":
		return []string{"refresh_cliproxy_accounts"}
	default:
		return nil
	}
}
