package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
)

// 用例包装，使 httpapi 永不直接碰 Store。

func (s *Service) GetUpstream(ctx context.Context, id string) (domain.Upstream, error) {
	return s.Store.Upstream(ctx, id)
}

func (s *Service) DeleteUpstream(ctx context.Context, id string) error {
	return s.Store.DeleteUpstream(ctx, id)
}

// CaptureUpstreamBrowserTokens 持久化浏览器登录会话抓取的 token。
func (s *Service) CaptureUpstreamBrowserTokens(ctx context.Context, id, access, refresh string) (domain.Upstream, error) {
	u, err := s.Store.Upstream(ctx, id)
	if err != nil {
		return domain.Upstream{}, err
	}
	if access != "" {
		u.Sub2APIAccessToken = access
	}
	if refresh != "" {
		u.Sub2APIRefreshToken = refresh
	}
	out, err := s.Store.UpdateUpstream(ctx, u)
	if err != nil {
		return domain.Upstream{}, err
	}
	if err := s.CheckUpstream(ctx, id); err != nil {
		return out.Public(), err
	}
	out, err = s.Store.Upstream(ctx, id)
	return out.Public(), err
}

func (s *Service) GetCard(ctx context.Context, id string) (domain.ModelCard, error) {
	return s.Cards.Card(ctx, id)
}

func (s *Service) GetRevenueCard(ctx context.Context, id string) (domain.RevenueCard, error) {
	return s.Store.RevenueCard(ctx, id)
}

func (s *Service) DeleteRevenueCard(ctx context.Context, id string) error {
	return s.Store.DeleteRevenueCard(ctx, id)
}

func (s *Service) MarkOpsEventRead(ctx context.Context, id string) error {
	return s.Store.MarkOpsEventRead(ctx, id)
}

func (s *Service) AckOpsEvent(ctx context.Context, id string) error {
	return s.Store.AckOpsEvent(ctx, id)
}

func (s *Service) MarkOpsEventsRead(ctx context.Context, filter domain.OpsEventFilter) error {
	return s.Store.MarkOpsEventsRead(ctx, filter)
}

func (s *Service) AckOpsEvents(ctx context.Context, filter domain.OpsEventFilter) error {
	return s.Store.AckOpsEvents(ctx, filter)
}

func (s *Service) RecordAudit(ctx context.Context, log domain.AuditLog) error {
	return s.Store.CreateAudit(ctx, log)
}

func (s *Service) PublicSiteSettings(ctx context.Context) (siteName, siteIcon string, err error) {
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return "", "", err
	}
	return cfg.SiteName, cfg.SiteIcon, nil
}

func (s *Service) ExportData(ctx context.Context) (ExportData, error) {
	raw, err := s.Store.ExportData(ctx)
	if err != nil {
		return ExportData{}, err
	}
	return exportFromStore(raw), nil
}

func (s *Service) ImportData(ctx context.Context, in ExportData) error {
	return s.Store.ImportData(ctx, exportToStore(in))
}

func exportFromStore(in store.ExportData) ExportData {
	out := ExportData{Version: in.Version, Tables: make(map[string][]RowMap, len(in.Tables))}
	for table, rows := range in.Tables {
		converted := make([]RowMap, len(rows))
		for i, row := range rows {
			converted[i] = RowMap(row)
		}
		out.Tables[table] = converted
	}
	return out
}

func exportToStore(in ExportData) store.ExportData {
	out := store.ExportData{Version: in.Version, Tables: make(map[string][]store.RowMap, len(in.Tables))}
	for table, rows := range in.Tables {
		converted := make([]store.RowMap, len(rows))
		for i, row := range rows {
			converted[i] = store.RowMap(row)
		}
		out.Tables[table] = converted
	}
	return out
}

func (s *Service) ListTGChannels(ctx context.Context) ([]domain.TGChannel, error) {
	return s.Store.ListTGChannels(ctx)
}

func (s *Service) GetTGChannel(ctx context.Context, id string) (domain.TGChannel, error) {
	return s.Store.TGChannel(ctx, id)
}

func (s *Service) DeleteTGChannel(ctx context.Context, id string) error {
	return s.Store.DeleteTGChannel(ctx, id)
}

func (s *Service) ListTGMessages(ctx context.Context, channelID string, limit int) ([]domain.TGMessage, error) {
	return s.Store.TGMessages(ctx, channelID, limit)
}

func (s *Service) DeleteAllTGMessages(ctx context.Context) error {
	return s.Store.DeleteAllTGMessages(ctx)
}

func (s *Service) DeleteTGMessage(ctx context.Context, id string) error {
	return s.Store.DeleteTGMessage(ctx, id)
}
