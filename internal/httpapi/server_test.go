package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/store"
)

func newHTTPTestServer(t *testing.T) (*store.Store, *httptest.Server) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "http.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	t.Cleanup(ts.Close)
	return st, ts
}

func TestAuthSessionFlow(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	post := func(path, body string, cookie *http.Cookie) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := post("/api/setup", `{"username":"admin","password":"secret12"}`, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup=%d", resp.StatusCode)
	}
	resp = post("/api/auth/login", `{"username":"admin","password":"secret12"}`, nil)
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) == 0 {
		t.Fatalf("login=%d cookies=%v", resp.StatusCode, resp.Cookies())
	}
	cookie := resp.Cookies()[0]
	resp.Body.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	req.AddCookie(cookie)
	resp, _ = ts.Client().Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me=%d", resp.StatusCode)
	}
}

func TestUnsafeMethodRejectsBadOrigin(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/setup", strings.NewReader(`{"username":"admin","password":"secret12"}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestExportMarksBackupSensitive(t *testing.T) {
	st, _ := newHTTPTestServer(t)
	user, err := st.CreateUser(t.Context(), "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateSession(t.Context(), user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/export", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rr := httptest.NewRecorder()
	srv := &Server{App: app.New(st)}
	srv.auth(srv.exportData)(rr, req)
	if rr.Code != http.StatusOK || rr.Header().Get("X-Backup-Contains-Secrets") != "true" || !strings.Contains(rr.Header().Get("Content-Disposition"), "sensitive") {
		t.Fatalf("status=%d headers=%v", rr.Code, rr.Header())
	}
}

func TestPublicSettingsDoesNotRequireAuth(t *testing.T) {
	st, ts := newHTTPTestServer(t)
	cfg, _ := st.Settings(t.Context())
	cfg.SiteName, cfg.SiteIcon = "GG API", "/logo.png"
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Get(ts.URL + "/api/public/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK || out["site_name"] != "GG API" || len(out) != 2 {
		t.Fatalf("status=%d out=%v", resp.StatusCode, out)
	}
}

func TestRetiredMonitorRoutesReturnNotFound(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	for _, path := range []string{
		"/api/cards", "/api/cards/x/check", "/api/monitor/status", "/api/public/monitor/status",
		"/api/scheduler/availability", "/api/scheduler/traffic", "/api/scheduler/traffic/status",
		"/api/scheduler/ggapi/settings", "/api/scheduler/ggapi/affinity-cache",
	} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, resp.StatusCode)
		}
	}
}

func TestRetiredProfitMessageAndPoolRoutesReturnNotFound(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	for _, path := range []string{
		"/api/ops/profit", "/api/tg/session/status", "/api/tg/channels", "/api/tg/messages",
		"/api/pools/cliproxy/config", "/api/pools/cliproxy/accounts",
	} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, resp.StatusCode)
		}
	}
	for _, path := range []string{
		"/api/cost-bindings", "/api/ops/notifications", "/api/ops/self-check", "/api/settings/export",
	} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("现有路由 %s status=%d", path, resp.StatusCode)
		}
	}
}

// 遍历 legacyPaths 本身，新增条目自动纳入覆盖。
func TestLegacyPathsRedirect(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	client := ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for pattern, want := range legacyPaths {
		path := pattern
		if path == "/{$}" {
			path = "/"
		}
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != want {
			t.Errorf("%s status=%d location=%q want %q", path, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
}

// 旧地址表至少要覆盖这些历史路径，避免重构时整表被改空。
func TestLegacyPathsCoverRetiredFeatures(t *testing.T) {
	for path, want := range map[string]string{
		"/": "/admin/balances", "/admin": "/admin/balances", "/status": "/admin/balances",
		"/admin/status": "/admin/balances", "/admin/profit": "/admin/balances",
		"/admin/messages": "/admin/balances", "/admin/pools": "/admin/balances",
		"/admin/scheduler": "/admin/costs", "/admin/ops": "/admin/events",
		"/admin/merchant-balance": "/admin/revenue",
	} {
		pattern := path
		if pattern == "/" {
			pattern = "/{$}"
		}
		if got := legacyPaths[pattern]; got != want {
			t.Errorf("legacyPaths[%q] = %q, want %q", pattern, got, want)
		}
	}
}

func TestCostBindingRoutesRequireAuth(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/cost-bindings"}, {http.MethodPost, "/api/cost-bindings"},
		{http.MethodPatch, "/api/cost-bindings/x"}, {http.MethodDelete, "/api/cost-bindings/x"},
		{http.MethodGet, "/api/cost-bindings/channels?provider=ggapi"}, {http.MethodPost, "/api/cost-bindings/x/adopt"},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(`{}`))
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d", tc.method, tc.path, resp.StatusCode)
		}
	}
}
