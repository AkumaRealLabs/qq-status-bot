package domain

import "testing"

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
	// current has manual group + wrong managed; price hits gpt_low only
	got := TargetGroups(tiers, managed, 0.05, "manual,gpt_stable")
	if !SameGroups(got, []string{"manual", "gpt_low"}) {
		t.Fatalf("got=%v", got)
	}
	// empty current + no price match → empty
	if got := TargetGroups(tiers, managed, 9, ""); len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestJoinGroups(t *testing.T) {
	if got := JoinGroups([]string{"a", "b"}); got != "a,b" {
		t.Fatalf("got=%q", got)
	}
}
