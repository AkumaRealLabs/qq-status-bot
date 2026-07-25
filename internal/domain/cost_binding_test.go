package domain

import "testing"

func TestCostPerUnit(t *testing.T) {
	if value, reason := CostPerUnitFromManual("0.14"); value != 0.14 || reason != "" {
		t.Fatalf("manual value=%v reason=%q", value, reason)
	}
	if _, reason := CostPerUnitFromManual(""); reason == "" {
		t.Fatal("空手动成本应返回缺失原因")
	}
	if value, reason := CostPerUnitFromUpstreamKey("0.045", 2); value != 0.09 || reason != "" {
		t.Fatalf("upstream value=%v reason=%q", value, reason)
	}
	if _, reason := CostPerUnitFromUpstreamKey("x", 1); reason == "" {
		t.Fatal("无效 Key 成本应返回缺失原因")
	}
}

func TestEffectiveRatio(t *testing.T) {
	if got := EffectiveRatio("0.045", 2); got != "0.09" {
		t.Fatalf("got %q", got)
	}
	if got := EffectiveRatio("custom", 2); got != "custom" {
		t.Fatalf("got %q", got)
	}
}
