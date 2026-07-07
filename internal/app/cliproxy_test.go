package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

func TestCLIProxyAuthFilesFlow(t *testing.T) {
	var sawAuth, sawKey, uploadedName, uploadedContent, deletedName, resetAuthIndex string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth, sawKey = r.Header.Get("Authorization"), r.Header.Get("X-Management-Key")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{
				"name": "acc.json", "provider": "codex", "status": "ready", "email": "a@example.test",
				"account_type": "team", "auth_index": "7", "size": 12, "success": 2, "failed": 1,
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/auth-files":
			uploadedName = r.URL.Query().Get("name")
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			b, _ := io.ReadAll(file)
			uploadedContent = string(b)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") != "acc.json" {
				t.Fatalf("download name = %q", r.URL.Query().Get("name"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v0/management/auth-files":
			deletedName = r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/reset-quota":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			resetAuthIndex = body["auth_index"]
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "models": []string{"gpt-5.5"}})
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	svc := testCLIProxyService(t, ts.URL, "secret")
	svc.Client = monitor.Client{HTTP: ts.Client()}

	accounts, err := svc.CLIProxyAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer secret" || sawKey != "secret" {
		t.Fatalf("headers auth=%q key=%q", sawAuth, sawKey)
	}
	if len(accounts) != 1 || accounts[0].Name != "acc.json" || accounts[0].AuthIndex != "7" || accounts[0].Success != 2 || accounts[0].Failed != 1 {
		t.Fatalf("accounts = %+v", accounts)
	}
	if err := svc.UploadCLIProxyAccount(t.Context(), "acc.json", `{"token":"x"}`); err != nil {
		t.Fatal(err)
	}
	if uploadedName != "acc.json" || uploadedContent != `{"token":"x"}` {
		t.Fatalf("upload name=%q content=%q", uploadedName, uploadedContent)
	}
	b, contentType, err := svc.DownloadCLIProxyAccount(t.Context(), "acc.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"ok":true}` || !strings.Contains(contentType, "application/json") {
		t.Fatalf("download body=%q type=%q", string(b), contentType)
	}
	if err := svc.DeleteCLIProxyAccount(t.Context(), "acc.json"); err != nil {
		t.Fatal(err)
	}
	if deletedName != "acc.json" {
		t.Fatalf("deleted name = %q", deletedName)
	}
	out, err := svc.ResetCLIProxyQuota(t.Context(), "acc.json")
	if err != nil {
		t.Fatal(err)
	}
	if resetAuthIndex != "7" || out.AuthIndex != "7" || len(out.Models) != 1 {
		t.Fatalf("resetAuthIndex=%q out=%+v", resetAuthIndex, out)
	}
}

func TestCLIProxyConfigDoesNotReturnSecret(t *testing.T) {
	svc := testCLIProxyService(t, "http://127.0.0.1:8317/v0/management", "secret")
	cfg, err := svc.CLIProxyConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManagementKey != "" || !cfg.ManagementKeySet {
		t.Fatalf("public cfg = %+v", cfg)
	}
	stored, err := svc.Store.CLIProxyConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stored.ManagementKey != "secret" {
		t.Fatalf("stored key = %q", stored.ManagementKey)
	}
}

func TestCLIProxyHTTPStatusHasClearError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "nope"})
			}))
			defer ts.Close()
			svc := testCLIProxyService(t, ts.URL, "secret")
			svc.Client = monitor.Client{HTTP: ts.Client()}
			_, err := svc.CLIProxyAccounts(t.Context())
			if err == nil {
				t.Fatal("expected error")
			}
			if code, ok := ErrorStatus(err); !ok || code != http.StatusBadGateway {
				t.Fatalf("status error = %v code=%d ok=%v", err, code, ok)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("CLIProxyAPI HTTP %d", status)) || !strings.Contains(err.Error(), "nope") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func testCLIProxyService(t *testing.T, baseURL, key string) *Service {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.Store.UpdateCLIProxyConfig(t.Context(), domain.CLIProxyConfig{Name: "CPA", BaseURL: baseURL, ManagementKey: key, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return svc
}
