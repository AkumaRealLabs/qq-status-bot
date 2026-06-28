package epay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSummaryReadsMerchantBalanceOnly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("act") != "query" || q.Get("pid") != "1000" || q.Get("key") != "secret" {
			t.Fatalf("bad query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "money": "44.33"})
	}))
	defer ts.Close()

	out := (Client{HTTP: ts.Client()}).MerchantBalance(t.Context(), Config{BaseURL: ts.URL, PID: "1000", Key: "secret"})
	if out.Balance != 44.33 || out.Error != "" {
		t.Fatalf("summary = %+v", out)
	}
}

func TestMerchantBalanceNetworkErrorHidesKey(t *testing.T) {
	out := (Client{HTTP: http.DefaultClient}).MerchantBalance(t.Context(), Config{BaseURL: "http://127.0.0.1:1", PID: "1000", Key: "secret"})
	if out.Error == "" {
		t.Fatal("expected error")
	}
	if strings.Contains(out.Error, "secret") || strings.Contains(out.Error, "key=") {
		t.Fatalf("leaked key in error: %q", out.Error)
	}
}

func TestTodayOrdersReturnsSuccessfulDetails(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	now := time.Now().Format("2006-01-02 15:04:05")
	old := start.Add(-time.Second).Format("2006-01-02 15:04:05")
	var pages int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("act") != "orders" || q.Get("pid") != "1000" || q.Get("key") != "secret" {
			t.Fatalf("bad query: %s", r.URL.RawQuery)
		}
		pages++
		if q.Get("page") == "1" {
			items := []map[string]any{
				{"trade_no": "E-1", "money": "12.34", "type": "alipay", "status": "1", "endtime": now},
				{"trade_no": "E-2", "money": "99", "type": "wxpay", "status": "0", "endtime": now},
			}
			for len(items) < 50 {
				items = append(items, map[string]any{"trade_no": "P", "money": "1", "status": "0", "endtime": now})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "data": items})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "data": []map[string]any{{"trade_no": "OLD", "money": "1", "status": "1", "endtime": old}}})
	}))
	defer ts.Close()

	orders, err := (Client{HTTP: ts.Client()}).TodayOrders(t.Context(), Config{BaseURL: ts.URL, PID: "1000", Key: "secret"}, start)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(orders) != 1 || orders[0].RemoteID != "E-1" || orders[0].Amount != 12.34 || orders[0].PaymentType != "alipay" || orders[0].Status != "success" {
		t.Fatalf("orders=%+v pages=%d", orders, pages)
	}
}
