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
	if err := domain.ValidateAxonHubControlMode(cfg.ControlMode); err != nil {
		return domain.AxonHubConfig{}, BadRequest(err)
	}
	old, err := s.app.Store.AxonHubConfig(ctx)
	if err != nil {
		return domain.AxonHubConfig{}, err
	}
	cfg = cfg.MergeUpdate(old)
	parsed, parseErr := url.Parse(cfg.BaseURL)
	if cfg.BaseURL != "" && (parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "") {
		return domain.AxonHubConfig{}, ErrBadRequest("AxonHub Base URL 必须使用 http 或 https")
	}
	out, err := s.app.Store.UpdateAxonHubConfig(ctx, cfg)
	if err != nil {
		return out.Public(), err
	}
	s.resetAxonHubSession()
	scheduler, schedulerErr := s.app.Store.SchedulerConfig(ctx)
	if schedulerErr == nil && scheduler.Provider == domain.SchedulerProviderAxonHub && out.ControlMode == domain.AxonHubControlActive {
		preflight, preflightErr := s.AxonHubPreflight(ctx)
		if preflightErr != nil || !preflight.OK {
			_, _ = s.app.Store.UpdateAxonHubConfig(ctx, old)
			s.resetAxonHubSession()
			if preflightErr != nil {
				return old.Public(), preflightErr
			}
			return old.Public(), ErrBadRequest("AxonHub 预检未通过")
		}
	}
	return out.Public(), nil
}

func (s *SchedulerService) axonHubBackend(ctx context.Context) (SchedulerBackend, domain.AxonHubConfig, error) {
	cfg, err := s.app.Store.AxonHubConfig(ctx)
	if err != nil {
		return nil, cfg, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.AdminEmail) == "" || strings.TrimSpace(cfg.AdminPassword) == "" {
		return nil, cfg, errAxonHubNotConfigured
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
	return out, nil
}

func (s *SchedulerService) AxonHubPreflight(ctx context.Context) (domain.AxonHubPreflight, error) {
	out := domain.AxonHubPreflight{OK: true}
	add := func(name string, ok bool, message string) {
		out.Checks = append(out.Checks, domain.AxonHubPreflightCheck{Name: name, OK: ok, Message: message})
		out.OK = out.OK && ok
	}
	backend, _, err := s.axonHubBackend(ctx)
	if err != nil {
		add("连接", false, err.Error())
		return out, nil
	}
	channels, err := backend.Channels(ctx)
	if err != nil {
		add("连接", false, err.Error())
		return out, nil
	}
	add("连接", true, fmt.Sprintf("已读取 %d 个渠道", len(channels)))
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return out, err
	}
	byID := map[string]domain.SchedulerChannel{}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	bound, duplicates, archived, unsupported, duplicateTags := 0, []string{}, []string{}, []string{}, []string{}
	seen := map[string]string{}
	for _, card := range cards {
		if !card.Enabled || !card.PoolEnabled || strings.TrimSpace(card.AxonHubChannelID) == "" {
			continue
		}
		bound++
		if previous := seen[card.AxonHubChannelID]; previous != "" {
			duplicates = append(duplicates, previous+" / "+card.Name)
		} else {
			seen[card.AxonHubChannelID] = card.Name
		}
		channel, found := byID[card.AxonHubChannelID]
		if !found || channel.Archived {
			archived = append(archived, card.Name)
			continue
		}
		if !domain.AxonHubSupportsGPTModel(channel.Models, card.Model) {
			unsupported = append(unsupported, card.Name)
		}
		managed := 0
		for _, tag := range channel.Tags {
			if domain.IsAxonHubManagedTag(tag) {
				managed++
			}
		}
		if managed > 1 {
			duplicateTags = append(duplicateTags, channel.Name)
		}
	}
	out.Bound = bound
	add("绑定数量", bound == 10, fmt.Sprintf("已绑定 %d / 10 个渠道", bound))
	add("绑定唯一", len(duplicates) == 0, listMessage(duplicates, "无重复绑定"))
	add("渠道状态", len(archived) == 0, listMessage(archived, "绑定渠道均未归档"))
	add("模型能力", len(unsupported) == 0, listMessage(unsupported, "绑定渠道均支持卡片 GPT 模型"))
	add("托管标签", len(duplicateTags) == 0, listMessage(duplicateTags, "每个渠道最多一个托管标签"))
	add("档位隔离", domain.ValidateNonOverlappingSchedulerTiers(domain.DefaultAxonHubTiers()) == nil, "payg_low 与 payg_stable 无重叠")
	return out, nil
}

