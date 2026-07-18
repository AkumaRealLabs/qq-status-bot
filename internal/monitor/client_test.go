package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/browsercdp"
)

type browserHTTPFunc func(context.Context, string, string, []byte, map[string]string) (int, []byte, error)

func (f browserHTTPFunc) Do(ctx context.Context, method, rawURL string, body []byte, headers map[string]string) (int, []byte, error) {
	return f(ctx, method, rawURL, body, headers)
}

func TestProbeSendsFixedModelPayload(t *testing.T) {
	var saw bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body struct {
			Model string `json:"model"`
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
			MaxOutputTokens int  `json:"max_output_tokens"`
			Stream          bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		saw = body.Model == "gpt-5.5" &&
			len(body.Input) == 1 &&
			body.Input[0].Role == "user" &&
			len(body.Input[0].Content) == 1 &&
			body.Input[0].Content[0].Type == "input_text" &&
			body.Input[0].Content[0].Text == "ping" &&
			body.MaxOutputTokens == 2 &&
			!body.Stream
		_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "pong"})
	}))
	defer s.Close()

	got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
	if !saw || !got.Success || got.Status != StatusOperational || got.Input != "ping" {
		t.Fatalf("saw=%v got=%+v", saw, got)
	}
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
	if err == nil || !strings.Contains(err.Error(), "浏览器登录") || !strings.Contains(err.Error(), "采集 Token") {
		t.Fatalf("err = %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("requestCount = %d, want 0", requestCount)
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

func TestProbeExtractsNestedResponseText(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{
				"content": []any{map[string]any{"text": "Lake!"}},
			}},
		})
	}))
	defer s.Close()

	got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
	if got.Status != StatusOperational || got.Output != "Lake!" || !got.Success {
		t.Fatalf("got=%+v", got)
	}
}

func TestProbeExtractsSSEText(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"pong"}` + "\n\n"))
		_, _ = w.Write([]byte("event: response.output_text.done\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.done","text":"pong"}` + "\n\n"))
	}))
	defer s.Close()

	got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
	if got.Status != StatusOperational || got.Output != "pong" || !got.Success {
		t.Fatalf("got=%+v", got)
	}
}

func TestProbeClassifiesFailures(t *testing.T) {
	oldDegraded := degradedAfter
	defer func() {
		degradedAfter = oldDegraded
	}()

	t.Run("non empty reply succeeds", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "blue"})
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusOperational || !got.Success || got.Output != "blue" {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("empty reply", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": ""})
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusFailed || got.Success {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("http error keeps body", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad key", http.StatusUnauthorized)
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusFailed || got.HTTPStatus != http.StatusUnauthorized || got.Error == "" {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("non json success keeps body", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("error code: 1010"))
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusFailed || got.HTTPStatus != http.StatusOK || !strings.Contains(got.Error, "error code: 1010") {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("degraded", func(t *testing.T) {
		degradedAfter = -time.Nanosecond
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "pong"})
		}))
		defer s.Close()
		got := (Client{HTTP: s.Client()}).Probe(t.Context(), s.URL, "sk-test", "gpt-5.5")
		if got.Status != StatusDegraded || !got.Success {
			t.Fatalf("got=%+v", got)
		}
	})
}

func TestProbeCodexCLIUsesTempConfigAndEnvKey(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fake.log")
	t.Setenv("AUM_FAKE_CODEX_LOG", logPath)
	fake := fakeCodex(t, `#!/bin/sh
set -eu
case " $* " in *" --ask-for-approval "*) echo "bad approval arg" >&2; exit 15;; esac
config="$CODEX_HOME/config.toml"
[ "$AUM_CODEX_API_KEY" = "sk-card-secret" ] || { echo "missing card key" >&2; exit 13; }
grep -q 'base_url = "https://codex.example.test/v1"' "$config" || { cat "$config" >&2; exit 10; }
grep -q 'env_key = "AUM_CODEX_API_KEY"' "$config" || exit 11
! grep -q 'sk-card-secret' "$config" || { echo "key leaked" >&2; exit 12; }
instr=$(grep '^model_instructions_file = ' "$config" | sed 's/model_instructions_file = "\(.*\)"/\1/')
[ -f "$instr" ] || { echo "missing instructions" >&2; exit 16; }
grep -qx 'Answer briefly\.' "$instr" || { cat "$instr" >&2; exit 17; }
{
  printf 'args:%s\n' "$*"
  cat "$config"
} > "$AUM_FAKE_CODEX_LOG"
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
    break
  fi
  shift
done
[ -n "$out" ] || exit 14
printf 'pong\n' > "$out"
`)

	got := (Client{ProbeMode: ProbeModeCLI, CodexPath: fake}).Probe(t.Context(), "https://codex.example.test", "sk-card-secret", "gpt-5.5")
	if !got.Success || got.Status != StatusOperational || got.HTTPStatus != 0 || got.Output != "pong" || got.Input != "ping" {
		t.Fatalf("got=%+v", got)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logBytes)
	for _, want := range []string{"args:exec", " ping", "approval_policy=\"never\"", "--skip-git-repo-check", "--ephemeral", "--ignore-rules", "model_provider = \"aum_card\"", "model_instructions_file = ", "project_doc_max_bytes = 0", "web_search = \"disabled\"", "model_reasoning_effort = \"low\"", "model_verbosity = \"low\"", "model_reasoning_summary = \"none\"", "inherit = \"none\"", "disable_response_storage = true", "wire_api = \"responses\""} {
		if !strings.Contains(logText, want) {
			t.Fatalf("fake codex log missing %q:\n%s", want, logText)
		}
	}
}

