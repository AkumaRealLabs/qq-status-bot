package epay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if out.MerchantBalance != 44.33 || out.Error != "" {
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
