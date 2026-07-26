package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
)

type stubNotifier struct {
	sent []string
	err  error
}

func (n *stubNotifier) Send(_ context.Context, message string) error {
	n.sent = append(n.sent, message)
	return n.err
}

func newOpsTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "ops.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return New(st), st
}

// 未配置 Telegram 时必须报 400 而不是静默成功，否则用户以为通知已生效。
func TestTestNotificationRequiresTelegramConfig(t *testing.T) {
	svc, st := newOpsTestService(t)
	notifier := &stubNotifier{}
	svc.Notify = notifier

	err := svc.TestNotification(t.Context())
	if err == nil {
		t.Fatal("未配置 Telegram 时应报错")
	}
	if !IsBadRequest(err) {
		t.Fatalf("应是 ErrBadRequest，实际 %T: %v", err, err)
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("不应发出通知，实际 %+v", notifier.sent)
	}

	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.TelegramBotToken = "bot-token"
	cfg.TelegramChatID = "chat-id"
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := svc.TestNotification(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.sent) != 1 {
		t.Fatalf("应发出一条测试通知，实际 %+v", notifier.sent)
	}
}

func TestNotificationRulesRoundTrip(t *testing.T) {
	svc, _ := newOpsTestService(t)
	want := domain.NotificationRules{
		Enabled:          true,
		EventTypes:       map[string]bool{"balance_low": true, "cost_sync_failed": false},
		FailureThreshold: 3,
		Recovery:         true,
	}
	if _, err := svc.SaveNotificationRules(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, err := svc.NotificationRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled != want.Enabled || got.FailureThreshold != want.FailureThreshold || got.Recovery != want.Recovery {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.EventTypes["balance_low"] || got.EventTypes["cost_sync_failed"] {
		t.Fatalf("event types = %+v", got.EventTypes)
	}
}

// 告警产生的运维事件要带上正确的 severity 与 target，恢复事件是 success。
func TestCreateAlertOpsEventSetsSeverityAndTarget(t *testing.T) {
	svc, st := newOpsTestService(t)
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "上游A", Type: "new-api", BaseURL: "http://x", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	svc.createAlertOpsEvent(t.Context(), u, "balance", false, "余额不足")
	svc.createAlertOpsEvent(t.Context(), u, "balance", true, "余额恢复")

	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("应有 2 条事件，实际 %d", len(events))
	}
	var severities []string
	for _, e := range events {
		severities = append(severities, e.Severity)
		if e.TargetID == "" {
			t.Fatalf("事件缺少 target: %+v", e)
		}
	}
	if !(severities[0] == "success" && severities[1] == "warning") &&
		!(severities[0] == "warning" && severities[1] == "success") {
		t.Fatalf("severity 应各有 warning/success，实际 %+v", severities)
	}
}

func TestOpsEventAndAuditReadsApplyFilters(t *testing.T) {
	svc, st := newOpsTestService(t)
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "上游A", Type: "new-api", BaseURL: "http://x", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc.createAlertOpsEvent(t.Context(), u, "balance", false, "余额不足")
	if _, err := st.CreateOpsEvent(t.Context(), domain.OpsEvent{Type: "cost_sync_failed", Severity: "warning", Title: "同步失败"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordAudit(t.Context(), domain.AuditLog{Actor: "admin", Action: "PATCH /api/settings"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordAudit(t.Context(), domain.AuditLog{Actor: "admin", Action: "POST /api/upstreams"}); err != nil {
		t.Fatal(err)
	}

	all, err := svc.OpsEvents(t.Context(), domain.OpsEventFilter{Limit: 10})
	if err != nil || len(all) != 2 {
		t.Fatalf("events=%+v err=%v", all, err)
	}
	filtered, err := svc.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "cost_sync_failed", Limit: 10})
	if err != nil || len(filtered) != 1 || filtered[0].Type != "cost_sync_failed" {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	groups, err := svc.OpsEventGroups(t.Context(), domain.OpsEventFilter{Limit: 10})
	if err != nil || len(groups) == 0 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	logs, err := svc.AuditLogs(t.Context(), "PATCH /api/settings", "", 10)
	if err != nil || len(logs) != 1 || logs[0].Action != "PATCH /api/settings" {
		t.Fatalf("audit=%+v err=%v", logs, err)
	}
}

func TestSelfCheckReportsCoreItems(t *testing.T) {
	svc, _ := newOpsTestService(t)
	// 让浏览器侧车探测走一个真实但会 404 的地址，避免测试依赖外部服务。
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer sidecar.Close()
	t.Setenv("BROWSER_PROXY_URL", sidecar.URL)
	t.Setenv("BROWSER_DEBUG_URL", sidecar.URL)
	svc.Client.HTTP = sidecar.Client()

	out, err := svc.SelfCheck(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]domain.SelfCheckItem{}
	for _, item := range out.Items {
		byName[item.Name] = item
	}
	for _, name := range []string{"app", "database_writable", "disk_space", "build_version", "browser_http", "browser_cdp", "database_backup"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("自检缺少 %s，实际 %+v", name, byName)
		}
	}
	if byName["app"].Status != "ok" || byName["database_writable"].Status != "ok" {
		t.Fatalf("app/db 应为 ok：%+v", byName)
	}
	// 侧车返回 404，探测项要落到 error 而不是 ok。
	if byName["browser_http"].Status != "error" || byName["browser_cdp"].Status != "error" {
		t.Fatalf("404 侧车应判为 error：%+v", byName)
	}
	if out.CheckedAt.IsZero() {
		t.Fatal("CheckedAt 未填")
	}
}

func TestCheckHTTPWithHostStatuses(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "cdp.internal" {
			http.Error(w, "bad host", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer okServer.Close()

	item := checkHTTPWithHost(t.Context(), okServer.Client(), "probe", okServer.URL, "cdp.internal")
	if item.Status != "ok" {
		t.Fatalf("覆盖 Host 头后应 ok：%+v", item)
	}
	item = checkHTTPWithHost(t.Context(), okServer.Client(), "probe", okServer.URL, "wrong.host")
	if item.Status != "error" {
		t.Fatalf("Host 不匹配时应 error：%+v", item)
	}
	// 连不上的地址也要归到 error，而不是 panic 或 ok。
	item = checkHTTP(t.Context(), okServer.Client(), "probe", "http://127.0.0.1:1")
	if item.Status != "error" {
		t.Fatalf("连接失败应 error：%+v", item)
	}
}

func TestBrowserCDPHostHeader(t *testing.T) {
	for _, tc := range []struct {
		name, env, rawurl, want string
	}{
		{"环境变量优先", "custom:1234", "http://browser:9222", "custom:1234"},
		{"IP 保留原端口", "", "http://127.0.0.1:19222", "127.0.0.1:19222"},
		{"localhost 补默认端口", "", "http://localhost", "localhost:80"},
		{"https 补 443", "", "https://127.0.0.1", "127.0.0.1:443"},
		{"容器名回落到本机默认", "", "http://browser:9222", "127.0.0.1:19222"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BROWSER_DEBUG_HOST_HEADER", tc.env)
			if got := browserCDPHostHeader(tc.rawurl); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnvDefault(t *testing.T) {
	t.Setenv("AUM_TEST_KEY", "  ")
	if got := envDefault("AUM_TEST_KEY", "fallback"); got != "fallback" {
		t.Fatalf("空白值应回落，实际 %q", got)
	}
	t.Setenv("AUM_TEST_KEY", " value ")
	if got := envDefault("AUM_TEST_KEY", "fallback"); got != "value" {
		t.Fatalf("应去空格取值，实际 %q", got)
	}
}

func TestCheckItem(t *testing.T) {
	if item := checkItem("x", nil, "都好"); item.Status != "ok" || item.Message != "都好" {
		t.Fatalf("item = %+v", item)
	}
	if item := checkItem("x", errors.New("炸了"), "都好"); item.Status != "error" || item.Message != "炸了" {
		t.Fatalf("item = %+v", item)
	}
}
