package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

func (s *ProbeService) SaveCard(ctx context.Context, id string, in domain.ModelCard) (domain.ModelCard, error) {
	var old domain.ModelCard
	if id != "" {
		var err error
		old, err = s.app.Cards.Card(ctx, id)
		if err != nil {
			return domain.ModelCard{}, err
		}
		in = in.MergeUpdate(old)
	}
	card, err := s.normalizeCard(ctx, in)
	if err != nil {
		return domain.ModelCard{}, err
	}
	if card.PoolEnabled && card.SchedulerChannelID != "" {
		cards, err := s.app.Cards.ListCards(ctx)
		if err != nil {
			return domain.ModelCard{}, err
		}
		for _, item := range cards {
			if item.PoolEnabled && item.ID != id && item.SchedulerChannelID == card.SchedulerChannelID {
				return domain.ModelCard{}, ErrBadRequest("scheduler channel already bound to another card")
			}
		}
	}
	if id == "" {
		out, err := s.app.Cards.CreateCard(ctx, card)
		if err == nil {
			if err := s.app.recordCardCostSnapshot(ctx, out); err != nil {
				return out.Public(), err
			}
		}
		if err == nil && out.PoolEnabled && out.SchedulerChannelID != "" {
			s.app.syncSchedulerGroupsBestEffort(ctx)
		}
		return out.Public(), err
	}
	// normalize 会构造新结构体；从 old 重新挂上运行时身份字段。
	// SchedulerAutoDisabled 已在 normalize 前由 MergeUpdate 决定。
	card.ID = old.ID
	card.LastError = old.LastError
	card.FailureCount = old.FailureCount
	card.SchedulerAutoDisabledAt = old.SchedulerAutoDisabledAt
	card.SortOrder = old.SortOrder
	card.CreatedAt = old.CreatedAt
	out, err := s.app.Cards.UpdateCard(ctx, card)
	changedBinding := old.UpstreamID != out.UpstreamID || old.KeyID != out.KeyID || old.SchedulerChannelID != out.SchedulerChannelID || old.PoolEnabled != out.PoolEnabled || old.ManualCostRatio != out.ManualCostRatio
	if err == nil && old.SchedulerChannelID != "" && (old.SchedulerChannelID != out.SchedulerChannelID || !out.PoolEnabled) {
		if err := s.app.recordInactiveCostSnapshot(ctx, old, "渠道绑定已变更"); err != nil {
			return out.Public(), err
		}
	}
	if err == nil && (changedBinding || old.SchedulerChannelName != out.SchedulerChannelName || old.Name != out.Name) {
		if err := s.app.recordCardCostSnapshot(ctx, out); err != nil {
			return out.Public(), err
		}
	}
	if err == nil && out.PoolEnabled && changedBinding && out.SchedulerChannelID != "" {
		s.app.syncSchedulerGroupsBestEffort(ctx)
	}
	return out.Public(), err
}

func (s *ProbeService) normalizeCard(ctx context.Context, in domain.ModelCard) (domain.ModelCard, error) {
	card := domain.ModelCard{
		Name:                  strings.TrimSpace(in.Name),
		BaseURL:               strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		APIKey:                strings.TrimSpace(in.APIKey),
		UpstreamID:            strings.TrimSpace(in.UpstreamID),
		KeyID:                 strings.TrimSpace(in.KeyID),
		Model:                 domain.ProbeModel,
		DisplayGroup:          strings.TrimSpace(in.DisplayGroup),
		PoolEnabled:           in.PoolEnabled,
		PoolEnabledSet:        true,
		ManualCostRatio:       strings.TrimSpace(in.ManualCostRatio),
		SchedulerGroup:        strings.TrimSpace(in.SchedulerGroup),
		SchedulerChannelID:    strings.TrimSpace(in.SchedulerChannelID),
		SchedulerChannelName:  strings.TrimSpace(in.SchedulerChannelName),
		SchedulerAutoDisabled: in.SchedulerAutoDisabled,
		Enabled:               in.Enabled,
		PublicEnabled:         in.PublicEnabled,
		SortOrder:             in.SortOrder,
	}
	if !in.PoolEnabledSet {
		card.PoolEnabled = true
	}
	custom := card.BaseURL != "" || card.APIKey != ""
	if !card.PoolEnabled {
		card.ManualCostRatio, card.SchedulerGroup, card.SchedulerChannelID, card.SchedulerChannelName = "", "", "", ""
		card.SchedulerAutoDisabled = false
	} else if !custom {
		card.ManualCostRatio = ""
	} else if card.ManualCostRatio != "" {
		ratio, err := strconv.ParseFloat(card.ManualCostRatio, 64)
		if err != nil || ratio < 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return card, ErrBadRequest("manual_cost_ratio is invalid")
		}
	}
	if custom {
		if card.Name == "" || card.BaseURL == "" || card.APIKey == "" {
			return card, ErrBadRequest("name, base_url and api_key are required")
		}
		card.UpstreamID, card.KeyID = "", ""
		return card, nil
	}
	if card.UpstreamID == "" || card.KeyID == "" {
		return card, ErrBadRequest("upstream_id and key_id are required")
	}
	u, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		return card, err
	}
	k, err := s.app.Store.Key(ctx, card.KeyID)
	if err != nil {
		return card, err
	}
	if k.UpstreamID != card.UpstreamID {
		return card, ErrBadRequest("key does not belong to upstream")
	}
	if card.Name == "" {
		card.Name = domain.CardName(u, &k)
	}
	return card, nil
}

