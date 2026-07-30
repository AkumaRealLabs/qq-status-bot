package browsercdp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTabUsesPutAndLoopbackHostHeader(t *testing.T) {
	var method, host, requestURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, host, requestURI = r.Method, r.Host, r.RequestURI
		_ = json.NewEncoder(w).Encode(debugTab{ID: "tab-1", WebSocketDebuggerURL: "ws://127.0.0.1/devtools/page/tab-1"})
	}))
	defer server.Close()

	client := Client{DebugURL: server.URL, HostHeader: "127.0.0.1:9222", HTTP: server.Client()}
	tab, err := client.createTab(t.Context(), "https://status.ggapi.cc/?x=1")
	if err != nil {
		t.Fatal(err)
	}
	if tab.ID != "tab-1" || method != http.MethodPut || host != "127.0.0.1:9222" {
		t.Fatalf("请求错误: tab=%+v method=%s host=%s", tab, method, host)
	}
	if !strings.HasPrefix(requestURI, "/json/new?") || !strings.Contains(requestURI, "status.ggapi.cc") {
		t.Fatalf("创建页面 URL 错误: %s", requestURI)
	}
}

func TestDockerDebugURLUsesLoopbackHandshakeHost(t *testing.T) {
	client := Client{DebugURL: "http://browser:9222"}
	if got := client.hostHeader(); got != "127.0.0.1:9222" {
		t.Fatalf("hostHeader=%q", got)
	}
	if got := client.connectAddress(); got != "browser:9222" {
		t.Fatalf("connectAddress=%q", got)
	}
}
