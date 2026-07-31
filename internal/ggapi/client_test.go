package ggapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClientSearchesPagesAndConvertsBalance(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("GGAPI 请求必须是 GET: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("管理令牌未正确发送: %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/user/search":
			requests++
			page, _ := strconv.Atoi(r.URL.Query().Get("p"))
			if page == 1 {
				items := make([]map[string]any, pageSize)
				for i := range items {
					items[i] = map[string]any{"id": i + 1, "email": "other@example.com", "role": "user", "status": 1}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": items, "total": pageSize + 1})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": 7, "email": "name@example.com", "username": "name", "role": 1, "status": 1, "quota": 1250000}}, "total": pageSize + 1})
		case "/api/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota_per_unit": 500000, "quota_display_type": "CNY", "usd_exchange_rate": 1.2}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: server.Client()}
	user, err := client.VerifyEmail(context.Background(), "name@example.com")
	if err != nil || user.ID != "7" {
		t.Fatalf("分页邮箱匹配失败: user=%+v err=%v", user, err)
	}
	if requests != 2 {
		t.Fatalf("应查询两页，实际 %d 次", requests)
	}
	balance, err := client.Balance(context.Background(), user)
	if err != nil || balance.Amount != 3 || balance.Currency != "CNY" {
		t.Fatalf("余额换算错误: %+v err=%v", balance, err)
	}
}

func TestClientBalanceMatchesGGAPIDisplayModes(t *testing.T) {
	status := map[string]any{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": status})
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: server.Client()}
	user := User{Quota: 1250000}
	tests := []struct {
		name     string
		status   map[string]any
		amount   float64
		currency string
	}{
		{name: "美元不套用人民币汇率", status: map[string]any{"quota_per_unit": 500000, "quota_display_type": "USD", "usd_exchange_rate": 7.2}, amount: 2.5, currency: "USD"},
		{name: "人民币", status: map[string]any{"quota_per_unit": 500000, "quota_display_type": "CNY", "usd_exchange_rate": 7.2}, amount: 18, currency: "CNY"},
		{name: "自定义币种", status: map[string]any{"quota_per_unit": 500000, "quota_display_type": "CUSTOM", "custom_currency_symbol": "EUR", "custom_currency_exchange_rate": 0.9}, amount: 2.25, currency: "EUR"},
		{name: "令牌", status: map[string]any{"quota_per_unit": 500000, "quota_display_type": "TOKENS"}, amount: 1250000, currency: "Tokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status = test.status
			balance, err := client.Balance(context.Background(), user)
			if err != nil || balance.Amount != test.amount || balance.Currency != test.currency {
				t.Fatalf("余额显示不一致: balance=%+v err=%v", balance, err)
			}
		})
	}
	status = map[string]any{"quota_display_type": "USD"}
	if _, err := client.Balance(context.Background(), user); err == nil {
		t.Fatal("缺少 quota_per_unit 时不应返回错误余额")
	}
}

func TestClientVerifyEmailRejectsAmbiguousOrInvalidUsers(t *testing.T) {
	active := func(id int) map[string]any {
		return map[string]any{"id": id, "email": "name@example.com", "role": 1, "status": 1}
	}
	tests := []struct {
		name  string
		users []map[string]any
	}{
		{name: "重复邮箱", users: []map[string]any{active(1), active(2)}},
		{name: "禁用账号", users: []map[string]any{{"id": 1, "email": "name@example.com", "role": 1, "status": 2}}},
		{name: "删除账号", users: []map[string]any{{"id": 1, "email": "name@example.com", "role": 1, "status": 1, "DeletedAt": "2026-07-31T00:00:00Z"}}},
		{name: "管理员账号", users: []map[string]any{{"id": 1, "email": "name@example.com", "role": 10, "status": 1}}},
		{name: "缺少角色", users: []map[string]any{{"id": 1, "email": "name@example.com", "status": 1}}},
		{name: "缺少状态", users: []map[string]any{{"id": 1, "email": "name@example.com", "role": 1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": test.users})
			}))
			defer server.Close()
			client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: server.Client()}
			if _, err := client.VerifyEmail(context.Background(), "name@example.com"); err == nil {
				t.Fatal("不符合要求的账号不应通过验证")
			}
		})
	}
}

func TestClientLimitsResponsesAndTimeouts(t *testing.T) {
	t.Run("响应上限", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		}))
		defer server.Close()
		client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: server.Client()}
		if _, err := client.GetUser(context.Background(), "1"); err == nil || !strings.Contains(err.Error(), "响应过大") {
			t.Fatalf("应拒绝超限响应: %v", err)
		}
	})

	t.Run("请求超时", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 1}})
		}))
		defer server.Close()
		client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: server.Client(), Timeout: 5 * time.Millisecond}
		if _, err := client.GetUser(context.Background(), "1"); err == nil {
			t.Fatal("超时请求不应成功")
		}
	})
}

func TestClientRecognizesNotFoundEnvelope(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "record not found"})
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: server.Client()}
	if _, err := client.GetUser(context.Background(), "7"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("应识别成功状态中的不存在响应: %v", err)
	}
}

func TestClientRejectsHTTPBaseURL(t *testing.T) {
	client := Client{BaseURL: "http://example.com", AdminToken: "secret"}
	if _, err := client.GetUser(context.Background(), "1"); err == nil {
		t.Fatal("GGAPI 客户端不应允许 HTTP")
	}
}

func TestClientTLSFixtureUsesServerCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // 测试服务证书
	client := Client{BaseURL: server.URL, AdminToken: "secret", HTTP: &http.Client{Transport: transport}}
	if _, err := client.VerifyEmail(context.Background(), "none@example.com"); err == nil {
		t.Fatal("空搜索结果应拒绝绑定")
	}
}
