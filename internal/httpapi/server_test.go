package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ai-upstream-monitor/internal/app"
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
