package domain

import "testing"

func TestShouldNotify(t *testing.T) {
	rules := DefaultNotificationRules()
	if !ShouldNotify(rules, "probe_failed", false) {
		t.Fatal("enabled probe_failed should notify")
	}
	if ShouldNotify(rules, "probe_internal_error", false) {
		t.Fatal("disabled event type should not notify")
	}
	rules.Enabled = false
	if ShouldNotify(rules, "probe_failed", false) {
		t.Fatal("disabled rules should not notify")
	}
	rules = DefaultNotificationRules()
	rules.Recovery = false
	if ShouldNotify(rules, "probe_failed", true) {
		t.Fatal("recover with Recovery=false should not notify")
	}
	if !ShouldNotify(rules, "probe_failed", false) {
		t.Fatal("non-recover should still notify when event enabled")
	}
}

func TestAlertEventType(t *testing.T) {
	et, tt, id := AlertEventType("ping:card1", false)
	if et != "probe_failed" || tt != "card" || id != "card1" {
		t.Fatalf("ping: %s %s %s", et, tt, id)
	}
	et, tt, id = AlertEventType("quota:c2", false)
	if et != "quota_exhausted" || tt != "card" || id != "c2" {
		t.Fatalf("quota: %s %s %s", et, tt, id)
	}
	et, tt, id = AlertEventType("internal:c3", false)
	if et != "probe_internal_error" || tt != "card" || id != "c3" {
		t.Fatalf("internal: %s %s %s", et, tt, id)
	}
	et, tt, _ = AlertEventType("balance", false)
	if et != "balance_low" || tt != "upstream" {
		t.Fatalf("balance: %s %s", et, tt)
	}
	et, _, _ = AlertEventType("unknown", true)
	if et != "system_recovered" {
		t.Fatalf("recover unknown: %s", et)
	}
	et, _, _ = AlertEventType("unknown", false)
	if et != "system_warning" {
		t.Fatalf("fail unknown: %s", et)
	}
}

func TestAlertOpsTitleAndActions(t *testing.T) {
	if got := AlertOpsTitle("probe_failed", true); got != "已恢复" {
		t.Fatalf("title recover=%q", got)
	}
	if got := AlertOpsTitle("quota_exhausted", false); got != "余额不足/成本池不可用" {
		t.Fatalf("title=%q", got)
	}
	if got := AlertOpsActions("probe_failed"); len(got) != 1 || got[0] != "check_card" {
		t.Fatalf("actions=%v", got)
	}
	if got := AlertOpsActions("cliproxy_error"); len(got) != 1 || got[0] != "refresh_cliproxy_accounts" {
		t.Fatalf("actions=%v", got)
	}
}

func TestProbeAlertKind(t *testing.T) {
	kind, msg := ProbeAlertKind("卡A", "id1", "insufficient_quota")
	if kind != "quota:id1" || msg == "" {
		t.Fatalf("kind=%q msg=%q", kind, msg)
	}
	kind, msg = ProbeAlertKind("卡A", "id1", "timeout")
	if kind != "ping:id1" {
		t.Fatalf("kind=%q", kind)
	}
	_ = msg
}
