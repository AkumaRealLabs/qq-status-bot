package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ai-upstream-monitor/internal/domain"
)

const (
	ggapiOptionRetryTimes              = "RetryTimes"
	ggapiOptionRetryStatusCodes        = "AutomaticRetryStatusCodes"
	ggapiOptionDisableStatusCodes      = "AutomaticDisableStatusCodes"
	ggapiOptionChannelTestMode         = "monitor_setting.channel_test_mode"
	ggapiOptionAutoTestEnabled         = "monitor_setting.auto_test_channel_enabled"
	ggapiOptionAutoTestMinutes         = "monitor_setting.auto_test_channel_minutes"
	ggapiOptionAutomaticDisable        = "AutomaticDisableChannelEnabled"
	ggapiOptionAutomaticEnable         = "AutomaticEnableChannelEnabled"
	ggapiOptionAffinityEnabled         = "channel_affinity_setting.enabled"
	ggapiOptionAffinitySwitchOnSuccess = "channel_affinity_setting.switch_on_success"
	ggapiOptionAffinityKeepDisabled    = "channel_affinity_setting.keep_on_channel_disabled"
	ggapiOptionAffinityMaxEntries      = "channel_affinity_setting.max_entries"
	ggapiOptionAffinityDefaultTTL      = "channel_affinity_setting.default_ttl_seconds"
	ggapiOptionAffinityRules           = "channel_affinity_setting.rules"
)

var ggapiManagedOptionKeys = []string{
	ggapiOptionRetryTimes,
	ggapiOptionRetryStatusCodes,
	ggapiOptionDisableStatusCodes,
	ggapiOptionChannelTestMode,
	ggapiOptionAutoTestEnabled,
	ggapiOptionAutoTestMinutes,
	ggapiOptionAutomaticDisable,
	ggapiOptionAutomaticEnable,
	ggapiOptionAffinityEnabled,
	ggapiOptionAffinitySwitchOnSuccess,
	ggapiOptionAffinityKeepDisabled,
	ggapiOptionAffinityMaxEntries,
	ggapiOptionAffinityDefaultTTL,
	ggapiOptionAffinityRules,
}

func (s *SchedulerService) GGAPISettings(ctx context.Context) (domain.GGAPISettings, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.GGAPISettings{}, err
	}
	if !schedulerConfigured(cfg) {
		return domain.GGAPISettings{}, errSchedulerNotConfigured
	}
	options, err := s.ggapiOptions(ctx, cfg)
	if err != nil {
		return domain.GGAPISettings{}, err
	}
	return ggapiSettingsFromOptions(options), nil
}

func (s *SchedulerService) SaveGGAPISettings(ctx context.Context, settings domain.GGAPISettings) (domain.GGAPISettingsUpdateResult, error) {
	if err := validateGGAPISettings(settings); err != nil {
		return domain.GGAPISettingsUpdateResult{}, BadRequest(err)
	}
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.GGAPISettingsUpdateResult{}, err
	}
	if !schedulerConfigured(cfg) {
		return domain.GGAPISettingsUpdateResult{}, errSchedulerNotConfigured
	}
	before, err := s.ggapiOptions(ctx, cfg)
	if err != nil {
		return domain.GGAPISettingsUpdateResult{}, err
	}
	desired := ggapiSettingsOptionValues(settings)
	result := domain.GGAPISettingsUpdateResult{Complete: true, Applied: []string{}, Settings: ggapiSettingsFromOptions(before)}
	for _, key := range ggapiManagedOptionKeys {
		value := desired[key]
		if ggapiOptionValuesEqual(key, before[key], value) {
			continue
		}
		var raw map[string]any
		err := s.schedulerJSON(ctx, cfg, http.MethodPut, "/api/option/", map[string]any{"key": key, "value": value}, &raw)
		if err == nil {
			if ok, exists := raw["success"].(bool); exists && !ok {
				err = errors.New(schedulerMessage(raw))
			}
		}
		if err != nil {
			result.Complete = false
			result.FailedKey = key
			result.Error = ggapiSettingsSafeError(err)
			break
		}
		result.Applied = append(result.Applied, key)
		before[key] = value
	}
	after, readErr := s.ggapiOptions(ctx, cfg)
	if readErr == nil {
		result.Settings = ggapiSettingsFromOptions(after)
		if result.Complete {
			for _, key := range ggapiManagedOptionKeys {
				if !ggapiOptionValuesEqual(key, after[key], desired[key]) {
					result.Complete = false
					result.FailedKey = key
					result.Error = "GGAPI 设置回读与提交值不一致"
					break
				}
			}
		}
	} else if result.Error == "" {
		result.Complete = false
		result.Error = "设置写入后回读失败"
	}
	s.recordGGAPISettingsChange(ctx, result, result.Complete)
	return result, nil
}

