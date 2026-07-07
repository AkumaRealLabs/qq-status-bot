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
	var apiCallAuthIndex string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth, sawKey = r.Header.Get("Authorization"), r.Header.Get("X-Management-Key")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{
				"name": "acc.json", "provider": "codex", "status": "ready", "email": "a@example.test",
				"account_type": "team", "account_id": "acct_1", "auth_index": "7", "size": 12, "success": 2, "failed": 1,
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
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			apiCallAuthIndex = fmt.Sprint(body["authIndex"])
			header, _ := body["header"].(map[string]any)
			if body["method"] != http.MethodGet || body["url"] != cliproxyCodexUsageURL || header["Chatgpt-Account-Id"] != "acct_1" {
				t.Fatalf("api-call body = %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"plan_type":                "team",
					"rate_limit_reset_credits": map[string]any{"available_count": 1},
					"rate_limit": map[string]any{
						"secondary_window": map[string]any{
							"used_percent":         71,
							"limit_window_seconds": 2592000,
							"reset_at":             1786045440,
						},
					},
				},
			})
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
	if err := svc.UploadCLIProxyAccount(t.Context(), "acc.json", `{"type":"codex","access_token":"x"}`); err != nil {
		t.Fatal(err)
	}
	var uploaded map[string]any
	if err := json.Unmarshal([]byte(uploadedContent), &uploaded); err != nil {
		t.Fatalf("uploaded content is not json: %v", err)
	}
	if uploadedName != "acc.json" || uploaded["type"] != "codex" || uploaded["access_token"] != "x" {
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
	quota, err := svc.CLIProxyAccountQuota(t.Context(), "acc.json", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if apiCallAuthIndex != "7" || quota.PlanType != "team" || quota.RateLimitResetCreditsAvailable == nil || *quota.RateLimitResetCreditsAvailable != 1 {
		t.Fatalf("apiCallAuthIndex=%q quota=%+v", apiCallAuthIndex, quota)
	}
	if len(quota.Windows) != 1 || quota.Windows[0].Label != "月限额" || quota.Windows[0].RemainingPercent == nil || *quota.Windows[0].RemainingPercent != 29 {
		t.Fatalf("quota windows = %+v", quota.Windows)
	}
}

func TestCLIProxyUploadConvertsSub2APIAccount(t *testing.T) {
	content, err := normalizeCLIProxyAuthJSON(`{
		"name": "acct",
		"platform": "openai",
		"type": "oauth",
		"credentials": {
			"access_token": "at",
			"refresh_token": "rt",
			"expires_at": "2026-08-05T13:40:42Z"
		},
		"extra": {"ignored": true}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "codex" || out["auth_kind"] != "oauth" || out["access_token"] != "at" || out["refresh_token"] != "rt" || out["expired"] != "2026-08-05T13:40:42Z" {
		t.Fatalf("converted = %+v", out)
	}
	if _, ok := out["extra"]; ok {
		t.Fatalf("sub2api extra leaked into auth file: %+v", out)
	}
}

func TestCLIProxyUploadConvertsSub2APICodexSession(t *testing.T) {
	content, err := normalizeCLIProxyAuthJSON(`{
		"user": {"id": "u1", "email": "a@example.test"},
		"account": {"id": "acct_1", "planType": "plus"},
		"accessToken": "at",
		"sessionToken": "ignored"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "codex" || out["access_token"] != "at" || out["email"] != "a@example.test" || out["account_id"] != "acct_1" || out["plan_type"] != "plus" {
		t.Fatalf("converted = %+v", out)
	}
	if _, ok := out["sessionToken"]; ok {
		t.Fatalf("sessionToken should be dropped: %+v", out)
	}
}

func TestCLIProxyUploadConvertsTopLevelSub2APIOAuth(t *testing.T) {
	content, err := normalizeCLIProxyAuthJSON(`{"type":"oauth","access_token":"at","refresh_token":"rt"}`)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "codex" || out["access_token"] != "at" || out["refresh_token"] != "rt" {
		t.Fatalf("converted = %+v", out)
	}
}

func TestCLIProxyUploadPreservesCPAAuthJSON(t *testing.T) {
	files, err := normalizeCLIProxyAuthUploads("", `{"access_token":"at","refresh_token":"rt","email":"a@example.test","disabled":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "codex-a-example-test.json" {
		t.Fatalf("files = %+v", files)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(files[0].Content), &out); err != nil {
		t.Fatal(err)
	}
	if out["access_token"] != "at" || out["refresh_token"] != "rt" || out["disabled"] != false {
		t.Fatalf("preserved = %+v", out)
	}
	if _, ok := out["auth_kind"]; ok {
		t.Fatalf("cpa auth should not be rewritten: %+v", out)
	}
}

func TestCLIProxyUploadRejectsMultiAccountSub2APIExport(t *testing.T) {
	_, err := normalizeCLIProxyAuthJSON(`{"type":"sub2api-data","accounts":[{"platform":"openai","type":"oauth","credentials":{"access_token":"a"}},{"platform":"openai","type":"oauth","credentials":{"access_token":"b"}}]}`)
	if err == nil || !strings.Contains(err.Error(), "多个账号") {
		t.Fatalf("err = %v", err)
	}
}

func TestCLIProxyUploadAutoNamesAndSplitsSub2APIExport(t *testing.T) {
	files, err := normalizeCLIProxyAuthUploads("", `{"type":"sub2api-data","accounts":[
		{"platform":"openai","type":"oauth","credentials":{"access_token":"a","email":"a@example.test"}},
		{"platform":"openai","type":"oauth","credentials":{"access_token":"b","email":"b@example.test"}}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files len = %d", len(files))
	}
	if files[0].Name != "codex-a-example-test.json" || files[1].Name != "codex-b-example-test.json" {
		t.Fatalf("names = %q, %q", files[0].Name, files[1].Name)
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
