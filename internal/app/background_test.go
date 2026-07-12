package app

import (
	"context"
	"encoding/json"
	"errors"
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

type probeRunnerFunc func(context.Context, string, string, string) monitor.ProbeResult

func (f probeRunnerFunc) Probe(ctx context.Context, baseURL, key, model string) monitor.ProbeResult {
	return f(ctx, baseURL, key, model)
}

func testBackgroundService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return New(st)
}

func setCardProbeTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	old := cardProbeTimeout
	cardProbeTimeout = timeout
	t.Cleanup(func() { cardProbeTimeout = old })
}

func TestCheckAllCardTimeoutPersistsAndContinues(t *testing.T) {
	setCardProbeTimeout(t, 25*time.Millisecond)
	svc := testBackgroundService(t)
	slow, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{Name: "慢卡", BaseURL: "slow", APIKey: "sk-slow", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fast, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{Name: "快卡", BaseURL: "fast", APIKey: "sk-fast", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc.Prober = probeRunnerFunc(func(ctx context.Context, baseURL, _, _ string) monitor.ProbeResult {
		if baseURL == "slow" {
			<-ctx.Done()
			return monitor.ProbeResult{Status: monitor.StatusError, Input: "ping", Error: ctx.Err().Error()}
		}
		return monitor.ProbeResult{Success: true, Status: monitor.StatusOperational, Input: "ping", Output: "pong"}
	})

	if err := svc.CheckAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotSlow, err := svc.Store.Card(t.Context(), slow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSlow.FailureCount != 1 || !strings.Contains(gotSlow.LastError, "探测超时") {
		t.Fatalf("slow card = %+v", gotSlow)
	}
	slowRuns, err := svc.Store.RecentProbesForCard(t.Context(), slow.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(slowRuns) != 1 || slowRuns[0].Status != monitor.StatusFailed || !strings.Contains(slowRuns[0].Error, "探测超时") {
		t.Fatalf("slow runs = %+v", slowRuns)
	}
	gotFast, err := svc.Store.Card(t.Context(), fast.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotFast.FailureCount != 0 {
		t.Fatalf("fast card = %+v", gotFast)
	}
	fastRuns, err := svc.Store.RecentProbesForCard(t.Context(), fast.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(fastRuns) != 1 || !fastRuns[0].Success {
		t.Fatalf("fast runs = %+v", fastRuns)
	}
}

func TestCardProbeBudgetIncludesInternalRetry(t *testing.T) {
	setCardProbeTimeout(t, 25*time.Millisecond)
	svc := testBackgroundService(t)
	rules := domain.DefaultNotificationRules()
	rules.InternalRetryCount = 5
	rules.InternalRetryIntervalMs = 100
	if _, err := svc.Store.UpdateNotificationRules(t.Context(), rules); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.Prober = probeRunnerFunc(func(context.Context, string, string, string) monitor.ProbeResult {
		calls++
		return monitor.ProbeResult{Status: monitor.StatusError, Input: "ping", Error: "model instructions file is empty"}
	})
	start := time.Now()
	got := svc.Probe.probeCard(t.Context(), "https://probe.example.test", "sk", domain.ProbeModel)
	if calls != 1 || got.Status != monitor.StatusFailed || !strings.Contains(got.Error, "探测超时") {
		t.Fatalf("calls=%d got=%+v", calls, got)
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("probe exceeded its total budget: %s", elapsed)
	}
}

func TestCheckCardParentExpiryPersistsWithoutSideEffects(t *testing.T) {
	setCardProbeTimeout(t, time.Second)
	svc := testBackgroundService(t)
	card, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{
		Name: "到期卡", BaseURL: "https://probe.example.test", APIKey: "sk", Enabled: true, SchedulerChannelID: "channel-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Prober = probeRunnerFunc(func(ctx context.Context, _, _, _ string) monitor.ProbeResult {
		<-ctx.Done()
		return monitor.ProbeResult{Status: monitor.StatusError, Input: "ping", Error: ctx.Err().Error()}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	err = svc.CheckCard(ctx, card.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CheckCard error = %v", err)
	}
	got, err := svc.Store.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FailureCount != 1 || !strings.Contains(got.LastError, "deadline exceeded") {
		t.Fatalf("card = %+v", got)
	}
	runs, err := svc.Store.RecentProbesForCard(t.Context(), card.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Success {
		t.Fatalf("runs = %+v", runs)
	}
	var events, schedulerLogs int
	if err := svc.Store.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM ops_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM scheduler_logs`).Scan(&schedulerLogs); err != nil {
		t.Fatal(err)
	}
	if events != 0 || schedulerLogs != 0 {
		t.Fatalf("events=%d scheduler_logs=%d", events, schedulerLogs)
	}
}

func TestCheckCardCancellationDoesNotCreateFailure(t *testing.T) {
	setCardProbeTimeout(t, time.Second)
	svc := testBackgroundService(t)
	card, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{Name: "取消卡", BaseURL: "https://probe.example.test", APIKey: "sk", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	svc.Prober = probeRunnerFunc(func(ctx context.Context, _, _, _ string) monitor.ProbeResult {
		close(started)
		<-ctx.Done()
		return monitor.ProbeResult{Status: monitor.StatusError, Input: "ping", Error: ctx.Err().Error()}
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- svc.CheckCard(ctx, card.ID) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckCard error = %v", err)
	}
	got, err := svc.Store.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FailureCount != 0 || got.LastError != "" {
		t.Fatalf("card = %+v", got)
	}
	runs, err := svc.Store.RecentProbesForCard(t.Context(), card.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestRunSchedulerTaskUsesIndependentContexts(t *testing.T) {
	runSchedulerTask(t.Context(), "check", 10*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	for _, name := range []string{"TG", "retention", "revenue", "pending recharge"} {
		ran := false
		runSchedulerTask(t.Context(), name, time.Second, func(ctx context.Context) error {
			ran = ctx.Err() == nil
			return nil
		})
		if !ran {
			t.Fatalf("%s inherited the previous task context", name)
		}
	}
}

func TestCheckCycleTimeoutCoversAllCardBatches(t *testing.T) {
	svc := testBackgroundService(t)
	for range 16 {
		if _, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{Name: "批次卡", BaseURL: "https://probe.example.test", APIKey: "sk", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	want := schedulerCheckOverhead + time.Duration(limitedBatches(16))*cardProbeTimeout
	if got := svc.checkCycleTimeout(t.Context()); got < want {
		t.Fatalf("check timeout = %s, want at least %s", got, want)
	}
}

func TestSchedulerRevenueRefreshUsesDueIntervalAndSkipsDisabledCards(t *testing.T) {
	var mu sync.Mutex
	orderRequests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/topup" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		orderRequests++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "order", "amount": 1.5, "status": "completed"}}})
	}))
	defer ts.Close()

	svc := testBackgroundService(t)
	defaults, err := svc.Store.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range defaults {
		if err := svc.Store.DeleteRevenueCard(t.Context(), card.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Store.CreateRevenueCard(t.Context(), domain.RevenueCard{
		Name: "启用收入", SourceType: domain.RevenueNewAPIOrders, BaseURL: ts.URL, UserID: "1", AccessToken: "token", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.CreateRevenueCard(t.Context(), domain.RevenueCard{
		Name: "停用收入", SourceType: domain.RevenueNewAPIOrders, BaseURL: ts.URL, UserID: "1", AccessToken: "token", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.UpdateCLIProxyConfig(t.Context(), domain.CLIProxyConfig{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	state := schedulerState{lastRetention: now, lastRecharge: now, lastCLIProxy: now}
	svc.runSchedulerTick(t.Context(), now, &state)
	svc.runSchedulerTick(t.Context(), now.Add(schedulerRevenueInterval-time.Second), &state)
	svc.runSchedulerTick(t.Context(), now.Add(schedulerRevenueInterval), &state)
	mu.Lock()
	hits := orderRequests
	mu.Unlock()
	if hits != 2 {
		t.Fatalf("revenue requests = %d, want 2", hits)
	}
	var snapshots int
	if err := svc.Store.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM revenue_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if snapshots != 2 {
		t.Fatalf("revenue snapshots = %d, want 2", snapshots)
	}
}

func TestRefreshPendingBalanceRechargeLogsContinuesAndSkipsDisabledUpstream(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	profileRequests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/topup/self":
			keyword := r.URL.Query().Get("keyword")
			mu.Lock()
			seen[keyword]++
			mu.Unlock()
			if keyword == "bad" {
				http.Error(w, "temporary failure", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"trade_no": keyword, "payment_method": "alipay", "status": "COMPLETED"}}})
		case "/api/user/self":
			mu.Lock()
			profileRequests++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota": 1}})
		case "/api/token/", "/api/user/self/groups", "/api/user/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	svc := testBackgroundService(t)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	enabled, err := svc.Store.CreateUpstream(t.Context(), domain.Upstream{Name: "启用上游", Type: "newapi", BaseURL: ts.URL, UserID: "1", AccessToken: "token", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := svc.Store.CreateUpstream(t.Context(), domain.Upstream{Name: "停用上游", Type: "newapi", BaseURL: ts.URL, UserID: "1", AccessToken: "token", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := svc.Store.SaveBalanceRechargeLog(t.Context(), domain.BalanceRechargeLog{UpstreamID: enabled.ID, Method: "order", RemoteOrderID: "bad", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := svc.Store.SaveBalanceRechargeLog(t.Context(), domain.BalanceRechargeLog{UpstreamID: enabled.ID, Method: "order", RemoteOrderID: "ok", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	okTwo, err := svc.Store.SaveBalanceRechargeLog(t.Context(), domain.BalanceRechargeLog{UpstreamID: enabled.ID, Method: "order", RemoteOrderID: "ok-two", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.SaveBalanceRechargeLog(t.Context(), domain.BalanceRechargeLog{UpstreamID: disabled.ID, Method: "order", RemoteOrderID: "skip", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshPendingBalanceRechargeLogs(t.Context()); err == nil {
		t.Fatal("expected one refresh failure")
	}
	gotBad, err := svc.Store.BalanceRechargeLog(t.Context(), enabled.ID, bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotBad.Status != "pending" || gotBad.Message == "" {
		t.Fatalf("bad log = %+v", gotBad)
	}
	gotOK, err := svc.Store.BalanceRechargeLog(t.Context(), enabled.ID, ok.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOK.Status != "success" || gotOK.RawStatus != "COMPLETED" {
		t.Fatalf("ok log = %+v", gotOK)
	}
	gotOKTwo, err := svc.Store.BalanceRechargeLog(t.Context(), enabled.ID, okTwo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOKTwo.Status != "success" || gotOKTwo.RawStatus != "COMPLETED" {
		t.Fatalf("second ok log = %+v", gotOKTwo)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["bad"] != 1 || seen["ok"] != 1 || seen["ok-two"] != 1 || seen["skip"] != 0 {
		t.Fatalf("order requests = %+v", seen)
	}
	if profileRequests != 1 {
		t.Fatalf("balance refreshes = %d, want 1", profileRequests)
	}
}

func TestCLIProxyQuotaRefreshSkipsDisabledPool(t *testing.T) {
	var mu sync.Mutex
	authCalls, quotaCalls := 0, 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v0/management/auth-files":
			authCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{
				{"name": "ready.json", "auth_index": "1"},
				{"name": "disabled.json", "auth_index": "2", "disabled": true},
				{"name": "unavailable.json", "auth_index": "3", "unavailable": true},
			}})
		case "/v0/management/api-call":
			quotaCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": 10}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	svc := testBackgroundService(t)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.Store.UpdateCLIProxyConfig(t.Context(), domain.CLIProxyConfig{Name: "CPA", BaseURL: ts.URL, ManagementKey: "key", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	attempted, err := svc.CLIProxy.refreshCLIProxyQuotas(t.Context())
	if err != nil || attempted {
		t.Fatalf("disabled refresh attempted=%v err=%v", attempted, err)
	}
	mu.Lock()
	if authCalls != 0 || quotaCalls != 0 {
		mu.Unlock()
		t.Fatalf("disabled pool made requests auth=%d quota=%d", authCalls, quotaCalls)
	}
	mu.Unlock()
	if _, err := svc.Store.UpdateCLIProxyConfig(t.Context(), domain.CLIProxyConfig{Name: "CPA", BaseURL: ts.URL, ManagementKey: "key", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	attempted, err = svc.CLIProxy.refreshCLIProxyQuotas(t.Context())
	if err != nil || !attempted {
		t.Fatalf("enabled refresh attempted=%v err=%v", attempted, err)
	}
	mu.Lock()
	if authCalls != 1 || quotaCalls != 1 {
		mu.Unlock()
		t.Fatalf("enabled pool requests auth=%d quota=%d", authCalls, quotaCalls)
	}
	mu.Unlock()
	var snapshots int
	if err := svc.Store.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM cliproxy_quota_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 {
		t.Fatalf("quota snapshots = %d", snapshots)
	}
}
