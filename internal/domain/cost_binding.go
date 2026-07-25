package domain

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	CostSourceUpstreamKey = "upstream_key"
	CostSourceManual      = "manual"
)

// SchedulerCostBinding 只描述成本来源与调度渠道绑定，不承载探测或渠道健康状态。
type SchedulerCostBinding struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	UpstreamID              string    `json:"upstream_id,omitempty"`
	UpstreamName            string    `json:"upstream_name,omitempty"`
	KeyID                   string    `json:"key_id,omitempty"`
	KeyName                 string    `json:"key_name,omitempty"`
	KeyGroup                string    `json:"key_group,omitempty"`
	KeyRatio                string    `json:"key_group_ratio,omitempty"`
	BalanceRate             float64   `json:"balance_rate,omitempty"`
	ManualCostRatio         string    `json:"manual_cost_ratio,omitempty"`
	SourceType              string    `json:"source_type"`
	EffectiveCost           float64   `json:"effective_cost,omitempty"`
	CostAvailable           bool      `json:"cost_available"`
	MissingReason           string    `json:"missing_reason,omitempty"`
	GGAPIExternalTakeover   bool      `json:"ggapi_external_takeover"`
	GGAPIOwnershipReason    string    `json:"ggapi_ownership_reason,omitempty"`
	AxonHubExternalTakeover bool      `json:"axonhub_external_takeover"`
	AxonHubOwnershipReason  string    `json:"axonhub_ownership_reason,omitempty"`
	SchedulerChannelID      string    `json:"scheduler_channel_id,omitempty"`
	SchedulerChannelName    string    `json:"scheduler_channel_name,omitempty"`
	AxonHubChannelID        string    `json:"axonhub_channel_id,omitempty"`
	AxonHubChannelName      string    `json:"axonhub_channel_name,omitempty"`
	Enabled                 bool      `json:"enabled"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func NormalizeCostBinding(in SchedulerCostBinding) SchedulerCostBinding {
	out := in
	out.Name = strings.TrimSpace(out.Name)
	out.UpstreamID = strings.TrimSpace(out.UpstreamID)
	out.KeyID = strings.TrimSpace(out.KeyID)
	out.ManualCostRatio = strings.TrimSpace(out.ManualCostRatio)
	out.SchedulerChannelID = strings.TrimSpace(out.SchedulerChannelID)
	out.SchedulerChannelName = strings.TrimSpace(out.SchedulerChannelName)
	out.AxonHubChannelID = strings.TrimSpace(out.AxonHubChannelID)
	out.AxonHubChannelName = strings.TrimSpace(out.AxonHubChannelName)
	if out.UpstreamID != "" || out.KeyID != "" {
		out.SourceType = CostSourceUpstreamKey
		out.ManualCostRatio = ""
	} else {
		out.SourceType = CostSourceManual
	}
	return out
}

func ValidateCostBinding(in SchedulerCostBinding) error {
	in = NormalizeCostBinding(in)
	if in.Name == "" {
		return errors.New("名称不能为空")
	}
	if in.SourceType == CostSourceUpstreamKey {
		if in.UpstreamID == "" || in.KeyID == "" {
			return errors.New("上游成本来源必须同时选择上游和 Key")
		}
		return nil
	}
	if in.ManualCostRatio == "" {
		return nil
	}
	ratio, err := strconv.ParseFloat(in.ManualCostRatio, 64)
	if err != nil || ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return errors.New("手动成本倍率必须大于 0")
	}
	return nil
}

// CostFieldOwnership 记录 AUM 最近确认的成本字段基线；外部改动后暂停自动覆盖。
type CostFieldOwnership struct {
	Provider         string    `json:"provider"`
	ChannelID        string    `json:"channel_id"`
	ChannelName      string    `json:"channel_name,omitempty"`
	RemoteGroups     []string  `json:"remote_groups,omitempty"`
	RemotePriority   int64     `json:"remote_priority,omitempty"`
	RemoteWeight     int       `json:"remote_weight,omitempty"`
	Managed          bool      `json:"managed"`
	ExternalTakeover bool      `json:"external_takeover"`
	LastReason       string    `json:"last_reason,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}
