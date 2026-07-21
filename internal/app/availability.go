package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *SchedulerService) AvailabilityPolicy(ctx context.Context, upstreamID string) (domain.AvailabilityPolicy, error) {
	if _, err := s.app.Store.Upstream(ctx, upstreamID); err != nil {
		return domain.AvailabilityPolicy{}, err
	}
	return s.app.Store.AvailabilityPolicy(ctx, upstreamID)
}

func (s *SchedulerService) SaveAvailabilityPolicy(ctx context.Context, upstreamID string, policy domain.AvailabilityPolicy) (domain.AvailabilityPolicy, error) {
	if _, err := s.app.Store.Upstream(ctx, upstreamID); err != nil {
		return domain.AvailabilityPolicy{}, err
	}
	policy = policy.Normalized()
	if err := domain.ValidateAvailabilityPolicy(policy); err != nil {
		return domain.AvailabilityPolicy{}, BadRequest(err)
	}
	if _, err := s.app.Store.UpdateAvailabilityPolicy(ctx, upstreamID, policy); err != nil {
		return domain.AvailabilityPolicy{}, err
	}
	// 策略生效时立即重算，但 observe 只写本地运行态。
	if err := s.ReconcileAvailability(ctx); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		return policy, err
	}
	return policy, nil
}

// ReconcileAvailability 是每分钟的控制面循环：同步绑定、纠正渠道漂移并重放中断的动作意图。
func (s *SchedulerService) ReconcileAvailability(ctx context.Context) error {
	eligible, err := s.hasManagedAvailabilityCards(ctx)
	if err != nil {
		return err
	}
	if !eligible {
		return nil
	}
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if !schedulerConfigured(cfg) {
		return errSchedulerNotConfigured
	}
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return err
	}
	byID := make(map[string]domain.SchedulerChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	return s.reconcileAvailabilityWithChannels(ctx, byID)
}

func (s *SchedulerService) hasManagedAvailabilityCards(ctx context.Context) (bool, error) {
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return false, err
	}
	upstreams, err := s.app.Store.ListUpstreams(ctx)
	if err != nil {
		return false, err
	}
	byID := make(map[string]domain.Upstream, len(upstreams))
	for _, upstream := range upstreams {
		byID[upstream.ID] = upstream
	}
	for _, card := range cards {
		if upstream, ok := byID[card.UpstreamID]; ok && managedAvailabilityCard(card, upstream) {
			return true, nil
		}
	}
	return false, nil
}

