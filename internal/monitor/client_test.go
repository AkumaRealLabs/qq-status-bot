package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func fakeCodex(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
