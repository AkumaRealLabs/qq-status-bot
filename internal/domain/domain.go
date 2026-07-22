package domain

import (
	"errors"
	"strings"
	"time"
)

const ProbeModel = "gpt-5.6-sol"

// NormalizeProbeModel：空模型回退到默认探测模型。
func NormalizeProbeModel(model string) string {
	if model = strings.TrimSpace(model); model != "" {
		return model
	}
	return ProbeModel
}

type Upstream struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	Type                    string    `json:"type"`
	BaseURL                 string    `json:"base_url"`
	Enabled                 bool      `json:"enabled"`
	UserID                  string    `json:"user_id,omitempty"`
	AccessToken             string    `json:"access_token,omitempty"`
	AccessTokenSet          bool      `json:"access_token_set,omitempty"`
	Email                   string    `json:"email,omitempty"`
	Password                string    `json:"password,omitempty"`
	PasswordSet             bool      `json:"password_set,omitempty"`
	Sub2APIAccessToken      string    `json:"sub2api_access_token,omitempty"`
	Sub2APIAccessTokenSet   bool      `json:"sub2api_access_token_set,omitempty"`
	Sub2APIRefreshToken     string    `json:"sub2api_refresh_token,omitempty"`
	Sub2APIRefreshTokenSet  bool      `json:"sub2api_refresh_token_set,omitempty"`
	BalanceRate             float64   `json:"balance_rate"`
	LowBalanceThreshold     float64   `json:"low_balance_threshold"`
	BalanceGuardMode        string    `json:"balance_guard_mode"`
	BalanceCloseThreshold   float64   `json:"balance_close_threshold"`
	BalanceRecoverThreshold float64   `json:"balance_recover_threshold"`
	RunwayWarningHours      float64   `json:"runway_warning_hours"`
	LastError               string    `json:"last_error"`
	FailureCount            int       `json:"failure_count"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type APIKey struct {
	ID          string    `json:"id"`
	UpstreamID  string    `json:"upstream_id"`
	RemoteID    string    `json:"remote_id"`
	Name        string    `json:"name"`
	Key         string    `json:"key,omitempty"`
	KeySet      bool      `json:"key_set,omitempty"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
	Group       string    `json:"group"`
	GroupRatio  string    `json:"group_ratio"`
	Quota       float64   `json:"quota"`
	UsedQuota   float64   `json:"used_quota"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ModelCard struct {
	ID                      string     `json:"id"`
	Name                    string     `json:"name"`
	BaseURL                 string     `json:"base_url,omitempty"`
	APIKey                  string     `json:"api_key,omitempty"`
	APIKeySet               bool       `json:"api_key_set,omitempty"`
	UpstreamID              string     `json:"upstream_id,omitempty"`
	UpstreamName            string     `json:"upstream_name,omitempty"`
	Type                    string     `json:"type,omitempty"`
	KeyID                   string     `json:"key_id,omitempty"`
	KeyName                 string     `json:"key_name,omitempty"`
	KeyGroup                string     `json:"key_group,omitempty"`
	KeyRatio                string     `json:"key_group_ratio,omitempty"`
	EffectiveRatio          string     `json:"effective_ratio,omitempty"`
	Model                   string     `json:"model"`
	DisplayGroup            string     `json:"display_group"`
	PoolEnabled             bool       `json:"pool_enabled"`
	PoolEnabledSet          bool       `json:"-"`
	ManualCostRatio         string     `json:"manual_cost_ratio,omitempty"`
	SchedulerGroup          string     `json:"scheduler_group,omitempty"`
	SchedulerChannelID      string     `json:"scheduler_channel_id,omitempty"`
	SchedulerChannelName    string     `json:"scheduler_channel_name,omitempty"`
	AxonHubChannelID        string     `json:"axonhub_channel_id,omitempty"`
	AxonHubChannelName      string     `json:"axonhub_channel_name,omitempty"`
	SchedulerAutoDisabled   bool       `json:"scheduler_auto_disabled"`
	SchedulerAutoDisabledAt *time.Time `json:"scheduler_auto_disabled_at,omitempty"`
	ProbeMuted              bool       `json:"probe_muted"`
	Enabled                 bool       `json:"enabled"`
	PublicEnabled           bool       `json:"public_enabled"`
	SortOrder               int        `json:"sort_order"`
	LastError               string     `json:"last_error"`
	FailureCount            int        `json:"failure_count"`
	History                 []ProbeRun `json:"history,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type PublicModelCard struct {
	Name            string           `json:"name"`
	DisplayGroup    string           `json:"display_group"`
	ProbeMuted      bool             `json:"probe_muted"`
	AutoProbePaused bool             `json:"auto_probe_paused"`
	LastError       string           `json:"last_error,omitempty"`
	History         []PublicProbeRun `json:"history,omitempty"`
}

