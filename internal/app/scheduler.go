package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	if err := domain.ValidateTrafficConfig(cfg.TrafficMode, cfg.TrafficProfile, cfg.TrafficPollSecs); err != nil {
		return domain.SchedulerConfig{}, BadRequest(err)
	}
	old, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.SchedulerConfig{}, err
	}
	cfg = cfg.MergeUpdate(old)
	// provider 只能经显式切换接口变更，避免旧配置表单绕过 AxonHub 预检。
	cfg.Provider = old.Provider
	if err := domain.ValidateSchedulerTiers(cfg.Tiers); err != nil {
		return domain.SchedulerConfig{}, BadRequest(err)
	}
	if err := domain.ValidateSchedulerUnassignedGroup(cfg.UnassignedGroup, cfg.Tiers); err != nil {
		return domain.SchedulerConfig{}, BadRequest(err)
	}
	out, err := s.app.Store.UpdateSchedulerConfig(ctx, cfg)
	if err == nil {
		err = s.recordCurrentSaleSnapshots(ctx)
	}
	return out.Public(), err
}

func (s *SchedulerService) SeedSchedulerSnapshots(ctx context.Context) error {
	if err := s.recordCurrentSaleSnapshots(ctx); err != nil {
		return err
	}
	return s.recordCurrentCostSnapshots(ctx)
}

