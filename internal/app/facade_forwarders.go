package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/onebot"
)

// 公开 API 转发器：保持 httpapi 与既有调用方仍挂在 *Service 上。

func (s *Service) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	return s.Scheduler.SchedulerConfig(ctx)
}

func (s *Service) SaveSchedulerConfig(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerConfig, error) {
	return s.Scheduler.SaveSchedulerConfig(ctx, cfg)
}

func (s *Service) SeedSchedulerSnapshots(ctx context.Context) error {
	return s.Scheduler.SeedSchedulerSnapshots(ctx)
}

func (s *Service) SchedulerChannels(ctx context.Context, keyword string) ([]domain.SchedulerChannel, error) {
	return s.Scheduler.SchedulerChannels(ctx, keyword)
}

func (s *Service) SchedulerGroups(ctx context.Context) ([]domain.SchedulerGroup, error) {
	return s.Scheduler.SchedulerGroups(ctx)
}

func (s *Service) SchedulerLogs(ctx context.Context, limit int) ([]domain.SchedulerLog, error) {
	return s.Scheduler.SchedulerLogs(ctx, limit)
}

func (s *Service) ApplySchedulerGroups(ctx context.Context) (domain.SchedulerApplyResult, error) {
	return s.Scheduler.ApplySchedulerGroups(ctx)
}

func (s *Service) SetCardSchedulerChannelStatus(ctx context.Context, cardID string, status int) (domain.ModelCard, error) {
	return s.Scheduler.SetCardSchedulerChannelStatus(ctx, cardID, status)
}

func (s *Service) AvailabilityPolicy(ctx context.Context, upstreamID string) (domain.AvailabilityPolicy, error) {
	return s.Scheduler.AvailabilityPolicy(ctx, upstreamID)
}

func (s *Service) SaveAvailabilityPolicy(ctx context.Context, upstreamID string, policy domain.AvailabilityPolicy) (domain.AvailabilityPolicy, error) {
	return s.Scheduler.SaveAvailabilityPolicy(ctx, upstreamID, policy)
}

func (s *Service) AvailabilityRows(ctx context.Context, upstreamID, state string) ([]domain.AvailabilityView, error) {
	return s.Scheduler.AvailabilityRows(ctx, upstreamID, state)
}

func (s *Service) AvailabilityAction(ctx context.Context, cardID, action string, minutes int) (domain.AvailabilityView, error) {
	return s.Scheduler.AvailabilityAction(ctx, cardID, action, minutes)
}

func (s *Service) ReconcileAvailability(ctx context.Context) error {
	return s.Scheduler.ReconcileAvailability(ctx)
}

func (s *Service) Profit(ctx context.Context, window string) (domain.ProfitResponse, error) {
	return s.ProfitSvc.Profit(ctx, window)
}

// cards/check/upstreams 使用的内部钩子 — 仍挂在 Service 上以便薄调用。

func (s *Service) recordCurrentCostSnapshots(ctx context.Context) error {
	return s.Scheduler.recordCurrentCostSnapshots(ctx)
}

func (s *Service) recordCardCostSnapshot(ctx context.Context, card domain.ModelCard) error {
	return s.Scheduler.recordCardCostSnapshot(ctx, card)
}

func (s *Service) recordInactiveCostSnapshot(ctx context.Context, card domain.ModelCard, reason string) error {
	return s.Scheduler.recordInactiveCostSnapshot(ctx, card, reason)
}

func (s *Service) syncSchedulerGroupsBestEffort(ctx context.Context) {
	s.Scheduler.syncSchedulerGroupsBestEffort(ctx)
}

func (s *Service) applySchedulerAutomation(ctx context.Context, card domain.ModelCard, success bool, failures int) error {
	return s.Scheduler.applySchedulerAutomation(ctx, card, success, failures)
}

// 探测 / 卡片公开 API

func (s *Service) SaveCard(ctx context.Context, id string, in domain.ModelCard) (domain.ModelCard, error) {
	return s.Probe.SaveCard(ctx, id, in)
}

func (s *Service) SortCards(ctx context.Context, ids []string) error {
	return s.Probe.SortCards(ctx, ids)
}

func (s *Service) DeleteCard(ctx context.Context, id string) error {
	return s.Probe.DeleteCard(ctx, id)
}

func (s *Service) CheckCard(ctx context.Context, cardID string) error {
	return s.Probe.CheckCard(ctx, cardID)
}

