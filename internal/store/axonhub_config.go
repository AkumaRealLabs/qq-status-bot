package store

import (
	"context"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) AxonHubConfig(ctx context.Context) (domain.AxonHubConfig, error) {
	var cfg domain.AxonHubConfig
	err := s.row(ctx, `SELECT axonhub_base_url, axonhub_admin_email, axonhub_admin_password, axonhub_control_mode FROM settings WHERE id='default'`).
		Scan(&cfg.BaseURL, &cfg.AdminEmail, &cfg.AdminPassword, &cfg.ControlMode)
	cfg.ControlMode = domain.NormalizeAxonHubControlMode(cfg.ControlMode)
	return cfg, err
}

func (s *Store) UpdateAxonHubConfig(ctx context.Context, cfg domain.AxonHubConfig) (domain.AxonHubConfig, error) {
	old, err := s.AxonHubConfig(ctx)
	if err != nil {
		return cfg, err
	}
	cfg = cfg.MergeUpdate(old)
	_, err = s.exec(ctx, `UPDATE settings SET axonhub_base_url=?, axonhub_admin_email=?, axonhub_admin_password=?, axonhub_control_mode=? WHERE id='default'`, cfg.BaseURL, cfg.AdminEmail, cfg.AdminPassword, cfg.ControlMode)
	return cfg, err
}

func (s *Store) UpdateSchedulerProvider(ctx context.Context, provider string) error {
	_, err := s.exec(ctx, `UPDATE settings SET scheduler_provider=? WHERE id='default'`, domain.NormalizeSchedulerProvider(provider))
	return err
}
