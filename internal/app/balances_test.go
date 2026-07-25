package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

type balanceRefreshTracker struct {
	mu       sync.Mutex
	inFlight int
	max      int
}

func (t *balanceRefreshTracker) enter() func() {
	t.mu.Lock()
	t.inFlight++
	if t.inFlight > t.max {
		t.max = t.inFlight
	}
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.inFlight--
		t.mu.Unlock()
	}
}

func TestRefreshBalancesKeepsParallelFailuresPerUpstream(t *testing.T) {
	tracker := &balanceRefreshTracker{}
	newServer := func(fail bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			leave := tracker.enter()
			defer leave()
			time.Sleep(30 * time.Millisecond)
			if fail {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			switch r.URL.Path {
			case "/api/user/self":
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota": 500000}})
			case "/api/token/":
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			case "/api/user/self/groups", "/api/user/groups":
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
			default:
				http.NotFound(w, r)
			}
		}))
	}
	okServer, failedServer := newServer(false), newServer(true)
	defer okServer.Close()
	defer failedServer.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "balances.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	okUpstream, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "正常上游", Type: "newapi", BaseURL: okServer.URL, UserID: "1", AccessToken: "token", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	failedUpstream, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "限流上游", Type: "newapi", BaseURL: failedServer.URL, UserID: "2", AccessToken: "token", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "停用上游", Type: "newapi", BaseURL: failedServer.URL, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: okServer.Client()}

	result, err := svc.RefreshBalances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Succeeded != 1 || result.Failed != 1 || len(result.Results) != 2 {
		t.Fatalf("result=%+v", result)
	}
	tracker.mu.Lock()
	maxParallel := tracker.max
	tracker.mu.Unlock()
	if maxParallel < 2 {
		t.Fatalf("max parallel requests=%d, want at least 2", maxParallel)
	}
	rows, err := svc.BalanceRows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, row := range rows {
		byID[row["id"].(string)] = row
	}
	if errorText, _ := byID[okUpstream.ID]["error"].(string); errorText != "" {
		t.Fatalf("successful upstream error=%q", errorText)
	}
	if errorText, _ := byID[failedUpstream.ID]["error"].(string); !strings.Contains(errorText, "429") {
		t.Fatalf("failed upstream error=%q", errorText)
	}
}

func TestRefreshBalancesRunsOneCostSyncAfterBatch(t *testing.T) {
	var mu sync.Mutex
	channelReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota": 500000}})
		case "/api/token/":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case "/api/user/self/groups", "/api/user/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
		case "/api/channel/":
			mu.Lock()
			channelReads++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "batch-sync.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"上游一", "上游二"} {
		if _, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: name, Type: "newapi", BaseURL: server.URL, UserID: "1", AccessToken: "token", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: server.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned"}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.RefreshBalances(t.Context())
	if err != nil || result.Succeeded != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	mu.Lock()
	reads := channelReads
	mu.Unlock()
	if reads != 1 {
		t.Fatalf("cost sync channel reads=%d, want 1", reads)
	}
	logs, err := st.SchedulerLogsForProvider(t.Context(), domain.SchedulerProviderGGAPI, 10)
	if err != nil || len(logs) != 1 || logs[0].Action != "group_sync" {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}