func (s *ProbeService) SortCards(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return ErrBadRequest("ids are required")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return ErrBadRequest("card id is required")
		}
		if _, ok := seen[id]; ok {
			return ErrBadRequest("duplicate card id")
		}
		seen[id] = struct{}{}
	}
	return s.app.Cards.UpdateCardOrder(ctx, ids)
}

func (s *ProbeService) DeleteCard(ctx context.Context, id string) error {
	card, err := s.app.Cards.Card(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := s.app.recordInactiveCostSnapshot(ctx, card, "卡片已删除"); err != nil {
			return err
		}
	}
	return s.app.Cards.DeleteCard(ctx, id)
}

func (s *ProbeService) CheckCard(ctx context.Context, cardID string) error {
	card, err := s.app.Cards.Card(ctx, cardID)
	if err != nil {
		return err
	}
	if card.BaseURL != "" {
		return s.checkCustomCard(ctx, card)
	}
	u, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		return err
	}
	if card.KeyID == "" {
		msg := "未选择 Key"
		if _, err := s.app.Store.SaveProbe(ctx, u.ID, card.ID, monitor.ProbeResult{Status: monitor.StatusFailed, Error: msg}); err != nil {
			return err
		}
		failures := card.FailureCount + 1
		if err := s.app.Cards.UpdateCardProbeState(ctx, card.ID, msg, failures); err != nil {
			return err
		}
		return s.app.applySchedulerAutomation(ctx, card, false, failures)
	}
	key, err := s.app.Store.Key(ctx, card.KeyID)
	if err != nil {
		return err
	}
	muteAt := s.app.probeMuteFailureThreshold(ctx)
	probe := s.probeWithInternalRetry(ctx, u.BaseURL, key.Key, domain.ProbeModel)
	if _, err := s.app.Store.SaveProbe(ctx, u.ID, card.ID, probe); err != nil {
		return err
	}
	if monitor.IsInternalProbeError(probe.Error) {
		return s.app.alert(ctx, u, "internal:"+card.ID, true, card.Name+" 本地探测错误: "+probe.Error)
	}
	failures := 0
	lastErr := ""
	if !probe.Success {
		failures = card.FailureCount + 1
		lastErr = probe.Error
	}
	if err := s.app.Cards.UpdateCardProbeState(ctx, card.ID, lastErr, failures); err != nil {
		return err
	}
	_ = s.app.applySchedulerAutomation(ctx, card, probe.Success, failures)
	if probe.Success {
		_ = s.app.alert(ctx, u, "internal:"+card.ID, false, card.Name+" 本地探测已恢复")
		_ = s.app.alert(ctx, u, "quota:"+card.ID, false, card.Name+" 余额不足状态已恢复")
		return s.app.alert(ctx, u, "ping:"+card.ID, false, card.Name+" 探测已恢复")
	}
	if domain.SuppressProbeAlert(card.SchedulerAutoDisabled, failures, muteAt) {
		return nil
	}
	kind, msg := probeAlertKind(card, probe)
	return s.app.alert(ctx, u, kind, failures >= muteAt, msg)
}

