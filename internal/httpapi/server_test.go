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
	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

func TestAuthSessionFlow(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := &Server{App: app.New(st)}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	post := func(path, body string, cookie *http.Cookie) *http.Response {
		req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
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

	resp := post("/api/setup", `{"username":"admin","password":"secret"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = post("/api/auth/login", `{"username":"admin","password":"secret"}`, nil)
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) == 0 {
		t.Fatalf("login status=%d cookies=%v", resp.StatusCode, resp.Cookies())
	}
	cookie := resp.Cookies()[0]
	resp.Body.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	req.AddCookie(cookie)
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = post("/api/auth/logout", `{}`, cookie)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDecodeRejectsLargeJSONBody(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	body := `{"username":"` + strings.Repeat("a", defaultJSONBodyLimit) + `","password":"secret"}`
	resp, err := ts.Client().Post(ts.URL+"/api/setup", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := &Server{App: app.New(st)}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	if resp, err := ts.Client().Post(ts.URL+"/api/setup", "application/json", strings.NewReader(`{"username":"admin","password":"secret"}`)); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	for i := 0; i < loginFailLimit; i++ {
		resp, err := ts.Client().Post(ts.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"bad"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", i, resp.StatusCode)
		}
	}
	resp, err := ts.Client().Post(ts.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSecureCookieBehindHTTPSProxy(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()
	if resp, err := ts.Client().Post(ts.URL+"/api/setup", "application/json", strings.NewReader(`{"username":"admin","password":"secret"}`)); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) == 0 || !resp.Cookies()[0].Secure {
		t.Fatalf("status=%d cookies=%v", resp.StatusCode, resp.Cookies())
	}
}

func TestUnsafeMethodRejectsBadOrigin(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/setup", strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestExportMarksBackupSensitive(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("status=%d disposition=%q sensitive=%q", rr.Code, rr.Header().Get("Content-Disposition"), rr.Header().Get("X-Backup-Contains-Secrets"))
	}
}

func TestPublicSettingsDoesNotRequireAuth(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.SiteName = "GG API"
	cfg.SiteIcon = "/logo.png"
	cfg.EpayBaseURL = "https://pay.example.test"
	cfg.EpayPID = "1000"
	cfg.EpayKey = "secret"
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/public/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["site_name"] != "GG API" || out["site_icon"] != "/logo.png" || len(out) != 2 {
		t.Fatalf("public settings = %#v", out)
	}
}

func TestPublicMonitorStatusDoesNotRequireAuth(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "公开", BaseURL: "https://api.example.test", APIKey: "sk-public", DisplayGroup: "生产", Enabled: true, PublicEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", card.ID, monitor.ProbeResult{Status: monitor.StatusOperational, Input: "公开测试题目", ExpectedAnswer: "banana", Output: "banana"}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/public/monitor/status?window=1h")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(out)
	if strings.Contains(string(body), "sk-public") || !strings.Contains(string(body), "公开测试题目") || !strings.Contains(string(body), "banana") || !strings.Contains(string(body), "生产") {
		t.Fatalf("public body = %s", body)
	}
}

func TestUpdateCardCanClearDisplayGroup(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "卡片", BaseURL: "https://api.example.test", APIKey: "sk-test", DisplayGroup: "生产", SchedulerGroup: "gpt_low", SchedulerChannelID: "9", SchedulerChannelName: "通道", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/cards/"+card.ID, strings.NewReader(`{"display_group":"","scheduler_group":""}`))
	req.SetPathValue("id", card.ID)
	rr := httptest.NewRecorder()
	(&Server{App: app.New(st)}).updateCard(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	got, err := st.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayGroup != "" {
		t.Fatalf("display_group = %q", got.DisplayGroup)
	}
	if got.SchedulerGroup != "" || got.SchedulerChannelID != "" || got.SchedulerChannelName != "" {
		t.Fatalf("scheduler binding = %+v", got)
	}
}

func TestUpdateCardChangingSchedulerChannelClearsAutoDisabled(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "卡片", BaseURL: "https://api.example.test", APIKey: "sk-test", SchedulerGroup: "gpt_low", SchedulerChannelID: "9", SchedulerChannelName: "旧通道", SchedulerAutoDisabled: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/cards/"+card.ID, strings.NewReader(`{"scheduler_channel_id":"10","scheduler_channel_name":"新通道"}`))
	req.SetPathValue("id", card.ID)
	rr := httptest.NewRecorder()
	(&Server{App: app.New(st)}).updateCard(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	got, err := st.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchedulerAutoDisabled || got.SchedulerChannelID != "10" || got.SchedulerChannelName != "新通道" || got.SchedulerGroup != "gpt_low" {
		t.Fatalf("card = %+v", got)
	}
}

func TestSchedulerTierValidationReturnsBadRequest(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/scheduler/config", strings.NewReader(`{"scheduler_tiers":[{"tag":"","group":"gpt_low","price_min":0,"price_max":1}]}`))
	rr := httptest.NewRecorder()
	(&Server{App: app.New(st)}).updateSchedulerConfig(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "分组名称不能为空") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLegacyAdminPathsRedirect(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()
	client := ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	for path, want := range map[string]string{"/status": "/admin/status", "/balances": "/admin/balances", "/revenue": "/admin/revenue", "/merchant-balance": "/admin/revenue", "/admin/merchant-balance": "/admin/revenue", "/upstreams": "/admin/upstreams", "/scheduler": "/admin/scheduler", "/settings": "/admin/settings"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != want {
			t.Fatalf("%s status=%d location=%q", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

func TestRevenueRoutesRequireAuth(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/revenue/today"},
		{http.MethodGet, "/api/revenue/cards"},
		{http.MethodGet, "/api/revenue/cards/card-id/orders"},
		{http.MethodPost, "/api/revenue/cards"},
		{http.MethodPost, "/api/revenue/cards/order"},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(`{}`))
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestSchedulerRoutesRequireAuth(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/scheduler/config"},
		{http.MethodPatch, "/api/scheduler/config"},
		{http.MethodGet, "/api/scheduler/groups"},
		{http.MethodGet, "/api/scheduler/channels"},
		{http.MethodGet, "/api/scheduler/logs"},
		{http.MethodPost, "/api/scheduler/groups/apply"},
		{http.MethodPost, "/api/cards/card-id/scheduler/status"},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(`{}`))
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestTGRoutesRequireAuth(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: app.New(st)}).Routes())
	defer ts.Close()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/tg/session/status"},
		{http.MethodPost, "/api/tg/session/start"},
		{http.MethodGet, "/api/tg/channels"},
		{http.MethodPost, "/api/tg/messages/refresh"},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(`{}`))
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestTGSessionStatusAPI(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveTGSession(t.Context(), domain.TGSession{APIID: 123, APIHash: "hash", Phone: "+100", Authorized: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(t.Context(), "admin", "hash"); err != nil {
		t.Fatal(err)
	}
	user, err := st.UserByUsername(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateSession(t.Context(), user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/tg/session/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rr := httptest.NewRecorder()
	(&Server{App: app.New(st)}).auth((&Server{App: app.New(st)}).tgSessionStatus)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["authorized"] != true || out["configured"] != true || out["phone"] != "+100" {
		t.Fatalf("out = %#v", out)
	}
}
