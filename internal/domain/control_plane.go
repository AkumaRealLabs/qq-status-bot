package domain

import "time"

const (
	ControlOwnerAUM      = "aum"
	ControlOwnerGGAPI    = "ggapi"
	ControlOwnerExternal = "external"
	ControlOwnerObserved = "observed"

	ControlSourceManual       = "manual"
	ControlSourceBalance      = "balance"
	ControlSourceProbe        = "probe"
	ControlSourceTraffic      = "traffic"
	ControlSourceCost         = "cost"
	ControlSourceControlPlane = "control_plane"
)

// SchedulerChannelLifecycle 记录 AUM 与 GGAPI 之间的渠道控制权和写入确认。
// 这里只保存控制运行态，不复制 GGAPI 的业务配置。
type SchedulerChannelLifecycle struct {
	ChannelID              string    `json:"channel_id"`
	ChannelName            string    `json:"channel_name,omitempty"`
	RemoteStatus           int       `json:"remote_status"`
	RemotePriority         int64     `json:"remote_priority"`
	RemoteWeight           uint      `json:"remote_weight"`
	Owner                  string    `json:"owner"`
	ExternalTakeover       bool      `json:"external_takeover"`
	AUMDisabled            bool      `json:"aum_disabled"`
	LastAUMStatus          int       `json:"last_aum_status,omitempty"`
	LastAUMWriteAt         time.Time `json:"last_aum_write_at,omitempty"`
	LastSource             string    `json:"last_source,omitempty"`
	LastReason             string    `json:"last_reason,omitempty"`
	TrafficSince           time.Time `json:"traffic_since,omitempty"`
	AffinityCleanupPending bool      `json:"affinity_cleanup_pending"`
	AffinityCleanupRetryAt time.Time `json:"affinity_cleanup_retry_at,omitempty"`
	AffinityCleanupRetries int       `json:"affinity_cleanup_retries"`
	AffinityCleanupError   string    `json:"affinity_cleanup_error,omitempty"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SchedulerControlPlaneChannel struct {
	ChannelID              string               `json:"channel_id"`
	ChannelName            string               `json:"channel_name,omitempty"`
	Managed                bool                 `json:"managed"`
	RemoteStatus           int                  `json:"remote_status"`
	RemotePriority         int64                `json:"remote_priority"`
	RemoteWeight           uint                 `json:"remote_weight"`
	Owner                  string               `json:"owner"`
	ExternalTakeover       bool                 `json:"external_takeover"`
	AUMDisabled            bool                 `json:"aum_disabled"`
	CloseSource            string               `json:"close_source,omitempty"`
	CloseReason            string               `json:"close_reason,omitempty"`
	TrafficSince           time.Time            `json:"traffic_since,omitempty"`
	NewTrafficRequests     int                  `json:"new_traffic_requests"`
	SessionFailures        int                  `json:"session_failures"`
	AffinityCleanupPending bool                 `json:"affinity_cleanup_pending"`
	AffinityCleanupRetryAt time.Time            `json:"affinity_cleanup_retry_at,omitempty"`
	AffinityCleanupError   string               `json:"affinity_cleanup_error,omitempty"`
	Availability           *AvailabilityView    `json:"availability,omitempty"`
	Traffic                *TrafficChannelState `json:"traffic,omitempty"`
	UpdatedAt              time.Time            `json:"updated_at"`
}

type SchedulerControlPlane struct {
	Traffic  TrafficStatus                  `json:"traffic"`
	Channels []SchedulerControlPlaneChannel `json:"channels"`
	Logs     []SchedulerLog                 `json:"logs"`
}