func (s *SchedulerService) reconcileAvailabilityWithChannels(ctx context.Context, channels map[string]domain.SchedulerChannel) error {
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	upstreams, err := s.app.Store.ListUpstreams(ctx)
	if err != nil {
		return err
	}
	byUpstream := make(map[string]domain.Upstream, len(upstreams))
	for _, upstream := range upstreams {
		byUpstream[upstream.ID] = upstream
	}
	seen := map[string]bool{}
	for _, card := range cards {
		upstream, ok := byUpstream[card.UpstreamID]
		if !ok || !managedAvailabilityCard(card, upstream) {
			continue
		}
		channel, found := channels[card.SchedulerChannelID]
		if !found {
			if err := s.markAvailabilityBindingInvalid(ctx, card, upstream); err != nil {
				return err
			}
			continue
		}
		seen[channel.ID] = true
		row, err := s.ensureAvailabilityBinding(ctx, card, upstream, channel)
		if err != nil {
			return err
		}
		policy, err := s.app.Store.AvailabilityPolicy(ctx, upstream.ID)
		if err != nil {
			return err
		}
		latest, fresh, remain, runway := s.availabilityBalance(ctx, upstream, policy)
		_ = latest
		row, err = s.applyAvailabilityDecision(ctx, row, policy, fresh, remain)
		if err != nil {
			return err
		}
		if runway.Warning {
			s.recordRunwayWarning(ctx, upstream, row, runway)
		}
		// 单个渠道远端失败已写入 pending intent 和退避时间，不能阻塞其余渠道。
		_ = s.driveAvailabilityAction(ctx, row, policy, fresh, remain)
	}
	rows, err := s.app.Store.ChannelAvailabilities(ctx, "", "")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Managed && !seen[row.ChannelID] {
			if _, err := s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
				next.Managed = false
				next.PendingAction, next.PendingStatus, next.RetryAt = "", 0, nil
				next.LastError = "绑定已解除，AUM 不会自动恢复该渠道"
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func managedAvailabilityCard(card domain.ModelCard, upstream domain.Upstream) bool {
	// 调度器远端始终由 SchedulerConfig 指向 new-api，但渠道可以绑定来自
	// new-api 或 sub2api 的号池卡片；余额与探测 blocker 仍按各自上游隔离。
	return card.Enabled && card.PoolEnabled && strings.TrimSpace(card.SchedulerChannelID) != "" && strings.TrimSpace(card.UpstreamID) != "" && (upstream.Type == "newapi" || upstream.Type == "sub2api")
}

func (s *SchedulerService) ensureAvailabilityBinding(ctx context.Context, card domain.ModelCard, upstream domain.Upstream, channel domain.SchedulerChannel) (domain.ChannelAvailability, error) {
	seed := domain.ChannelAvailability{
		ChannelID: channel.ID, ChannelName: domain.FirstNonEmpty(channel.Name, card.SchedulerChannelName), CardID: card.ID, CardName: card.Name,
		UpstreamID: upstream.ID, UpstreamName: upstream.Name, Managed: true, DesiredStatus: 1, ActualStatus: channel.Status,
	}
	return s.mutateAvailability(ctx, seed, func(next *domain.ChannelAvailability) {
		next.ChannelName = seed.ChannelName
		next.CardID, next.CardName = card.ID, card.Name
		next.UpstreamID, next.UpstreamName = upstream.ID, upstream.Name
		next.Managed = true
		next.ActualStatus = channel.Status
		if channel.Status == 3 && next.DisabledAt == nil {
			now := time.Now().UTC()
			next.DisabledAt = &now
		}
	})
}

func (s *SchedulerService) markAvailabilityBindingInvalid(ctx context.Context, card domain.ModelCard, upstream domain.Upstream) error {
	seed := domain.ChannelAvailability{ChannelID: card.SchedulerChannelID, ChannelName: card.SchedulerChannelName, CardID: card.ID, CardName: card.Name, UpstreamID: upstream.ID, UpstreamName: upstream.Name, Managed: false}
	updated, changed, err := s.mutateAvailabilityIfChanged(ctx, seed, func(next *domain.ChannelAvailability) bool {
		changed := next.Managed || next.PendingAction != "" || next.PendingStatus != 0 || next.RetryAt != nil || next.LastError != "调度器中找不到该渠道，绑定已失效"
		if !changed {
			return false
		}
		next.Managed = false
		next.PendingAction, next.PendingStatus, next.RetryAt = "", 0, nil
		next.LastError = "调度器中找不到该渠道，绑定已失效"
		return true
	})
	if err == nil && changed {
		s.recordAvailabilityLog(ctx, updated, "binding_invalid", "error", "调度器中找不到该渠道，已解除 AUM 所有权", "binding_invalid")
	}
	return err
}

// ReleaseAvailabilityBinding 仅放弃控制权，绝不为解绑/退出号池自动打开旧渠道。
func (s *SchedulerService) ReleaseAvailabilityBinding(ctx context.Context, card domain.ModelCard, reason string) error {
	if strings.TrimSpace(card.SchedulerChannelID) == "" {
		return nil
	}
	row, found, err := s.app.Store.ChannelAvailability(ctx, card.SchedulerChannelID)
	if err != nil || !found || row.CardID != card.ID {
		return err
	}
	updated, err := s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
		next.Managed = false
		next.PendingAction, next.PendingStatus, next.RetryAt = "", 0, nil
		next.LastError = reason
	})
	if err == nil {
		s.recordAvailabilityLog(ctx, updated, "release", "skipped", reason, "binding_released")
	}
	return err
}

func (s *SchedulerService) availabilityBalance(ctx context.Context, upstream domain.Upstream, policy domain.AvailabilityPolicy) (domain.BalanceSnapshot, bool, float64, domain.BalanceRunway) {
	latest, err := s.app.Store.LatestBalance(ctx, upstream.ID)
	if err != nil {
		return domain.BalanceSnapshot{}, false, 0, domain.BalanceRunway{}
	}
	_, _, remain := domain.ConvertedBalanceValues(upstream.Type, domain.BalanceRate(upstream), latest.Balance, latest.Used, latest.Remain)
	settings, err := s.app.Store.Settings(ctx)
	interval := 5 * time.Minute
	if err == nil {
		interval = time.Duration(settings.CheckIntervalMinutes) * time.Minute
	}
	fresh := domain.FreshBalance(latest, time.Now().UTC(), interval)
	snapshots, err := s.app.Store.BalanceSnapshotsSince(ctx, upstream.ID, time.Now().UTC().Add(-6*time.Hour))
	if err != nil {
		return latest, fresh, remain, domain.BalanceRunway{}
	}
	for i := range snapshots {
		_, _, snapshots[i].Remain = domain.ConvertedBalanceValues(upstream.Type, domain.BalanceRate(upstream), snapshots[i].Balance, snapshots[i].Used, snapshots[i].Remain)
	}
	return latest, fresh, remain, domain.PredictBalanceRunway(snapshots, policy.RunwayWarningHours)
}