func (s *SchedulerService) recordCurrentSaleSnapshots(ctx context.Context) error {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	current := map[string]bool{}
	for _, tier := range domain.NormalizeSchedulerTiers(cfg.Tiers) {
		group := strings.TrimSpace(tier.Group)
		if group == "" {
			continue
		}
		current[group] = true
		if _, err := s.app.Store.SaveSchedulerGroupSaleSnapshot(ctx, domain.SchedulerGroupSaleSnapshot{
			Group: group, Tag: strings.TrimSpace(tier.Tag), SalePrice: tier.SalePrice, Active: true, EffectiveAt: now,
		}); err != nil {
			return err
		}
	}
	groups, err := s.app.Store.SchedulerSaleSnapshotGroups(ctx, now)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if current[group] {
			continue
		}
		latest, ok, err := s.app.Store.LatestSchedulerGroupSaleSnapshot(ctx, group)
		if err != nil {
			return err
		}
		if !ok || !latest.Active {
			continue
		}
		if _, err := s.app.Store.SaveSchedulerGroupSaleSnapshot(ctx, domain.SchedulerGroupSaleSnapshot{
			Group: group, Tag: latest.Tag, Active: false, EffectiveAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *SchedulerService) recordCurrentCostSnapshots(ctx context.Context) error {
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	for _, card := range cards {
		if err := s.recordCardCostSnapshot(ctx, card); err != nil {
			return err
		}
	}
	return nil
}

func (s *SchedulerService) recordCardCostSnapshot(ctx context.Context, card domain.ModelCard) error {
	snap := s.cardCostSnapshot(ctx, card)
	if snap.ChannelID == "" {
		return nil
	}
	_, err := s.app.Store.SaveSchedulerChannelCostSnapshot(ctx, snap)
	return err
}

func (s *SchedulerService) recordInactiveCostSnapshot(ctx context.Context, card domain.ModelCard, reason string) error {
	snap := s.cardCostSnapshot(ctx, card)
	if snap.ChannelID == "" {
		return nil
	}
	snap.Active, snap.CostPerUnit, snap.MissingReason = false, 0, reason
	_, err := s.app.Store.SaveSchedulerChannelCostSnapshot(ctx, snap)
	return err
}

func (s *SchedulerService) cardCostSnapshot(ctx context.Context, card domain.ModelCard) domain.SchedulerChannelCostSnapshot {
	provider := domain.SchedulerProviderGGAPI
	channelID, channelName := card.SchedulerChannelID, card.SchedulerChannelName
	if cfg, err := s.app.Store.SchedulerConfig(ctx); err == nil && cfg.Provider == domain.SchedulerProviderAxonHub {
		provider, channelID, channelName = domain.SchedulerProviderAxonHub, card.AxonHubChannelID, card.AxonHubChannelName
	}
	snap := domain.SchedulerChannelCostSnapshot{
		Provider: provider, ChannelID: channelID, ChannelName: channelName,
		CardID: card.ID, CardName: card.Name, MissingReason: "缺成本绑定", EffectiveAt: time.Now().UTC(),
	}
	if snap.ChannelID == "" {
		return snap
	}
	if !card.PoolEnabled {
		snap.MissingReason = "纯监控"
		return snap
	}
	if card.BaseURL != "" {
		snap.SourceType = "manual_cost_ratio"
		ratio, reason := domain.CostPerUnitFromManual(card.ManualCostRatio)
		if reason != "" {
			snap.MissingReason = reason
			return snap
		}
		snap.CostPerUnit, snap.Active, snap.MissingReason = ratio, true, ""
		return snap
	}
	key, err := s.app.Store.Key(ctx, card.KeyID)
	if err != nil {
		snap.MissingReason = "未绑定上游 Key"
		return snap
	}
	upstream, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		snap.MissingReason = "未绑定上游"
		return snap
	}
	snap.SourceType = "upstream_key"
	snap.UpstreamID, snap.UpstreamName = upstream.ID, upstream.Name
	snap.KeyID, snap.KeyName = key.ID, key.Name
	ratio, reason := domain.CostPerUnitFromUpstreamKey(key.GroupRatio, domain.BalanceRate(upstream))
	if reason != "" {
		snap.MissingReason = reason
		return snap
	}
	snap.CostPerUnit, snap.Active, snap.MissingReason = ratio, true, ""
	return snap
}

func (s *SchedulerService) SchedulerChannels(ctx context.Context, keyword string) ([]domain.SchedulerChannel, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Provider == domain.SchedulerProviderAxonHub {
		return s.AxonHubChannels(ctx, keyword)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, ErrBadRequest("请先配置调度器连接")
	}
	return s.fetchSchedulerChannels(ctx, cfg, keyword)
}

func (s *SchedulerService) fetchSchedulerChannels(ctx context.Context, cfg domain.SchedulerConfig, keyword string) ([]domain.SchedulerChannel, error) {
	var out []domain.SchedulerChannel
	for p := 1; ; p++ {
		values := url.Values{}
		values.Set("page_size", "100")
		values.Set("p", strconv.Itoa(p))
		if strings.TrimSpace(keyword) != "" {
			values.Set("keyword", strings.TrimSpace(keyword))
		}
		path := "/api/channel/"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		var raw map[string]any
		if err := s.schedulerJSON(ctx, cfg, http.MethodGet, path, nil, &raw); err != nil {
			return nil, err
		}
		if ok, exists := raw["success"].(bool); exists && !ok {
			return nil, errors.New(schedulerMessage(raw))
		}
		page := schedulerChannels(raw)
		out = append(out, page...)
		if len(page) < 100 {
			s.observeSchedulerChannels(ctx, out)
			if strings.TrimSpace(keyword) == "" {
				if err := s.pruneTrafficControls(ctx, out); err != nil {
					return nil, err
				}
			}
			return out, nil
		}
	}
}

func (s *SchedulerService) pruneTrafficControls(ctx context.Context, channels []domain.SchedulerChannel) error {
	remote := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		remote[channel.ID] = struct{}{}
	}
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	managed := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		if card.Enabled && card.PoolEnabled && strings.TrimSpace(card.SchedulerChannelID) != "" {
			managed[card.SchedulerChannelID] = struct{}{}
		}
	}
	rows, err := s.app.Store.TrafficControls(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		_, remoteExists := remote[row.ChannelID]
		_, managedExists := managed[row.ChannelID]
		if remoteExists && managedExists {
			continue
		}
		if err := s.app.Store.DeleteTrafficControl(ctx, row.ChannelID); err != nil {
			return err
		}
	}
	return nil
}

