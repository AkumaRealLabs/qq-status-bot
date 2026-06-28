package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTodayOrderRevenueNewapiPaginatesAndStopsAtOlderOrder(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	now := time.Now().Format(time.RFC3339)
	old := start.Add(-time.Second).Format(time.RFC3339)
	var pages int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/topup" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "admin-token" || r.Header.Get("New-Api-User") != "1" {
			t.Fatalf("bad newapi admin auth: auth=%q user=%q", r.Header.Get("Authorization"), r.Header.Get("New-Api-User"))
		}
		pages++
		if r.URL.Query().Get("p") == "1" {
			items := []map[string]any{
				{"amount": 10000, "money": 10, "status": "success", "complete_time": time.Now().Unix()},
				{"amount": 2, "status": "paid", "created_at": now},
				{"amount": 99, "status": "pending", "created_at": now},
			}
			for len(items) < revenueOrderPageSize {
				items = append(items, map[string]any{"amount": 1, "status": "pending", "created_at": now})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []map[string]any{
			{"amount": 100, "status": "completed", "created_at": old},
		}}})
	}))
	defer ts.Close()

	out, err := (Client{HTTP: ts.Client()}).TodayOrderRevenue(t.Context(), &Upstream{Type: "newapi", BaseURL: ts.URL, UserID: "1", AccessToken: "admin-token"}, start)
	if err != nil {
		t.Fatal(err)
	}
	if out.Revenue != 12 || pages != 2 {
		t.Fatalf("out=%+v pages=%d", out, pages)
	}
}

func TestTodayRevenueOrdersReturnsDetails(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/topup" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []map[string]any{
			{"trade_no": "N-1", "amount": 10000, "money": 10.5, "payment_method": "stripe", "status": "success", "complete_time": time.Now().Unix()},
			{"trade_no": "N-2", "money": 99, "status": "pending", "complete_time": time.Now().Unix()},
		}}})
	}))
	defer ts.Close()

	orders, err := (Client{HTTP: ts.Client()}).TodayRevenueOrders(t.Context(), &Upstream{Type: "newapi", BaseURL: ts.URL, UserID: "1", AccessToken: "token"}, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].RemoteID != "N-1" || orders[0].Amount != 10.5 || orders[0].PaymentType != "stripe" {
		t.Fatalf("orders = %+v", orders)
	}
}

func TestTodayOrderRevenueSub2apiCountsCompletedOrders(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/payment/orders" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("bad authorization: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"orders": []map[string]any{
			{"amount": 7.25, "status": "COMPLETED", "paid_at": time.Now().UnixMilli()},
			{"amount": 3, "status": "failed", "paid_at": time.Now().UnixMilli()},
		}}})
	}))
	defer ts.Close()

	out, err := (Client{HTTP: ts.Client()}).TodayOrderRevenue(t.Context(), &Upstream{Type: "sub2api", BaseURL: ts.URL, Sub2APIAccessToken: "token"}, start)
	if err != nil {
		t.Fatal(err)
	}
	if out.Revenue != 7.25 {
		t.Fatalf("out = %+v", out)
	}
}

func TestTodayOrderRevenueSub2apiUsesAdminAPIKey(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/payment/orders" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatalf("bad api key: %q", r.Header.Get("x-api-key"))
		}
		if r.URL.Query().Get("page_size") != "50" {
			t.Fatalf("bad query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []map[string]any{
			{"amount": 8.5, "status": "COMPLETED", "paid_at": time.Now().UnixMilli()},
		}}})
	}))
	defer ts.Close()

	out, err := (Client{HTTP: ts.Client()}).TodayOrderRevenue(t.Context(), &Upstream{Type: "sub2api", BaseURL: ts.URL, AdminAPIKey: "admin-secret"}, start)
	if err != nil {
		t.Fatal(err)
	}
	if out.Revenue != 8.5 {
		t.Fatalf("out = %+v", out)
	}
}
