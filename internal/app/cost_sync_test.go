package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

func TestApplyGGAPICostsWritesOnlyCostFieldsAndPausesAfterExternalChange(t *testing.T) {
	channel := map[string]any{"id": 9, "name": "GPT", "status": 1, "priority": 10, "weight": 77, "group": "manual"}
	putCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/channel/" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{channel}})
		case r.URL.Path == "/api/channel/" && r.Method == http.MethodPut:
			putCount++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 3 || body["id"] != float64(9) || body["group"] != "manual,gpt_low,gpt_stable" || body["priority"] != float64(100) {
				t.Fatalf("unexpected cost write body=%v", body)
			}
			if _, ok := body["status"]; ok {
				t.Fatalf("status must not be written: %v", body)
			}
			if _, ok := body["weight"]; ok {
				t.Fatalf("weight must not be written: %v", body)
			}
			channel["group"], channel["priority"] = body["group"], int(body["priority"].(float64))
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc, st := newCostSyncTestService(t, server)
	upstream, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "上游", Type: "newapi", BaseURL: server.URL, Enabled: true, BalanceRate: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), upstream.ID, []monitor.APIKey{{RemoteID: "key", Name: "Key", Key: "secret", GroupRatio: "0.1"}}); err != nil {
		t.Fatal(err)
	}
	keys, _ := st.ListKeys(t.Context(), upstream.ID)
	if _, err := st.CreateCostBinding(t.Context(), domain.SchedulerCostBinding{Name: "成本", UpstreamID: upstream.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", SchedulerChannelName: "GPT", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.ApplySchedulerGroups(t.Context())
	if err != nil || result.Updated != 1 || putCount != 1 {
		t.Fatalf("result=%+v puts=%d err=%v", result, putCount, err)
	}
	channel["group"] = "external"
	result, err = svc.ApplySchedulerGroups(t.Context())
	if err != nil || result.Skipped != 1 || putCount != 1 {
		t.Fatalf("external result=%+v puts=%d err=%v", result, putCount, err)
	}
	ownership, found, err := st.CostFieldOwnership(t.Context(), domain.SchedulerProviderGGAPI, "9")
	if err != nil || !found || !ownership.ExternalTakeover || ownership.Managed {
		t.Fatalf("ownership=%+v found=%v err=%v", ownership, found, err)
	}
}

func TestAutomaticCostSyncFailureIsDeduplicatedAndRecovers(t *testing.T) {
	failing := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			http.NotFound(w, r)
			return
		}
		if failing {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	}))
	defer server.Close()
	svc, st := newCostSyncTestService(t, server)
	svc.Scheduler.syncSchedulerGroupsBestEffort(t.Context())
	svc.Scheduler.syncSchedulerGroupsBestEffort(t.Context())
	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "cost_sync_failed", Limit: 10})
	if err != nil || len(events) != 1 || events[0].Severity != "warning" {
		t.Fatalf("failure events=%+v err=%v", events, err)
	}
	failing = false
	svc.Scheduler.syncSchedulerGroupsBestEffort(t.Context())
	events, err = st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "cost_sync_failed", Limit: 10})
	if err != nil || len(events) != 2 || events[0].Severity != "success" {
		t.Fatalf("recovery events=%+v err=%v", events, err)
	}
}

func newCostSyncTestService(t *testing.T, server *httptest.Server) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "cost-sync.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client.HTTP = server.Client()
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned"}); err != nil {
		t.Fatal(err)
	}
	return svc, st
}
