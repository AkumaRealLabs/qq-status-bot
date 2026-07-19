package domain

import (
	"testing"
	"time"
)

func TestAvailabilityPolicyThreeLines(t *testing.T) {
	valid := AvailabilityPolicy{BalanceGuardMode: BalanceGuardActive, LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20, RunwayWarningHours: 24}
	if err := ValidateAvailabilityPolicy(valid); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []AvailabilityPolicy{
		{BalanceGuardMode: BalanceGuardActive, LowBalanceThreshold: 19, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20},
		{BalanceGuardMode: BalanceGuardActive, LowBalanceThreshold: 30, BalanceCloseThreshold: 20, BalanceRecoverThreshold: 20},
		{BalanceGuardMode: "enabled", LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20},
	} {
		if err := ValidateAvailabilityPolicy(policy); err == nil {
			t.Fatalf("expected policy error: %+v", policy)
		}
	}
}

func TestAvailabilityBalanceObserveAndActive(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	row := ChannelAvailability{Managed: true, ActualStatus: 1, Blockers: []AvailabilityBlocker{{Kind: BlockerBalanceLow, Since: now}}}
	observe := AvailabilityPolicy{BalanceGuardMode: BalanceGuardObserve, LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20}
	if got := AvailabilityDecisionFor(now, observe, row); got.State != AvailabilityWarning || got.DesiredStatus != 1 {
		t.Fatalf("observe decision = %+v", got)
	}
	active := observe
	active.BalanceGuardMode = BalanceGuardActive
	if got := AvailabilityDecisionFor(now, active, row); got.State != AvailabilityBlocked || got.DesiredStatus != 2 {
		t.Fatalf("active decision = %+v", got)
	}
}

func TestFreshBalanceAndRecoveryEligibility(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	if !FreshBalance(BalanceSnapshot{CheckedAt: now.Add(-9 * time.Minute)}, now, 5*time.Minute) {
		t.Fatal("fresh snapshot rejected")
	}
	if FreshBalance(BalanceSnapshot{CheckedAt: now.Add(-11 * time.Minute)}, now, 5*time.Minute) {
		t.Fatal("stale snapshot accepted")
	}
	closed := now.Add(-16 * time.Minute)
	row := ChannelAvailability{DisabledAt: &closed, RecoverySuccess: 3}
	policy := AvailabilityPolicy{BalanceGuardMode: BalanceGuardActive, LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20}
	if !RecoveryEligible(now, row, policy, true, 20) {
		t.Fatal("eligible recovery rejected")
	}
	if RecoveryEligible(now, row, policy, false, 20) || RecoveryEligible(now, row, policy, true, 19.9) {
		t.Fatal("recovery bypassed fresh balance or recovery line")
	}
}

func TestPredictBalanceRunwayIgnoresRecharge(t *testing.T) {
	start := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	snaps := []BalanceSnapshot{
		{CheckedAt: start, Remain: 100},
		{CheckedAt: start.Add(time.Hour), Remain: 90},
		{CheckedAt: start.Add(2 * time.Hour), Remain: 130}, // 充值，不应作为消耗速度
		{CheckedAt: start.Add(3 * time.Hour), Remain: 120},
		{CheckedAt: start.Add(4 * time.Hour), Remain: 110},
		{CheckedAt: start.Add(5 * time.Hour), Remain: 100},
	}
	got := PredictBalanceRunway(snaps, 24)
	if got.Samples != 4 || got.RatePerHour != 10 || got.HoursRemaining != 10 || !got.Warning {
		t.Fatalf("runway = %+v", got)
	}
}

func TestForceEnableAndManualHoldTakePrecedence(t *testing.T) {
	now := time.Now().UTC()
	until := now.Add(30 * time.Minute)
	policy := AvailabilityPolicy{BalanceGuardMode: BalanceGuardActive, LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20}
	row := ChannelAvailability{Managed: true, Blockers: []AvailabilityBlocker{{Kind: BlockerProbeFailed}}, Override: OverrideForceEnable, OverrideUntil: &until}
	if got := AvailabilityDecisionFor(now, policy, row); got.State != AvailabilityForcedOn || got.DesiredStatus != 1 {
		t.Fatalf("force decision = %+v", got)
	}
	row.Override, row.OverrideUntil = OverrideManualHold, nil
	if got := AvailabilityDecisionFor(now, policy, row); got.State != AvailabilityManualOff || got.DesiredStatus != 2 {
		t.Fatalf("hold decision = %+v", got)
	}
}