func (s *Service) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	return s.Probe.ListCards(ctx)
}

func (s *Service) MonitorStatus(ctx context.Context, window string) (map[string]any, error) {
	return s.Probe.MonitorStatus(ctx, window)
}

func (s *Service) PublicMonitorStatus(ctx context.Context, window string) (domain.PublicMonitorStatus, error) {
	return s.Probe.PublicMonitorStatus(ctx, window)
}

// OneBot 公开 API

func (s *Service) OneBotStatus(ctx context.Context) (domain.OneBotStatus, error) {
	return s.OneBot.Status(ctx)
}

func (s *Service) AuthorizeOneBotEvent(ctx context.Context, signature string, payload []byte) error {
	return s.OneBot.AuthorizeEvent(ctx, signature, payload)
}

func (s *Service) HandleOneBotEvent(ctx context.Context, event onebot.Event) error {
	return s.OneBot.HandleEvent(ctx, event)
}

func (s *Service) CheckDue(ctx context.Context) error {
	return s.Probe.CheckDue(ctx)
}

func (s *Service) CheckAll(ctx context.Context) error {
	return s.Probe.CheckAll(ctx)
}

func (s *Service) CheckUpstream(ctx context.Context, upstreamID string) error {
	return s.Probe.CheckUpstream(ctx, upstreamID)
}

// CLIProxy 公开 API

func (s *Service) CLIProxyConfig(ctx context.Context) (domain.CLIProxyConfig, error) {
	return s.CLIProxy.CLIProxyConfig(ctx)
}

func (s *Service) SaveCLIProxyConfig(ctx context.Context, cfg domain.CLIProxyConfig) (domain.CLIProxyConfig, error) {
	return s.CLIProxy.SaveCLIProxyConfig(ctx, cfg)
}

func (s *Service) CLIProxyAccounts(ctx context.Context) ([]domain.CLIProxyAuthFile, error) {
	return s.CLIProxy.CLIProxyAccounts(ctx)
}

func (s *Service) UploadCLIProxyAccount(ctx context.Context, name, content string) error {
	return s.CLIProxy.UploadCLIProxyAccount(ctx, name, content)
}

func (s *Service) DownloadCLIProxyAccount(ctx context.Context, name string) ([]byte, string, error) {
	return s.CLIProxy.DownloadCLIProxyAccount(ctx, name)
}

func (s *Service) DeleteCLIProxyAccount(ctx context.Context, name string) error {
	return s.CLIProxy.DeleteCLIProxyAccount(ctx, name)
}

func (s *Service) ResetCLIProxyQuota(ctx context.Context, name string) (domain.CLIProxyResetQuotaResult, error) {
	return s.CLIProxy.ResetCLIProxyQuota(ctx, name)
}

func (s *Service) CLIProxyAccountQuota(ctx context.Context, name, authIndex, accountID, accountType string) (domain.CLIProxyQuota, error) {
	return s.CLIProxy.CLIProxyAccountQuota(ctx, name, authIndex, accountID, accountType)
}

// TG 公开 API

func (s *Service) TGSessionStatus(ctx context.Context) (TGSessionStatus, error) {
	return s.TG.TGSessionStatus(ctx)
}

func (s *Service) StartTGSession(ctx context.Context, apiID int, apiHash, phone string) (TGSessionStatus, error) {
	return s.TG.StartTGSession(ctx, apiID, apiHash, phone)
}

func (s *Service) VerifyTGSession(ctx context.Context, code string) (TGSessionStatus, error) {
	return s.TG.VerifyTGSession(ctx, code)
}

func (s *Service) TGSessionPassword(ctx context.Context, password string) (TGSessionStatus, error) {
	return s.TG.TGSessionPassword(ctx, password)
}

func (s *Service) SaveTGChannel(ctx context.Context, id string, in domain.TGChannel) (domain.TGChannel, error) {
	return s.TG.SaveTGChannel(ctx, id, in)
}

func (s *Service) SyncTGChannels(ctx context.Context) ([]domain.TGChannel, error) {
	return s.TG.SyncTGChannels(ctx)
}

func (s *Service) RefreshTGMessagesDue(ctx context.Context) error {
	return s.TG.RefreshTGMessagesDue(ctx)
}

func (s *Service) RefreshTGMessages(ctx context.Context, channelID string) error {
	return s.TG.RefreshTGMessages(ctx, channelID)
}