func (s *SchedulerService) applyAvailabilityDecision(ctx context.Context, row domain.ChannelAvailability, policy domain.AvailabilityPolicy, fresh bool, remain float64) (domain.ChannelAvailability, error) {
	now := time.Now().UTC()
	return s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
		if next.Override == domain.OverrideForceEnable && (next.OverrideUntil == nil || !now.Before(*next.OverrideUntil)) {
			next.Override, next.OverrideUntil = "", nil
		}
		next.Blockers = domain.ApplyBalanceBlocker(next.Blockers, policy, remain, fresh, now)
		decision := domain.AvailabilityDecisionFor(now, policy, *next)
		next.DesiredStatus = decision.DesiredStatus
		if decision.HardBlocked && next.DisabledAt == nil {
			next.DisabledAt = &now
			next.RecoverySuccess = 0
		}
	})
}

func (s *SchedulerService) driveAvailabilityAction(ctx context.Context, row domain.ChannelAvailability, policy domain.AvailabilityPolicy, fresh bool, remain float64) error {
	if !row.Managed {
		return nil
	}
	if row.PendingAction == "" && (row.ActualStatus == 0 || row.ActualStatus == row.DesiredStatus) {
		return nil
	}
	if row.DesiredStatus == 1 && (row.ActualStatus == 2 || row.ActualStatus == 3) && row.DisabledAt != nil && !domain.RecoveryEligible(time.Now().UTC(), row, policy, fresh, remain) {
		return nil
	}
	now := time.Now().UTC()
	if row.RetryAt != nil && now.Before(*row.RetryAt) {
		return nil
	}
	if row.PendingAction == "" {
		updated, err := s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
			next.PendingStatus = next.DesiredStatus
			next.PendingAction = availabilityAction(next.DesiredStatus)
			next.RetryAt = nil
		})
		if err != nil {
			return err
		}
		row = updated
	}
	return s.executeAvailabilityAction(ctx, row)
}

func availabilityAction(status int) string {
	if status == 2 {
		return domain.AvailabilityActionDisable
	}
	return domain.AvailabilityActionEnable
}

func (s *SchedulerService) executeAvailabilityAction(ctx context.Context, row domain.ChannelAvailability) error {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if !schedulerConfigured(cfg) {
		return errSchedulerNotConfigured
	}
	channel, found, err := s.schedulerChannel(ctx, cfg, row.ChannelID)
	if err == nil && !found {
		return s.finishAvailabilityMissing(ctx, row)
	}
	if err == nil && channel.Status == row.PendingStatus {
		return s.finishAvailabilityAction(ctx, row, channel.Status)
	}
	if err == nil {
		err = s.setSchedulerChannelStatus(ctx, row.ChannelID, row.PendingStatus)
	}
	if err == nil {
		channel, found, err = s.schedulerChannel(ctx, cfg, row.ChannelID)
		if err == nil && !found {
			return s.finishAvailabilityMissing(ctx, row)
		}
		if err == nil && channel.Status != row.PendingStatus {
			err = fmt.Errorf("调度器写入后状态校验失败：期望 %d，实际 %d", row.PendingStatus, channel.Status)
		}
	}
	if err != nil {
		return s.failAvailabilityAction(ctx, row, err)
	}
	return s.finishAvailabilityAction(ctx, row, channel.Status)
}

func (s *SchedulerService) finishAvailabilityMissing(ctx context.Context, row domain.ChannelAvailability) error {
	updated, err := s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
		next.Managed = false
		next.PendingAction, next.PendingStatus, next.RetryAt = "", 0, nil
		next.LastError = "调度器中找不到该渠道，绑定已失效"
	})
	if err == nil {
		s.recordAvailabilityLog(ctx, updated, "binding_invalid", "error", updated.LastError, "binding_invalid")
	}
	return err
}

