package store

import (
	"path/filepath"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func TestAvailabilityPolicyAndChannelCAS(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "availability.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	upstream, err := s.CreateUpstream(t.Context(), domain.Upstream{Name: "new-api", Type: "newapi", BaseURL: "https://example.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	policy := domain.AvailabilityPolicy{BalanceGuardMode: domain.BalanceGuardActive, LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20, RunwayWarningHours: 24}
	if _, err := s.UpdateAvailabilityPolicy(t.Context(), upstream.ID, policy); err != nil {
		t.Fatal(err)
	}
	gotPolicy, err := s.AvailabilityPolicy(t.Context(), upstream.ID)
	if err != nil || gotPolicy != policy {
		t.Fatalf("policy=%+v err=%v", gotPolicy, err)
	}
	closed := time.Now().UTC()
	row := domain.ChannelAvailability{ChannelID: "9", ChannelName: "渠道", CardID: "card", UpstreamID: upstream.ID, Managed: true, DesiredStatus: 2, ActualStatus: 1, DisabledAt: &closed,
		Blockers: []domain.AvailabilityBlocker{{Kind: domain.BlockerProbeFailed, Since: closed}}}
	ok, err := s.SaveChannelAvailabilityCAS(t.Context(), row, 0)
	if err != nil || !ok {
		t.Fatalf("insert ok=%v err=%v", ok, err)
	}
	stored, found, err := s.ChannelAvailability(t.Context(), "9")
	if err != nil || !found || stored.Version != 1 || !domain.HasBlocker(stored.Blockers, domain.BlockerProbeFailed) {
		t.Fatalf("stored=%+v found=%v err=%v", stored, found, err)
	}
	stored.LastError = "remote failed"
	ok, err = s.SaveChannelAvailabilityCAS(t.Context(), stored, 0)
	if err != nil || ok {
		t.Fatalf("stale create should conflict: ok=%v err=%v", ok, err)
	}
	ok, err = s.SaveChannelAvailabilityCAS(t.Context(), stored, stored.Version)
	if err != nil || !ok {
		t.Fatalf("update ok=%v err=%v", ok, err)
	}
	exported, err := s.ExportData(t.Context())
	if err != nil || len(exported.Tables["channel_availability"]) != 1 {
		t.Fatalf("export rows=%d err=%v", len(exported.Tables["channel_availability"]), err)
	}
}
