package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	TrafficModeOff         = "off"
	TrafficModeObserve     = "observe"
	TrafficModeActive      = "active"
	TrafficProfileBalanced = "balanced"

	TrafficEventSuccess     = "success"
	TrafficEventSoftFailure = "soft_failure"
	TrafficEventHardFailure = "hard_failure"
	TrafficEventUserError   = "user_error"
)

// TrafficEvent 是脱敏后的单次请求尝试。禁止把原始错误、请求体、token、IP 或用户名写入这里。
type TrafficEvent struct {
	ID                string    `json:"id"`
	DedupeKey         string    `json:"-"`
	Source            string    `json:"source"`
	OccurredAt        time.Time `json:"occurred_at"`
	ChannelID         string    `json:"channel_id"`
	ChannelName       string    `json:"channel_name,omitempty"`
	Model             string    `json:"model"`
	Group             string    `json:"group,omitempty"`
	RequestID         string    `json:"request_id,omitempty"`
	UpstreamRequestID string    `json:"upstream_request_id,omitempty"`
	Kind              string    `json:"kind"`
	HTTPStatus        int       `json:"http_status,omitempty"`
	ErrorType         string    `json:"error_type,omitempty"`
	ErrorCode         string    `json:"error_code,omitempty"`
	DurationMS        int       `json:"duration_ms,omitempty"`
	TTFTMS            int       `json:"ttft_ms,omitempty"`
	StreamEnded       bool      `json:"stream_ended"`
	Tokens            int64     `json:"tokens,omitempty"`
	RetryCount        int       `json:"retry_count,omitempty"`
	RetrySucceeded    bool      `json:"retry_succeeded,omitempty"`
}

// TrafficWindow 是渠道×模型在一个窗口内的统计。
type TrafficWindow struct {
	ChannelID     string    `json:"channel_id"`
	ChannelName   string    `json:"channel_name,omitempty"`
	Model         string    `json:"model"`
	Group         string    `json:"group,omitempty"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	Requests      int       `json:"requests"`
	Successes     int       `json:"successes"`
	SoftFailures  int       `json:"soft_failures"`
	HardFailures  int       `json:"hard_failures"`
	AuthFailures  int       `json:"auth_failures"`
	UserErrors    int       `json:"user_errors"`
	P95TTFTMS     int       `json:"p95_ttft_ms"`
	AvgTTFTMS     int       `json:"avg_ttft_ms"`
	FailureRate   float64   `json:"failure_rate"`
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`
}