func TestProbeCodexCLIFailureIsRecordedAndRedacted(t *testing.T) {
	fake := fakeCodex(t, `#!/bin/sh
echo "user" >&2
echo "ping" >&2
echo "warning: Codex could not find bubblewrap on PATH." >&2
echo "ERROR: exceeded retry limit, last status: 429 Too Many Requests, request id: req-1 sk-card-secret" >&2
echo "ERROR: exceeded retry limit, last status: 429 Too Many Requests, request id: req-1 sk-card-secret" >&2
exit 42
`)
	got := (Client{ProbeMode: ProbeModeCLI, CodexPath: fake}).Probe(t.Context(), "https://codex.example.test", "sk-card-secret", "gpt-5.5")
	if got.Success || got.Status != StatusError || !strings.Contains(got.Error, "429 Too Many Requests") || !strings.Contains(got.Error, "[redacted]") || strings.Contains(got.Error, "sk-card-secret") || strings.Contains(got.Error, "ping") || strings.Contains(got.Error, "bubblewrap") {
		t.Fatalf("got=%+v", got)
	}
}

func TestCodexCLIErrorKeepsUpstreamMessage(t *testing.T) {
	output := []byte(`ERROR: {
  "error": {
    "message": "Unsupported value: 'minimal' is not supported with the 'gpt-5.5' model. Supported values are: 'none', 'low', 'medium', 'high', and 'xhigh'.",
    "type": "invalid_request_error",
    "param": "reasoning.effort"
  }
}`)
	got := codexCLIError(errors.New("exit status 1"), output, "")
	if !strings.Contains(got, "Unsupported value: 'minimal'") || strings.Contains(got, "invalid_request_error") {
		t.Fatalf("got=%q", got)
	}
}

func TestIsInternalProbeError(t *testing.T) {
	if !IsInternalProbeError("model instructions file is empty: /tmp/aum-codex-probe-123") {
		t.Fatal("expected internal probe error")
	}
	for _, output := range []string{
		`WARNING: proceeding, even though we could not create PATH aliases: Refusing to create helper binaries under temporary dir "/tmp" (codex_home: AbsolutePathBuf("/tmp/aum-codex-probe-123/codex-home"))
ERROR: unexpected status 502 Bad Gateway: error code: 502`,
		`WARNING: proceeding, even though we could not create PATH aliases: Refusing to create helper binaries under temporary dir "/tmp" (codex_home: AbsolutePathBuf("/tmp/aum-codex-probe-123/codex-home"))
ERROR: unexpected status 503 Service Unavailable: Service temporarily unavailable`,
	} {
		if IsInternalProbeError(output) {
			t.Fatalf("upstream CLI failure misclassified as internal: %q", output)
		}
		got := codexCLIError(errors.New("exit status 1"), []byte(output), "")
		if !strings.Contains(got, "unexpected status") {
			t.Fatalf("upstream CLI failure was not preserved: %q", got)
		}
	}
}

func TestProbeCodexCLIRealOptIn(t *testing.T) {
	if os.Getenv("AUM_REAL_CODEX_CLI_TEST") != "1" {
		t.Skip("set AUM_REAL_CODEX_CLI_TEST=1 with AUM_REAL_CODEX_BASE_URL and AUM_REAL_CODEX_API_KEY")
	}
	baseURL, key := os.Getenv("AUM_REAL_CODEX_BASE_URL"), os.Getenv("AUM_REAL_CODEX_API_KEY")
	if baseURL == "" || key == "" {
		t.Skip("AUM_REAL_CODEX_BASE_URL and AUM_REAL_CODEX_API_KEY are required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	got := (Client{ProbeMode: ProbeModeCLI}).Probe(ctx, baseURL, key, "gpt-5.5")
	if !got.Success {
		t.Fatalf("got=%+v", got)
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

func fakeCodex(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
