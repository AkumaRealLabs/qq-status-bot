package domain

import "testing"

func TestUsageUnits(t *testing.T) {
	got, ok := UsageUnits(50000, 0.1)
	if !ok || got != 1 {
		t.Fatalf("usage=%v ok=%v want 1,true", got, ok)
	}
	if _, ok := UsageUnits(100, 0); ok {
		t.Fatal("groupRatio 0 should fail")
	}
	if _, ok := UsageUnits(-1, 1); ok {
		t.Fatal("negative quota should fail")
	}
}

func TestLineProfit(t *testing.T) {
	rev, cost, profit := LineProfit(10, 0.2, 0.14)
	if rev != 2 {
		t.Fatalf("rev=%v want 2", rev)
	}
	if cost < 1.399 || cost > 1.401 {
		t.Fatalf("cost=%v want ~1.4", cost)
	}
	if profit < 0.599 || profit > 0.601 {
		t.Fatalf("profit=%v want ~0.6", profit)
	}
}

func TestMergeMeta(t *testing.T) {
	if got := MergeMeta("", "a"); got != "a" {
		t.Fatalf("got %q", got)
	}
	if got := MergeMeta("a", "a"); got != "a" {
		t.Fatalf("got %q", got)
	}
	if got := MergeMeta("a", "b"); got != "mixed" {
		t.Fatalf("got %q", got)
	}
	if got := MergeMeta("a", ""); got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("", "  ", "x"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := FirstNonEmpty(); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestBalanceConsumptionCost(t *testing.T) {
	snaps := []BalanceSnapshot{
		{Remain: 100},
		{Remain: 80},
		{Remain: 90},
		{Remain: 50},
	}
	// 增量：20 + 0 + 40 = 60（只计减少）
	if got := BalanceConsumptionCost("other", 1, snaps); got != 60 {
		t.Fatalf("cost=%v want 60", got)
	}
	// newapi 在费率前先除以 500000
	snaps2 := []BalanceSnapshot{{Remain: 500000}, {Remain: 250000}}
	if got := BalanceConsumptionCost("newapi", 2, snaps2); got != 1 {
		t.Fatalf("newapi cost=%v want 1", got)
	}
}

func TestCostPerUnitHelpers(t *testing.T) {
	if v, reason := CostPerUnitFromManual("0.14"); v != 0.14 || reason != "" {
		t.Fatalf("manual v=%v reason=%q", v, reason)
	}
	if _, reason := CostPerUnitFromManual(""); reason == "" {
		t.Fatal("empty manual should miss")
	}
	if v, reason := CostPerUnitFromUpstreamKey("0.045", 2); v != 0.09 || reason != "" {
		t.Fatalf("key v=%v reason=%q", v, reason)
	}
	if _, reason := CostPerUnitFromUpstreamKey("x", 1); reason == "" {
		t.Fatal("bad ratio should miss")
	}
}

func TestEffectiveRatio(t *testing.T) {
	if got := EffectiveRatio("0.045", 2); got != "0.09" {
		t.Fatalf("ratio=%q", got)
	}
	if got := EffectiveRatio("custom", 2); got != "custom" {
		t.Fatalf("non-numeric=%q", got)
	}
}
