package store

import (
	"context"
	"strings"

	"ai-upstream-monitor/internal/domain"
)

func (s *Store) CLIProxyConfig(ctx context.Context) (domain.CLIProxyConfig, error) {
	var cfg domain.CLIProxyConfig
	var enabled int
	err := s.row(ctx, `SELECT cliproxy_name, cliproxy_base_url, cliproxy_management_key, cliproxy_enabled FROM settings WHERE id='default'`).
		Scan(&cfg.Name, &cfg.BaseURL, &cfg.ManagementKey, &enabled)
	cfg.Enabled = boolFromInt(enabled)
	cfg.ManagementKeySet = strings.TrimSpace(cfg.ManagementKey) != ""
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = domain.DefaultCLIProxyName
	}
	return cfg, err
}

// UpdateCLIProxyConfig 持久化 CLIProxy 配置。调用方应先 MergeUpdate。
// 对直连 store 的调用方保留防御性 MergeUpdate。
func (s *Store) UpdateCLIProxyConfig(ctx context.Context, cfg domain.CLIProxyConfig) (domain.CLIProxyConfig, error) {
	old, err := s.CLIProxyConfig(ctx)
	if err != nil {
		return cfg, err
	}
	cfg = cfg.MergeUpdate(old)
	_, err = s.exec(ctx, `UPDATE settings SET cliproxy_name=?, cliproxy_base_url=?, cliproxy_management_key=?, cliproxy_enabled=? WHERE id='default'`,
		cfg.Name, cfg.BaseURL, cfg.ManagementKey, boolInt(cfg.Enabled))
	cfg.ManagementKeySet = cfg.ManagementKey != ""
	return cfg, err
}
