package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"ai-upstream-monitor/internal/axonhub"
	"ai-upstream-monitor/internal/domain"
)

var errAxonHubNotConfigured = errors.New("AxonHub 未配置")

func (s *SchedulerService) AxonHubConfig(ctx context.Context) (domain.AxonHubConfig, error) {
	cfg, err := s.app.Store.AxonHubConfig(ctx)
	return cfg.Public(), err
}

func (s *SchedulerService) SaveAxonHubConfig(ctx context.Context, cfg domain.AxonHubConfig) (domain.AxonHubConfig, error) {
	old, err := s.app.Store.AxonHubConfig(ctx)
	if err != nil {
		return domain.AxonHubConfig{}, err
	}
	cfg = cfg.MergeUpdate(old)
	parsed, parseErr := url.Parse(cfg.BaseURL)
	if cfg.BaseURL != "" && (parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "") {
		return domain.AxonHubConfig{}, ErrBadRequest("AxonHub Base URL 必须使用 http 或 https")
	}
	// 退役健康控制后，AxonHub 仅保留成本字段同步；旧控制模式统一收敛为 active/off。
	if strings.EqualFold(strings.TrimSpace(cfg.ControlMode), domain.AxonHubControlOff) {
		cfg.ControlMode = domain.AxonHubControlOff
	} else {
		cfg.ControlMode = domain.AxonHubControlActive
	}
	out, err := s.app.Store.UpdateAxonHubConfig(ctx, cfg)
	s.resetAxonHubSession()
	return out.Public(), err
}

func (s *SchedulerService) axonHubBackend(ctx context.Context) (axonHubBackend, domain.AxonHubConfig, error) {
	cfg, err := s.app.Store.AxonHubConfig(ctx)
	if err != nil {
		return axonHubBackend{}, cfg, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.AdminEmail) == "" || strings.TrimSpace(cfg.AdminPassword) == "" {
		return axonHubBackend{}, cfg, errAxonHubNotConfigured
	}
	return axonHubBackend{service: s, cfg: cfg}, cfg, nil
}

func (s *SchedulerService) axonHubClient(ctx context.Context, cfg domain.AxonHubConfig) (axonhub.Client, error) {
	s.axonHubAuthMu.Lock()
	defer s.axonHubAuthMu.Unlock()
	now := time.Now().UTC()
	if s.axonHubToken != "" && s.axonHubTokenBaseURL == cfg.BaseURL && s.axonHubTokenAdminEmail == cfg.AdminEmail && s.axonHubTokenExpiresAt.After(now.Add(time.Minute)) {
		return axonhub.Client{BaseURL: cfg.BaseURL, Token: s.axonHubToken, HTTP: s.app.Client.HTTP}, nil
	}
	client := axonhub.Client{BaseURL: cfg.BaseURL, HTTP: s.app.Client.HTTP}
	token, expiresAt, err := client.SignIn(ctx, cfg.AdminEmail, cfg.AdminPassword)
	if err != nil {
		return axonhub.Client{}, err
	}
	s.axonHubToken, s.axonHubTokenExpiresAt = token, expiresAt
	s.axonHubTokenBaseURL, s.axonHubTokenAdminEmail = cfg.BaseURL, cfg.AdminEmail
	return axonhub.Client{BaseURL: cfg.BaseURL, Token: token, HTTP: s.app.Client.HTTP}, nil
}

func (s *SchedulerService) resetAxonHubSession() {
	s.axonHubAuthMu.Lock()
	defer s.axonHubAuthMu.Unlock()
	s.axonHubToken, s.axonHubTokenBaseURL, s.axonHubTokenAdminEmail = "", "", ""
	s.axonHubTokenExpiresAt = time.Time{}
}

func (s *SchedulerService) TestAxonHub(ctx context.Context) error {
	backend, _, err := s.axonHubBackend(ctx)
	if err != nil {
		return err
	}
	_, err = backend.Channels(ctx)
	return err
}

