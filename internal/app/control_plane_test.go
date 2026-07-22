package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
)

func TestControlPlaneExternalStatusRequiresReadoption(t *testing.T) {
	remoteStatus := 2
	statusWrites := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/channel/" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "name": "渠道9", "status": remoteStatus, "priority": 100, "weight": 100}}}})
		case r.URL.Path == "/api/channel/9/status" && r.Method == http.MethodPost:
			statusWrites++
			var body struct {
				Status int `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			remoteStatus = body.Status
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc, st := newControlPlaneTestService(t, server)
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	row, found, err := st.SchedulerChannelLifecycle(t.Context(), "9")
	if err != nil || !found || row.Owner != domain.ControlOwnerExternal || !row.ExternalTakeover || row.AUMDisabled {
		t.Fatalf("row=%+v found=%v err=%v", row, found, err)
	}
	if err := svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 1, domain.ControlSourceTraffic, "自动恢复", false); err == nil {
		t.Fatal("external status was restored without readoption")
	}
	if statusWrites != 0 {
		t.Fatalf("status writes=%d", statusWrites)
	}
	if _, err := svc.AdoptSchedulerControlPlaneChannel(t.Context(), "9"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 1, domain.ControlSourceManual, "重新接管后启用", true); err != nil {
		t.Fatal(err)
	}
	if statusWrites != 1 || remoteStatus != 1 {
		t.Fatalf("writes=%d status=%d", statusWrites, remoteStatus)
	}
	if err := svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 2, domain.ControlSourceManual, "AUM 再次关闭", false); err != nil {
		t.Fatal(err)
	}
	remoteStatus = 1
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	row, found, err = st.SchedulerChannelLifecycle(t.Context(), "9")
	if err != nil || !found || row.Owner != domain.ControlOwnerExternal || !row.ExternalTakeover || row.AUMDisabled {
		t.Fatalf("external enable row=%+v found=%v err=%v", row, found, err)
	}
}

func TestControlPlaneStatusThreeIsNeverWritten(t *testing.T) {
	statusWrites := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/channel/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "name": "渠道9", "status": 3}}}})
			return
		}
		if r.URL.Path == "/api/channel/9/status" {
			statusWrites++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	svc, st := newControlPlaneTestService(t, server)
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 1, domain.ControlSourceManual, "尝试启用", true); err == nil {
		t.Fatal("status 3 was writable")
	}
	row, _, err := st.SchedulerChannelLifecycle(t.Context(), "9")
	if err != nil || row.Owner != domain.ControlOwnerGGAPI || row.RemoteStatus != 3 || statusWrites != 0 {
		t.Fatalf("row=%+v writes=%d err=%v", row, statusWrites, err)
	}
}

func TestControlPlaneOnlyManagesExplicitlyBoundChannels(t *testing.T) {
	channelReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		channelReads++
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
			{"id": 16, "name": "生图渠道", "status": 1},
			{"id": 17, "name": "GGAPI 自动关闭渠道", "status": 3},
			{"id": 18, "name": "已绑定渠道", "status": 1},
		}}})
	}))
	defer server.Close()

	svc, st := newControlPlaneTestService(t, server)
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{
		Name: "已绑定卡片", BaseURL: "https://upstream.invalid", APIKey: "secret", Model: "m",
		PoolEnabled: true, SchedulerChannelID: "18", SchedulerChannelName: "已绑定渠道", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Scheduler.ControlPlane(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rows := make(map[string]domain.SchedulerControlPlaneChannel, len(view.Channels))
	for _, row := range view.Channels {
		rows[row.ChannelID] = row
	}
	if row := rows["16"]; row.Managed || row.Owner != domain.ControlOwnerObserved || row.ExternalTakeover || row.CloseReason != "未纳入 AUM 管理，仅观察远端状态" {
		t.Fatalf("unbound row=%+v", row)
	}
	if row := rows["17"]; row.Managed || row.Owner != domain.ControlOwnerGGAPI {
		t.Fatalf("status 3 row=%+v", row)
	}
	if row := rows["18"]; !row.Managed || row.Owner != domain.ControlOwnerAUM {
		t.Fatalf("bound row=%+v", row)
	}
	if channelReads != 1 {
		t.Fatalf("control plane channel reads=%d want=1", channelReads)
	}
}

func TestControlPlaneConcurrentCloseWritesOnce(t *testing.T) {
	remoteStatus := 1
	statusWrites, cacheClears := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/channel/":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "name": "渠道9", "status": remoteStatus}}}})
		case "/api/channel/9/status":
			statusWrites++
			remoteStatus = 2
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/option/channel_affinity_cache":
			cacheClears++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc, _ := newControlPlaneTestService(t, server)
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 2, domain.ControlSourceTraffic, "并发关闭", false)
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
	if statusWrites != 1 || cacheClears != 1 {
		t.Fatalf("status writes=%d cache clears=%d", statusWrites, cacheClears)
	}
}

func TestControlPlaneGGAPIRecoveryResetsTrafficStart(t *testing.T) {
	remoteStatus := 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "name": "渠道9", "status": remoteStatus, "priority": 10, "weight": 20}}}})
	}))
	defer server.Close()
	svc, st := newControlPlaneTestService(t, server)
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _, _ := st.SchedulerChannelLifecycle(t.Context(), "9")
	time.Sleep(time.Millisecond)
	remoteStatus = 1
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _, err := st.SchedulerChannelLifecycle(t.Context(), "9")
	if err != nil || after.Owner != domain.ControlOwnerAUM || after.ExternalTakeover || !after.TrafficSince.After(before.TrafficSince) {
		t.Fatalf("before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestControlPlaneEnableKeepsPendingAffinityCleanup(t *testing.T) {
	remoteStatus := 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/channel/" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "name": "渠道9", "status": remoteStatus}}}})
		case r.URL.Path == "/api/channel/9/status" && r.Method == http.MethodPost:
			var body struct {
				Status int `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			remoteStatus = body.Status
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case r.URL.Path == "/api/option/channel_affinity_cache" && r.Method == http.MethodDelete:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "redis unavailable"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc, st := newControlPlaneTestService(t, server)
	if err := svc.Scheduler.SeedControlPlaneBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 2, domain.ControlSourceManual, "手动关闭", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.coordinateSchedulerStatus(t.Context(), "9", 1, domain.ControlSourceManual, "重新启用", true); err != nil {
		t.Fatal(err)
	}
	row, found, err := st.SchedulerChannelLifecycle(t.Context(), "9")
	if err != nil || !found || !row.AffinityCleanupPending || row.AffinityCleanupRetries != 1 || remoteStatus != 1 {
		t.Fatalf("row=%+v found=%v remote=%d err=%v", row, found, remoteStatus, err)
	}
}

func newControlPlaneTestService(t *testing.T, server *httptest.Server) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "control-plane.sqlite"))
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
