package domain

import (
	"fmt"
	"strings"
	"time"
)

// 跑道估算的取样窗口与最小样本跨度。
// 窗口取近 24 小时：太短受单次波动影响大，太长会把昨天的高峰摊平。
// 跨度不足 1 小时（例如服务刚重启、上游刚创建）时不做判断，避免噪音告警。
const (
	RunwayWindow       = 24 * time.Hour
	RunwayMinimumSpan  = time.Hour
	RunwayAlertKind    = "runway:balance"
	defaultRunwayHours = 24
)

// RunwayEstimate 是余额跑道估算结果，数值均为换算后（余额倍率、newapi 额度归一）的口径。
// Valid 与 Burning 分开：样本不足时完全跳过判断；样本足够但余额没在净消耗
// （刚充值、零流量）时属于「健康」，要让已激活的告警恢复。
type RunwayEstimate struct {
	Valid       bool    `json:"valid"`         // 样本足够（≥2 个有效点且跨度 ≥ 1 小时）
	Burning     bool    `json:"burning"`       // 余额在净消耗
	BurnPerHour float64 `json:"burn_per_hour"` // 每小时净消耗；仅 Burning 时有意义
	HoursLeft   float64 `json:"hours_left"`    // 预计还能支撑的小时数；仅 Burning 时有意义
}

// EstimateRunway 用窗口内最早与最新的有效快照估算消耗速率。
// 快照可乱序传入；带 error 的快照不参与。
func EstimateRunway(u Upstream, history []BalanceSnapshot) RunwayEstimate {
	var oldest, newest BalanceSnapshot
	found := false
	for _, snap := range history {
		if strings.TrimSpace(snap.Error) != "" || snap.CheckedAt.IsZero() {
			continue
		}
		if !found {
			oldest, newest, found = snap, snap, true
			continue
		}
		if snap.CheckedAt.Before(oldest.CheckedAt) {
			oldest = snap
		}
		if snap.CheckedAt.After(newest.CheckedAt) {
			newest = snap
		}
	}
	if !found {
		return RunwayEstimate{}
	}
	span := newest.CheckedAt.Sub(oldest.CheckedAt)
	if span < RunwayMinimumSpan {
		return RunwayEstimate{}
	}
	rate := BalanceRate(u)
	_, _, remainOld := ConvertedBalanceValues(u.Type, rate, oldest.Balance, oldest.Used, oldest.Remain)
	_, _, remainNew := ConvertedBalanceValues(u.Type, rate, newest.Balance, newest.Used, newest.Remain)
	burn := (remainOld - remainNew) / span.Hours()
	if burn <= 0 {
		return RunwayEstimate{Valid: true}
	}
	return RunwayEstimate{Valid: true, Burning: true, BurnPerHour: burn, HoursLeft: remainNew / burn}
}

// RunwayWarnHours 返回生效的跑道告警阈值；≤0 时与 store 的落库口径一致回落到 24。
func RunwayWarnHours(u Upstream) float64 {
	if u.RunwayWarningHours <= 0 {
		return defaultRunwayHours
	}
	return u.RunwayWarningHours
}

// RunwayLow 判断是否应触发「余额预计耗尽」告警。
func RunwayLow(u Upstream, est RunwayEstimate) bool {
	return est.Valid && est.Burning && est.HoursLeft <= RunwayWarnHours(u)
}

// RunwayAlertMessage 生成告警文案。
func RunwayAlertMessage(name string, est RunwayEstimate) string {
	return fmt.Sprintf("%s 余额预计 %.1f 小时后耗尽（每小时约消耗 %.2f）", name, est.HoursLeft, est.BurnPerHour)
}

// AlertRecoverMessage 把告警 kind 翻译成可读的恢复文案，
// 避免通知里出现「上游A runway:balance 已恢复」这类内部标识。
func AlertRecoverMessage(name, kind string) string {
	label := kind
	switch {
	case kind == "balance":
		label = "余额"
	case kind == "credential":
		label = "凭据"
	case kind == "balance_query":
		label = "额度查询"
	case strings.HasPrefix(kind, "runway:"):
		label = "余额消耗速度"
	}
	return name + " " + label + " 已恢复"
}
