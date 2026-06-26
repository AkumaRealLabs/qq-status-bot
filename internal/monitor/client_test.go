package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewAPIHeadersKeysAndProbe(t *testing.T) {
	var sawAuth, sawUser, sawProbe bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "tok" {
			sawAuth = true
		}
		if r.Header.Get("New-Api-User") == "42" {
			sawUser = true
		}
		switch r.URL.Path {
		case "/api/user/self":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota": 9, "used_quota": 1}})
		case "/api/token/":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "k1", "key": "sk-****", "name": "main", "group": "fast"}}})
		case "/api/user/self/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"fast": map[string]any{"desc": "高并发-特惠通道", "ratio": 0.08},
			}})
		case "/api/token/k1/key":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"key": "sk-live"}})
		case "/v1/responses":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			sawProbe = body["model"] == "gpt-test" && body["input"] == "ping" && body["stream"] == false
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ok"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()

	u := &Upstream{Type: "newapi", BaseURL: s.URL, UserID: "42", AccessToken: "tok"}
	got, err := (Client{HTTP: s.Client()}).Check(t.Context(), u, "gpt-test", "sk-live")
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth || !sawUser || !sawProbe || !got.Probe.Success || got.Keys[0].Key != "sk-live" ||
		got.Keys[0].Description != "高并发-特惠通道" || got.Keys[0].Group != "fast" || got.Keys[0].GroupRatio != "0.08" {
		t.Fatalf("bad result: auth=%v user=%v probe=%v got=%+v", sawAuth, sawUser, sawProbe, got)
	}
}

func TestSub2APIRefreshFallback(t *testing.T) {
	var loginUsed bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			http.Error(w, "expired", http.StatusUnauthorized)
		case "/api/v1/auth/login":
			loginUsed = true
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": "new-access", "refresh_token": "new-refresh"}})
		case "/api/v1/user/profile":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Fatalf("bad auth header %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"balance": 3}})
		case "/api/v1/api-keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []any{
				map[string]any{"id": 1, "key": "sk-sub", "name": "sub-main", "group_id": 1},
			}}})
		case "/api/v1/groups/available":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"id": 1, "name": "ChatGPT", "description": "高并发-稳定通道", "rate_multiplier": 0.1},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()

	u := &Upstream{Type: "sub2api", BaseURL: s.URL, Email: "a@b.c", Password: "pw", Sub2APIRefreshToken: "old"}
	got, err := (Client{HTTP: s.Client()}).Check(t.Context(), u, "gpt-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if !loginUsed || u.Sub2APIAccessToken != "new-access" || got.Probe.Error != "未选择 Key" ||
		got.Keys[0].Description != "高并发-稳定通道" || got.Keys[0].Group != "ChatGPT" || got.Keys[0].GroupRatio != "0.1" {
		t.Fatalf("bad fallback: login=%v upstream=%+v result=%+v", loginUsed, u, got)
	}
}

func TestSub2APIUsesCapturedAccessToken(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh", "/api/v1/auth/login":
			t.Fatalf("should use captured access token, got %s", r.URL.Path)
		case "/api/v1/user/profile":
			if r.Header.Get("Authorization") != "Bearer captured" {
				t.Fatalf("bad auth header %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"balance": 3}})
		case "/api/v1/api-keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []any{
				map[string]any{"id": 1, "key": "sk-sub", "group": map[string]any{"id": 2, "name": "ChatGPT", "description": "高并发-特惠通道", "rate_multiplier": "0.08x"}},
			}}})
		case "/api/v1/groups/available":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()

	u := &Upstream{Type: "sub2api", BaseURL: s.URL, Sub2APIAccessToken: "captured"}
	got, err := (Client{HTTP: s.Client()}).Check(t.Context(), u, "gpt-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Keys[0].Description != "高并发-特惠通道" || got.Keys[0].Group != "ChatGPT" || got.Keys[0].GroupRatio != "0.08" {
		t.Fatalf("bad key metadata: %+v", got)
	}
}
