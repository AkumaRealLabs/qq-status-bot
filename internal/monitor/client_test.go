package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/browsercdp"
)

type browserHTTPFunc func(context.Context, string, string, []byte, map[string]string) (int, []byte, error)

func (f browserHTTPFunc) Do(ctx context.Context, method, rawURL string, body []byte, headers map[string]string) (int, []byte, error) {
	return f(ctx, method, rawURL, body, headers)
}

func TestApplySub2TokensPreservesRefreshWhenMissing(t *testing.T) {
	u := &Upstream{Sub2APIAccessToken: "old-access", Sub2APIRefreshToken: "old-refresh"}
	if !applySub2Tokens(u, map[string]any{"data": map[string]any{"access_token": "new-access"}}) {
		t.Fatal("token apply failed")
	}
	if u.Sub2APIAccessToken != "new-access" || u.Sub2APIRefreshToken != "old-refresh" {
		t.Fatalf("tokens = %+v", u)
	}
	if !applySub2Tokens(u, map[string]any{"data": map[string]any{"access_token": "newer-access", "refresh_token": "new-refresh"}}) {
		t.Fatal("token apply failed")
	}
	if u.Sub2APIAccessToken != "newer-access" || u.Sub2APIRefreshToken != "new-refresh" {
		t.Fatalf("tokens = %+v", u)
	}
}

func TestSub2APIAuthExplainsMissingCredentialsWithoutLoginRequest(t *testing.T) {
	requestCount := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer s.Close()

	err := (Client{HTTP: s.Client()}).sub2apiForceAuth(t.Context(), &Upstream{BaseURL: s.URL})
	if err == nil || !strings.Contains(err.Error(), "邮箱密码") || !strings.Contains(err.Error(), "CF 浏览器登录") || !strings.Contains(err.Error(), "采集 Token") {
		t.Fatalf("err = %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("requestCount = %d, want 0", requestCount)
	}
}

func TestSub2APILoginUsesEmailPasswordWithoutBrowser(t *testing.T) {
	loginRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			http.NotFound(w, r)
			return
		}
		loginRequests++
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["email"] != "user@example.com" || body["password"] != "secret" {
			t.Fatalf("body=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": "access", "refresh_token": "refresh"}})
	}))
	defer server.Close()
	browserCalled := false
	upstream := &Upstream{BaseURL: server.URL, Email: "user@example.com", Password: "secret"}
	err := (Client{HTTP: server.Client(), Browser: browserHTTPFunc(func(context.Context, string, string, []byte, map[string]string) (int, []byte, error) {
		browserCalled = true
		return 0, nil, nil
	})}).sub2apiForceAuth(t.Context(), upstream)
	if err != nil || loginRequests != 1 || browserCalled || upstream.Sub2APIAccessToken != "access" || upstream.Sub2APIRefreshToken != "refresh" {
		t.Fatalf("err=%v loginRequests=%d browserCalled=%v upstream=%+v", err, loginRequests, browserCalled, upstream)
	}
}

