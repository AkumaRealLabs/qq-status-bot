package httpapi

import (
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