type TrafficChannelState struct {
	ChannelID             string         `json:"channel_id"`
	ChannelName           string         `json:"channel_name,omitempty"`
	Managed               bool           `json:"managed"`
	State                 string         `json:"state"`
	Reason                string         `json:"reason,omitempty"`
	DesiredStatus         int            `json:"desired_status"`
	ActualStatus          int            `json:"actual_status"`
	BasePriority          int64          `json:"base_priority"`
	ActualPriority        int64          `json:"actual_priority"`
	BaseWeight            uint           `json:"base_weight"`
	ActualWeight          uint           `json:"actual_weight"`
	HealthScore           float64        `json:"health_score"`
	HealthyBaselineTTFTMS int            `json:"healthy_baseline_ttft_ms,omitempty"`
	Model                 string         `json:"model,omitempty"`
	Window15s             *TrafficWindow `json:"window_15s,omitempty"`
	Window1m              *TrafficWindow `json:"window_1m,omitempty"`
	Window5m              *TrafficWindow `json:"window_5m,omitempty"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type TrafficStatus struct {
	Mode         string                `json:"mode"`
	Profile      string                `json:"profile"`
	Connected    bool                  `json:"connected"`
	LastPollAt   time.Time             `json:"last_poll_at,omitempty"`
	LastEventAt  time.Time             `json:"last_event_at,omitempty"`
	LagSeconds   int                   `json:"lag_seconds"`
	BacklogPages int                   `json:"backlog_pages"`
	Frozen       bool                  `json:"frozen"`
	FreezeReason string                `json:"freeze_reason,omitempty"`
	Channels     []TrafficChannelState `json:"channels"`
}

type TrafficControlState struct {
	ChannelID         string    `json:"channel_id"`
	BasePriority      int64     `json:"base_priority"`
	BaseWeight        uint      `json:"base_weight"`
	DesiredPriority   int64     `json:"desired_priority"`
	DesiredWeight     uint      `json:"desired_weight"`
	ActualPriority    int64     `json:"actual_priority"`
	ActualWeight      uint      `json:"actual_weight"`
	DesiredStatus     int       `json:"desired_status"`
	ActualStatus      int       `json:"actual_status"`
	State             string    `json:"state"`
	Reason            string    `json:"reason,omitempty"`
	FailureWindows    int       `json:"failure_windows"`
	RecoveryStage     int       `json:"recovery_stage"`
	CooldownUntil     time.Time `json:"cooldown_until,omitempty"`
	LastProbeAt       time.Time `json:"last_probe_at,omitempty"`
	RecoverySuccesses int       `json:"recovery_successes"`
	StageChangedAt    time.Time `json:"stage_changed_at,omitempty"`
	RetryAt           time.Time `json:"retry_at,omitempty"`
	RetryCount        int       `json:"retry_count"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type TrafficCursor struct {
	Source       string    `json:"source"`
	CursorAt     time.Time `json:"cursor_at,omitempty"`
	ScanStartAt  time.Time `json:"scan_start_at,omitempty"`
	ScanEndAt    time.Time `json:"scan_end_at,omitempty"`
	NextPage     int       `json:"next_page,omitempty"`
	LastPollAt   time.Time `json:"last_poll_at,omitempty"`
	LastEventAt  time.Time `json:"last_event_at,omitempty"`
	BacklogPages int       `json:"backlog_pages"`
	LastError    string    `json:"last_error,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func NormalizeTrafficMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case TrafficModeObserve, TrafficModeActive:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return TrafficModeOff
	}
}

func NormalizeTrafficProfile(profile string) string {
	if strings.TrimSpace(profile) == "" {
		return TrafficProfileBalanced
	}
	return strings.ToLower(strings.TrimSpace(profile))
}

func NormalizeTrafficPollSeconds(seconds int) int {
	if seconds < 1 {
		return 5
	}
	if seconds > 60 {
		return 60
	}
	return seconds
}

func ValidateTrafficConfig(mode, profile string, pollSeconds int) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != TrafficModeOff && mode != TrafficModeObserve && mode != TrafficModeActive {
		return errors.New("scheduler_traffic_mode must be off, observe or active")
	}
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile != "" && profile != TrafficProfileBalanced {
		return errors.New("scheduler_traffic_profile must be balanced")
	}
	if pollSeconds < 0 || pollSeconds > 60 {
		return errors.New("scheduler_log_poll_seconds must be between 1 and 60")
	}
	return nil
}

// ClassifyTrafficError 只返回结构化分类；errorText 仅用于瞬时分类，不得持久化。
func ClassifyTrafficError(status int, errorType, errorCode, errorText string) string {
	joined := strings.ToLower(strings.TrimSpace(errorType + " " + errorCode + " " + errorText))
	for _, needle := range []string{"quota_exhausted", "insufficient_quota", "insufficient_balance", "invalid_api_key", "account_disabled", "account_deactivated", "余额不足", "额度不足"} {
		if strings.Contains(joined, needle) {
			return TrafficEventHardFailure
		}
	}
	for _, needle := range []string{"context_length", "context_window", "content_policy", "content_filter", "invalid_request", "prompt_too_long"} {
		if strings.Contains(joined, needle) {
			return TrafficEventUserError
		}
	}
	if status == 401 || status == 403 {
		return TrafficEventSoftFailure
	}
	if status >= 500 || status == 429 || status == 408 || status == 0 {
		return TrafficEventSoftFailure
	}
	if status >= 400 && status < 500 {
		return TrafficEventUserError
	}
	if joined != "" {
		return TrafficEventSoftFailure
	}
	return TrafficEventSuccess
}

func TrafficDedupeKey(event TrafficEvent) string {
	value := strings.Join([]string{
		event.Source, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.RequestID, event.ChannelID, event.Kind,
		event.UpstreamRequestID, event.Model, event.ErrorType, event.ErrorCode, strconv.Itoa(event.HTTPStatus),
		strconv.Itoa(event.DurationMS), strconv.Itoa(event.TTFTMS), strconv.FormatBool(event.StreamEnded), strconv.FormatInt(event.Tokens, 10),
		strconv.Itoa(event.RetryCount), strconv.FormatBool(event.RetrySucceeded),
	}, "|")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func AggregateTraffic(events []TrafficEvent, start, end time.Time) []TrafficWindow {
	type key struct{ channel, model string }
	groups := map[key]*TrafficWindow{}
	ttfts := map[key][]int{}
	for _, event := range events {
		if event.OccurredAt.Before(start) || !event.OccurredAt.Before(end) {
			continue
		}
		k := key{event.ChannelID, NormalizeProbeModel(event.Model)}
		row := groups[k]
		if row == nil {
			row = &TrafficWindow{ChannelID: event.ChannelID, ChannelName: event.ChannelName, Model: k.model, Group: event.Group, WindowStart: start, WindowEnd: end}
			groups[k] = row
		}
		row.Requests++
		switch event.Kind {
		case TrafficEventSuccess:
			row.Successes++
			if event.OccurredAt.After(row.LastSuccessAt) {
				row.LastSuccessAt = event.OccurredAt
			}
		case TrafficEventSoftFailure:
			row.SoftFailures++
			if event.HTTPStatus == 401 || event.HTTPStatus == 403 {
				row.AuthFailures++
			}
		case TrafficEventHardFailure:
			row.HardFailures++
		case TrafficEventUserError:
			row.UserErrors++
		}
		if event.TTFTMS > 0 {
			ttfts[k] = append(ttfts[k], event.TTFTMS)
		}
	}
	out := make([]TrafficWindow, 0, len(groups))
	for _, row := range groups {
		values := ttfts[key{row.ChannelID, row.Model}]
		if len(values) > 0 {
			sort.Ints(values)
			sum := 0
			for _, value := range values {
				sum += value
			}
			row.AvgTTFTMS = sum / len(values)
			index := (95*len(values)+99)/100 - 1
			if index < 0 {
				index = 0
			}
			row.P95TTFTMS = values[index]
		}
		denom := row.Requests - row.UserErrors
		if denom > 0 {
			row.FailureRate = float64(row.SoftFailures+row.HardFailures) / float64(denom)
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChannelID == out[j].ChannelID {
			return out[i].Model < out[j].Model
		}
		return out[i].ChannelID < out[j].ChannelID
	})
	return out
}

// TrafficDecision 按最坏模型决定渠道动作；软故障无替代渠道时最多降到 10%。
func TrafficDecision(window15s, window1m, window5m TrafficWindow, alternativeHealthy bool, now time.Time) (state string, status int, priority int64, weight uint, reason string, score float64) {
	status, priority, weight = 1, 0, 100
	samples15s := window15s.Requests - window15s.UserErrors
	samples1m := window1m.Requests - window1m.UserErrors
	score = 100
	if samples1m > 0 {
		score -= float64(window1m.SoftFailures) / float64(samples1m) * 100
	}
	if window1m.P95TTFTMS > 0 {
		score -= float64(window1m.P95TTFTMS) / 1000
	}
	if score < 0 {
		score = 0
	}
	if window15s.HardFailures > 0 || window1m.HardFailures > 0 {
		return "hard_blocked", 2, -2000, 0, "结构化硬故障", 0
	}
	if window15s.AuthFailures >= 2 {
		return "hard_blocked", 2, -2000, 0, "15 秒内两次上游鉴权失败", 0
	}
	if samples15s >= 10 && window15s.FailureRate >= 0.8 {
		if !alternativeHealthy {
			return "degraded", 1, -2000, 10, "末路保护：严重退化但保留最后候选", score
		}
		return "soft_blocked", 2, -2000, 0, "15 秒严重退化", 0
	}
	if samples1m >= 10 && window1m.FailureRate >= 0.5 {
		return "degraded", 1, -2000, 10, "1 分钟失败率 >= 50%", score
	}
	if samples15s >= 5 && window15s.FailureRate >= 0.3 {
		if !alternativeHealthy {
			return "degraded", 1, -2000, 10, "末路保护：保留最后候选", score
		}
		return "warning", 1, -1000, 50, "15 秒失败率 >= 30%", score
	}
	if samples15s >= 3 && window15s.SoftFailures+window15s.HardFailures == samples15s {
		return "probe_required", 1, 0, 100, "低流量连续三次失败，等待主动探测确认", score
	}
	if samples1m > 0 && window5m.P95TTFTMS > 0 && window1m.P95TTFTMS >= 2*window5m.P95TTFTMS && window1m.P95TTFTMS-window5m.P95TTFTMS >= 1000 {
		if !alternativeHealthy {
			return "degraded", 1, -2000, 10, "末路保护：延迟退化但保留最后候选", score
		}
		return "warning", 1, -1000, 50, "延迟相对健康基准退化", score
	}
	_ = now
	return "healthy", status, priority, weight, "健康", score
}

// TrafficRecoveryTarget 返回 10% -> 25% -> 50% -> 100% 的恢复阶梯。
func TrafficRecoveryTarget(stage int, changedAt, now time.Time) (nextStage int, weightPercent uint, complete bool) {
	if stage <= 0 {
		return 1, 10, false
	}
	elapsed := now.Sub(changedAt)
	switch stage {
	case 1:
		if elapsed >= time.Minute {
			return 2, 25, false
		}
		return 1, 10, false
	case 2:
		if elapsed >= time.Minute {
			return 3, 50, false
		}
		return 2, 25, false
	case 3:
		if elapsed >= 2*time.Minute {
			return 4, 100, true
		}
		return 3, 50, false
	default:
		return 4, 100, true
	}
}
