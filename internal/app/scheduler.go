package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

var errSchedulerNotConfigured = errors.New("scheduler not configured")

func (s *SchedulerService) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	return cfg.Public(), err
}

func (s *SchedulerService) SaveSchedulerConfig(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerConfig, error) {
	old, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.SchedulerConfig{}, err
	}
	cfg = cfg.MergeUpdate(old)
	cfg.Provider = old.Provider
	if err := domain.ValidateSchedulerTiers(cfg.Tiers); err != nil {
		return domain.SchedulerConfig{}, BadRequest(err)
	}
	if err := domain.ValidateSchedulerUnassignedGroup(cfg.UnassignedGroup, cfg.Tiers); err != nil {
		return domain.SchedulerConfig{}, BadRequest(err)
	}
	out, err := s.app.Store.UpdateSchedulerConfig(ctx, cfg)
	return out.Public(), err
}

func costBindingProjection(binding domain.SchedulerCostBinding) domain.SchedulerCostBinding {
	binding.SourceType = domain.CostSourceManual
	if binding.UpstreamID != "" || binding.KeyID != "" {
		binding.SourceType = domain.CostSourceUpstreamKey
		if binding.UpstreamID == "" {
			binding.MissingReason = "未绑定上游"
			return binding
		}
		if binding.KeyID == "" || binding.KeyName == "" {
			binding.MissingReason = "未绑定上游 Key"
			return binding
		}
		cost, reason := domain.CostPerUnitFromUpstreamKey(binding.KeyRatio, binding.BalanceRate)
		binding.EffectiveCost, binding.MissingReason = cost, reason
		binding.CostAvailable = reason == ""
		return binding
	}
	cost, reason := domain.CostPerUnitFromManual(binding.ManualCostRatio)
	binding.EffectiveCost, binding.MissingReason = cost, reason
	binding.CostAvailable = reason == ""
	return binding
}

func (s *SchedulerService) SchedulerChannelsForProvider(ctx context.Context, provider, keyword string) ([]domain.SchedulerChannel, error) {
	provider = domain.NormalizeSchedulerProvider(provider)
	if provider == domain.SchedulerProviderAxonHub {
		return s.AxonHubChannels(ctx, keyword)
	}
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !schedulerConfigured(cfg) {
		return nil, ErrBadRequest("请先配置 GGAPI 连接")
	}
	return s.fetchSchedulerChannels(ctx, cfg, keyword)
}

func (s *SchedulerService) SchedulerChannels(ctx context.Context, keyword string) ([]domain.SchedulerChannel, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.SchedulerChannelsForProvider(ctx, cfg.Provider, keyword)
}

func (s *SchedulerService) fetchSchedulerChannels(ctx context.Context, cfg domain.SchedulerConfig, keyword string) ([]domain.SchedulerChannel, error) {
	var out []domain.SchedulerChannel
	for page := 1; ; page++ {
		values := url.Values{"page_size": {"100"}, "p": {strconv.Itoa(page)}}
		if strings.TrimSpace(keyword) != "" {
			values.Set("keyword", strings.TrimSpace(keyword))
		}
		var raw map[string]any
		if err := s.schedulerJSON(ctx, cfg, http.MethodGet, "/api/channel/?"+values.Encode(), nil, &raw); err != nil {
			return nil, err
		}
		if ok, exists := raw["success"].(bool); exists && !ok {
			return nil, errors.New(schedulerMessage(raw))
		}
		rows := schedulerChannels(raw)
		out = append(out, rows...)
		if len(rows) < 100 {
			return out, nil
		}
	}
}

func (s *SchedulerService) SchedulerGroups(ctx context.Context) ([]domain.SchedulerGroup, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Provider == domain.SchedulerProviderAxonHub {
		return []domain.SchedulerGroup{{Name: domain.AxonHubTagLow}, {Name: domain.AxonHubTagStable}}, nil
	}
	if !schedulerConfigured(cfg) {
		return nil, ErrBadRequest("请先配置 GGAPI 连接")
	}
	if groups, err := s.fetchSchedulerGroups(ctx, cfg, "/api/user/self/groups"); err == nil {
		return groups, nil
	}
	return s.fetchSchedulerGroups(ctx, cfg, "/api/user/groups")
}