func (s *SchedulerService) recordGGAPISettingsChange(ctx context.Context, result domain.GGAPISettingsUpdateResult, success bool) {
	status := "success"
	message := fmt.Sprintf("GGAPI 设置已更新 %d 项", len(result.Applied))
	severity := "info"
	if !success {
		status, severity = "error", "warning"
		failed := domain.FirstNonEmpty(result.FailedKey, "回读验证")
		message = fmt.Sprintf("GGAPI 设置部分生效：已更新 %d 项，失败项 %s", len(result.Applied), failed)
	}
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{Action: "ggapi_settings", Status: status, Reason: result.FailedKey, Message: message})
	_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{
		Type: "ggapi_settings_changed", Severity: severity, Title: "GGAPI 设置变更", Message: message,
		TargetType: "scheduler", Actions: []string{"scheduler_ggapi_settings"},
	})
}

func (s *SchedulerService) ggapiOptions(ctx context.Context, cfg domain.SchedulerConfig) (map[string]string, error) {
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodGet, "/api/option/", nil, &raw); err != nil {
		return nil, err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return nil, errors.New(schedulerMessage(raw))
	}
	out := map[string]string{}
	for _, item := range schedulerArray(firstScheduler(raw, "data", "items")) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := schedulerString(firstScheduler(m, "key", "Key"))
		if key != "" {
			out[key] = schedulerString(firstScheduler(m, "value", "Value"))
		}
	}
	return out, nil
}

func ggapiSettingsFromOptions(values map[string]string) domain.GGAPISettings {
	rules := json.RawMessage(strings.TrimSpace(values[ggapiOptionAffinityRules]))
	if !json.Valid(rules) {
		rules = json.RawMessage("[]")
	}
	return domain.GGAPISettings{
		RetryTimes:                     parseInt(values[ggapiOptionRetryTimes]),
		AutomaticRetryStatusCodes:      values[ggapiOptionRetryStatusCodes],
		AutomaticDisableStatusCodes:    values[ggapiOptionDisableStatusCodes],
		ChannelTestMode:                values[ggapiOptionChannelTestMode],
		AutoTestChannelEnabled:         parseBool(values[ggapiOptionAutoTestEnabled]),
		AutoTestChannelMinutes:         parseFloat(values[ggapiOptionAutoTestMinutes]),
		AutomaticDisableChannelEnabled: parseBool(values[ggapiOptionAutomaticDisable]),
		AutomaticEnableChannelEnabled:  parseBool(values[ggapiOptionAutomaticEnable]),
		Affinity: domain.GGAPIAffinitySettings{
			Enabled: parseBool(values[ggapiOptionAffinityEnabled]), SwitchOnSuccess: parseBool(values[ggapiOptionAffinitySwitchOnSuccess]),
			KeepOnChannelDisabled: parseBool(values[ggapiOptionAffinityKeepDisabled]), MaxEntries: parseInt(values[ggapiOptionAffinityMaxEntries]),
			DefaultTTLSeconds: parseInt(values[ggapiOptionAffinityDefaultTTL]), Rules: rules,
		},
	}
}

