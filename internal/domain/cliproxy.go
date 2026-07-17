package domain

import (
	"errors"
	"path/filepath"
	"strings"
)

type CLIProxyConfig struct {
	Name             string `json:"name"`
	BaseURL          string `json:"base_url"`
	ManagementKey    string `json:"management_key,omitempty"`
	ManagementKeySet bool   `json:"management_key_set"`
	Enabled          bool   `json:"enabled"`
}

type CLIProxyAuthFile struct {
	Name           string `json:"name"`
	Provider       string `json:"provider,omitempty"`
	Type           string `json:"type,omitempty"`
	Status         string `json:"status,omitempty"`
	StatusMessage  string `json:"status_message,omitempty"`
	Email          string `json:"email,omitempty"`
	AccountType    string `json:"account_type,omitempty"`
	Account        string `json:"account,omitempty"`
	Source         string `json:"source,omitempty"`
	AuthIndex      string `json:"auth_index,omitempty"`
	Size           int64  `json:"size,omitempty"`
	ModTime        string `json:"modtime,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	LastRefresh    string `json:"last_refresh,omitempty"`
	Success        int64  `json:"success"`
	Failed         int64  `json:"failed"`
	RecentRequests any    `json:"recent_requests,omitempty"`
	RuntimeOnly    bool   `json:"runtime_only,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
	Unavailable    bool   `json:"unavailable,omitempty"`
}

type CLIProxyQuota struct {
	PlanType                       string                `json:"plan_type,omitempty"`
	SubscriptionActiveUntil        string                `json:"subscription_active_until,omitempty"`
	RateLimitResetCreditsAvailable *int64                `json:"rate_limit_reset_credits_available,omitempty"`
	Windows                        []CLIProxyQuotaWindow `json:"windows"`
}

type CLIProxyQuotaWindow struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	UsedPercent      *float64 `json:"used_percent,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	ResetAt          string   `json:"reset_at,omitempty"`
}

type CLIProxyResetQuotaResult struct {
	Status    string   `json:"status,omitempty"`
	AuthIndex string   `json:"auth_index,omitempty"`
	Models    []string `json:"models,omitempty"`
}

// IsCodexCLIProxyAccount 判断授权文件是否属于 Codex 账号；xAI 不进入本项目号池管理。
func IsCodexCLIProxyAccount(account CLIProxyAuthFile) bool {
	provider := strings.ToLower(strings.TrimSpace(account.Provider))
	typ := strings.ToLower(strings.TrimSpace(account.Type))
	if provider == "xai" || typ == "xai" {
		return false
	}
	return provider == "codex" || typ == "codex" || strings.Contains(strings.ToLower(account.Name), "codex")
}

func ValidateCLIProxyAuthFileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("文件名不能为空")
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return errors.New("文件名不能包含路径")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		return errors.New("文件名必须以 .json 结尾")
	}
	return nil
}

func (c CLIProxyConfig) Public() CLIProxyConfig {
	c.ManagementKeySet = strings.TrimSpace(c.ManagementKey) != ""
	c.ManagementKey = ""
	if strings.TrimSpace(c.Name) == "" {
		c.Name = "CLIProxyAPI"
	}
	return c
}