func listMessage(items []string, fallback string) string {
	if len(items) == 0 {
		return fallback
	}
	return strings.Join(items, "、")
}

func (s *SchedulerService) SwitchProvider(ctx context.Context, provider, controlMode string) (domain.SchedulerConfig, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != domain.SchedulerProviderGGAPI && provider != domain.SchedulerProviderAxonHub {
		return domain.SchedulerConfig{}, ErrBadRequest("调度器 provider 必须是 ggapi 或 axonhub")
	}
	if provider == domain.SchedulerProviderAxonHub {
		if err := domain.ValidateAxonHubControlMode(controlMode); err != nil {
			return domain.SchedulerConfig{}, BadRequest(err)
		}
		if domain.NormalizeAxonHubControlMode(controlMode) == domain.AxonHubControlActive {
			preflight, err := s.AxonHubPreflight(ctx)
			if err != nil {
				return domain.SchedulerConfig{}, err
			}
			if !preflight.OK {
				return domain.SchedulerConfig{}, ErrBadRequest("AxonHub 预检未通过")
			}
		}
		cfg, err := s.app.Store.AxonHubConfig(ctx)
		if err != nil {
			return domain.SchedulerConfig{}, err
		}
		cfg.ControlMode = controlMode
		if _, err := s.app.Store.UpdateAxonHubConfig(ctx, cfg); err != nil {
			return domain.SchedulerConfig{}, err
		}
	}
	if err := s.app.Store.UpdateSchedulerProvider(ctx, provider); err != nil {
		return domain.SchedulerConfig{}, err
	}
	_ = s.recordCurrentCostSnapshots(ctx)
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	return cfg.Public(), err
}

func (s *SchedulerService) AdoptAxonHubChannel(ctx context.Context, channelID string) (domain.AxonHubChannelLifecycle, error) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	backend, _, err := s.axonHubBackend(ctx)
	if err != nil {
		return domain.AxonHubChannelLifecycle{}, err
	}
	channels, err := backend.Channels(ctx)
	if err != nil {
		return domain.AxonHubChannelLifecycle{}, err
	}
	channel, found := channelByID(channels, channelID)
	if !found || channel.Archived {
		return domain.AxonHubChannelLifecycle{}, ErrBadRequest("AxonHub 中找不到可接管渠道")
	}
	row, _, err := s.app.Store.AxonHubChannelLifecycle(ctx, channelID)
	if err != nil {
		return row, err
	}
	now := time.Now().UTC()
	row.ChannelID, row.ChannelName = channel.ID, channel.Name
	row.RemoteStatus, row.RemoteTags, row.RemoteManagedTag, row.RemoteWeight = channel.RemoteStatus, channel.Tags, domain.AxonHubManagedTag(channel.Tags), channel.OrderingWeight
	row.Owner, row.ExternalTakeover, row.AUMDisabled = domain.ControlOwnerAUM, false, false
	row.LastAUMStatus, row.LastAUMTag, row.LastAUMWeight = channel.RemoteStatus, row.RemoteManagedTag, channel.OrderingWeight
	row.LastAUMWriteAt, row.LastSource, row.LastReason, row.UpdatedAt = now, domain.ControlSourceManual, "在 AUM 重新接管", now
	if err := s.app.Store.SaveAxonHubChannelLifecycle(ctx, row); err != nil {
		return row, err
	}
	s.logAxonHub(ctx, row, "control_plane", "success", "adopt", row.LastReason)
	return row, nil
}

func (s *SchedulerService) AdoptBoundAxonHubChannels(ctx context.Context) (int, error) {
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return 0, err
	}
	seen, adopted := map[string]bool{}, 0
	for _, card := range cards {
		id := strings.TrimSpace(card.AxonHubChannelID)
		if !card.Enabled || !card.PoolEnabled || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := s.AdoptAxonHubChannel(ctx, id); err != nil {
			continue
		}
		adopted++
	}
	return adopted, nil
}

func channelByID(channels []domain.SchedulerChannel, id string) (domain.SchedulerChannel, bool) {
	for _, channel := range channels {
		if channel.ID == id {
			return channel, true
		}
	}
	return domain.SchedulerChannel{}, false
}

