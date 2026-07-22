package domain

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	BalanceGuardObserve = "observe"
	BalanceGuardActive  = "active"

	AvailabilityHealthy      = "healthy"
	AvailabilityWarning      = "warning"
	AvailabilitySuspect      = "suspect"
	AvailabilityBlocked      = "blocked"
	AvailabilityRecovering   = "recovering"
	AvailabilityForcedOn     = "forced_on"
	AvailabilityManualOff    = "manual_off"
	AvailabilityActionFailed = "action_failed"
	AvailabilityUnmanaged    = "unmanaged"
	AvailabilityExternalOff  = "external_disabled"

	BlockerBalanceLow     = "balance_low"
	BlockerQuotaExhausted = "quota_exhausted"
	BlockerProbeFailed    = "probe_failed"

	OverrideForceEnable = "force_enable"
	OverrideManualHold  = "manual_hold"

	AvailabilityActionEnable  = "enable"
	AvailabilityActionDisable = "disable"
)

const (
	AvailabilityRecoveryMinDuration = 15 * time.Minute
	AvailabilityRecoverySuccesses   = 3
)

// AvailabilityPolicy 是上游余额保护策略。金额均为余额页使用的折算元。
// low_balance_threshold 保留在 Upstream 中，作为预警线。
type AvailabilityPolicy struct {
	BalanceGuardMode        string  `json:"balance_guard_mode"`
	LowBalanceThreshold     float64 `json:"low_balance_threshold"`
	BalanceCloseThreshold   float64 `json:"balance_close_threshold"`
	BalanceRecoverThreshold float64 `json:"balance_recover_threshold"`
	RunwayWarningHours      float64 `json:"runway_warning_hours"`
}

func DefaultAvailabilityPolicy(lowBalanceThreshold float64) AvailabilityPolicy {
	return AvailabilityPolicy{
		BalanceGuardMode:    BalanceGuardObserve,
		LowBalanceThreshold: lowBalanceThreshold,
		RunwayWarningHours:  24,
	}
}

func (p AvailabilityPolicy) Normalized() AvailabilityPolicy {
	p.BalanceGuardMode = strings.ToLower(strings.TrimSpace(p.BalanceGuardMode))
	if p.BalanceGuardMode == "" {
		p.BalanceGuardMode = BalanceGuardObserve
	}
	if p.RunwayWarningHours <= 0 {
		p.RunwayWarningHours = 24
	}
	return p
}

func (p AvailabilityPolicy) BalanceGuardConfigured() bool {
	return p.BalanceCloseThreshold >= 0 && p.BalanceRecoverThreshold > p.BalanceCloseThreshold
}

func (p AvailabilityPolicy) BalanceGuardActive() bool {
	return p.Normalized().BalanceGuardMode == BalanceGuardActive && p.BalanceGuardConfigured()
}

func ValidateAvailabilityPolicy(p AvailabilityPolicy) error {
	p = p.Normalized()
	if p.BalanceGuardMode != BalanceGuardObserve && p.BalanceGuardMode != BalanceGuardActive {
		return errors.New("balance_guard_mode must be observe or active")
	}
	if p.LowBalanceThreshold < 0 || p.BalanceCloseThreshold < 0 || p.BalanceRecoverThreshold < 0 || p.RunwayWarningHours < 0 {
		return errors.New("余额策略阈值不能为负数")
	}
	// 新安装和旧数据的关闭/恢复线都为零时，代表尚未配置摘流线。
	if p.BalanceCloseThreshold == 0 && p.BalanceRecoverThreshold == 0 {
		return nil
	}
	if p.BalanceRecoverThreshold <= p.BalanceCloseThreshold {
		return errors.New("恢复线必须大于关闭线")
	}
	if p.LowBalanceThreshold < p.BalanceRecoverThreshold {
		return errors.New("预警线必须大于或等于恢复线")
	}
	return nil
}

type AvailabilityBlocker struct {
	Kind     string    `json:"kind"`
	Since    time.Time `json:"since"`
	Message  string    `json:"message,omitempty"`
	Observed bool      `json:"observed,omitempty"`
}

