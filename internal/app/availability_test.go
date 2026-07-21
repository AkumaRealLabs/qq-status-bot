package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

func TestAvailabilityBalanceObserveThenActiveCascadesSub2APIBoundChannels(t *testing.T) {
	status := map[string]int{"9": 1, "10": 1}
	var actions []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/channel/":
			items := []map[string]any{}
			for _, id := range []string{"9", "10"} {
				items = append(items, map[string]any{"id": id, "name": "渠道" + id, "status": status[id]})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
		case "/api/channel/9/status", "/api/channel/10/status":
			var body struct {
				Status int `json:"status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			id := r.URL.Path[len("/api/channel/") : len(r.URL.Path)-len("/status")]
			status[id] = body.Status
			actions = append(actions, body.Status)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "availability.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	upstream, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "上游", Type: "sub2api", BaseURL: server.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), upstream.ID, []monitor.APIKey{{RemoteID: "key-1", Key: "sk-1"}, {RemoteID: "key-2", Key: "sk-2"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), upstream.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, channelID := range []string{"9", "10"} {
		if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "卡片" + channelID, UpstreamID: upstream.ID, KeyID: keys[i].ID, Model: domain.ProbeModel, PoolEnabled: true, SchedulerChannelID: channelID, SchedulerChannelName: "渠道" + channelID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: server.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveBalance(t.Context(), upstream.ID, monitor.Balance{Remain: 5}, "", 1); err != nil {
		t.Fatal(err)
	}
	observe := domain.AvailabilityPolicy{BalanceGuardMode: domain.BalanceGuardObserve, LowBalanceThreshold: 30, BalanceCloseThreshold: 10, BalanceRecoverThreshold: 20, RunwayWarningHours: 24}
	if _, err := st.UpdateAvailabilityPolicy(t.Context(), upstream.ID, observe); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileAvailability(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("observe must not write remote status: %v", actions)
	}
	rows, err := svc.AvailabilityRows(t.Context(), upstream.ID, "")
	if err != nil || len(rows) != 2 || rows[0].State != domain.AvailabilityWarning {
		t.Fatalf("observe rows=%+v err=%v", rows, err)
	}
	active := observe
	active.BalanceGuardMode = domain.BalanceGuardActive
	if _, err := st.UpdateAvailabilityPolicy(t.Context(), upstream.ID, active); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileAvailability(t.Context()); err != nil {
		t.Fatal(err)
	}
	sort.Ints(actions)
	if len(actions) != 2 || actions[0] != 2 || actions[1] != 2 || status["9"] != 2 || status["10"] != 2 {
		t.Fatalf("active actions=%v status=%v", actions, status)
	}
}

func TestReconcileMissingChannelNotifiesOnlyOnTransition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []any{}}})
	}))
	defer server.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "availability.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	upstream, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "上游", Type: "newapi", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "失效绑定", UpstreamID: upstream.ID, PoolEnabled: true, SchedulerChannelID: "9", SchedulerChannelName: "已删除渠道", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: server.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned"}); err != nil {
		t.Fatal(err)
	}
	notifier := &mockNotifier{}
	svc.Notify = notifier

	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.ReconcileAvailability(t.Context())
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	logs, err := svc.SchedulerLogs(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Reason != "binding_invalid" || logs[0].CardID != card.ID {
		t.Fatalf("logs = %+v", logs)
	}
	notifier.mu.Lock()
	notificationCount := len(notifier.msgs)
	notifier.mu.Unlock()
	if notificationCount != 1 {
		t.Fatalf("notifications = %d, want 1", notificationCount)
	}
	row, found, err := st.ChannelAvailability(t.Context(), "9")
	if err != nil || !found || row.Managed || row.LastError == "" {
		t.Fatalf("row=%+v found=%v err=%v", row, found, err)
	}
	version := row.Version
	if err := svc.ReconcileAvailability(t.Context()); err != nil {
		t.Fatal(err)
	}
	row, _, err = st.ChannelAvailability(t.Context(), "9")
	if err != nil || row.Version != version {
		t.Fatalf("unchanged reconcile wrote state: version=%d want=%d err=%v", row.Version, version, err)
	}
}