func (s *SchedulerService) ReconcileAxonHub(ctx context.Context) error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	backend, cfg, err := s.axonHubBackend(ctx)
	if err != nil {
		return err
	}
	channels, err := backend.Channels(ctx)
	if err != nil {
		return err
	}
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	byID := map[string]domain.SchedulerChannel{}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	targets := map[string]domain.AxonHubCostTarget{}
	costKnown := map[string]bool{}
	cardsByChannel := map[string]domain.ModelCard{}
	for _, card := range cards {
		id := strings.TrimSpace(card.AxonHubChannelID)
		channel, found := byID[id]
		if !card.Enabled || !card.PoolEnabled || id == "" || !found || channel.Archived || !domain.AxonHubSupportsGPTModel(channel.Models, card.Model) {
			continue
		}
		if _, duplicate := cardsByChannel[id]; duplicate {
			continue
		}
		cardsByChannel[id] = card
		cost, ok := s.cardPrice(ctx, card)
		if !ok {
			continue
		}
		costKnown[id] = true
		tier, matched := domain.AxonHubTierForCost(cost)
		target := domain.AxonHubCostTarget{Cost: cost}
		if matched {
			target.Tag = tier.Tag
		}
		targets[id] = target
	}
	weights := domain.AxonHubOrderingWeights(targets)
	now := time.Now().UTC()
	for channelID, card := range cardsByChannel {
		channel := byID[channelID]
		target := targets[channelID]
		weight := weights[channelID]
		if !costKnown[channelID] || target.Tag == "" {
			weight = channel.OrderingWeight
		}
		row, found, rowErr := s.app.Store.AxonHubChannelLifecycle(ctx, channelID)
		if rowErr != nil {
			continue
		}
		managedTag := domain.AxonHubManagedTag(channel.Tags)
		if !found {
			row = domain.AxonHubChannelLifecycle{
				ChannelID: channel.ID, ChannelName: channel.Name, RemoteStatus: channel.RemoteStatus, RemoteTags: channel.Tags,
				RemoteManagedTag: managedTag, RemoteWeight: channel.OrderingWeight, DesiredTag: target.Tag, DesiredWeight: weight,
				Owner: domain.ControlOwnerObserved, LastReason: "首次观察基线，等待管理员接管", UpdatedAt: now,
			}
			_ = s.app.Store.SaveAxonHubChannelLifecycle(ctx, row)
			continue
		}
		drift := channel.RemoteStatus != row.RemoteStatus || managedTag != row.RemoteManagedTag || channel.OrderingWeight != row.RemoteWeight
		if drift && !row.ExternalTakeover {
			row.Owner, row.ExternalTakeover, row.AUMDisabled = domain.ControlOwnerExternal, true, false
			row.LastSource, row.LastReason = domain.ControlOwnerExternal, "AxonHub 状态、托管标签或权重发生未经 AUM 确认的变化"
			s.logAxonHub(ctx, row, "control_plane", "skipped", "external_takeover", row.LastReason)
		}
		row.ChannelName, row.RemoteStatus, row.RemoteTags = channel.Name, channel.RemoteStatus, channel.Tags
		row.RemoteManagedTag, row.RemoteWeight, row.DesiredTag, row.DesiredWeight = managedTag, channel.OrderingWeight, target.Tag, weight
		row.UpdatedAt = now
		if cfg.ControlMode != domain.AxonHubControlActive || row.Owner != domain.ControlOwnerAUM || row.ExternalTakeover || channel.Archived {
			_ = s.app.Store.SaveAxonHubChannelLifecycle(ctx, row)
			continue
		}
		if !row.RetryAt.IsZero() && now.Before(row.RetryAt) {
			_ = s.app.Store.SaveAxonHubChannelLifecycle(ctx, row)
			continue
		}
		targetTags := channel.Tags
		if costKnown[channelID] {
			targetTags = domain.AxonHubTargetTags(channel.Tags, target.Tag)
		}
		if costKnown[channelID] && (!domain.SameGroups(channel.Tags, targetTags) || channel.OrderingWeight != weight) {
			actual, writeErr := backend.UpdateFields(ctx, channel, targetTags, weight)
			if writeErr == nil {
				actual, writeErr = s.verifyAxonHubChannel(ctx, backend, channel.ID, "fields", targetTags, weight, "")
			}
			if writeErr != nil {
				s.failAxonHubAction(ctx, &row, "fields", target.Tag, weight, "", writeErr, now)
				continue
			}
			channel = actual
			row.RemoteTags, row.RemoteManagedTag, row.RemoteWeight = actual.Tags, domain.AxonHubManagedTag(actual.Tags), actual.OrderingWeight
			row.LastAUMTag, row.LastAUMWeight, row.LastAUMWriteAt = row.RemoteManagedTag, actual.OrderingWeight, now
			row.LastSource, row.LastReason = domain.ControlSourceCost, "同步成本档位与池内权重"
			s.logAxonHub(ctx, row, "group_sync", "success", "", row.LastReason)
		}
		desiredStatus, reason := s.axonHubBalanceStatus(ctx, card, row, channel, now)
		if desiredStatus != "" && desiredStatus != channel.RemoteStatus {
			actual, writeErr := backend.UpdateStatus(ctx, channel, desiredStatus)
			if writeErr == nil {
				actual, writeErr = s.verifyAxonHubChannel(ctx, backend, channel.ID, "status", nil, 0, desiredStatus)
			}
			if writeErr != nil {
				s.failAxonHubAction(ctx, &row, "status", row.DesiredTag, row.DesiredWeight, desiredStatus, writeErr, now)
				continue
			}
			row.RemoteStatus, row.LastAUMStatus, row.LastAUMWriteAt = actual.RemoteStatus, actual.RemoteStatus, now
			row.LastSource, row.LastReason = domain.ControlSourceBalance, reason
			row.AUMDisabled = desiredStatus == domain.AxonHubStatusDisabled
			if row.AUMDisabled {
				row.AUMDisabledAt = now
			} else {
				row.AUMDisabledAt = time.Time{}
			}
			s.logAxonHub(ctx, row, map[bool]string{true: "disable", false: "restore"}[row.AUMDisabled], "success", "balance_guard", reason)
		}
		row.PendingAction, row.PendingStatus, row.PendingTag, row.PendingWeight = "", "", "", 0
		row.RetryAt, row.RetryCount, row.LastError, row.UpdatedAt = time.Time{}, 0, "", now
		_ = s.app.Store.SaveAxonHubChannelLifecycle(ctx, row)
	}
	return nil
}