func (s *SchedulerService) SchedulerGroups(ctx context.Context) ([]domain.SchedulerGroup, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Provider == domain.SchedulerProviderAxonHub {
		return []domain.SchedulerGroup{{Name: domain.AxonHubTagLow}, {Name: domain.AxonHubTagStable}}, nil
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, ErrBadRequest("请先配置调度器连接")
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
		if err := s.ReconcileAxonHub(ctx); err != nil {
			return domain.SchedulerApplyResult{}, err
		}
		return domain.SchedulerApplyResult{}, nil
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return domain.SchedulerApplyResult{}, errSchedulerNotConfigured
	}
	if err := domain.ValidateSchedulerUnassignedGroup(cfg.UnassignedGroup, cfg.Tiers); err != nil {
		// 与未连接区分：后台 best-effort 静默跳过，避免每轮巡检刷 error 日志；手动 apply 仍返回 400。
		return domain.SchedulerApplyResult{}, ErrBadRequest(err.Error())
	}
	unassigned := strings.TrimSpace(cfg.UnassignedGroup)
	tiers := domain.NormalizeSchedulerTiers(cfg.Tiers)
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return domain.SchedulerApplyResult{}, err
	}
	var out domain.SchedulerApplyResult
	poolCards := make([]domain.ModelCard, 0, len(cards))
	for _, card := range cards {
		if card.PoolEnabled {
			poolCards = append(poolCards, card)
		}
	}
	if len(poolCards) == 0 {
		return out, nil
	}
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return domain.SchedulerApplyResult{}, err
	}
	channelsByID := make(map[string]domain.SchedulerChannel, len(channels))
	for _, channel := range channels {
		channelsByID[channel.ID] = channel
	}
	costs := make(map[string]float64, len(poolCards))
	for _, card := range poolCards {
		channelID := strings.TrimSpace(card.SchedulerChannelID)
		price, ok := s.cardPrice(ctx, card)
		if _, found := channelsByID[channelID]; channelID != "" && found && ok {
			costs[channelID] = price
		}
	}
	priorities := domain.CostPriorities(costs)
	managed := domain.ManagedGroups(tiers)
	var changes []string
	for _, card := range poolCards {
		channelID := strings.TrimSpace(card.SchedulerChannelID)
		price, ok := costs[channelID]
		current, found := channelsByID[channelID]
		priority, hasPriority := priorities[channelID]
		if !ok || channelID == "" || !found || !hasPriority {
			out.Skipped++
			continue
		}
		basePriority := priority
		if control, exists, controlErr := s.app.Store.TrafficControl(ctx, channelID); controlErr == nil && exists {
			control.BasePriority = basePriority
			control.UpdatedAt = time.Now().UTC()
			if cfg.TrafficMode == domain.TrafficModeActive {
				forceEnabled := false
				if availability, found, _ := s.app.Store.ChannelAvailability(ctx, channelID); found && availability.Override == domain.OverrideForceEnable {
					forceEnabled = availability.OverrideUntil == nil || time.Now().UTC().Before(*availability.OverrideUntil)
				}
				if !forceEnabled {
					switch control.State {
					case "warning":
						priority -= 1000
					case "degraded", "soft_blocked", "hard_blocked", "hard_recovering":
						priority -= 2000
					case "recovering":
						if control.RecoveryStage >= 3 {
							priority -= 1000
						} else {
							priority -= 2000
						}
					}
				}
			}
			control.DesiredPriority = priority
			_ = s.app.Store.SaveTrafficControl(ctx, control)
		}
		groups := domain.AssignedTargetGroups(tiers, managed, price, current.Group, unassigned)
		if domain.SameGroups(domain.SplitGroups(current.Group), groups) && current.Priority == priority {
			out.Unchanged++
			continue
		}
		group := domain.JoinGroups(groups)
		if err := s.setSchedulerChannelGroup(ctx, cfg, current, group, priority); err != nil {
			if errors.Is(err, errControlPlaneExternalTakeover) || errors.Is(err, errControlPlaneOwnedByGGAPI) {
				out.Skipped++
				continue
			}
			return out, err
		}
		// new-api 对部分写入可能 success 但不生效；始终校验分组、优先级与原权重。
		actual, found, err := s.schedulerChannel(ctx, cfg, channelID)
		if err != nil {
			return out, err
		}
		if !found || !domain.SameGroups(domain.SplitGroups(actual.Group), groups) || actual.Priority != priority || actual.Weight != current.Weight {
			out.Skipped++
			_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
				Action:  "group_sync",
				Status:  "error",
				Message: fmt.Sprintf("%s: 写入分组 %q、优先级 %d 后校验失败（实际分组 %q、优先级 %d、权重 %d -> %d）", domain.FirstNonEmpty(current.Name, card.SchedulerChannelName, channelID), group, priority, domain.FirstNonEmpty(actual.Group, "-"), actual.Priority, current.Weight, actual.Weight),
			})
			continue
		}
		channelsByID[channelID] = actual
		changes = append(changes, fmt.Sprintf("%s: 成本 %g，分组 %s -> %s，优先级 %d -> %d", domain.FirstNonEmpty(current.Name, card.SchedulerChannelName, channelID), price, domain.FirstNonEmpty(current.Group, "-"), group, current.Priority, priority))
		out.Updated++
	}
	if out.Updated > 0 {
		_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
			Action:  "group_sync",
			Status:  "success",
			Message: schedulerGroupSyncMessage(out, changes),
		})
	}
	return out, nil
}

