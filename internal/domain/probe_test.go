package domain

import (
	"testing"
	"time"
)

func TestEffectiveThresholds(t *testing.T) {
	def := DefaultNotificationRules()
	if got := EffectiveFailureThreshold(NotificationRules{}); got != def.FailureThreshold {
		t.Fatalf("empty failure threshold = %d, want %d", got, def.FailureThreshold)
	}
	if got := EffectiveMuteThreshold(NotificationRules{}); got != def.MuteFailureThreshold {
		t.Fatalf("empty mute threshold = %d, want %d", got, def.MuteFailureThreshold)
	}
	if got := EffectiveFailureThreshold(NotificationRules{FailureThreshold: 7}); got != 7 {
		t.Fatalf("custom failure = %d", got)
	}
	if got := EffectiveMuteThreshold(NotificationRules{MuteFailureThreshold: 9}); got != 9 {
		t.Fatalf("custom mute = %d", got)
	}
}

func TestProbeMutedAndSuppress(t *testing.T) {
	muteAt := 4
	if !ProbeMuted(4, muteAt, false) {
		t.Fatal("failure count at mute should mute")
	}
	if !ProbeMuted(1, muteAt, true) {
		t.Fatal("auto-disabled should mute")
	}
	if ProbeMuted(1, muteAt, false) {
		t.Fatal("below mute should not mute")
	}
	// 仅在静音边界精确次数告警；其余失败次数抑制。
	if !SuppressProbeAlert(false, 3, muteAt) {
		t.Fatal("failures != muteAt should suppress")
	}
	if SuppressProbeAlert(false, 4, muteAt) {
		t.Fatal("exact mute boundary should not suppress")
	}
	if !SuppressProbeAlert(true, 4, muteAt) {
		t.Fatal("auto-disabled should suppress")
	}
	// muteAt <= 0 回退默认（4）
	if SuppressProbeAlert(false, 4, 0) {
		t.Fatal("default mute boundary should not suppress")
	}
}

func TestShouldAutoDisableScheduler(t *testing.T) {
	if !ShouldAutoDisableScheduler(true, "ch1", false, 4, 4, false) {
		t.Fatal("should auto-disable at mute boundary")
	}
	if ShouldAutoDisableScheduler(true, "ch1", false, 4, 4, true) {
		t.Fatal("already disabled")
	}
	if ShouldAutoDisableScheduler(true, "ch1", true, 4, 4, false) {
		t.Fatal("success should not disable")
	}
	if ShouldAutoDisableScheduler(false, "ch1", false, 4, 4, false) {
		t.Fatal("non-pool should not disable")
	}
	if ShouldAutoDisableScheduler(true, "", false, 4, 4, false) {
		t.Fatal("no channel should not disable")
	}
	if ShouldAutoDisableScheduler(true, "ch1", false, 3, 4, false) {
		t.Fatal("before mute boundary should not disable")
	}
}

func TestSchedulerRestoreReady(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if SchedulerRestoreReady(nil, nil, now) {
		t.Fatal("nil disabledAt")
	}
	tooRecent := now.Add(-5 * time.Minute)
	if SchedulerRestoreReady(&tooRecent, []ProbeRun{{Success: true}, {Success: true}, {Success: true}}, now) {
		t.Fatal("too soon")
	}
	old := now.Add(-20 * time.Minute)
	if SchedulerRestoreReady(&old, []ProbeRun{{Success: true}, {Success: true}}, now) {
		t.Fatal("need 3 probes")
	}
	if SchedulerRestoreReady(&old, []ProbeRun{{Success: true}, {Success: false}, {Success: true}}, now) {
		t.Fatal("need 3 successes")
	}
	if !SchedulerRestoreReady(&old, []ProbeRun{{Success: true}, {Success: true}, {Success: true}}, now) {
		t.Fatal("should restore")
	}
	if !NeedsSchedulerRestoreTimestamp(nil) {
		t.Fatal("needs timestamp")
	}
	if NeedsSchedulerRestoreTimestamp(&old) {
		t.Fatal("has timestamp")
	}
}

func TestIsQuotaProbeError(t *testing.T) {
	if !IsQuotaProbeError("余额不足，请充值") {
		t.Fatal("cn quota")
	}
	if !IsQuotaProbeError("insufficient_quota for model") {
		t.Fatal("en quota")
	}
	if IsQuotaProbeError("connection refused") {
		t.Fatal("not quota")
	}
}

func TestUpstreamAlerting(t *testing.T) {
	if !UpstreamAlerting(2, 2) {
		t.Fatal("at threshold")
	}
	if UpstreamAlerting(1, 2) {
		t.Fatal("below threshold")
	}
	if !UpstreamAlerting(2, 0) {
		t.Fatal("default threshold is 2")
	}
}
