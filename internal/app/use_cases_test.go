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

func TestCaptureUpstreamBrowserTokensValidatesAndClearsError(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer browser-access" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/user/profile":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"balance": 12.5}})
		case "/api/v1/api-keys", "/api/v1/groups/available":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{
		Name: "S", Type: "sub2api", BaseURL: upstreamServer.URL, Enabled: true,
		LastError: "old error", FailureCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: upstreamServer.Client()}

	out, err := svc.CaptureUpstreamBrowserTokens(t.Context(), u.ID, "browser-access", "browser-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Sub2APIAccessTokenSet || !out.Sub2APIRefreshTokenSet || out.LastError != "" || out.FailureCount != 0 {
		t.Fatalf("out = %+v", out)
	}
	saved, err := st.Upstream(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Sub2APIAccessToken != "browser-access" || saved.Sub2APIRefreshToken != "browser-refresh" || saved.LastError != "" || saved.FailureCount != 0 {
		t.Fatalf("saved = %+v", saved.Public())
	}
}