func (s *ProbeService) checkCustomCard(ctx context.Context, card domain.ModelCard) error {
	muteAt := s.app.probeMuteFailureThreshold(ctx)
	if card.APIKey == "" {
		msg := "未填写 Key"
		if _, err := s.app.Store.SaveProbe(ctx, "", card.ID, monitor.ProbeResult{Status: monitor.StatusFailed, Error: msg}); err != nil {
			return err
		}
		failures := card.FailureCount + 1
		if err := s.app.Cards.UpdateCardProbeState(ctx, card.ID, msg, failures); err != nil {
			return err
		}
		return s.app.applySchedulerAutomation(ctx, card, false, failures)
	}
	probe := s.probeWithInternalRetry(ctx, card.BaseURL, card.APIKey, domain.ProbeModel)
	if _, err := s.app.Store.SaveProbe(ctx, "", card.ID, probe); err != nil {
		return err
	}
	pseudo := domain.Upstream{ID: "card:" + card.ID, Name: card.Name}
	if monitor.IsInternalProbeError(probe.Error) {
		return s.app.alert(ctx, pseudo, "internal:"+card.ID, true, card.Name+" 本地探测错误: "+probe.Error)
	}
	failures := 0
	lastErr := ""
	if !probe.Success {
		failures = card.FailureCount + 1
		lastErr = probe.Error
	}
	if err := s.app.Cards.UpdateCardProbeState(ctx, card.ID, lastErr, failures); err != nil {
		return err
	}
	if probe.Success {
		_ = s.app.alert(ctx, pseudo, "internal:"+card.ID, false, card.Name+" 本地探测已恢复")
		_ = s.app.alert(ctx, pseudo, "quota:"+card.ID, false, card.Name+" 余额不足状态已恢复")
		_ = s.app.alert(ctx, pseudo, "ping:"+card.ID, false, card.Name+" 探测已恢复")
	} else {
		if !domain.SuppressProbeAlert(card.SchedulerAutoDisabled, failures, muteAt) {
			kind, msg := probeAlertKind(card, probe)
			_ = s.app.alert(ctx, pseudo, kind, failures >= muteAt, msg)
		}
	}
	return s.app.applySchedulerAutomation(ctx, card, probe.Success, failures)
}

func (s *ProbeService) probeWithInternalRetry(ctx context.Context, baseURL, key, model string) monitor.ProbeResult {
	retries, interval := s.app.probeInternalRetryPolicy(ctx)
	probe := s.app.Prober.Probe(ctx, baseURL, key, model)
	for attempt := 0; attempt < retries; attempt++ {
		if probe.Success || !monitor.IsInternalProbeError(probe.Error) {
			break
		}
		if interval > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return probe
			case <-timer.C:
			}
		}
		probe = s.app.Prober.Probe(ctx, baseURL, key, model)
	}
	return probe
}

func probeAlertKind(card domain.ModelCard, probe monitor.ProbeResult) (string, string) {
	return domain.ProbeAlertKind(card.Name, card.ID, probe.Error)
}

func (s *ProbeService) MonitorStatus(ctx context.Context, window string) (map[string]any, error) {
	return s.monitorStatus(ctx, window, false)
}

func (s *ProbeService) PublicMonitorStatus(ctx context.Context, window string) (map[string]any, error) {
	return s.monitorStatus(ctx, window, true)
}

func (s *ProbeService) monitorStatus(ctx context.Context, window string, publicOnly bool) (map[string]any, error) {
	since, label, _ := windowSince(window)
	cards, err := s.enrichedCards(ctx, since, 0)
	if err != nil {
		return nil, err
	}
	if publicOnly {
		public := publicCards(cards)
		total, ok, failed, latency, samples := statusSummary(public)
		return map[string]any{
			"window": label, "rows": public, "requests": total, "success": ok, "failed": failed,
			"success_rate": percent(ok, total), "avg_latency": avg(latency, samples),
		}, nil
	}
	total, ok, failed, latency, samples := 0, 0, 0, 0, 0
	for _, c := range cards {
		if c.ProbeMuted {
			continue
		}
		for _, p := range c.History {
			total++
			if p.Success {
				ok++
			} else {
				failed++
			}
			if p.LatencyMS > 0 {
				latency += p.LatencyMS
				samples++
			}
		}
	}
	return map[string]any{
		"window": label, "rows": domain.PublicModelCards(cards), "requests": total, "success": ok, "failed": failed,
		"success_rate": percent(ok, total), "avg_latency": avg(latency, samples),
	}, nil
}