func (s *SchedulerService) verifyAxonHubChannel(ctx context.Context, backend SchedulerBackend, channelID, kind string, tags []string, weight int, status string) (domain.SchedulerChannel, error) {
	channels, err := backend.Channels(ctx)
	if err != nil {
		return domain.SchedulerChannel{}, err
	}
	actual, found := channelByID(channels, channelID)
	if !found {
		return actual, errors.New("AxonHub 写入后找不到渠道")
	}
	if kind == "fields" && (!domain.SameGroups(actual.Tags, tags) || actual.OrderingWeight != weight) {
		return actual, errors.New("AxonHub 标签或权重回读不一致")
	}
	if kind == "status" && actual.RemoteStatus != status {
		return actual, errors.New("AxonHub 状态回读不一致")
	}
	return actual, nil
}

func (s *SchedulerService) axonHubBalanceStatus(ctx context.Context, card domain.ModelCard, row domain.AxonHubChannelLifecycle, channel domain.SchedulerChannel, now time.Time) (string, string) {
	if card.UpstreamID == "" {
		return "", ""
	}
	upstream, err := s.app.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		return "", ""
	}
	policy, err := s.app.Store.AvailabilityPolicy(ctx, upstream.ID)
	if err != nil || !policy.BalanceGuardActive() || !policy.BalanceGuardConfigured() {
		return "", ""
	}
	_, fresh, remain, _ := s.availabilityBalance(ctx, upstream, policy)
	if fresh && remain <= policy.BalanceCloseThreshold && channel.RemoteStatus == domain.AxonHubStatusEnabled {
		return domain.AxonHubStatusDisabled, fmt.Sprintf("新鲜余额 %.4f 达到关闭线 %.4f", remain, policy.BalanceCloseThreshold)
	}
	if row.AUMDisabled && channel.RemoteStatus == domain.AxonHubStatusDisabled && !row.AUMDisabledAt.IsZero() && now.Sub(row.AUMDisabledAt) >= domain.AvailabilityRecoveryMinDuration && fresh && remain >= policy.BalanceRecoverThreshold {
		return domain.AxonHubStatusEnabled, fmt.Sprintf("AUM 关闭已满 15 分钟且新鲜余额 %.4f 达到恢复线 %.4f", remain, policy.BalanceRecoverThreshold)
	}
	return "", ""
}

