package store

import (
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func TestTrafficEventDedupAndSanitizedSchema(t *testing.T) {
	s := testStore(t)
	event := domain.TrafficEvent{Source: "error", OccurredAt: time.Now().UTC(), ChannelID: "9", Model: "m", RequestID: "req", Kind: domain.TrafficEventSoftFailure, HTTPStatus: 429, ErrorType: "rate_limit"}
	inserted, err := s.SaveTrafficEvent(t.Context(), event)
	if err != nil || !inserted {
		t.Fatalf("first insert=%v err=%v", inserted, err)
	}
	inserted, err = s.SaveTrafficEvent(t.Context(), event)
	if err != nil || inserted {
		t.Fatalf("duplicate insert=%v err=%v", inserted, err)
	}
	rows, err := s.TrafficEventsSince(t.Context(), event.OccurredAt.Add(-time.Second))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	cols, err := s.columns(t.Context(), "scheduler_traffic_events")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"username", "ip", "token", "request_body", "error_message"} {
		if cols[forbidden] {
			t.Fatalf("sensitive column %q exists", forbidden)
		}
	}
}

func TestTrafficRuntimeDataExcludedFromExportAndClearedOnImport(t *testing.T) {
	s := testStore(t)
	event := domain.TrafficEvent{Source: "consumption", OccurredAt: time.Now().UTC(), ChannelID: "9", Model: "m", Kind: domain.TrafficEventSuccess}
	if _, err := s.SaveTrafficEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTrafficCursor(t.Context(), domain.TrafficCursor{Source: "consumption", CursorAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	exported, err := s.ExportData(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exported.Tables["scheduler_traffic_events"]; ok {
		t.Fatal("traffic events leaked into export")
	}
	if _, ok := exported.Tables["scheduler_traffic_cursors"]; ok {
		t.Fatal("traffic cursor leaked into export")
	}
	if err := s.ImportData(t.Context(), exported); err != nil {
		t.Fatal(err)
	}
	rows, err := s.TrafficEventsSince(t.Context(), time.Time{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("runtime events after import=%d err=%v", len(rows), err)
	}
	cursors, err := s.TrafficCursors(t.Context())
	if err != nil || len(cursors) != 0 {
		t.Fatalf("runtime cursors after import=%d err=%v", len(cursors), err)
	}
}

func TestTrafficControlRoundTrip(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	want := domain.TrafficControlState{
		ChannelID: "9", BasePriority: 100, BaseWeight: 80, DesiredPriority: -900, DesiredWeight: 40,
		ActualPriority: -900, ActualWeight: 40, DesiredStatus: 1, ActualStatus: 1, State: "warning", Reason: "test",
		FailureWindows: 2, RecoveryStage: 1, CooldownUntil: now.Add(time.Minute), LastProbeAt: now,
		RecoverySuccesses: 2, StageChangedAt: now, RetryAt: now.Add(2 * time.Minute), RetryCount: 1, UpdatedAt: now,
	}
	if err := s.SaveTrafficControl(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.TrafficControl(t.Context(), "9")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if got.BasePriority != want.BasePriority || got.BaseWeight != want.BaseWeight || got.DesiredPriority != want.DesiredPriority || got.DesiredWeight != want.DesiredWeight || got.RecoverySuccesses != 2 || got.State != "warning" {
		t.Fatalf("got=%+v", got)
	}
}