// PublicMonitorStatus 是公开状态页及外部只读查询共用的状态投影。
// 字段名保持与既有 /api/public/monitor/status 响应一致。
type PublicMonitorStatus struct {
	Window      string            `json:"window"`
	Rows        []PublicModelCard `json:"rows"`
	Requests    int               `json:"requests"`
	Success     int               `json:"success"`
	Failed      int               `json:"failed"`
	SuccessRate float64           `json:"success_rate"`
	AvgLatency  int               `json:"avg_latency"`
}

// OneBotStatus 是后台连通性检查的最小公开结果。
type OneBotStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type BalanceSnapshot struct {
	ID         string    `json:"id"`
	UpstreamID string    `json:"upstream_id"`
	CheckedAt  time.Time `json:"checked_at"`
	Balance    float64   `json:"balance"`
	Used       float64   `json:"used"`
	Remain     float64   `json:"remain"`
	Requests   int       `json:"requests"`
	Error      string    `json:"error"`
	LatencyMS  int       `json:"latency_ms"`
}

type ProbeRun struct {
	ID         string    `json:"id,omitempty"`
	UpstreamID string    `json:"upstream_id,omitempty"`
	CardID     string    `json:"card_id,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	Model      string    `json:"model"`
	Input      string    `json:"input,omitempty"`
	Status     string    `json:"status"`
	Output     string    `json:"output,omitempty"`
	HTTPStatus int       `json:"http_status"`
	LatencyMS  int       `json:"latency_ms"`
	Success    bool      `json:"success"`
	Error      string    `json:"error"`
	Purpose    string    `json:"purpose,omitempty"`
}

type PublicProbeRun struct {
	CheckedAt  time.Time `json:"checked_at"`
	Status     string    `json:"status"`
	Input      string    `json:"input,omitempty"`
	Output     string    `json:"output,omitempty"`
	HTTPStatus int       `json:"http_status"`
	LatencyMS  int       `json:"latency_ms"`
	Success    bool      `json:"-"`
	Error      string    `json:"error,omitempty"`
}

type AlertEvent struct {
	ID         string    `json:"id"`
	UpstreamID string    `json:"upstream_id"`
	Type       string    `json:"type"`
	Recover    bool      `json:"recover"`
	Sent       bool      `json:"sent"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

type BalanceRechargeLog struct {
	ID            string    `json:"id"`
	UpstreamID    string    `json:"upstream_id"`
	Method        string    `json:"method"`
	Amount        float64   `json:"amount"`
	PaymentType   string    `json:"payment_type"`
	RemoteOrderID string    `json:"remote_order_id"`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
	RawStatus     string    `json:"raw_status,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Settings struct {
	CheckIntervalMinutes  int               `json:"check_interval_minutes"`
	TelegramBotToken      string            `json:"telegram_bot_token,omitempty"`
	TelegramBotTokenSet   bool              `json:"telegram_bot_token_set,omitempty"`
	TelegramChatID        string            `json:"telegram_chat_id,omitempty"`
	OneBotEnabled         bool              `json:"onebot_enabled"`
	OneBotBaseURL         string            `json:"onebot_base_url,omitempty"`
	OneBotHTTPToken       string            `json:"onebot_http_token,omitempty"`
	OneBotHTTPTokenSet    bool              `json:"onebot_http_token_set,omitempty"`
	OneBotWebhookToken    string            `json:"onebot_webhook_token,omitempty"`
	OneBotWebhookTokenSet bool              `json:"onebot_webhook_token_set,omitempty"`
	OneBotGroupIDs        []string          `json:"onebot_group_ids"`
	ProbeModel            string            `json:"probe_model"`
	SiteName              string            `json:"site_name"`
	SiteIcon              string            `json:"site_icon"`
	EpayBaseURL           string            `json:"epay_base_url"`
	EpayPID               string            `json:"epay_pid"`
	EpayKey               string            `json:"epay_key"`
	EpayKeySet            bool              `json:"epay_key_set,omitempty"`
	NotificationRules     NotificationRules `json:"notification_rules"`
}

type SchedulerConfig struct {
	Provider        string          `json:"scheduler_provider"`
	BaseURL         string          `json:"scheduler_base_url"`
	UserID          string          `json:"scheduler_user_id"`
	AccessToken     string          `json:"scheduler_access_token"`
	AccessTokenSet  bool            `json:"scheduler_access_token_set,omitempty"`
	UnassignedGroup string          `json:"scheduler_unassigned_group"`
	Tiers           []SchedulerTier `json:"scheduler_tiers"`
	TrafficMode     string          `json:"scheduler_traffic_mode"`
	TrafficProfile  string          `json:"scheduler_traffic_profile"`
	TrafficPollSecs int             `json:"scheduler_log_poll_seconds"`
}

type SchedulerTier struct {
	Tag       string  `json:"tag"`
	Group     string  `json:"group"`
	PriceMin  float64 `json:"price_min"`
	PriceMax  float64 `json:"price_max"`
	SalePrice float64 `json:"sale_price"`
}

type SchedulerApplyResult struct {
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
}

func DefaultSchedulerTiers() []SchedulerTier {
	return []SchedulerTier{
		{Tag: "gpt_low", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1, SalePrice: 0.1},
		{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0, PriceMax: 0.25, SalePrice: 0.25},
	}
}

func NormalizeSchedulerTiers(in []SchedulerTier) []SchedulerTier {
	if in == nil {
		return DefaultSchedulerTiers()
	}
	out := make([]SchedulerTier, 0, len(in))
	for _, tier := range in {
		out = append(out, SchedulerTier{
			Tag:       strings.TrimSpace(tier.Tag),
			Group:     strings.TrimSpace(tier.Group),
			PriceMin:  tier.PriceMin,
			PriceMax:  tier.PriceMax,
			SalePrice: schedulerSalePrice(tier),
		})
	}
	return out
}

func schedulerSalePrice(tier SchedulerTier) float64 {
	if tier.SalePrice > 0 {
		return tier.SalePrice
	}
	keys := []string{strings.ToLower(strings.TrimSpace(tier.Tag)), strings.ToLower(strings.TrimSpace(tier.Group))}
	for _, def := range DefaultSchedulerTiers() {
		for _, key := range keys {
			if key == def.Tag || key == def.Group {
				return def.SalePrice
			}
		}
	}
	return tier.PriceMax
}

func ValidateSchedulerTiers(tiers []SchedulerTier) error {
	tags := map[string]bool{}
	groups := map[string]bool{}
	for _, tier := range NormalizeSchedulerTiers(tiers) {
		if tier.Tag == "" {
			return errors.New("分组名称不能为空")
		}
		if tier.Group == "" {
			return errors.New("调度器分组不能为空")
		}
		tagKey := strings.ToLower(tier.Tag)
		if tags[tagKey] {
			return errors.New("分组名称不能重复")
		}
		if groups[tier.Group] {
			return errors.New("调度器分组不能重复")
		}
		tags[tagKey] = true
		groups[tier.Group] = true
		if tier.PriceMin < 0 || tier.PriceMax < 0 || tier.PriceMax < tier.PriceMin {
			return errors.New("调度器价格区间无效")
		}
		if tier.SalePrice <= 0 {
			return errors.New("调度器售价必须大于 0")
		}
	}
	return nil
}

// ValidateSchedulerUnassignedGroup 校验未分配分组：非空且不得与价格档位分组重名。
func ValidateSchedulerUnassignedGroup(unassigned string, tiers []SchedulerTier) error {
	unassigned = strings.TrimSpace(unassigned)
	if unassigned == "" {
		return errors.New("请配置未分配分组（调度器中需已存在该分组）")
	}
	if strings.ContainsAny(unassigned, ",，;；") {
		return errors.New("未分配分组只能是单个分组名")
	}
	for _, tier := range NormalizeSchedulerTiers(tiers) {
		if strings.TrimSpace(tier.Group) == unassigned {
			return errors.New("未分配分组不能与价格档位的调度器分组相同")
		}
	}
	return nil
}

type SchedulerChannel struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Status         int      `json:"status"`
	Priority       int64    `json:"priority"`
	Weight         uint     `json:"weight"`
	Tag            string   `json:"tag,omitempty"`
	Type           string   `json:"type,omitempty"`
	Group          string   `json:"group,omitempty"`
	Models         []string `json:"models,omitempty"`
	RemoteStatus   string   `json:"remote_status,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	OrderingWeight int      `json:"ordering_weight,omitempty"`
	Archived       bool     `json:"archived,omitempty"`
}

type SchedulerGroup struct {
	Name        string `json:"name"`
	Ratio       string `json:"ratio,omitempty"`
	Description string `json:"description,omitempty"`
}

type SchedulerLog struct {
	ID          string    `json:"id"`
	CardID      string    `json:"card_id"`
	CardName    string    `json:"card_name"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	Action      string    `json:"action"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	Reason      string    `json:"reason,omitempty"`
	Provider    string    `json:"provider"`
	CreatedAt   time.Time `json:"created_at"`
}

type SchedulerChannelCostSnapshot struct {
	ID            string    `json:"id"`
	ChannelID     string    `json:"channel_id"`
	ChannelName   string    `json:"channel_name,omitempty"`
	CardID        string    `json:"card_id,omitempty"`
	CardName      string    `json:"card_name,omitempty"`
	SourceType    string    `json:"source_type,omitempty"`
	UpstreamID    string    `json:"upstream_id,omitempty"`
	UpstreamName  string    `json:"upstream_name,omitempty"`
	KeyID         string    `json:"key_id,omitempty"`
	KeyName       string    `json:"key_name,omitempty"`
	CostPerUnit   float64   `json:"cost_per_unit,omitempty"`
	Active        bool      `json:"active"`
	MissingReason string    `json:"missing_reason,omitempty"`
	Provider      string    `json:"provider"`
	EffectiveAt   time.Time `json:"effective_at"`
}

type SchedulerGroupSaleSnapshot struct {
	ID          string    `json:"id"`
	Group       string    `json:"group"`
	Tag         string    `json:"tag,omitempty"`
	SalePrice   float64   `json:"sale_price,omitempty"`
	Active      bool      `json:"active"`
	EffectiveAt time.Time `json:"effective_at"`
}

type RevenueCard struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	SourceType     string    `json:"source_type"`
	BaseURL        string    `json:"base_url,omitempty"`
	UserID         string    `json:"user_id,omitempty"`
	AccessToken    string    `json:"access_token,omitempty"`
	AccessTokenSet bool      `json:"access_token_set,omitempty"`
	AdminAPIKey    string    `json:"admin_api_key,omitempty"`
	AdminAPIKeySet bool      `json:"admin_api_key_set,omitempty"`
	EpayPID        string    `json:"epay_pid,omitempty"`
	EpayKey        string    `json:"epay_key,omitempty"`
	EpayKeySet     bool      `json:"epay_key_set,omitempty"`
	UpstreamID     string    `json:"upstream_id,omitempty"`
	UpstreamName   string    `json:"upstream_name,omitempty"`
	Enabled        bool      `json:"enabled"`
	SortOrder      int       `json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type RevenueRow struct {
	RevenueCard
	Revenue   float64   `json:"revenue"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

type TGSession struct {
	ID             string    `json:"id"`
	APIID          int       `json:"api_id"`
	APIHash        string    `json:"api_hash,omitempty"`
	Phone          string    `json:"phone"`
	CodeHash       string    `json:"-"`
	SessionBlob    []byte    `json:"-"`
	Authorized     bool      `json:"authorized"`
	PasswordNeeded bool      `json:"password_needed"`
	LastError      string    `json:"last_error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type TGChannel struct {
	ID           string    `json:"id"`
	DisplayName  string    `json:"display_name"`
	Identifier   string    `json:"identifier"`
	Username     string    `json:"username,omitempty"`
	PeerID       int64     `json:"peer_id"`
	AccessHash   int64     `json:"access_hash,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Enabled      bool      `json:"enabled"`
	MessageLimit int       `json:"message_limit"`
	PinnedOnly   bool      `json:"pinned_only"`
	LastSyncAt   time.Time `json:"last_sync_at"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type TGMessage struct {
	ID          string    `json:"id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name,omitempty"`
	RemoteID    int       `json:"remote_id"`
	PublishedAt time.Time `json:"published_at"`
	Text        string    `json:"text"`
	MediaType   string    `json:"media_type,omitempty"`
	MediaPath   string    `json:"media_path,omitempty"`
	MediaURL    string    `json:"media_url,omitempty"`
	MediaCached bool      `json:"media_cached"`
	Link        string    `json:"link,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

func NormalizeCheckInterval(minutes int) int {
	if minutes < 1 {
		return 5
	}
	return minutes
}

func CardName(upstream Upstream, key *APIKey) string {
	name := strings.TrimSpace(upstream.Name)
	if key == nil {
		return name
	}
	keyName := strings.TrimSpace(key.Name)
	if keyName == "" {
		keyName = strings.TrimSpace(key.Description)
	}
	if keyName == "" {
		keyName = strings.TrimSpace(key.ID)
	}
	if keyName == "" {
		return name
	}
	return name + " · " + keyName
}

func BalanceRate(u Upstream) float64 {
	if u.BalanceRate <= 0 {
		return 1
	}
	return u.BalanceRate
}

func NormalizedBalanceValues(upstreamType string, balance, used, remain float64) (float64, float64, float64) {
	if upstreamType == "newapi" {
		return balance / 500000, used / 500000, remain / 500000
	}
	return balance, used, remain
}

func ConvertedBalanceValues(upstreamType string, rate, balance, used, remain float64) (float64, float64, float64) {
	balance, used, remain = NormalizedBalanceValues(upstreamType, balance, used, remain)
	if rate <= 0 {
		rate = 1
	}
	return balance * rate, used * rate, remain * rate
}

func LowBalance(u Upstream, b BalanceSnapshot) bool {
	if u.LowBalanceThreshold <= 0 {
		return false
	}
	_, _, remain := ConvertedBalanceValues(u.Type, BalanceRate(u), b.Balance, b.Used, b.Remain)
	return remain <= u.LowBalanceThreshold
}

type AlertState struct {
	Active bool
	LastAt time.Time
}

type AlertDecision struct {
	Type     string
	Recover  bool
	Message  string
	NewState AlertState
}

func DecideAlert(now time.Time, kind string, failing bool, message string, prev AlertState) (AlertDecision, bool) {
	if !failing {
		if prev.Active {
			return AlertDecision{Type: kind, Recover: true, Message: message, NewState: AlertState{}}, true
		}
		return AlertDecision{NewState: prev}, false
	}
	if prev.Active && now.Sub(prev.LastAt) < alertCooldown(kind) {
		return AlertDecision{NewState: prev}, false
	}
	return AlertDecision{
		Type:     kind,
		Message:  message,
		NewState: AlertState{Active: true, LastAt: now},
	}, true
}

func alertCooldown(kind string) time.Duration {
	if strings.HasPrefix(kind, "quota:") {
		return 6 * time.Hour
	}
	return time.Hour
}