func (s *SchedulerService) finishAvailabilityAction(ctx context.Context, row domain.ChannelAvailability, actual int) error {
	applied := false
	updated, err := s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
		next.ActualStatus = actual
		if next.PendingAction != row.PendingAction || next.PendingStatus != row.PendingStatus {
			return
		}
		applied = true
		next.PendingAction, next.PendingStatus, next.RetryAt, next.RetryCount, next.LastError = "", 0, nil, 0, ""
		now := time.Now().UTC()
		if actual == 2 {
			if next.DisabledAt == nil {
				next.DisabledAt = &now
			}
			next.RecoverySuccess = 0
		} else if actual == 1 {
			next.DisabledAt = nil
			next.RecoverySuccess = 0
		}
	})
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	if updated.CardID != "" {
		_ = s.app.Cards.UpdateCardSchedulerAutoDisabled(ctx, updated.CardID, actual == 2)
	}
	action := availabilityAction(actual)
	s.recordAvailabilityLog(ctx, updated, action, "success", availabilityActionMessage(actual), action)
	return nil
}

func (s *SchedulerService) failAvailabilityAction(ctx context.Context, row domain.ChannelAvailability, actionErr error) error {
	message := actionErr.Error()
	changed := row.LastError != message
	applied := false
	updated, err := s.mutateAvailability(ctx, row, func(next *domain.ChannelAvailability) {
		if next.PendingAction != row.PendingAction || next.PendingStatus != row.PendingStatus {
			return
		}
		applied = true
		next.RetryCount++
		next.RetryAt = retryAt(time.Now().UTC(), next.RetryCount)
		next.LastError = message
	})
	if err != nil {
		return err
	}
	if applied && changed {
		s.recordAvailabilityLog(ctx, updated, updated.PendingAction, "error", message, "availability_action_failed")
	}
	return actionErr
}

func retryAt(now time.Time, count int) *time.Time {
	delays := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}
	if count < 1 {
		count = 1
	}
	if count > len(delays) {
		count = len(delays)
	}
	value := now.Add(delays[count-1])
	return &value
}

func availabilityActionMessage(status int) string {
	if status == 2 {
		return "可用性策略已关闭调度器渠道"
	}
	return "可用性策略已恢复调度器渠道"
}

func (s *SchedulerService) mutateAvailability(ctx context.Context, seed domain.ChannelAvailability, mutate func(*domain.ChannelAvailability)) (domain.ChannelAvailability, error) {
	current, _, err := s.mutateAvailabilityIfChanged(ctx, seed, func(next *domain.ChannelAvailability) bool {
		mutate(next)
		return true
	})
	return current, err
}

// mutateAvailabilityIfChanged 只在回调确认状态发生变化时执行 CAS，供需要按状态转换记录事件的路径使用。
func (s *SchedulerService) mutateAvailabilityIfChanged(ctx context.Context, seed domain.ChannelAvailability, mutate func(*domain.ChannelAvailability) bool) (domain.ChannelAvailability, bool, error) {
	for attempt := 0; attempt < 6; attempt++ {
		current, found, err := s.app.Store.ChannelAvailability(ctx, seed.ChannelID)
		if err != nil {
			return domain.ChannelAvailability{}, false, err
		}
		expected := int64(0)
		if !found {
			current = seed
			if current.DesiredStatus == 0 {
				current.DesiredStatus = 1
			}
		} else {
			expected = current.Version
		}
		if !mutate(&current) {
			return current, false, nil
		}
		current.UpdatedAt = time.Now().UTC()
		ok, err := s.app.Store.SaveChannelAvailabilityCAS(ctx, current, expected)
		if err != nil {
			return domain.ChannelAvailability{}, false, err
		}
		if ok {
			current.Version = expected + 1
			return current, true, nil
		}
	}
	return domain.ChannelAvailability{}, false, errors.New("渠道可用性状态并发更新过多，请重试")
}

func schedulerConfigured(cfg domain.SchedulerConfig) bool {
	return strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.UserID) != "" && strings.TrimSpace(cfg.AccessToken) != ""
}