func (s *SchedulerService) AxonHubChannels(ctx context.Context, keyword string) ([]domain.SchedulerChannel, error) {
	backend, _, err := s.axonHubBackend(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := backend.Channels(ctx)
	if err != nil {
		return nil, err
	}
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	out := make([]domain.SchedulerChannel, 0, len(rows))
	for _, row := range rows {
		if row.Archived || (keyword != "" && !strings.Contains(strings.ToLower(row.Name+" "+row.ID), keyword)) {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *SchedulerService) AxonHubPreflight(ctx context.Context) (domain.AxonHubPreflight, error) {
	out := domain.AxonHubPreflight{OK: true}
	backend, _, err := s.axonHubBackend(ctx)
	if err != nil {
		out.OK = false
		out.Checks = append(out.Checks, domain.AxonHubPreflightCheck{Name: "连接", Message: err.Error()})
		return out, nil
	}
	channels, err := backend.Channels(ctx)
	if err != nil {
		out.OK = false
		out.Checks = append(out.Checks, domain.AxonHubPreflightCheck{Name: "连接", Message: err.Error()})
		return out, nil
	}
	bindings, err := s.app.Store.ListCostBindings(ctx)
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		if binding.Enabled && binding.AxonHubChannelID != "" {
			out.Bound++
			if seen[binding.AxonHubChannelID] {
				out.OK = false
			}
			seen[binding.AxonHubChannelID] = true
		}
	}
	out.Checks = append(out.Checks,
		domain.AxonHubPreflightCheck{Name: "连接", OK: true, Message: fmt.Sprintf("已读取 %d 个渠道", len(channels))},
		domain.AxonHubPreflightCheck{Name: "绑定唯一", OK: out.OK, Message: fmt.Sprintf("已绑定 %d 个成本渠道", out.Bound)},
	)
	return out, nil
}

func (s *SchedulerService) SwitchProvider(ctx context.Context, provider, _ string) (domain.SchedulerConfig, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != domain.SchedulerProviderGGAPI && provider != domain.SchedulerProviderAxonHub {
		return domain.SchedulerConfig{}, ErrBadRequest("调度器 provider 必须是 ggapi 或 axonhub")
	}
	if provider == domain.SchedulerProviderAxonHub {
		if err := s.TestAxonHub(ctx); err != nil {
			return domain.SchedulerConfig{}, err
		}
	}
	if err := s.app.Store.UpdateSchedulerProvider(ctx, provider); err != nil {
		return domain.SchedulerConfig{}, err
	}
	if err := s.recordCurrentCostSnapshots(ctx); err != nil {
		return domain.SchedulerConfig{}, err
	}
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	return cfg.Public(), err
}

func (s *SchedulerService) applyAxonHubCosts(ctx context.Context) (domain.SchedulerApplyResult, error) {
	var out domain.SchedulerApplyResult
	backend, cfg, err := s.axonHubBackend(ctx)
	if err != nil {
		return out, err
	}
	if cfg.ControlMode == domain.AxonHubControlOff {
		return out, nil
	}
	bindings, err := s.app.Store.ListCostBindings(ctx)
	if err != nil {
		return out, err
	}
	channels, err := backend.Channels(ctx)
	if err != nil {
		return out, err
	}
	byID := map[string]domain.SchedulerChannel{}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	targets := map[string]domain.AxonHubCostTarget{}
	for _, binding := range bindings {
		binding = costBindingProjection(binding)
		if !binding.Enabled || !binding.CostAvailable || binding.AxonHubChannelID == "" {
			continue
		}
		if _, ok := byID[binding.AxonHubChannelID]; !ok {
			continue
		}
		if tier, ok := domain.AxonHubTierForCost(binding.EffectiveCost); ok {
			targets[binding.AxonHubChannelID] = domain.AxonHubCostTarget{Cost: binding.EffectiveCost, Tag: tier.Tag}
		}
	}
	weights := domain.AxonHubOrderingWeights(targets)
	for _, binding := range bindings {
		binding = costBindingProjection(binding)
		channel, found := byID[binding.AxonHubChannelID]
		target, hasTarget := targets[binding.AxonHubChannelID]
		weight, hasWeight := weights[binding.AxonHubChannelID]
		if !binding.Enabled || !binding.CostAvailable || binding.AxonHubChannelID == "" || !found || channel.Archived || !hasTarget || !hasWeight {
			out.Skipped++
			continue
		}
		targetTags := domain.AxonHubTargetTags(channel.Tags, target.Tag)
		ownership, exists, err := s.app.Store.CostFieldOwnership(ctx, domain.SchedulerProviderAxonHub, channel.ID)
		if err != nil {
			return out, err
		}
		managedTag := domain.AxonHubManagedTag(channel.Tags)
		if exists && ownership.Managed && !ownership.ExternalTakeover && (managedTag != domain.AxonHubManagedTag(ownership.RemoteGroups) || channel.OrderingWeight != ownership.RemoteWeight) {
			ownership.ExternalTakeover, ownership.Managed = true, false
			ownership.LastReason, ownership.UpdatedAt = "AxonHub 托管标签或权重发生外部修改", time.Now().UTC()
			_ = s.app.Store.SaveCostFieldOwnership(ctx, ownership)
		}
		if exists && ownership.ExternalTakeover {
			out.Skipped++
			continue
		}
		if domain.SameGroups(channel.Tags, targetTags) && channel.OrderingWeight == weight {
			out.Unchanged++
			_ = s.app.Store.SaveCostFieldOwnership(ctx, domain.CostFieldOwnership{Provider: domain.SchedulerProviderAxonHub, ChannelID: channel.ID, ChannelName: channel.Name, RemoteGroups: channel.Tags, RemoteWeight: channel.OrderingWeight, Managed: true, UpdatedAt: time.Now().UTC()})
			continue
		}
		actual, err := backend.UpdateFields(ctx, channel, targetTags, weight)
		if err != nil {
			return out, err
		}
		if actual.RemoteStatus != channel.RemoteStatus || !domain.SameGroups(actual.Tags, targetTags) || actual.OrderingWeight != weight {
			return out, errors.New("AxonHub 成本字段写入校验失败")
		}
		_ = s.app.Store.SaveCostFieldOwnership(ctx, domain.CostFieldOwnership{Provider: domain.SchedulerProviderAxonHub, ChannelID: actual.ID, ChannelName: actual.Name, RemoteGroups: actual.Tags, RemoteWeight: actual.OrderingWeight, Managed: true, UpdatedAt: time.Now().UTC()})
		out.Updated++
	}
	s.logCostSync(ctx, domain.SchedulerProviderAxonHub, out)
	return out, nil
}

func channelByID(channels []domain.SchedulerChannel, id string) (domain.SchedulerChannel, bool) {
	for _, channel := range channels {
		if channel.ID == id {
			return channel, true
		}
	}
	return domain.SchedulerChannel{}, false
}