func statusSummary(cards []domain.PublicModelCard) (total, ok, failed, latency, samples int) {
	for _, c := range cards {
		if c.ProbeMuted {
			continue
		}
		for _, p := range c.History {
			total++
			if p.Success {
				ok++
			} else {
				failed++
			}
			if p.LatencyMS > 0 {
				latency += p.LatencyMS
				samples++
			}
		}
	}
	return total, ok, failed, latency, samples
}

func publicCards(cards []domain.ModelCard) []domain.PublicModelCard {
	out := []domain.PublicModelCard{}
	for _, c := range cards {
		if !c.PublicEnabled {
			continue
		}
		card := domain.PublicModelCard{Name: c.Name, DisplayGroup: c.DisplayGroup, ProbeMuted: c.ProbeMuted}
		for i := range c.History {
			p := c.History[i]
			p.Status = probeStatusLabel(p.Status)
			p.Error = publicProbeError(p)
			card.History = append(card.History, domain.PublicProbeRun{
				CheckedAt: p.CheckedAt, Status: p.Status, Input: p.Input,
				Output: p.Output, HTTPStatus: p.HTTPStatus, LatencyMS: p.LatencyMS, Success: p.Success, Error: p.Error,
			})
		}
		if !card.ProbeMuted {
			card.LastError = publicLastError(card)
		}
		out = append(out, card)
	}
	return out
}

func publicLastError(c domain.PublicModelCard) string {
	if len(c.History) == 0 || c.History[len(c.History)-1].Success {
		return ""
	}
	return c.History[len(c.History)-1].Error
}

func publicProbeError(p domain.ProbeRun) string {
	switch p.Status {
	case monitor.StatusError, "探测异常":
		return "探测异常"
	case monitor.StatusFailed, "请求失败":
		if p.HTTPStatus > 0 {
			return fmt.Sprintf("HTTP %d", p.HTTPStatus)
		}
		return "请求失败"
	default:
		return ""
	}
}

func probeStatusLabel(status string) string {
	switch status {
	case monitor.StatusOperational:
		return "正常"
	case monitor.StatusDegraded:
		return "延迟偏高"
	case monitor.StatusFailed:
		return "请求失败"
	case monitor.StatusError:
		return "探测异常"
	default:
		return status
	}
}

func (s *ProbeService) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	cards, err := s.enrichedCards(ctx, time.Now().Add(-time.Hour), 60)
	if err != nil {
		return nil, err
	}
	return domain.PublicModelCards(cards), nil
}

func (s *ProbeService) enrichedCards(ctx context.Context, since time.Time, probeLimit int) ([]domain.ModelCard, error) {
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	muteAt := s.app.probeMuteFailureThreshold(ctx)
	for i := range cards {
		var u domain.Upstream
		if cards[i].UpstreamID != "" {
			var err error
			u, err = s.app.Store.Upstream(ctx, cards[i].UpstreamID)
			if err == nil {
				cards[i].UpstreamName = u.Name
				cards[i].Type = u.Type
			}
		}
		if cards[i].KeyID != "" {
			if k, err := s.app.Store.Key(ctx, cards[i].KeyID); err == nil {
				cards[i].KeyName = k.Name
				cards[i].KeyGroup = k.Group
				cards[i].KeyRatio = k.GroupRatio
				cards[i].EffectiveRatio = domain.EffectiveRatio(k.GroupRatio, domain.BalanceRate(u))
			}
		} else if cards[i].BaseURL != "" && cards[i].ManualCostRatio != "" {
			cards[i].EffectiveRatio = cards[i].ManualCostRatio
		}
		history, err := s.app.Store.ProbesForCardSince(ctx, cards[i].ID, since, probeLimit)
		if err != nil {
			return nil, err
		}
		reverse(history)
		cards[i].History = history
		cards[i].ProbeMuted = domain.ProbeMuted(cards[i].FailureCount, muteAt, cards[i].SchedulerAutoDisabled)
	}
	return cards, nil
}