func (s *SchedulerService) RecordAvailabilityProbe(ctx context.Context, card domain.ModelCard, success, quotaExhausted bool, purpose string) error {
	if card.BaseURL != "" || card.UpstreamID == "" || card.SchedulerChannelID == "" || !card.PoolEnabled {
		return nil
	}
	upstream, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	if err != nil || (upstream.Type != "newapi" && upstream.Type != "sub2api") {
		return err
	}
	seed := domain.ChannelAvailability{ChannelID: card.SchedulerChannelID, ChannelName: card.SchedulerChannelName, CardID: card.ID, CardName: card.Name, UpstreamID: upstream.ID, UpstreamName: upstream.Name, Managed: true, DesiredStatus: 1}
	row, err := s.mutateAvailability(ctx, seed, func(next *domain.ChannelAvailability) {
		now := time.Now().UTC()
		next.Managed = true
		if success {
			next.Blockers = domain.RemoveBlocker(next.Blockers, domain.BlockerProbeFailed)
			next.Blockers = domain.RemoveBlocker(next.Blockers, domain.BlockerQuotaExhausted)
			if purpose == "recovery" {
				next.RecoverySuccess++
			}
			return
		}
		next.RecoverySuccess = 0
		if quotaExhausted {
			next.Blockers = domain.UpsertBlocker(next.Blockers, domain.AvailabilityBlocker{Kind: domain.BlockerQuotaExhausted, Since: now, Message: "探测明确返回额度耗尽"})
		} else {
			next.Blockers = domain.UpsertBlocker(next.Blockers, domain.AvailabilityBlocker{Kind: domain.BlockerProbeFailed, Since: now, Message: "三次探测均失败"})
		}
	})
	if err != nil {
		return err
	}
	policy, err := s.app.Store.AvailabilityPolicy(ctx, upstream.ID)
	if err != nil {
		return err
	}
	_, fresh, remain, _ := s.availabilityBalance(ctx, upstream, policy)
	row, err = s.applyAvailabilityDecision(ctx, row, policy, fresh, remain)
	if err != nil {
		return err
	}
	if err := s.driveAvailabilityAction(ctx, row, policy, fresh, remain); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		// 动作失败已作为 pending intent 落库，不能把正常的模型探测记成内部执行失败。
		return nil
	}
	return nil
}

func (s *SchedulerService) AvailabilityRows(ctx context.Context, upstreamID, state string) ([]domain.AvailabilityView, error) {
	// 管理端查看时先同步一次远端实际状态，并重放到期的 pending action。
	if err := s.ReconcileAvailability(ctx); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		return nil, err
	}
	rows, err := s.app.Store.ChannelAvailabilities(ctx, upstreamID, "")
	if err != nil {
		return nil, err
	}
	upstreams, err := s.app.Store.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.Upstream{}
	for _, upstream := range upstreams {
		byID[upstream.ID] = upstream
	}
	out := make([]domain.AvailabilityView, 0, len(rows))
	for _, row := range rows {
		upstream, ok := byID[row.UpstreamID]
		if !ok {
			continue
		}
		policy, err := s.app.Store.AvailabilityPolicy(ctx, upstream.ID)
		if err != nil {
			return nil, err
		}
		_, fresh, remain, runway := s.availabilityBalance(ctx, upstream, policy)
		view := domain.AvailabilityView{ChannelAvailability: row, State: domain.AvailabilityDecisionFor(time.Now().UTC(), policy, row).State, BalanceFresh: fresh, BalanceRemain: remain, Runway: runway}
		if state != "" && view.State != state {
			continue
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool {
		return availabilityStateRank(out[i].State) < availabilityStateRank(out[j].State)
	})
	return out, nil
}

func availabilityStateRank(state string) int {
	switch state {
	case domain.AvailabilityActionFailed:
		return 0
	case domain.AvailabilityBlocked:
		return 1
	case domain.AvailabilityWarning:
		return 2
	case domain.AvailabilityRecovering:
		return 3
	case domain.AvailabilityManualOff:
		return 4
	case domain.AvailabilityForcedOn:
		return 5
	case domain.AvailabilityHealthy:
		return 6
	default:
		return 7
	}
}

