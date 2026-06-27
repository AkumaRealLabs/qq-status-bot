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

type ProbeResult struct {
	HTTPStatus int
	Latency    time.Duration
	Status     string
	Input      string
	Output     string
	Success    bool
	Error      string
}

type CheckResult struct {
	Balance Balance
	Keys    []APIKey
	Probe   ProbeResult
}