func ggapiSettingsOptionValues(settings domain.GGAPISettings) map[string]string {
	return map[string]string{
		ggapiOptionRetryTimes:              strconv.Itoa(settings.RetryTimes),
		ggapiOptionRetryStatusCodes:        strings.TrimSpace(settings.AutomaticRetryStatusCodes),
		ggapiOptionDisableStatusCodes:      strings.TrimSpace(settings.AutomaticDisableStatusCodes),
		ggapiOptionChannelTestMode:         settings.ChannelTestMode,
		ggapiOptionAutoTestEnabled:         strconv.FormatBool(settings.AutoTestChannelEnabled),
		ggapiOptionAutoTestMinutes:         strconv.FormatFloat(settings.AutoTestChannelMinutes, 'f', -1, 64),
		ggapiOptionAutomaticDisable:        strconv.FormatBool(settings.AutomaticDisableChannelEnabled),
		ggapiOptionAutomaticEnable:         strconv.FormatBool(settings.AutomaticEnableChannelEnabled),
		ggapiOptionAffinityEnabled:         strconv.FormatBool(settings.Affinity.Enabled),
		ggapiOptionAffinitySwitchOnSuccess: strconv.FormatBool(settings.Affinity.SwitchOnSuccess),
		ggapiOptionAffinityKeepDisabled:    strconv.FormatBool(settings.Affinity.KeepOnChannelDisabled),
		ggapiOptionAffinityMaxEntries:      strconv.Itoa(settings.Affinity.MaxEntries),
		ggapiOptionAffinityDefaultTTL:      strconv.Itoa(settings.Affinity.DefaultTTLSeconds),
		ggapiOptionAffinityRules:           string(settings.Affinity.Rules),
	}
}

func ggapiOptionValuesEqual(key, current, desired string) bool {
	if key != ggapiOptionAffinityRules {
		return strings.TrimSpace(current) == strings.TrimSpace(desired)
	}
	var a, b any
	return json.Unmarshal([]byte(current), &a) == nil && json.Unmarshal([]byte(desired), &b) == nil && fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func validateGGAPISettings(settings domain.GGAPISettings) error {
	if settings.RetryTimes < 0 || settings.RetryTimes > 10 {
		return errors.New("重试次数必须在 0 到 10 之间")
	}
	if settings.ChannelTestMode != "scheduled_all" && settings.ChannelTestMode != "passive_recovery" {
		return errors.New("通道测试模式必须是 scheduled_all 或 passive_recovery")
	}
	if settings.AutoTestChannelMinutes <= 0 || settings.AutoTestChannelMinutes > 10080 {
		return errors.New("通道测试间隔必须大于 0 且不超过 10080 分钟")
	}
	if err := validateHTTPStatusRanges(settings.AutomaticRetryStatusCodes); err != nil {
		return fmt.Errorf("自动重试状态码：%w", err)
	}
	if err := validateHTTPStatusRanges(settings.AutomaticDisableStatusCodes); err != nil {
		return fmt.Errorf("自动禁用状态码：%w", err)
	}
	if settings.Affinity.MaxEntries <= 0 {
		return errors.New("亲和缓存容量必须大于 0")
	}
	if settings.Affinity.DefaultTTLSeconds <= 0 {
		return errors.New("亲和默认 TTL 必须大于 0")
	}
	return validateAffinityRules(settings.Affinity.Rules)
}

func validateHTTPStatusRanges(value string) error {
	value = strings.ReplaceAll(strings.TrimSpace(value), "，", ",")
	if value == "" {
		return nil
	}
	for _, token := range strings.Split(value, ",") {
		token = strings.ReplaceAll(strings.TrimSpace(token), " ", "")
		parts := strings.Split(token, "-")
		if len(parts) < 1 || len(parts) > 2 {
			return fmt.Errorf("无效范围 %q", token)
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil || start < 100 || start > 599 {
			return fmt.Errorf("无效状态码 %q", token)
		}
		end := start
		if len(parts) == 2 {
			end, err = strconv.Atoi(parts[1])
			if err != nil || end < start || end > 599 {
				return fmt.Errorf("无效范围 %q", token)
			}
		}
	}
	return nil
}

func validateAffinityRules(raw json.RawMessage) error {
	if !json.Valid(raw) {
		return errors.New("亲和规则 JSON 格式错误")
	}
	var rules []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rules); err != nil {
		return errors.New("亲和规则必须是数组")
	}
	names := map[string]bool{}
	for index, rule := range rules {
		name := rawString(rule["name"])
		if name == "" {
			return fmt.Errorf("第 %d 条规则缺少名称", index+1)
		}
		if names[name] {
			return fmt.Errorf("规则名 %q 重复", name)
		}
		names[name] = true
		for _, key := range []string{"model_regex", "path_regex"} {
			var patterns []string
			if err := json.Unmarshal(rule[key], &patterns); err != nil {
				return fmt.Errorf("规则 %q 的 %s 必须是字符串数组", name, key)
			}
			for _, pattern := range patterns {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("规则 %q 的正则 %q 无效", name, pattern)
				}
			}
		}
		if pattern := rawString(rule["value_regex"]); pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("规则 %q 的值正则无效", name)
			}
		}
		var ttl int
		if value, ok := rule["ttl_seconds"]; ok && json.Unmarshal(value, &ttl) != nil {
			return fmt.Errorf("规则 %q 的 TTL 必须是整数", name)
		}
		if ttl < 0 {
			return fmt.Errorf("规则 %q 的 TTL 不能为负数", name)
		}
		var sources []map[string]json.RawMessage
		if err := json.Unmarshal(rule["key_sources"], &sources); err != nil || len(sources) == 0 {
			return fmt.Errorf("规则 %q 至少需要一个键来源", name)
		}
		for _, source := range sources {
			typeName := rawString(source["type"])
			switch typeName {
			case "context_int", "context_string", "request_header":
				if rawString(source["key"]) == "" {
					return fmt.Errorf("规则 %q 的 %s 键来源缺少 key", name, typeName)
				}
			case "gjson":
				if rawString(source["path"]) == "" {
					return fmt.Errorf("规则 %q 的 gjson 键来源缺少 path", name)
				}
			default:
				return fmt.Errorf("规则 %q 使用了不支持的键来源 %q", name, typeName)
			}
		}
	}
	return nil
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return strings.TrimSpace(value)
}

func ggapiSettingsSafeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "GGAPI 设置请求超时"
	}
	return "GGAPI 拒绝或未完成该设置写入"
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed
}

func (s *SchedulerService) GGAPIAffinityCache(ctx context.Context) (domain.GGAPIAffinityCacheStats, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.GGAPIAffinityCacheStats{}, err
	}
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodGet, "/api/option/channel_affinity_cache", nil, &raw); err != nil {
		return domain.GGAPIAffinityCacheStats{}, err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return domain.GGAPIAffinityCacheStats{}, errors.New(schedulerMessage(raw))
	}
	data, _ := json.Marshal(firstScheduler(raw, "data"))
	var out domain.GGAPIAffinityCacheStats
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.ByRuleName == nil {
		out.ByRuleName = map[string]int{}
	}
	return out, nil
}

func (s *SchedulerService) ClearGGAPIAffinityCache(ctx context.Context, ruleName string, all bool) (domain.GGAPIAffinityCacheClearResult, error) {
	if !all && strings.TrimSpace(ruleName) == "" {
		return domain.GGAPIAffinityCacheClearResult{}, ErrBadRequest("必须提供 rule_name 或 all=true")
	}
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.GGAPIAffinityCacheClearResult{}, err
	}
	values := url.Values{}
	if all {
		values.Set("all", "true")
	} else {
		values.Set("rule_name", strings.TrimSpace(ruleName))
	}
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodDelete, "/api/option/channel_affinity_cache?"+values.Encode(), nil, &raw); err != nil {
		return domain.GGAPIAffinityCacheClearResult{}, err
	}
	if ok, exists := raw["success"].(bool); !exists || !ok {
		return domain.GGAPIAffinityCacheClearResult{}, errors.New(schedulerMessage(raw))
	}
	data := profitMap(firstScheduler(raw, "data"))
	return domain.GGAPIAffinityCacheClearResult{Deleted: schedulerInt(firstScheduler(data, "deleted"))}, nil
}

func ggapiAffinitySkipRules(settings domain.GGAPISettings) map[string]bool {
	out := map[string]bool{}
	var rules []map[string]json.RawMessage
	if json.Unmarshal(settings.Affinity.Rules, &rules) != nil {
		return out
	}
	for _, rule := range rules {
		var skip bool
		_ = json.Unmarshal(rule["skip_retry_on_failure"], &skip)
		if name := rawString(rule["name"]); name != "" && skip {
			out[name] = true
		}
	}
	return out
}

func sortedOptionKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
