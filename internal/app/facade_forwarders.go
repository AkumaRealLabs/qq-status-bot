package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
)

// 公开 API 转发器：保持 httpapi 与既有调用方仍挂在 *Service 上。

func (s *Service) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	return s.Scheduler.SchedulerConfig(ctx)
}

func (s *Service) SaveSchedulerConfig(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerConfig, error) {
	return s.Scheduler.SaveSchedulerConfig(ctx, cfg)
}

func (s *Service) AxonHubConfig(ctx context.Context) (domain.AxonHubConfig, error) {
	return s.Scheduler.AxonHubConfig(ctx)
}

func (s *Service) SaveAxonHubConfig(ctx context.Context, cfg domain.AxonHubConfig) (domain.AxonHubConfig, error) {
	return s.Scheduler.SaveAxonHubConfig(ctx, cfg)
}

func (s *Service) TestAxonHub(ctx context.Context) error {
	return s.Scheduler.TestAxonHub(ctx)
}

func (s *Service) AxonHubPreflight(ctx context.Context) (domain.AxonHubPreflight, error) {
	return s.Scheduler.AxonHubPreflight(ctx)
}

func (s *Service) SwitchSchedulerProvider(ctx context.Context, provider, controlMode string) (domain.SchedulerConfig, error) {
	return s.Scheduler.SwitchProvider(ctx, provider, controlMode)
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

func (s *Service) SchedulerChannelsForProvider(ctx context.Context, provider, keyword string) ([]domain.SchedulerChannel, error) {
	return s.Scheduler.SchedulerChannelsForProvider(ctx, provider, keyword)
}

func (s *Service) CostBindings(ctx context.Context) ([]domain.SchedulerCostBinding, error) {
	return s.Scheduler.CostBindings(ctx)
}

func (s *Service) SaveCostBinding(ctx context.Context, id string, in domain.SchedulerCostBinding) (domain.SchedulerCostBinding, error) {
	return s.Scheduler.SaveCostBinding(ctx, id, in)
}

func (s *Service) DeleteCostBinding(ctx context.Context, id string) error {
	return s.Scheduler.DeleteCostBinding(ctx, id)
}

func (s *Service) AdoptCostBinding(ctx context.Context, id, provider string) (domain.CostFieldOwnership, error) {
	return s.Scheduler.AdoptCostBinding(ctx, id, provider)
}

func (s *Service) Profit(ctx context.Context, window string) (domain.ProfitResponse, error) {
	return s.ProfitSvc.Profit(ctx, window)
}

// 余额刷新使用的内部成本同步钩子。

func (s *Service) recordCurrentCostSnapshots(ctx context.Context) error {
	return s.Scheduler.recordCurrentCostSnapshots(ctx)
}

func (s *Service) syncSchedulerGroupsBestEffort(ctx context.Context) {
	s.Scheduler.syncSchedulerGroupsBestEffort(ctx)
}

// OneBot 公开 API

func (s *Service) OneBotStatus(ctx context.Context) (domain.OneBotStatus, error) {
	return s.OneBot.Status(ctx)
}

func (s *Service) AuthorizeOneBotEvent(ctx context.Context, signature string, payload []byte) error {
	return s.OneBot.AuthorizeEvent(ctx, signature, payload)
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
