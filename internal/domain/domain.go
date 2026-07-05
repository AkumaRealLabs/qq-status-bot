package domain

import (
	"strings"
	"time"
)

const ProbeModel = "gpt-5.5"

type Upstream struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	BaseURL             string    `json:"base_url"`
	Enabled             bool      `json:"enabled"`
	UserID              string    `json:"user_id,omitempty"`
	AccessToken         string    `json:"access_token,omitempty"`
	Email               string    `json:"email,omitempty"`
	Password            string    `json:"password,omitempty"`
	Sub2APIAccessToken  string    `json:"sub2api_access_token,omitempty"`
	Sub2APIRefreshToken string    `json:"sub2api_refresh_token,omitempty"`
	BalanceRate         float64   `json:"balance_rate"`
	LowBalanceThreshold float64   `json:"low_balance_threshold"`
	LastError           string    `json:"last_error"`
	FailureCount        int       `json:"failure_count"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type APIKey struct {
	ID          string    `json:"id"`
	UpstreamID  string    `json:"upstream_id"`
	RemoteID    string    `json:"remote_id"`
	Name        string    `json:"name"`
	Key         string    `json:"key,omitempty"`
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
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	BaseURL               string     `json:"base_url,omitempty"`
	APIKey                string     `json:"api_key,omitempty"`
	UpstreamID            string     `json:"upstream_id,omitempty"`
	UpstreamName          string     `json:"upstream_name,omitempty"`
	Type                  string     `json:"type,omitempty"`
	KeyID                 string     `json:"key_id,omitempty"`
	KeyName               string     `json:"key_name,omitempty"`
	KeyGroup              string     `json:"key_group,omitempty"`
	KeyRatio              string     `json:"key_group_ratio,omitempty"`
	EffectiveRatio        string     `json:"effective_ratio,omitempty"`
	Model                 string     `json:"model"`
	DisplayGroup          string     `json:"display_group"`
	SchedulerChannelID    string     `json:"scheduler_channel_id,omitempty"`
	SchedulerChannelName  string     `json:"scheduler_channel_name,omitempty"`
	SchedulerAutoDisabled bool       `json:"scheduler_auto_disabled"`
	Enabled               bool       `json:"enabled"`
	PublicEnabled         bool       `json:"public_enabled"`
	SortOrder             int        `json:"sort_order"`
	LastError             string     `json:"last_error"`
	FailureCount          int        `json:"failure_count"`
	History               []ProbeRun `json:"history,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type PublicModelCard struct {
	Name         string           `json:"name"`
	DisplayGroup string           `json:"display_group"`
	LastError    string           `json:"last_error,omitempty"`
	History      []PublicProbeRun `json:"history,omitempty"`
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
	ID             string    `json:"id,omitempty"`
	UpstreamID     string    `json:"upstream_id,omitempty"`
	CardID         string    `json:"card_id,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
	Model          string    `json:"model"`
	Input          string    `json:"input,omitempty"`
	ExpectedAnswer string    `json:"expected_answer,omitempty"`
	Status         string    `json:"status"`
	Output         string    `json:"output,omitempty"`
	HTTPStatus     int       `json:"http_status"`
	LatencyMS      int       `json:"latency_ms"`
	Success        bool      `json:"success"`
	Error          string    `json:"error"`
}

type PublicProbeRun struct {
	CheckedAt      time.Time `json:"checked_at"`
	Status         string    `json:"status"`
	Input          string    `json:"input,omitempty"`
	ExpectedAnswer string    `json:"expected_answer,omitempty"`
	Output         string    `json:"output,omitempty"`
	HTTPStatus     int       `json:"http_status"`
	LatencyMS      int       `json:"latency_ms"`
	Success        bool      `json:"-"`
	Error          string    `json:"error,omitempty"`
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
	CheckIntervalMinutes int    `json:"check_interval_minutes"`
	TelegramBotToken     string `json:"telegram_bot_token,omitempty"`
	TelegramChatID       string `json:"telegram_chat_id,omitempty"`
	ProbeModel           string `json:"probe_model"`
	SiteName             string `json:"site_name"`
	SiteIcon             string `json:"site_icon"`
	EpayBaseURL          string `json:"epay_base_url"`
	EpayPID              string `json:"epay_pid"`
	EpayKey              string `json:"epay_key"`
}

type SchedulerConfig struct {
	BaseURL     string `json:"scheduler_base_url"`
	UserID      string `json:"scheduler_user_id"`
	AccessToken string `json:"scheduler_access_token"`
}

type SchedulerChannel struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status int      `json:"status"`
	Tag    string   `json:"tag,omitempty"`
	Type   string   `json:"type,omitempty"`
	Group  string   `json:"group,omitempty"`
	Models []string `json:"models,omitempty"`
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
	CreatedAt   time.Time `json:"created_at"`
}

type RevenueCard struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SourceType   string    `json:"source_type"`
	BaseURL      string    `json:"base_url,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	AdminAPIKey  string    `json:"admin_api_key,omitempty"`
	EpayPID      string    `json:"epay_pid,omitempty"`
	EpayKey      string    `json:"epay_key,omitempty"`
	UpstreamID   string    `json:"upstream_id,omitempty"`
	UpstreamName string    `json:"upstream_name,omitempty"`
	Enabled      bool      `json:"enabled"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
	if prev.Active && now.Sub(prev.LastAt) < time.Hour {
		return AlertDecision{NewState: prev}, false
	}
	return AlertDecision{
		Type:     kind,
		Message:  message,
		NewState: AlertState{Active: true, LastAt: now},
	}, true
}