func (s *SchedulerService) schedulerChannel(ctx context.Context, cfg domain.SchedulerConfig, channelID string) (domain.SchedulerChannel, bool, error) {
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return domain.SchedulerChannel{}, false, err
	}
	for _, channel := range channels {
		if channel.ID == channelID {
			return channel, true, nil
		}
	}
	return domain.SchedulerChannel{}, false, nil
}

func schedulerGroupSyncMessage(out domain.SchedulerApplyResult, changes []string) string {
	msg := fmt.Sprintf("成本调度：更新 %d 个，保持 %d 个，跳过 %d 个", out.Updated, out.Unchanged, out.Skipped)
	if len(changes) == 0 {
		return msg
	}
	if len(changes) > 6 {
		changes = append(changes[:6], fmt.Sprintf("还有 %d 个", len(changes)-6))
	}
	return msg + "；" + strings.Join(changes, "；")
}

func (s *SchedulerService) syncSchedulerGroupsBestEffort(ctx context.Context) {
	if _, err := s.ApplySchedulerGroups(ctx); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		// 未配置 unassigned：功能未就绪，不刷调度日志；手动「应用成本调度」仍会返回错误提示。
		if IsBadRequest(err) && strings.Contains(err.Error(), "未分配分组") {
			return
		}
		_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
			Action:  "group_sync",
			Status:  "error",
			Message: err.Error(),
		})
	}
}

func (s *SchedulerService) SetCardSchedulerChannelStatus(ctx context.Context, cardID string, status int) (domain.ModelCard, error) {
	if status != 1 && status != 2 {
		return domain.ModelCard{}, ErrBadRequest("status must be 1 or 2")
	}
	card, err := s.app.Cards.Card(ctx, cardID)
	if err != nil {
		return domain.ModelCard{}, err
	}
	if cfg, cfgErr := s.app.Store.SchedulerConfig(ctx); cfgErr == nil && cfg.Provider == domain.SchedulerProviderAxonHub {
		return domain.ModelCard{}, ErrBadRequest("AxonHub 状态仅由余额硬保护控制")
	}
	if !card.PoolEnabled {
		return domain.ModelCard{}, ErrBadRequest("card is monitor-only")
	}
	if card.SchedulerChannelID == "" {
		return domain.ModelCard{}, ErrBadRequest("scheduler channel required")
	}
	if s.availabilityManagedSchedulerCard(ctx, card) {
		action := "hold_off"
		if status == 1 {
			action = "force_enable"
		}
		if _, err := s.AvailabilityAction(ctx, card.ID, action, 30); err != nil {
			return domain.ModelCard{}, err
		}
		updated, err := s.app.Cards.Card(ctx, card.ID)
		return updated.Public(), err
	}
	if err := s.setSchedulerChannelStatus(ctx, card.SchedulerChannelID, status, domain.ControlSourceManual, schedulerManualMessage(status), card.SchedulerAutoDisabled); err != nil {
		s.logSchedulerAction(ctx, card, schedulerAction(status), "error", err.Error())
		return domain.ModelCard{}, err
	}
	if err := s.app.Cards.UpdateCardSchedulerAutoDisabled(ctx, card.ID, false); err != nil {
		s.logSchedulerAction(ctx, card, schedulerAction(status), "error", err.Error())
		return domain.ModelCard{}, err
	}
	card.SchedulerAutoDisabled = false
	s.logSchedulerAction(ctx, card, schedulerAction(status), "success", schedulerManualMessage(status))
	return card.Public(), nil
}

func (s *SchedulerService) availabilityManagedSchedulerCard(ctx context.Context, card domain.ModelCard) bool {
	if !card.PoolEnabled || card.BaseURL != "" || card.UpstreamID == "" || card.SchedulerChannelID == "" {
		return false
	}
	upstream, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	return err == nil && (upstream.Type == "newapi" || upstream.Type == "sub2api")
}