func (s *SchedulerService) fetchSchedulerGroups(ctx context.Context, cfg domain.SchedulerConfig, path string) ([]domain.SchedulerGroup, error) {
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return nil, errors.New(schedulerMessage(raw))
	}
	return schedulerGroups(raw), nil
}

func (s *SchedulerService) SchedulerLogs(ctx context.Context, limit int) ([]domain.SchedulerLog, error) {
	if cfg, err := s.app.Store.SchedulerConfig(ctx); err == nil {
		return s.app.Store.SchedulerLogsForProvider(ctx, cfg.Provider, limit)
	}
	return s.app.Store.SchedulerLogs(ctx, limit)
}

func (s *SchedulerService) ApplySchedulerGroups(ctx context.Context) (domain.SchedulerApplyResult, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.SchedulerApplyResult{}, err
	}
	if cfg.Provider == domain.SchedulerProviderAxonHub {
		return s.applyAxonHubCosts(ctx)
	}
	return s.applyGGAPICosts(ctx, cfg)
}

func (s *SchedulerService) applyGGAPICosts(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerApplyResult, error) {
	var out domain.SchedulerApplyResult
	if !schedulerConfigured(cfg) {
		return out, errSchedulerNotConfigured
	}
	if err := domain.ValidateSchedulerUnassignedGroup(cfg.UnassignedGroup, cfg.Tiers); err != nil {
		return out, BadRequest(err)
	}
	bindings, err := s.app.Store.ListCostBindings(ctx)
	if err != nil {
		return out, err
	}
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return out, err
	}
	byID := make(map[string]domain.SchedulerChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	costs := map[string]float64{}
	for _, binding := range bindings {
		binding = costBindingProjection(binding)
		if binding.Enabled && binding.CostAvailable && binding.SchedulerChannelID != "" {
			if _, ok := byID[binding.SchedulerChannelID]; ok {
				costs[binding.SchedulerChannelID] = binding.EffectiveCost
			}
		}
	}
	priorities := domain.CostPriorities(costs)
	tiers := domain.NormalizeSchedulerTiers(cfg.Tiers)
	managedGroups := domain.ManagedGroups(tiers)
	for _, binding := range bindings {
		binding = costBindingProjection(binding)
		channel, found := byID[binding.SchedulerChannelID]
		cost, hasCost := costs[binding.SchedulerChannelID]
		priority, hasPriority := priorities[binding.SchedulerChannelID]
		if !binding.Enabled || !binding.CostAvailable || binding.SchedulerChannelID == "" || !found || !hasCost || !hasPriority {
			out.Skipped++
			continue
		}
		targetGroups := domain.AssignedTargetGroups(tiers, managedGroups, cost, channel.Group, cfg.UnassignedGroup)
		ownership, exists, err := s.app.Store.CostFieldOwnership(ctx, domain.SchedulerProviderGGAPI, channel.ID)
		if err != nil {
			return out, err
		}
		currentGroups := domain.SplitGroups(channel.Group)
		if exists && ownership.Managed && !ownership.ExternalTakeover && (!domain.SameGroups(currentGroups, ownership.RemoteGroups) || channel.Priority != ownership.RemotePriority) {
			ownership.ExternalTakeover, ownership.Managed = true, false
			ownership.LastReason, ownership.UpdatedAt = "GGAPI 分组或优先级发生外部修改", time.Now().UTC()
			_ = s.app.Store.SaveCostFieldOwnership(ctx, ownership)
		}
		if exists && ownership.ExternalTakeover {
			out.Skipped++
			continue
		}
		if domain.SameGroups(currentGroups, targetGroups) && channel.Priority == priority {
			out.Unchanged++
			_ = s.app.Store.SaveCostFieldOwnership(ctx, domain.CostFieldOwnership{Provider: domain.SchedulerProviderGGAPI, ChannelID: channel.ID, ChannelName: channel.Name, RemoteGroups: currentGroups, RemotePriority: channel.Priority, RemoteWeight: int(channel.Weight), Managed: true, UpdatedAt: time.Now().UTC()})
			continue
		}
		if err := s.writeGGAPICostFields(ctx, cfg, channel, domain.JoinGroups(targetGroups), priority); err != nil {
			return out, err
		}
		actual, found, err := s.schedulerChannel(ctx, cfg, channel.ID)
		if err != nil || !found {
			if err == nil {
				err = errors.New("GGAPI 写入后找不到渠道")
			}
			return out, err
		}
		if !domain.SameGroups(domain.SplitGroups(actual.Group), targetGroups) || actual.Priority != priority || actual.Weight != channel.Weight || actual.Status != channel.Status {
			return out, errors.New("GGAPI 成本字段写入校验失败")
		}
		_ = s.app.Store.SaveCostFieldOwnership(ctx, domain.CostFieldOwnership{Provider: domain.SchedulerProviderGGAPI, ChannelID: actual.ID, ChannelName: actual.Name, RemoteGroups: domain.SplitGroups(actual.Group), RemotePriority: actual.Priority, RemoteWeight: int(actual.Weight), Managed: true, UpdatedAt: time.Now().UTC()})
		out.Updated++
	}
	s.logCostSync(ctx, domain.SchedulerProviderGGAPI, out)
	return out, nil
}

