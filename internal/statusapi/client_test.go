package statusapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const validConfig = `{"config":{"title":"GGAPI","description":"服务状态"},"matrixStatus":"ok","timestamp":123}`
const validMonitor = `{"monitorGroups":[{"id":1,"name":"服务","monitorList":[{"id":7,"name":"稳定池","type":"http"}]}],"data":{"heartbeatList":{"7":[{"status":1,"time":"2026-07-30 12:00:00 +0000","ping":123}]},"uptimeList":{"7_selected":0.999}},"uptimePeriod":"1y","timestamp":456}`

func TestFetchStatusPageAndEscapeQueries(t *testing.T) {
	requests := make(chan *http.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		if strings.HasSuffix(r.URL.Path, "/api/config") {
			_, _ = w.Write([]byte(validConfig))
			return
		}
		_, _ = w.Write([]byte(validMonitor))
	}))
	defer server.Close()

	page, err := (Client{HTTP: server.Client()}).Fetch(t.Context(), server.URL+"/status", "team & one", "1y/all")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "GGAPI" || page.Period != "1y" || len(page.Heartbeats[7]) != 1 || page.Timestamp != 456 {
		t.Fatalf("解析结果错误: %+v", page)
	}
	for range 2 {
		r := <-requests
		if r.URL.Query().Get("pageId") != "team & one" {
			t.Fatalf("pageId 未正确转义: %s", r.URL.RawQuery)
		}
		if strings.HasSuffix(r.URL.Path, "/api/monitor") && r.URL.Query().Get("period") != "1y/all" {
			t.Fatalf("period 未正确转义: %s", r.URL.RawQuery)
		}
	}
}

func TestFetchRejectsUpstreamFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{name: "non-2xx", handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusBadGateway) }, want: "HTTP 502"},
		{name: "invalid json", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{")) }, want: "无效 JSON"},
		{name: "too large", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		}, want: "超过 2 MiB"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			_, err := (Client{HTTP: server.Client()}).Fetch(t.Context(), server.URL, "default", "1y")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want %q", err, test.want)
			}
		})
	}
}

func TestFetchTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(validConfig))
	}))
	defer server.Close()
	_, err := (Client{HTTP: server.Client(), Timeout: 20 * time.Millisecond}).Fetch(context.Background(), server.URL, "default", "1y")
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("预期超时，实际 err=%v", err)
	}
}

func TestFetchRejectsMissingHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/config") {
			_, _ = w.Write([]byte(validConfig))
			return
		}
		_, _ = fmt.Fprint(w, `{"monitorGroups":[{"name":"服务","monitorList":[{"id":7,"name":"稳定池"}]}],"data":{"heartbeatList":{}}}`)
	}))
	defer server.Close()
	_, err := (Client{HTTP: server.Client()}).Fetch(t.Context(), server.URL, "default", "1y")
	if err == nil || !strings.Contains(err.Error(), "缺少心跳") {
		t.Fatalf("err=%v", err)
	}
}

func TestFetchRejectsInvalidURL(t *testing.T) {
	_, err := (Client{}).Fetch(t.Context(), "file:///tmp/status", "default", "1y")
	if err == nil || !strings.Contains(err.Error(), "HTTP/HTTPS") {
		t.Fatalf("err=%v", err)
	}
}