func (s *SchedulerService) failAxonHubAction(ctx context.Context, row *domain.AxonHubChannelLifecycle, action, tag string, weight int, status string, err error, now time.Time) {
	row.PendingAction, row.PendingTag, row.PendingWeight, row.PendingStatus = action, tag, weight, status
	row.RetryCount++
	row.RetryAt = controlPlaneRetryAt(now, row.RetryCount)
	row.LastError = axonHubSafeError(err)
	row.UpdatedAt = now
	_ = s.app.Store.SaveAxonHubChannelLifecycle(ctx, *row)
	s.logAxonHub(ctx, *row, action, "error", "retry", row.LastError)
}

func axonHubSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(message, "HTTP 401") || strings.Contains(message, "HTTP 403") {
		return "AxonHub 控制密钥无权限"
	}
	if strings.Contains(message, "HTTP 429") {
		return "AxonHub 请求被限流"
	}
	if strings.Contains(message, "超时") {
		return "AxonHub 请求超时"
	}
	if strings.Contains(message, "回读不一致") {
		return message
	}
	return "AxonHub 协调失败"
}

func (s *SchedulerService) logAxonHub(ctx context.Context, row domain.AxonHubChannelLifecycle, action, status, reason, message string) {
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
		Provider: domain.SchedulerProviderAxonHub, ChannelID: row.ChannelID, ChannelName: row.ChannelName,
		Action: action, Status: status, Reason: reason, Message: message,
	})
	if status == "error" || reason == "external_takeover" {
		severity := "warning"
		if status == "error" {
			severity = "error"
		}
		_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{
			Type: "axonhub_control_plane", Severity: severity, Title: "AxonHub 控制面异常", Message: domain.FirstNonEmpty(row.ChannelName, row.ChannelID) + " " + message,
			TargetType: "scheduler_channel", TargetID: row.ChannelID, Actions: []string{"scheduler_control_plane"},
		})
	}
}

func (s *SchedulerService) AxonHubControlPlane(ctx context.Context) (domain.AxonHubControlPlane, error) {
	if err := s.ReconcileAxonHub(ctx); err != nil && !errors.Is(err, errAxonHubNotConfigured) {
		return domain.AxonHubControlPlane{}, err
	}
	cfg, err := s.app.Store.AxonHubConfig(ctx)
	if err != nil {
		return domain.AxonHubControlPlane{}, err
	}
	backend, _, err := s.axonHubBackend(ctx)
	if err != nil {
		return domain.AxonHubControlPlane{}, err
	}
	channels, err := backend.Channels(ctx)
	if err != nil {
		return domain.AxonHubControlPlane{}, err
	}
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return domain.AxonHubControlPlane{}, err
	}
	byID := map[string]domain.SchedulerChannel{}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	out := domain.AxonHubControlPlane{Provider: domain.SchedulerProviderAxonHub, Mode: cfg.ControlMode, Channels: []domain.AxonHubControlPlaneChannel{}}
	for _, card := range cards {
		if !card.Enabled || !card.PoolEnabled || card.AxonHubChannelID == "" {
			continue
		}
		channel := byID[card.AxonHubChannelID]
		life, _, err := s.app.Store.AxonHubChannelLifecycle(ctx, card.AxonHubChannelID)
		if err != nil {
			return out, err
		}
		row := domain.AxonHubControlPlaneChannel{AxonHubChannelLifecycle: life, CardID: card.ID, CardName: card.Name, Model: card.Model, Models: channel.Models, Archived: channel.Archived, ModelSupported: domain.AxonHubSupportsGPTModel(channel.Models, card.Model)}
		row.Cost, row.CostAvailable = s.cardPrice(ctx, card)
		row.TargetTags = domain.AxonHubTargetTags(channel.Tags, row.DesiredTag)
		if card.UpstreamID != "" {
			if upstream, upstreamErr := s.app.Store.Upstream(ctx, card.UpstreamID); upstreamErr == nil {
				if policy, policyErr := s.app.Store.AvailabilityPolicy(ctx, upstream.ID); policyErr == nil {
					_, row.BalanceFresh, row.BalanceRemain, _ = s.availabilityBalance(ctx, upstream, policy)
				}
			}
		}
		out.Channels = append(out.Channels, row)
	}
	sort.Slice(out.Channels, func(i, j int) bool { return out.Channels[i].ChannelName < out.Channels[j].ChannelName })
	out.Logs, err = s.app.Store.SchedulerLogsForProvider(ctx, domain.SchedulerProviderAxonHub, 50)
	return out, err
}