func (s *SchedulerService) applySchedulerAutomation(ctx context.Context, card domain.ModelCard, success bool, failures int) error {
	if cfg, err := s.app.Store.SchedulerConfig(ctx); err == nil && cfg.Provider == domain.SchedulerProviderAxonHub {
		return nil
	}
	if success {
		if cfg, cfgErr := s.app.Store.SchedulerConfig(ctx); cfgErr == nil && cfg.TrafficMode == domain.TrafficModeActive {
			if control, found, controlErr := s.app.Store.TrafficControl(ctx, card.SchedulerChannelID); controlErr == nil && found && control.ActualStatus == 2 {
				if control.State == "soft_blocked" || control.State == "hard_blocked" || control.State == "recovering" || control.State == "hard_recovering" {
					// 真实流量控制尚未确认恢复，旧的探测自动恢复不能抢先打开渠道。
					return nil
				}
			}
		}
	}
	muteAt := s.app.probeMuteFailureThreshold(ctx)
	if domain.ShouldAutoDisableScheduler(card.PoolEnabled, card.SchedulerChannelID, success, failures, muteAt, card.SchedulerAutoDisabled) {
		if err := s.setSchedulerChannelStatus(ctx, card.SchedulerChannelID, 2, domain.ControlSourceProbe, fmt.Sprintf("连续探测失败 %d 次", muteAt), false); err != nil {
			if errors.Is(err, errSchedulerNotConfigured) {
				s.logSchedulerAction(ctx, card, "disable", "skipped", "调度器未配置")
				return nil
			}
			s.logSchedulerAction(ctx, card, "disable", "error", err.Error())
			return err
		}
		if err := s.app.Cards.UpdateCardSchedulerAutoDisabled(ctx, card.ID, true); err != nil {
			s.logSchedulerAction(ctx, card, "disable", "error", err.Error())
			return err
		}
		s.logSchedulerAction(ctx, card, "disable", "success", fmt.Sprintf("连续失败 %d 次，已关闭调度器渠道", muteAt))
		return nil
	}
	if success && card.SchedulerAutoDisabled && s.schedulerRestoreReady(ctx, card) {
		if err := s.setSchedulerChannelStatus(ctx, card.SchedulerChannelID, 1, domain.ControlSourceProbe, "连续探测恢复确认", card.SchedulerAutoDisabled); err != nil {
			if errors.Is(err, errSchedulerNotConfigured) {
				s.logSchedulerAction(ctx, card, "restore", "skipped", "调度器未配置")
				return nil
			}
			s.logSchedulerAction(ctx, card, "restore", "error", err.Error())
			return err
		}
		if err := s.app.Cards.UpdateCardSchedulerAutoDisabled(ctx, card.ID, false); err != nil {
			s.logSchedulerAction(ctx, card, "restore", "error", err.Error())
			return err
		}
		s.logSchedulerAction(ctx, card, "restore", "success", "连续成功 3 次且已关闭至少 15 分钟，已恢复调度器渠道")
		return nil
	}
	return nil
}

func schedulerAction(status int) string {
	if status == 1 {
		return "restore"
	}
	return "disable"
}

func schedulerManualMessage(status int) string {
	if status == 1 {
		return "手动启用调度器渠道"
	}
	return "手动关闭调度器渠道"
}

func (s *SchedulerService) logSchedulerAction(ctx context.Context, card domain.ModelCard, action, status, message string) {
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
		CardID:      card.ID,
		CardName:    card.Name,
		ChannelID:   card.SchedulerChannelID,
		ChannelName: card.SchedulerChannelName,
		Action:      action,
		Status:      status,
		Message:     message,
	})
	if status == "success" {
		severity := "warning"
		if action == "restore" {
			severity = "success"
		}
		actions := []string{}
		if action == "disable" {
			actions = []string{"scheduler_restore"}
		}
		_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{
			Type: "scheduler_changed", Severity: severity, Title: "调度器状态变更", Message: card.Name + " " + message,
			TargetType: "card", TargetID: card.ID, Actions: actions,
		})
	}
}

func (s *SchedulerService) schedulerRestoreReady(ctx context.Context, card domain.ModelCard) bool {
	// 副作用留在 app：补齐缺失的 disabled-at 时间戳。
	if domain.NeedsSchedulerRestoreTimestamp(card.SchedulerAutoDisabledAt) {
		now := time.Now().UTC()
		_ = s.app.Cards.UpdateCardSchedulerAutoDisabled(ctx, card.ID, true)
		card.SchedulerAutoDisabledAt = &now
		return false
	}
	probes, err := s.app.Store.RecentProbesForCard(ctx, card.ID, domain.SchedulerRestoreSuccessCount)
	if err != nil {
		return false
	}
	return domain.SchedulerRestoreReady(card.SchedulerAutoDisabledAt, probes, time.Now())
}

