package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
)

func (s *Service) SaveUpstream(ctx context.Context, id string, in domain.Upstream) (domain.Upstream, error) {
	if in.Name == "" || in.Type == "" || in.BaseURL == "" {
		return domain.Upstream{}, ErrBadRequest("name, type and base_url are required")
	}
	if in.Type != "newapi" && in.Type != "sub2api" {
		return domain.Upstream{}, ErrBadRequest("type must be newapi or sub2api")
	}
	if in.BalanceRate <= 0 {
		in.BalanceRate = 1
	}
	if id == "" {
		if !in.Enabled {
			in.Enabled = true
		}
		out, err := s.Store.CreateUpstream(ctx, in)
		return out.Public(), err
	}
	old, err := s.Store.Upstream(ctx, id)
	if err != nil {
		return domain.Upstream{}, err
	}
	in = in.MergeUpdate(old)
	out, err := s.Store.UpdateUpstream(ctx, in)
	if err == nil && domain.BalanceRate(old) != domain.BalanceRate(out) {
		if err := s.recordCurrentCostSnapshots(ctx); err != nil {
			return out.Public(), err
		}
		s.syncSchedulerGroupsBestEffort(ctx)
	}
	return out.Public(), err
}

func (s *Service) SyncKeys(ctx context.Context, upstreamID string) error {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return err
	}
	mu := toMonitorUpstream(u)
	result, err := s.Client.Check(ctx, &mu, "", "")
	if err != nil {
		return err
	}
	if err := s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken); err != nil {
		return err
	}
	if err := s.Store.SaveKeys(ctx, u.ID, result.Keys); err != nil {
		return err
	}
	if err := s.recordCurrentCostSnapshots(ctx); err != nil {
		return err
	}
	s.syncSchedulerGroupsBestEffort(ctx)
	return nil
}

func (s *Service) UpstreamRows(ctx context.Context) ([]map[string]any, error) {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(upstreams))
	for _, u := range upstreams {
		keys, err := s.Store.ListKeys(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		row := map[string]any{"upstream": u.Public(), "keys": domain.PublicAPIKeys(keys)}
		if b, err := s.Store.LatestBalance(ctx, u.ID); err == nil {
			row["balance"] = b
		}
		out = append(out, row)
	}
	return out, nil
}