func (s *SchedulerService) AvailabilityAction(ctx context.Context, cardID, action string, minutes int) (domain.AvailabilityView, error) {
	card, err := s.app.Cards.Card(ctx, cardID)
	if err != nil {
		return domain.AvailabilityView{}, err
	}
	if !card.PoolEnabled || card.SchedulerChannelID == "" || card.UpstreamID == "" || card.BaseURL != "" {
		return domain.AvailabilityView{}, ErrBadRequest("该卡片没有受 AUM 管理的调度器渠道")
	}
	upstream, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		return domain.AvailabilityView{}, err
	}
	if upstream.Type != "newapi" && upstream.Type != "sub2api" {
		return domain.AvailabilityView{}, ErrBadRequest("仅支持 new-api 或 sub2api 上游的调度器渠道")
	}
	if action == "check_now" {
		if err := s.app.Probe.CheckCard(ctx, card.ID); err != nil {
			return domain.AvailabilityView{}, err
		}
		return s.availabilityViewForCard(ctx, card.ID)
	}
	if action != "force_enable" && action != "hold_off" && action != "release_hold" {
		return domain.AvailabilityView{}, ErrBadRequest("未知渠道操作")
	}
	if minutes == 0 {
		minutes = 30
	}
	if action == "force_enable" && minutes != 15 && minutes != 30 && minutes != 60 && minutes != 240 {
		return domain.AvailabilityView{}, ErrBadRequest("force_enable 只支持 15/30/60/240 分钟")
	}
	seed := domain.ChannelAvailability{ChannelID: card.SchedulerChannelID, ChannelName: card.SchedulerChannelName, CardID: card.ID, CardName: card.Name, UpstreamID: upstream.ID, UpstreamName: upstream.Name, Managed: true, DesiredStatus: 1}
	row, err := s.mutateAvailability(ctx, seed, func(next *domain.ChannelAvailability) {
		now := time.Now().UTC()
		switch action {
		case "force_enable":
			until := now.Add(time.Duration(minutes) * time.Minute)
			next.Override, next.OverrideUntil, next.DesiredStatus = domain.OverrideForceEnable, &until, 1
		case "hold_off":
			next.Override, next.OverrideUntil, next.DesiredStatus = domain.OverrideManualHold, nil, 2
		case "release_hold":
			next.Override, next.OverrideUntil = "", nil
		}
	})
	if err != nil {
		return domain.AvailabilityView{}, err
	}
	policy, err := s.app.Store.AvailabilityPolicy(ctx, upstream.ID)
	if err != nil {
		return domain.AvailabilityView{}, err
	}
	_, fresh, remain, _ := s.availabilityBalance(ctx, upstream, policy)
	row, err = s.applyAvailabilityDecision(ctx, row, policy, fresh, remain)
	if err != nil {
		return domain.AvailabilityView{}, err
	}
	if err := s.driveAvailabilityAction(ctx, row, policy, fresh, remain); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		return domain.AvailabilityView{}, err
	}
	return s.availabilityViewForCard(ctx, card.ID)
}

func (s *SchedulerService) availabilityViewForCard(ctx context.Context, cardID string) (domain.AvailabilityView, error) {
	rows, err := s.AvailabilityRows(ctx, "", "")
	if err != nil {
		return domain.AvailabilityView{}, err
	}
	for _, row := range rows {
		if row.CardID == cardID {
			return row, nil
		}
	}
	return domain.AvailabilityView{}, ErrBadRequest("未找到渠道可用性运行态")
}

func (s *SchedulerService) recordAvailabilityLog(ctx context.Context, row domain.ChannelAvailability, action, status, message, reason string) {
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{CardID: row.CardID, CardName: row.CardName, ChannelID: row.ChannelID, ChannelName: row.ChannelName, Action: action, Status: status, Message: message, Reason: reason})
	severity := "warning"
	eventType := "availability_changed"
	if status == "error" {
		severity, eventType = "error", "availability_action_failed"
	} else if action == domain.AvailabilityActionEnable {
		severity = "success"
	}
	_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{Type: eventType, Severity: severity, Title: "渠道可用性变更", Message: row.ChannelName + " " + message, TargetType: "upstream", TargetID: row.UpstreamID, Actions: []string{"scheduler_availability"}})
	s.notifyAvailability(ctx, eventType, row.ChannelName+" "+message, false)
}

func (s *SchedulerService) recordRunwayWarning(ctx context.Context, upstream domain.Upstream, row domain.ChannelAvailability, runway domain.BalanceRunway) {
	if runway.Samples < 3 || !runway.Warning {
		return
	}
	_ = s.app.alert(ctx, upstream, "runway:"+row.ChannelID, true, fmt.Sprintf("%s 预计 %.1f 小时耗尽，受影响渠道：%s", upstream.Name, runway.HoursRemaining, row.ChannelName))
}

func (s *SchedulerService) notifyAvailability(ctx context.Context, eventType, message string, recover bool) {
	rules, err := s.app.Store.NotificationRules(ctx)
	if err != nil || !domain.ShouldNotify(rules, eventType, recover) {
		return
	}
	_ = s.app.Notify.Send(ctx, message)
}
