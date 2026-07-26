package domain

import (
	"math"
	"testing"
	"time"
)

func runwaySnap(at time.Time, remain float64, errText string) BalanceSnapshot {
	return BalanceSnapshot{CheckedAt: at, Remain: remain, Error: errText}
}

func TestEstimateRunway(t *testing.T) {
	base := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	u := Upstream{Type: "sub2api", BalanceRate: 1}
	for _, tc := range []struct {
		name        string
		upstream    Upstream
		history     []BalanceSnapshot
		wantValid   bool
		wantBurning bool
		wantBurn    float64
		wantHours   float64
	}{
		{
			name:     "匀速消耗：12 小时烧 6，剩 4 → 8 小时",
			upstream: u,
			history: []BalanceSnapshot{
				runwaySnap(base, 10, ""),
				runwaySnap(base.Add(12*time.Hour), 4, ""),
			},
			wantValid: true, wantBurning: true, wantBurn: 0.5, wantHours: 8,
		},
		{
			name:     "乱序传入结果一致",
			upstream: u,
			history: []BalanceSnapshot{
				runwaySnap(base.Add(12*time.Hour), 4, ""),
				runwaySnap(base.Add(6*time.Hour), 7, ""),
				runwaySnap(base, 10, ""),
			},
			wantValid: true, wantBurning: true, wantBurn: 0.5, wantHours: 8,
		},
		{
			name:     "带 error 的快照不参与",
			upstream: u,
			history: []BalanceSnapshot{
				runwaySnap(base, 10, ""),
				runwaySnap(base.Add(11*time.Hour), 0, "查询失败"),
				runwaySnap(base.Add(12*time.Hour), 4, ""),
			},
			wantValid: true, wantBurning: true, wantBurn: 0.5, wantHours: 8,
		},
		{
			name:      "只有一个点：无效",
			upstream:  u,
			history:   []BalanceSnapshot{runwaySnap(base, 10, "")},
			wantValid: false,
		},
		{
			name:     "跨度不足 1 小时：无效",
			upstream: u,
			history: []BalanceSnapshot{
				runwaySnap(base, 10, ""),
				runwaySnap(base.Add(30*time.Minute), 9, ""),
			},
			wantValid: false,
		},
		{
			name:     "余额回升（充值）：有效但不在消耗",
			upstream: u,
			history: []BalanceSnapshot{
				runwaySnap(base, 4, ""),
				runwaySnap(base.Add(2*time.Hour), 10, ""),
			},
			wantValid: true, wantBurning: false,
		},
		{
			name:     "余额没动：有效但不在消耗",
			upstream: u,
			history: []BalanceSnapshot{
				runwaySnap(base, 10, ""),
				runwaySnap(base.Add(6*time.Hour), 10, ""),
			},
			wantValid: true, wantBurning: false,
		},
		{
			name:      "空历史：无效",
			upstream:  u,
			history:   nil,
			wantValid: false,
		},
		{
			name:     "newapi 额度归一后按倍率换算",
			upstream: Upstream{Type: "newapi", BalanceRate: 2},
			history: []BalanceSnapshot{
				// 原始额度 5000000 → 归一 10 → ×2 = 20；1000000 → 归一 2 → ×2 = 4
				runwaySnap(base, 5000000, ""),
				runwaySnap(base.Add(8*time.Hour), 1000000, ""),
			},
			wantValid: true, wantBurning: true, wantBurn: 2, wantHours: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateRunway(tc.upstream, tc.history)
			if got.Valid != tc.wantValid || got.Burning != tc.wantBurning {
				t.Fatalf("Valid=%v Burning=%v, want Valid=%v Burning=%v (%+v)", got.Valid, got.Burning, tc.wantValid, tc.wantBurning, got)
			}
			if !tc.wantBurning {
				return
			}
			if math.Abs(got.BurnPerHour-tc.wantBurn) > 1e-9 || math.Abs(got.HoursLeft-tc.wantHours) > 1e-9 {
				t.Fatalf("got burn=%.4f hours=%.4f, want burn=%.4f hours=%.4f", got.BurnPerHour, got.HoursLeft, tc.wantBurn, tc.wantHours)
			}
		})
	}
}

func TestRunwayLow(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    Upstream
		est  RunwayEstimate
		want bool
	}{
		{"低于阈值触发", Upstream{RunwayWarningHours: 24}, RunwayEstimate{Valid: true, Burning: true, HoursLeft: 8}, true},
		{"高于阈值不触发", Upstream{RunwayWarningHours: 24}, RunwayEstimate{Valid: true, Burning: true, HoursLeft: 48}, false},
		{"无效估算不触发", Upstream{RunwayWarningHours: 24}, RunwayEstimate{Valid: false, Burning: true, HoursLeft: 1}, false},
		{"没在消耗不触发", Upstream{RunwayWarningHours: 24}, RunwayEstimate{Valid: true, Burning: false}, false},
		{"阈值 0 回落默认 24", Upstream{}, RunwayEstimate{Valid: true, Burning: true, HoursLeft: 20}, true},
		{"恰好等于阈值触发", Upstream{RunwayWarningHours: 8}, RunwayEstimate{Valid: true, Burning: true, HoursLeft: 8}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RunwayLow(tc.u, tc.est); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAlertRecoverMessage(t *testing.T) {
	for kind, want := range map[string]string{
		"balance":        "上游A 余额 已恢复",
		"credential":     "上游A 凭据 已恢复",
		"balance_query":  "上游A 额度查询 已恢复",
		"runway:balance": "上游A 余额消耗速度 已恢复",
		"other":          "上游A other 已恢复",
	} {
		if got := AlertRecoverMessage("上游A", kind); got != want {
			t.Fatalf("kind %q: got %q, want %q", kind, got, want)
		}
	}
}
