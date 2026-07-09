package domain

import (
	"testing"
	"time"
)

func TestConvertedBalanceValues(t *testing.T) {
	_, _, remain := ConvertedBalanceValues("newapi", 0.1, 5000000, 0, 5000000)
	if remain != 1 {
		t.Fatalf("newapi remain = %v, want 1", remain)
	}

	_, _, remain = ConvertedBalanceValues("sub2api", 0.1, 10, 0, 10)
	if remain != 1 {
		t.Fatalf("sub2api remain = %v, want 1", remain)
	}
}

func TestCardName(t *testing.T) {
	got := CardName(Upstream{Name: "A"}, &APIKey{Name: "main"})
	if got != "A · main" {
		t.Fatalf("name = %q", got)
	}
}

func TestNormalizeSchedulerTiersKeepsCustomRows(t *testing.T) {
	custom := []SchedulerTier{{Tag: "cheap", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1}}
	if got := NormalizeSchedulerTiers(custom); len(got) != 1 || got[0].Tag != "cheap" {
		t.Fatalf("custom tiers = %+v", got)
	}
	if got := NormalizeSchedulerTiers([]SchedulerTier{{Tag: "稳定池", Group: "gpt_stable", PriceMin: 0, PriceMax: 0.15}}); got[0].SalePrice != 0.25 {
		t.Fatalf("old scheduler tier sale price = %+v", got)
	}
	if got := NormalizeSchedulerTiers([]SchedulerTier{}); len(got) != 0 {
		t.Fatalf("empty tiers = %+v", got)
	}
	if err := ValidateSchedulerTiers([]SchedulerTier{
		{Tag: "a", Group: "gpt_low", PriceMin: 0, PriceMax: 1},
		{Tag: "b", Group: "gpt_low", PriceMin: 0, PriceMax: 1},
	}); err == nil {
		t.Fatal("duplicate scheduler group passed")
	}
}

func TestDecideAlertQuotaCooldown(t *testing.T) {
	now := time.Now()
	prev := AlertState{Active: true, LastAt: now.Add(-2 * time.Hour)}
	if _, send := DecideAlert(now, "ping:c1", true, "failed", prev); !send {
		t.Fatal("ping alert should cool down after one hour")
	}
	if _, send := DecideAlert(now, "quota:c1", true, "failed", prev); send {
		t.Fatal("quota alert should cool down for six hours")
	}
}
