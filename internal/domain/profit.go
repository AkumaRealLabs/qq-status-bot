package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// UsageUnits 将调度配额 + 分组倍率换算为售卖/成本用量单位。
// 公式：quota / 500000 / groupRatio
func UsageUnits(quota, groupRatio float64) (float64, bool) {
	if groupRatio <= 0 || quota < 0 {
		return 0, false
	}
	return quota / 500000 / groupRatio, true
}

// LineProfit 计算一行用量的收入、成本与利润。
func LineProfit(usage, salePrice, costPerUnit float64) (revenue, cost, profit float64) {
	revenue = usage * salePrice
	cost = usage * costPerUnit
	profit = revenue - cost
	return revenue, cost, profit
}

// MergeMeta 合并快照元数据标签；不同的非空值记为 "mixed"。
func MergeMeta(old, next string) string {
	next = strings.TrimSpace(next)
	if old == "" {
		return next
	}
	if next == "" || old == next {
		return old
	}
	return "mixed"
}

// FirstNonEmpty 返回 values 中第一个非空白字符串。
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// BalanceConsumptionCost 对有序快照中 remain 的正减少量求和
// （经类型相关规范化与余额费率换算）。
func BalanceConsumptionCost(upstreamType string, rate float64, snaps []BalanceSnapshot) float64 {
	var cost float64
	var prev float64
	for i, snap := range snaps {
		_, _, remain := ConvertedBalanceValues(upstreamType, rate, snap.Balance, snap.Used, snap.Remain)
		if i > 0 && remain < prev {
			cost += prev - remain
		}
		prev = remain
	}
	return cost
}

// CostPerUnitFromManual 解析手动成本倍率字符串。
// 成功返回 (cost, "")，无效时返回 (0, missingReason)。
func CostPerUnitFromManual(ratio string) (float64, string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(ratio), 64)
	if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, "缺手动成本"
	}
	return v, ""
}

// CostPerUnitFromUpstreamKey：Key 分组倍率 × 余额费率。
// 成功返回 (cost, "")，无效时返回 (0, missingReason)。
func CostPerUnitFromUpstreamKey(groupRatio string, balanceRate float64) (float64, string) {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(groupRatio), 64)
	if err != nil || ratio <= 0 || balanceRate <= 0 {
		return 0, "缺成本倍率"
	}
	return ratio * balanceRate, ""
}

// EffectiveRatio 将 groupRatio * balanceRate 格式化为展示用；非数字输入原样返回。
func EffectiveRatio(groupRatio string, balanceRate float64) string {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(groupRatio), 64)
	if err != nil {
		return groupRatio
	}
	out := fmt.Sprintf("%.6f", ratio*balanceRate)
	return strings.TrimRight(strings.TrimRight(out, "0"), ".")
}
