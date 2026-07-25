package app

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

func TestProfitUsesHistoricalCostSnapshotsByLogTime(t *testing.T) {
	first := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	second := first.Add(time.Minute)
	oldLog, newLog := first.Add(time.Second), second.Add(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/log/" {
			http.NotFound(w, r)
			return
		}
		items := []map[string]any{}
		if r.URL.Query().Get("group") == "gpt_low" {
			items = []map[string]any{
				{"quota": 10 * 500000 * 0.1, "channel": "9", "group": "gpt_low", "created_at": oldLog.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
				{"quota": 10 * 500000 * 0.1, "channel": "9", "group": "gpt_low", "created_at": newLog.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
	}))
	defer server.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerChannelCostSnapshot(t.Context(), domain.SchedulerChannelCostSnapshot{Provider: domain.SchedulerProviderGGAPI, ChannelID: "9", ChannelName: "ch", CardName: "手动成本", SourceType: domain.CostSourceManual, CostPerUnit: 0.10, Active: true, EffectiveAt: first}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerChannelCostSnapshot(t.Context(), domain.SchedulerChannelCostSnapshot{Provider: domain.SchedulerProviderGGAPI, ChannelID: "9", ChannelName: "ch", CardName: "手动成本", SourceType: domain.CostSourceManual, CostPerUnit: 0.14, Active: true, EffectiveAt: second}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerGroupSaleSnapshot(t.Context(), domain.SchedulerGroupSaleSnapshot{Group: "gpt_low", Tag: "low", SalePrice: 0.2, Active: true, EffectiveAt: first.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}

	svc := New(st)
	svc.Client = monitor.Client{HTTP: server.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 1, SalePrice: 0.2}},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Profit(t.Context(), "24h")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Complete || !closeProfitValue(out.Revenue, 4) || !closeProfitValue(out.Cost, 2.4) || !closeProfitValue(out.Profit, 1.6) {
		t.Fatalf("profit=%+v", out)
	}
}

func closeProfitValue(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
