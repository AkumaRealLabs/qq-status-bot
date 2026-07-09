package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
)

func (s *Service) Settings(ctx context.Context) (domain.Settings, error) {
	cfg, err := s.Store.Settings(ctx)
	return cfg.Public(), err
}

func (s *Service) UpdateSettings(ctx context.Context, cfg domain.Settings) (domain.Settings, error) {
	old, err := s.Store.Settings(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	cfg = cfg.MergeUpdate(old)
	out, err := s.Store.UpdateSettings(ctx, cfg)
	return out.Public(), err
}

func (s *Service) Health(ctx context.Context) (map[string]any, error) {
	if err := s.Store.DB.PingContext(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "db": s.Store.Driver}, nil
}
