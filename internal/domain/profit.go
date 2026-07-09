package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// UsageUnits converts scheduler quota + group ratio into sale/cost units.
// Formula: quota / 500000 / groupRatio
func UsageUnits(quota, groupRatio float64) (float64, bool) {
	if groupRatio <= 0 || quota < 0 {
		return 0, false
	}
	return quota / 500000 / groupRatio, true
}

// LineProfit computes revenue, cost, and profit for a usage line.
func LineProfit(usage, salePrice, costPerUnit float64) (revenue, cost, profit float64) {
	revenue = usage * salePrice
	cost = usage * costPerUnit
	profit = revenue - cost
	return revenue, cost, profit
}

// MergeMeta merges snapshot metadata labels; differing non-empty values become "mixed".
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

// FirstNonEmpty returns the first non-blank string among values.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// BalanceConsumptionCost sums positive remain decreases across ordered snapshots
// (converted with type-specific normalization and balance rate).
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

// CostPerUnitFromManual parses a manual cost ratio string.
// Returns (cost, "") on success or (0, missingReason) when invalid.
func CostPerUnitFromManual(ratio string) (float64, string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(ratio), 64)
	if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, "缺手动成本"
	}
	return v, ""
}

// CostPerUnitFromUpstreamKey multiplies key group ratio by balance rate.
// Returns (cost, "") on success or (0, missingReason) when invalid.
func CostPerUnitFromUpstreamKey(groupRatio string, balanceRate float64) (float64, string) {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(groupRatio), 64)
	if err != nil || ratio <= 0 || balanceRate <= 0 {
		return 0, "缺成本倍率"
	}
	return ratio * balanceRate, ""
}

// EffectiveRatio formats groupRatio * balanceRate for display; non-numeric input is returned as-is.
func EffectiveRatio(groupRatio string, balanceRate float64) string {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(groupRatio), 64)
	if err != nil {
		return groupRatio
	}
	out := fmt.Sprintf("%.6f", ratio*balanceRate)
	return strings.TrimRight(strings.TrimRight(out, "0"), ".")
}
