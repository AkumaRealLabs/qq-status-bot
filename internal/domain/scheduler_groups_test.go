package domain

import (
	"testing"
)

func TestCostPriorities(t *testing.T) {
	got := CostPriorities(map[string]float64{
		"cheap-a": 0.03,
		"cheap-b": 0.03,
		"middle":  0.04,
		"high":    0.1,
	})
	if got["cheap-a"] != 100 || got["cheap-b"] != 100 || got["middle"] != 99 || got["high"] != 98 {
		t.Fatalf("priorities=%v", got)
	}
	if got := CostPriorities(nil); len(got) != 0 {
		t.Fatalf("empty priorities=%v", got)
	}
}

func TestGroupsForPrice(t *testing.T) {
	tiers := []SchedulerTier{
		{Group: "gpt_low", PriceMin: 0, PriceMax: 0.1},
		{Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.25},
		{Group: "gpt_low", PriceMin: 0, PriceMax: 0.1}, // dup ignored
	}
	got := GroupsForPrice(tiers, 0.1)
	if len(got) != 2 || got[0] != "gpt_low" || got[1] != "gpt_stable" {
		t.Fatalf("got=%v", got)
	}
	if got := GroupsForPrice(tiers, 0.5); len(got) != 0 {
		t.Fatalf("out of range: %v", got)
	}
}

func TestManagedAndSplitGroups(t *testing.T) {
	tiers := []SchedulerTier{{Group: " a "}, {Group: ""}, {Group: "b"}}
	m := ManagedGroups(tiers)
	if !m["a"] || !m["b"] || m[""] {
		t.Fatalf("managed=%v", m)
	}
	got := SplitGroups(" a, b ,a, ,c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("split=%v", got)
	}
}

func TestSameGroups(t *testing.T) {
	if !SameGroups([]string{"a", "b"}, []string{"b", "a"}) {
		t.Fatal("order should not matter")
	}
	if SameGroups([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("length mismatch")
	}
	if SameGroups([]string{"a"}, []string{"b"}) {
		t.Fatal("different sets")
	}
}

func TestTargetGroupsKeepsUnmanaged(t *testing.T) {
	tiers := []SchedulerTier{
		{Group: "gpt_low", PriceMin: 0, PriceMax: 0.1},
		{Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.25},
	}
	managed := ManagedGroups(tiers)
	// 当前含手动分组 + 错误托管；价格仅命中 gpt_low
	got := TargetGroups(tiers, managed, 0.05, "manual,gpt_stable")
	if !SameGroups(got, []string{"manual", "gpt_low"}) {
		t.Fatalf("got=%v", got)
	}
	// 当前为空且无价格匹配 → 空结果
	if got := TargetGroups(tiers, managed, 9, ""); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestAssignedTargetGroupsUsesUnassigned(t *testing.T) {
	tiers := []SchedulerTier{
		{Group: "gpt_low", PriceMin: 0, PriceMax: 0.1},
		{Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.25},
	}
	managed := ManagedGroups(tiers)
	// 超档：落到 unassigned，并剥离旧托管组
	got := AssignedTargetGroups(tiers, managed, 0.5, "gpt_stable", "unassigned")
	if !SameGroups(got, []string{"unassigned"}) {
		t.Fatalf("got=%v", got)
	}
	// 已在 unassigned：保持
	if got := AssignedTargetGroups(tiers, managed, 0.5, "unassigned", "unassigned"); !SameGroups(got, []string{"unassigned"}) {
		t.Fatalf("got=%v", got)
	}
	// 命中档位时不再带 unassigned
	if got := AssignedTargetGroups(tiers, managed, 0.05, "unassigned", "unassigned"); !SameGroups(got, []string{"gpt_low"}) {
		t.Fatalf("got=%v", got)
	}
	// 保留其它手动分组
	if got := AssignedTargetGroups(tiers, managed, 0.5, "manual,gpt_stable", "unassigned"); !SameGroups(got, []string{"manual"}) {
		t.Fatalf("got=%v", got)
	}
}

func TestValidateSchedulerUnassignedGroup(t *testing.T) {
	tiers := []SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 0.1}}
	if err := ValidateSchedulerUnassignedGroup("", tiers); err == nil {
		t.Fatal("empty should fail")
	}
	if err := ValidateSchedulerUnassignedGroup("gpt_low", tiers); err == nil {
		t.Fatal("same as tier should fail")
	}
	if err := ValidateSchedulerUnassignedGroup("a,b", tiers); err == nil {
		t.Fatal("multi should fail")
	}
	if err := ValidateSchedulerUnassignedGroup("unassigned", tiers); err != nil {
		t.Fatal(err)
	}
}

func TestJoinGroups(t *testing.T) {
	if got := JoinGroups([]string{"a", "b"}); got != "a,b" {
		t.Fatalf("got=%q", got)
	}
}

func TestAxonHubTiersAndTags(t *testing.T) {
	if _, ok := AxonHubTierForCost(0.099); !ok {
		t.Fatal("0.099 should be payg_low")
	}
	if _, ok := AxonHubTierForCost(0.0995); ok {
		t.Fatal("gap between pools must not match")
	}
	stable, ok := AxonHubTierForCost(0.10)
	if !ok || stable.Tag != AxonHubTagStable {
		t.Fatalf("0.10 tier=%+v ok=%v", stable, ok)
	}
	if _, ok := AxonHubTierForCost(0.21); ok {
		t.Fatal("out-of-range cost should not match")
	}
	got := AxonHubTargetTags([]string{"manual", AxonHubTagLow, AxonHubTagStable, "keep"}, AxonHubTagStable)
	if !SameGroups(got, []string{"manual", "keep", AxonHubTagStable}) {
		t.Fatalf("tags=%v", got)
	}
	cleared := AxonHubTargetTags([]string{"manual", AxonHubTagLow}, "")
	if !SameGroups(cleared, []string{"manual"}) {
		t.Fatalf("cleared=%v", cleared)
	}
}

func TestAxonHubOrderingWeightsArePoolLocal(t *testing.T) {
	weights := AxonHubOrderingWeights(map[string]AxonHubCostTarget{
		"low-a":  {Cost: 0.01, Tag: AxonHubTagLow},
		"low-b":  {Cost: 0.01, Tag: AxonHubTagLow},
		"low-c":  {Cost: 0.02, Tag: AxonHubTagLow},
		"stable": {Cost: 0.11, Tag: AxonHubTagStable},
	})
	if weights["low-a"] != 100 || weights["low-b"] != 100 || weights["low-c"] != 90 || weights["stable"] != 100 {
		t.Fatalf("weights=%v", weights)
	}
	many := map[string]AxonHubCostTarget{}
	for i := 0; i < 12; i++ {
		many[string(rune('a'+i))] = AxonHubCostTarget{Cost: float64(i), Tag: AxonHubTagLow}
	}
	for _, weight := range AxonHubOrderingWeights(many) {
		if weight < 10 {
			t.Fatalf("weight below floor: %d", weight)
		}
	}
}
