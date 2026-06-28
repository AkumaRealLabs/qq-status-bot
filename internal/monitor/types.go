package monitor

import "time"

type Upstream struct {
	ID                  string
	Name                string
	Type                string
	BaseURL             string
	Enabled             bool
	UserID              string
	AccessToken         string
	Email               string
	Password            string
	AdminAPIKey         string
	Sub2APIAccessToken  string
	Sub2APIRefreshToken string
	LowBalanceThreshold float64
	FailureCount        int
}

type APIKey struct {
	RemoteID    string
	Name        string
	Key         string
	Status      string
	Description string
	Group       string
	GroupRatio  string
	Quota       float64
	UsedQuota   float64
}

type Balance struct {
	Balance  float64
	Used     float64
	Remain   float64
	Requests int
}

type RechargeMethod struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	MinAmount   float64        `json:"min_amount,omitempty"`
	MaxAmount   float64        `json:"max_amount,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
	Direct      bool           `json:"direct"`
	SDKOnly     bool           `json:"sdk_only,omitempty"`
	External    bool           `json:"external,omitempty"`
	ExternalURL string         `json:"external_url,omitempty"`
}

type RechargeCapabilities struct {
	OnlineEnabled bool             `json:"online_enabled"`
	RedeemEnabled bool             `json:"redeem_enabled"`
	ExternalURL   string           `json:"external_url,omitempty"`
	Methods       []RechargeMethod `json:"methods"`
}

type RechargeOrderRequest struct {
	Amount      float64 `json:"amount"`
	PaymentType string  `json:"payment_type"`
	ProductID   string  `json:"product_id,omitempty"`
}

type RechargeOrderResult struct {
	ResultType    string         `json:"result_type"`
	PaymentType   string         `json:"payment_type"`
	RemoteOrderID string         `json:"remote_order_id,omitempty"`
	Status        string         `json:"status,omitempty"`
	URL           string         `json:"url,omitempty"`
	QRCode        string         `json:"qr_code,omitempty"`
	Message       string         `json:"message,omitempty"`
	Raw           map[string]any `json:"raw,omitempty"`
}

type RevenueOrderTotal struct {
	Revenue   float64
	CheckedAt time.Time
}

type ProbeResult struct {
	HTTPStatus     int
	Latency        time.Duration
	Status         string
	Input          string
	ExpectedAnswer string
	Output         string
	Success        bool
	Error          string
}

type CheckResult struct {
	Balance Balance
	Keys    []APIKey
	Probe   ProbeResult
}