func TestSub2APILoginExplainsCloudflareBrowserFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<div id="cf-error-details">Cloudflare Ray ID: abc</div>`))
	}))
	defer server.Close()
	err := (Client{HTTP: server.Client()}).sub2apiForceAuth(t.Context(), &Upstream{
		BaseURL: server.URL, Email: "user@example.com", Password: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "CF 浏览器登录") {
		t.Fatalf("err=%v", err)
	}
}

func TestSub2APIAuthExplainsInvalidTokenWithoutPasswordFallback(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
	}))
	defer s.Close()

	err := (Client{HTTP: s.Client()}).sub2apiForceAuth(t.Context(), &Upstream{BaseURL: s.URL, Sub2APIRefreshToken: "expired"})
	if err == nil || !strings.Contains(err.Error(), "Token 已失效") || !strings.Contains(err.Error(), "重新") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoJSONRetriesSub2APICloudflare1010InBrowser(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error code: 1010", http.StatusForbidden)
	}))
	defer s.Close()

	called := false
	c := Client{
		HTTP: s.Client(),
		Browser: browserHTTPFunc(func(_ context.Context, method, rawURL string, body []byte, headers map[string]string) (int, []byte, error) {
			called = true
			if method != http.MethodGet || rawURL != s.URL+"/api/v1/user/profile" || len(body) != 0 || headers["Authorization"] != "Bearer token" {
				t.Fatalf("browser request method=%s url=%s body=%q headers=%v", method, rawURL, body, headers)
			}
			return http.StatusOK, []byte(`{"data":{"balance":12.5}}`), nil
		}),
	}
	var raw map[string]any
	if err := c.doJSON(t.Context(), http.MethodGet, s.URL+"/api/v1/user/profile", nil, bearer("token"), &raw); err != nil {
		t.Fatal(err)
	}
	if !called || num(obj(raw["data"])["balance"]) != 12.5 {
		t.Fatalf("called=%v raw=%v", called, raw)
	}
}

func TestDoJSONDoesNotRetryUnrelatedCloudflare1010(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error code: 1010", http.StatusForbidden)
	}))
	defer s.Close()

	c := Client{
		HTTP: s.Client(),
		Browser: browserHTTPFunc(func(context.Context, string, string, []byte, map[string]string) (int, []byte, error) {
			t.Fatal("unexpected browser retry")
			return 0, nil, nil
		}),
	}
	var raw map[string]any
	err := c.doJSON(t.Context(), http.MethodPost, s.URL+"/v1/responses", map[string]string{"input": "ping"}, nil, &raw)
	var httpErr httpStatusError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
}

func TestShouldRetryInBrowserRecognizesCloudflareBlockPage(t *testing.T) {
	body := []byte(`<div id="cf-error-details"><span>Cloudflare Ray ID: abc</span></div>`)
	if !shouldRetryInBrowser("https://example.test/api/v1/user/profile", http.StatusForbidden, body) {
		t.Fatal("expected Cloudflare block page retry")
	}
	if shouldRetryInBrowser("https://example.test/v1/responses", http.StatusForbidden, body) {
		t.Fatal("non-sub2api path must not use browser retry")
	}
	if shouldRetryInBrowser("https://example.test/api/v1/user/profile", http.StatusUnauthorized, body) {
		t.Fatal("non-403 response must not use browser retry")
	}
}

func TestShouldRetryInBrowserRecognizesSessionBindingMismatch(t *testing.T) {
	body := []byte(`{"code":"SESSION_BINDING_MISMATCH","message":"Session network fingerprint changed, please login again"}`)
	if !shouldRetryInBrowser("https://example.test/api/v1/user/profile", http.StatusUnauthorized, body) {
		t.Fatal("expected browser-bound session retry")
	}
	if shouldRetryInBrowser("https://example.test/v1/responses", http.StatusUnauthorized, body) {
		t.Fatal("non-sub2api path must not use browser retry")
	}
}

func TestSub2APICheckBrowserFallbackRealOptIn(t *testing.T) {
	if os.Getenv("AUM_REAL_BROWSER_CDP_TEST") != "1" {
		t.Skip("set AUM_REAL_BROWSER_CDP_TEST=1 with browser and upstream environment variables")
	}
	debugURL := os.Getenv("AUM_REAL_BROWSER_DEBUG_URL")
	baseURL := strings.TrimRight(os.Getenv("AUM_REAL_BROWSER_BASE_URL"), "/")
	token := os.Getenv("AUM_REAL_BROWSER_TOKEN")
	if debugURL == "" || baseURL == "" || token == "" {
		t.Fatal("AUM_REAL_BROWSER_DEBUG_URL, AUM_REAL_BROWSER_BASE_URL and AUM_REAL_BROWSER_TOKEN are required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	c := Client{
		HTTP: &http.Client{Timeout: 15 * time.Second},
		Browser: browsercdp.Client{
			DebugURL:   debugURL,
			HostHeader: os.Getenv("AUM_REAL_BROWSER_HOST_HEADER"),
		},
	}
	u := &Upstream{Type: "sub2api", BaseURL: baseURL, Sub2APIAccessToken: token}
	if _, err := c.sub2apiBalance(ctx, u); err != nil {
		t.Fatalf("balance: %v", err)
	}
	keys, err := c.sub2apiKeys(ctx, u)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("browser fallback check returned no API keys")
	}
}