func (s *SchedulerService) writeGGAPICostFields(ctx context.Context, cfg domain.SchedulerConfig, current domain.SchedulerChannel, group string, priority int64) error {
	id, err := strconv.Atoi(strings.TrimSpace(current.ID))
	if err != nil || id <= 0 {
		return ErrBadRequest("invalid scheduler channel id")
	}
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodPut, "/api/channel/", map[string]any{"id": id, "group": group, "priority": priority}, &raw); err != nil {
		return err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return errors.New(schedulerMessage(raw))
	}
	return nil
}

func (s *SchedulerService) schedulerChannel(ctx context.Context, cfg domain.SchedulerConfig, id string) (domain.SchedulerChannel, bool, error) {
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return domain.SchedulerChannel{}, false, err
	}
	for _, channel := range channels {
		if channel.ID == id {
			return channel, true, nil
		}
	}
	return domain.SchedulerChannel{}, false, nil
}

func (s *SchedulerService) logCostSync(ctx context.Context, provider string, out domain.SchedulerApplyResult) {
	status := "success"
	if out.Updated == 0 {
		status = "skipped"
	}
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{Provider: provider, Action: "group_sync", Status: status, Message: fmt.Sprintf("成本同步：更新 %d 个，未变更 %d 个，跳过 %d 个", out.Updated, out.Unchanged, out.Skipped)})
}

func (s *SchedulerService) syncSchedulerGroupsBestEffort(ctx context.Context) {
	_, err := s.ApplySchedulerGroups(ctx)
	if err == nil {
		s.recordCostSyncAlert(ctx, false, "成本同步已恢复")
		return
	}
	if errors.Is(err, errSchedulerNotConfigured) || (IsBadRequest(err) && strings.Contains(err.Error(), "未分配分组")) {
		return
	}
	provider := domain.SchedulerProviderGGAPI
	if cfg, cfgErr := s.app.Store.SchedulerConfig(ctx); cfgErr == nil {
		provider = cfg.Provider
	}
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{Provider: provider, Action: "group_sync", Status: "error", Message: err.Error()})
	s.recordCostSyncAlert(ctx, true, "成本同步失败: "+err.Error())
}

func (s *SchedulerService) recordCostSyncAlert(ctx context.Context, failing bool, message string) {
	const kind = "cost_sync"
	prev, err := s.app.Store.AlertState(ctx, "", kind)
	if err != nil {
		return
	}
	decision, send := domain.DecideAlert(time.Now(), kind, failing, message, prev)
	if !send {
		return
	}
	severity, title := "warning", "成本同步失败"
	if decision.Recover {
		severity, title = "success", "成本同步已恢复"
	}
	_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{
		Type: "cost_sync_failed", Severity: severity, Title: title, Message: message,
		TargetType: "scheduler", Actions: []string{"check_cost_bindings"},
	})
	rules, err := s.app.Store.NotificationRules(ctx)
	if err != nil {
		return
	}
	sent := false
	if domain.ShouldNotify(rules, "cost_sync_failed", decision.Recover) {
		sent = s.app.Notify.Send(ctx, message) == nil
	}
	_ = s.app.Store.SaveAlert(ctx, "", decision, sent)
}