func (s *SchedulerService) cardPrice(ctx context.Context, card domain.ModelCard) (float64, bool) {
	if card.BaseURL != "" {
		price, err := strconv.ParseFloat(strings.TrimSpace(card.ManualCostRatio), 64)
		if err != nil || price < 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return 0, false
		}
		return price, true
	}
	if card.KeyID == "" {
		return 0, false
	}
	key, err := s.app.Store.Key(ctx, card.KeyID)
	if err != nil {
		return 0, false
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(key.GroupRatio), 64)
	if err != nil {
		return 0, false
	}
	if card.UpstreamID != "" {
		if u, err := s.app.Store.Upstream(ctx, card.UpstreamID); err == nil {
			price *= domain.BalanceRate(u)
		}
	}
	return price, true
}

func (s *SchedulerService) setSchedulerChannelStatus(ctx context.Context, channelID string, status int, source, reason string, confirmedRestore bool) error {
	return s.coordinateSchedulerStatus(ctx, channelID, status, source, reason, confirmedRestore)
}

func (s *SchedulerService) clearSchedulerChannelAffinityCache(ctx context.Context, cfg domain.SchedulerConfig) error {
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodDelete, "/api/option/channel_affinity_cache?all=true", nil, &raw); err != nil {
		return err
	}
	if ok, exists := raw["success"].(bool); !exists || !ok {
		return errors.New(schedulerMessage(raw))
	}
	return nil
}

func (s *SchedulerService) setSchedulerChannelGroup(ctx context.Context, cfg domain.SchedulerConfig, current domain.SchedulerChannel, group string, priority int64) error {
	return s.coordinateSchedulerFields(ctx, cfg, current, group, priority, current.Weight, false, domain.ControlSourceCost, "成本分组基线")
}

func (s *SchedulerService) schedulerJSON(ctx context.Context, cfg domain.SchedulerConfig, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, joinSchedulerURL(cfg.BaseURL, path), r)
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
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("调度器 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if len(b) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return err
	}
	return nil
}

func joinSchedulerURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func schedulerMessage(raw map[string]any) string {
	for _, key := range []string{"message", "error", "msg"} {
		if v := strings.TrimSpace(fmt.Sprint(raw[key])); v != "" && v != "<nil>" {
			return v
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
		ch := domain.SchedulerChannel{
			ID:       schedulerString(firstScheduler(m, "id")),
			Name:     schedulerString(firstScheduler(m, "name", "channel_name")),
			Status:   schedulerInt(firstScheduler(m, "status")),
			Priority: int64(schedulerInt(firstScheduler(m, "priority"))),
			Weight:   schedulerUint(firstScheduler(m, "weight")),
			Tag:      schedulerString(firstScheduler(m, "tag")),
			Type:     schedulerString(firstScheduler(m, "type")),
			Group:    schedulerString(firstScheduler(m, "group")),
			Models:   schedulerStrings(firstScheduler(m, "models")),
		}
		if ch.ID != "" {
			out = append(out, ch)
		}
	}
	return out
}

func schedulerGroups(raw map[string]any) []domain.SchedulerGroup {
	data := firstScheduler(raw, "data", "groups", "items")
	seen := map[string]domain.SchedulerGroup{}
	if m, ok := data.(map[string]any); ok {
		for name, value := range m {
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
	group := domain.SchedulerGroup{
		Name:        schedulerString(firstScheduler(m, "name", "group", "group_name", "groupName", "id", "group_id")),
		Ratio:       schedulerRatioString(firstScheduler(m, "rate_multiplier", "rateMultiplier", "ratio", "group_ratio", "groupRatio")),
		Description: schedulerString(firstScheduler(m, "description", "desc", "remark", "memo", "note")),
	}
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

func schedulerArray(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, key := range []string{"items", "channels", "data"} {
			if a, ok := x[key].([]any); ok {
				return a
			}
		}
	}
	return nil
}

func firstScheduler(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func schedulerString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func schedulerRatioString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSuffix(strings.TrimSpace(x), "x")
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func schedulerInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func schedulerUint(v any) uint {
	n := schedulerInt(v)
	if n < 0 {
		return 0
	}
	return uint(n)
}

func schedulerStrings(v any) []string {
	items := schedulerArray(v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := schedulerString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}
