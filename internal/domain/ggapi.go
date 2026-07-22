package domain

import "encoding/json"

type GGAPIAffinitySettings struct {
	Enabled               bool            `json:"enabled"`
	SwitchOnSuccess       bool            `json:"switch_on_success"`
	KeepOnChannelDisabled bool            `json:"keep_on_channel_disabled"`
	MaxEntries            int             `json:"max_entries"`
	DefaultTTLSeconds     int             `json:"default_ttl_seconds"`
	Rules                 json.RawMessage `json:"rules"`
}

type GGAPISettings struct {
	RetryTimes                     int                   `json:"retry_times"`
	AutomaticRetryStatusCodes      string                `json:"automatic_retry_status_codes"`
	AutomaticDisableStatusCodes    string                `json:"automatic_disable_status_codes"`
	ChannelTestMode                string                `json:"channel_test_mode"`
	AutoTestChannelEnabled         bool                  `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes         float64               `json:"auto_test_channel_minutes"`
	AutomaticDisableChannelEnabled bool                  `json:"automatic_disable_channel_enabled"`
	AutomaticEnableChannelEnabled  bool                  `json:"automatic_enable_channel_enabled"`
	Affinity                       GGAPIAffinitySettings `json:"affinity"`
}

type GGAPISettingsUpdateResult struct {
	Complete  bool          `json:"complete"`
	Applied   []string      `json:"applied"`
	FailedKey string        `json:"failed_key,omitempty"`
	Error     string        `json:"error,omitempty"`
	Settings  GGAPISettings `json:"settings"`
}

type GGAPIAffinityCacheStats struct {
	Enabled       bool           `json:"enabled"`
	Total         int            `json:"total"`
	Unknown       int            `json:"unknown"`
	ByRuleName    map[string]int `json:"by_rule_name"`
	CacheCapacity int            `json:"cache_capacity"`
	CacheAlgo     string         `json:"cache_algo"`
}

type GGAPIAffinityCacheClearResult struct {
	Deleted int `json:"deleted"`
}