type ChannelAvailability struct {
	ChannelID       string                `json:"channel_id"`
	ChannelName     string                `json:"channel_name,omitempty"`
	CardID          string                `json:"card_id,omitempty"`
	CardName        string                `json:"card_name,omitempty"`
	UpstreamID      string                `json:"upstream_id,omitempty"`
	UpstreamName    string                `json:"upstream_name,omitempty"`
	Managed         bool                  `json:"managed"`
	Blockers        []AvailabilityBlocker `json:"blockers"`
	DesiredStatus   int                   `json:"desired_status"`
	ActualStatus    int                   `json:"actual_status"`
	DisabledAt      *time.Time            `json:"disabled_at,omitempty"`
	RecoverySuccess int                   `json:"recovery_success_count"`
	Override        string                `json:"override,omitempty"`
	OverrideUntil   *time.Time            `json:"override_until,omitempty"`
	PendingAction   string                `json:"pending_action,omitempty"`
	PendingStatus   int                   `json:"pending_status,omitempty"`
	RetryAt         *time.Time            `json:"retry_at,omitempty"`
	RetryCount      int                   `json:"retry_count"`
	LastError       string                `json:"last_error,omitempty"`
	Version         int64                 `json:"version"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

type AvailabilityDecision struct {
	State         string `json:"state"`
	DesiredStatus int    `json:"desired_status"`
	HardBlocked   bool   `json:"hard_blocked"`
	ObservedRisk  bool   `json:"observed_risk"`
}

type BalanceRunway struct {
	HoursRemaining float64 `json:"hours_remaining,omitempty"`
	RatePerHour    float64 `json:"rate_per_hour,omitempty"`
	Samples        int     `json:"samples"`
	Warning        bool    `json:"warning"`
}

// AvailabilityView 是管理端列表 DTO；公开状态页不会使用它。
type AvailabilityView struct {
	ChannelAvailability
	State         string        `json:"state"`
	BalanceFresh  bool          `json:"balance_fresh"`
	BalanceRemain float64       `json:"balance_remain,omitempty"`
	Runway        BalanceRunway `json:"runway"`
}

func UpsertBlocker(in []AvailabilityBlocker, blocker AvailabilityBlocker) []AvailabilityBlocker {
	for i := range in {
		if in[i].Kind == blocker.Kind {
			blocker.Since = in[i].Since
			if blocker.Since.IsZero() {
				blocker.Since = time.Now().UTC()
			}
			in[i] = blocker
			return NormalizeBlockers(in)
		}
	}
	if blocker.Since.IsZero() {
		blocker.Since = time.Now().UTC()
	}
	return NormalizeBlockers(append(in, blocker))
}

func RemoveBlocker(in []AvailabilityBlocker, kind string) []AvailabilityBlocker {
	out := in[:0]
	for _, blocker := range in {
		if blocker.Kind != kind {
			out = append(out, blocker)
		}
	}
	return NormalizeBlockers(out)
}

func NormalizeBlockers(in []AvailabilityBlocker) []AvailabilityBlocker {
	seen := map[string]AvailabilityBlocker{}
	for _, blocker := range in {
		blocker.Kind = strings.TrimSpace(blocker.Kind)
		if blocker.Kind == "" {
			continue
		}
		if old, ok := seen[blocker.Kind]; ok && !old.Since.IsZero() {
			blocker.Since = old.Since
		}
		seen[blocker.Kind] = blocker
	}
	out := make([]AvailabilityBlocker, 0, len(seen))
	for _, blocker := range seen {
		out = append(out, blocker)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

func HasBlocker(blockers []AvailabilityBlocker, kind string) bool {
	for _, blocker := range blockers {
		if blocker.Kind == kind {
			return true
		}
	}
	return false
}

func AvailabilityDecisionFor(now time.Time, policy AvailabilityPolicy, row ChannelAvailability) AvailabilityDecision {
	if !row.Managed {
		return AvailabilityDecision{State: AvailabilityUnmanaged, DesiredStatus: row.ActualStatus}
	}
	if row.Override == OverrideManualHold {
		return AvailabilityDecision{State: AvailabilityManualOff, DesiredStatus: 2}
	}
	if row.Override == OverrideForceEnable && row.OverrideUntil != nil && now.Before(*row.OverrideUntil) {
		return AvailabilityDecision{State: AvailabilityForcedOn, DesiredStatus: 1}
	}
	// 状态 3 是调度器自身的自动禁用；由调度器的被动恢复机制负责重新启用。
	if row.ActualStatus == 3 {
		return AvailabilityDecision{State: AvailabilityExternalOff, DesiredStatus: 3}
	}
	hard, observed := false, false
	for _, blocker := range row.Blockers {
		switch blocker.Kind {
		case BlockerProbeFailed, BlockerQuotaExhausted:
			hard = true
		case BlockerBalanceLow:
			if policy.BalanceGuardActive() {
				hard = true
			} else {
				observed = true
			}
		}
	}
	if hard {
		return AvailabilityDecision{State: AvailabilityBlocked, DesiredStatus: 2, HardBlocked: true, ObservedRisk: observed}
	}
	if len(row.Blockers) > 0 || observed {
		return AvailabilityDecision{State: AvailabilityWarning, DesiredStatus: 1, ObservedRisk: true}
	}
	if row.DisabledAt != nil || row.ActualStatus == 3 {
		return AvailabilityDecision{State: AvailabilityRecovering, DesiredStatus: 1}
	}
	if row.LastError != "" {
		return AvailabilityDecision{State: AvailabilityActionFailed, DesiredStatus: 1}
	}
	return AvailabilityDecision{State: AvailabilityHealthy, DesiredStatus: 1}
}

func FreshBalance(snapshot BalanceSnapshot, now time.Time, interval time.Duration) bool {
	if snapshot.CheckedAt.IsZero() || snapshot.Error != "" || interval <= 0 {
		return false
	}
	return !snapshot.CheckedAt.After(now) && now.Sub(snapshot.CheckedAt) <= 2*interval
}

func ApplyBalanceBlocker(blockers []AvailabilityBlocker, policy AvailabilityPolicy, remain float64, fresh bool, now time.Time) []AvailabilityBlocker {
	if !policy.BalanceGuardConfigured() || !fresh {
		return NormalizeBlockers(blockers)
	}
	if remain <= policy.BalanceCloseThreshold {
		return UpsertBlocker(blockers, AvailabilityBlocker{Kind: BlockerBalanceLow, Since: now, Message: "余额达到关闭线", Observed: policy.Normalized().BalanceGuardMode == BalanceGuardObserve})
	}
	if remain >= policy.BalanceRecoverThreshold {
		return RemoveBlocker(blockers, BlockerBalanceLow)
	}
	return NormalizeBlockers(blockers)
}

func RecoveryEligible(now time.Time, row ChannelAvailability, policy AvailabilityPolicy, freshBalance bool, remain float64) bool {
	if len(row.Blockers) != 0 || row.DisabledAt == nil || now.Sub(*row.DisabledAt) < AvailabilityRecoveryMinDuration || row.RecoverySuccess < AvailabilityRecoverySuccesses {
		return false
	}
	if policy.BalanceGuardActive() && (!freshBalance || remain < policy.BalanceRecoverThreshold) {
		return false
	}
	return true
}

// PredictBalanceRunway 只使用下降区间，取相邻成功快照消耗速度的中位数。
func PredictBalanceRunway(snapshots []BalanceSnapshot, warningHours float64) BalanceRunway {
	if warningHours <= 0 {
		warningHours = 24
	}
	valid := make([]BalanceSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Error == "" && !snapshot.CheckedAt.IsZero() {
			valid = append(valid, snapshot)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].CheckedAt.Before(valid[j].CheckedAt) })
	rates := make([]float64, 0, len(valid))
	for i := 1; i < len(valid); i++ {
		span := valid[i].CheckedAt.Sub(valid[i-1].CheckedAt)
		spent := valid[i-1].Remain - valid[i].Remain
		if span < 30*time.Minute || spent <= 0 {
			continue
		}
		rates = append(rates, spent/span.Hours())
	}
	if len(rates) < 3 || len(valid) == 0 {
		return BalanceRunway{Samples: len(rates)}
	}
	sort.Float64s(rates)
	rate := rates[len(rates)/2]
	if len(rates)%2 == 0 {
		rate = (rates[len(rates)/2-1] + rates[len(rates)/2]) / 2
	}
	if rate <= 0 {
		return BalanceRunway{Samples: len(rates)}
	}
	hours := valid[len(valid)-1].Remain / rate
	return BalanceRunway{HoursRemaining: hours, RatePerHour: rate, Samples: len(rates), Warning: hours <= warningHours}
}