func schedulerConfigured(cfg domain.SchedulerConfig) bool {
	return strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.UserID) != "" && strings.TrimSpace(cfg.AccessToken) != ""
}

func (s *SchedulerService) schedulerJSON(ctx context.Context, cfg domain.SchedulerConfig, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, joinSchedulerURL(cfg.BaseURL, path), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", cfg.AccessToken)
	req.Header.Set("New-Api-User", cfg.UserID)
	hc := s.app.Client.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("调度器 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if len(payload) == 0 || out == nil {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func joinSchedulerURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func schedulerMessage(raw map[string]any) string {
	for _, key := range []string{"message", "error", "msg"} {
		if value := strings.TrimSpace(fmt.Sprint(raw[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return "调度器返回失败"
}

func schedulerChannels(raw map[string]any) []domain.SchedulerChannel {
	items := schedulerArray(raw["data"])
	if len(items) == 0 {
		items = schedulerArray(raw["channels"])
	}
	out := make([]domain.SchedulerChannel, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		channel := domain.SchedulerChannel{ID: schedulerString(firstScheduler(m, "id")), Name: schedulerString(firstScheduler(m, "name", "channel_name")), Status: schedulerInt(firstScheduler(m, "status")), Priority: int64(schedulerInt(firstScheduler(m, "priority"))), Weight: schedulerUint(firstScheduler(m, "weight")), Tag: schedulerString(firstScheduler(m, "tag")), Type: schedulerString(firstScheduler(m, "type")), Group: schedulerString(firstScheduler(m, "group")), Models: schedulerStrings(firstScheduler(m, "models"))}
		if channel.ID != "" {
			out = append(out, channel)
		}
	}
	return out
}

func schedulerGroups(raw map[string]any) []domain.SchedulerGroup {
	data := firstScheduler(raw, "data", "groups", "items")
	seen := map[string]domain.SchedulerGroup{}
	if values, ok := data.(map[string]any); ok {
		for name, value := range values {
			group := schedulerGroup(name, value)
			if group.Name != "" {
				seen[group.Name] = group
			}
		}
	}
	for _, item := range schedulerArray(data) {
		group := schedulerGroup("", item)
		if group.Name != "" {
			seen[group.Name] = group
		}
	}
	out := make([]domain.SchedulerGroup, 0, len(seen))
	for _, group := range seen {
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func schedulerGroup(defaultName string, value any) domain.SchedulerGroup {
	m, _ := value.(map[string]any)
	group := domain.SchedulerGroup{Name: schedulerString(firstScheduler(m, "name", "group", "group_name", "groupName", "id", "group_id")), Ratio: schedulerRatioString(firstScheduler(m, "rate_multiplier", "rateMultiplier", "ratio", "group_ratio", "groupRatio")), Description: schedulerString(firstScheduler(m, "description", "desc", "remark", "memo", "note"))}
	if group.Name == "" {
		group.Name = strings.TrimSpace(defaultName)
	}
	if group.Name == "" && m == nil {
		group.Name = schedulerString(value)
	}
	if _, nested := value.(map[string]any); group.Ratio == "" && defaultName != "" && !nested {
		group.Ratio = schedulerRatioString(value)
	}
	return group
}

func schedulerArray(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case map[string]any:
		for _, key := range []string{"items", "channels", "data"} {
			if out, ok := typed[key].([]any); ok {
				return out
			}
		}
	}
	return nil
}

func firstScheduler(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func schedulerString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func schedulerRatioString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func schedulerInt(value any) int {
	out, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	return out
}
func schedulerUint(value any) uint {
	out, _ := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
	return uint(out)
}

func schedulerStrings(value any) []string {
	var out []string
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				out = append(out, value)
			}
		}
	case []string:
		out = append(out, typed...)
	case string:
		_ = json.Unmarshal([]byte(typed), &out)
	}
	return out
}
