package browsercdp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFetchExpressionKeepsRequestDataStructured(t *testing.T) {
	expr, err := fetchExpression("POST", "https://example.test/api", []byte(`{"value":"x"}`), map[string]string{"Authorization": "Bearer token"})
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(expr, "const request = ")
	end := strings.Index(expr[start:], ";\nconst response")
	if start < 0 || end < 0 {
		t.Fatalf("unexpected expression: %s", expr)
	}
	raw := expr[start+len("const request = ") : start+end]
	var request struct {
		URL     string `json:"url"`
		Options struct {
			Method      string            `json:"method"`
			Headers     map[string]string `json:"headers"`
			Body        string            `json:"body"`
			Credentials string            `json:"credentials"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatal(err)
	}
	if request.URL != "https://example.test/api" || request.Options.Method != "POST" || request.Options.Body != `{"value":"x"}` {
		t.Fatalf("request = %+v", request)
	}
	if request.Options.Headers["Authorization"] != "Bearer token" || request.Options.Headers["Content-Type"] != "application/json" || request.Options.Credentials != "include" {
		t.Fatalf("options = %+v", request.Options)
	}
}

func TestClientUsesLoopbackHostHeaderForDockerDNS(t *testing.T) {
	c := Client{DebugURL: "http://browser:9222"}
	if got := c.hostHeader(); got != "127.0.0.1:19222" {
		t.Fatalf("hostHeader = %q", got)
	}
	if got := c.connectAddress(); got != "browser:9222" {
		t.Fatalf("connectAddress = %q", got)
	}
}

func TestClientRealOptIn(t *testing.T) {
	if os.Getenv("AUM_REAL_BROWSER_CDP_TEST") != "1" {
		t.Skip("set AUM_REAL_BROWSER_CDP_TEST=1 with browser and upstream environment variables")
	}
	debugURL := os.Getenv("AUM_REAL_BROWSER_DEBUG_URL")
	baseURL := strings.TrimRight(os.Getenv("AUM_REAL_BROWSER_BASE_URL"), "/")
	token := os.Getenv("AUM_REAL_BROWSER_TOKEN")
	if debugURL == "" || baseURL == "" || token == "" {
		t.Fatal("AUM_REAL_BROWSER_DEBUG_URL, AUM_REAL_BROWSER_BASE_URL and AUM_REAL_BROWSER_TOKEN are required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	status, body, err := (Client{DebugURL: debugURL, HostHeader: os.Getenv("AUM_REAL_BROWSER_HOST_HEADER")}).Do(
		ctx, http.MethodGet, baseURL+"/api/v1/user/profile", nil, map[string]string{"Authorization": "Bearer " + token},
	)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || response["data"] == nil {
		t.Fatalf("status=%d code=%v message=%v", status, response["code"], response["message"])
	}
}
