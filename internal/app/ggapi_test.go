package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func TestGGAPISettingsPartialWriteReturnsReadback(t *testing.T) {
	rules := `[{"name":"codex","model_regex":["^gpt-"],"path_regex":["/v1/responses"],"key_sources":[{"type":"gjson","path":"prompt_cache_key"}],"value_regex":"","ttl_seconds":0,"skip_retry_on_failure":true,"include_using_group":true,"include_model_name":false,"include_rule_name":true,"param_override_template":{"unknown":[1,2]},"future_field":"kept"}]`
	options := ggapiSettingsOptionValues(domain.GGAPISettings{
		RetryTimes: 1, AutomaticRetryStatusCodes: "429,500-503", AutomaticDisableStatusCodes: "401",
		ChannelTestMode: "passive_recovery", AutoTestChannelEnabled: true, AutoTestChannelMinutes: 1,
		AutomaticDisableChannelEnabled: true, AutomaticEnableChannelEnabled: true,
		Affinity: domain.GGAPIAffinitySettings{Enabled: true, SwitchOnSuccess: true, MaxEntries: 1000, DefaultTTLSeconds: 3600, Rules: json.RawMessage(rules)},
	})
	putCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/option/" && r.Method == http.MethodGet:
			items := make([]map[string]any, 0, len(options))
			for key, value := range options {
				items = append(items, map[string]any{"key": key, "value": value})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": items})
		case r.URL.Path == "/api/option/" && r.Method == http.MethodPut:
			putCount++
			var body struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Key == ggapiOptionRetryStatusCodes {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "rejected"})
				return
			}
			options[body.Key] = body.Value
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc, _ := newControlPlaneTestService(t, server)
	settings, err := svc.GGAPISettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	settings.RetryTimes = 2
	settings.AutomaticRetryStatusCodes = "429,500"
	result, err := svc.SaveGGAPISettings(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.FailedKey != ggapiOptionRetryStatusCodes || len(result.Applied) != 1 || result.Settings.RetryTimes != 2 || putCount != 2 {
		t.Fatalf("result=%+v puts=%d", result, putCount)
	}
	if string(result.Settings.Affinity.Rules) != rules {
		t.Fatalf("rules changed: %s", result.Settings.Affinity.Rules)
	}
}

func TestGGAPISettingsDetectsIgnoredWriteOnReadback(t *testing.T) {
	options := ggapiSettingsOptionValues(validGGAPISettingsForTest())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/option/" && r.Method == http.MethodGet:
			items := make([]map[string]any, 0, len(options))
			for key, value := range options {
				items = append(items, map[string]any{"key": key, "value": value})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": items})
		case r.URL.Path == "/api/option/" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc, _ := newControlPlaneTestService(t, server)
	settings := validGGAPISettingsForTest()
	settings.RetryTimes++
	result, err := svc.SaveGGAPISettings(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.FailedKey != ggapiOptionRetryTimes || result.Settings.RetryTimes == settings.RetryTimes {
		t.Fatalf("result=%+v", result)
	}
}

func TestValidateGGAPISettingsRejectsInvalidRule(t *testing.T) {
	settings := domain.GGAPISettings{
		ChannelTestMode: "scheduled_all", AutoTestChannelMinutes: 1,
		Affinity: domain.GGAPIAffinitySettings{MaxEntries: 1, DefaultTTLSeconds: 1, Rules: json.RawMessage(`[{"name":"x","model_regex":["["],"path_regex":[],"key_sources":[{"type":"gjson","path":"id"}]}]`)},
	}
	if _, err := New(nil).Scheduler.SaveGGAPISettings(t.Context(), settings); err == nil || !IsBadRequest(err) {
		t.Fatalf("validation err=%v", err)
	}
}

func TestParseTrafficEventClassifiesAffinitySessionFailure(t *testing.T) {
	event, ok := parseTrafficEvent("error", 5, map[string]any{
		"created_at": timeNowUnix(), "channel": 9, "model_name": "gpt-test", "status_code": 502,
		"other": `{"admin_info":{"channel_affinity":{"rule_name":"codex","using_group":"gpt"}}}`,
	}, map[string]bool{"codex": true})
	if !ok || !event.AffinityHit || event.AffinityRule != "codex" || event.AffinityGroup != "gpt" || !event.SessionScoped {
		t.Fatalf("event=%+v ok=%v", event, ok)
	}
}

func TestParseTrafficEventKeepsExplicitAffinityMiss(t *testing.T) {
	event, ok := parseTrafficEvent("consume", 2, map[string]any{
		"created_at": timeNowUnix(), "channel": 9, "model_name": "gpt-test",
		"other": `{"admin_info":{"channel_affinity":{"rule_name":"codex","using_group":"gpt","hit":false}}}`,
	}, nil)
	if !ok || event.AffinityHit || event.AffinityRule != "codex" || event.AffinityGroup != "gpt" {
		t.Fatalf("event=%+v ok=%v", event, ok)
	}
}

func validGGAPISettingsForTest() domain.GGAPISettings {
	return domain.GGAPISettings{
		RetryTimes: 1, AutomaticRetryStatusCodes: "429,500-503", AutomaticDisableStatusCodes: "401",
		ChannelTestMode: "passive_recovery", AutoTestChannelEnabled: true, AutoTestChannelMinutes: 1,
		AutomaticDisableChannelEnabled: true, AutomaticEnableChannelEnabled: true,
		Affinity: domain.GGAPIAffinitySettings{
			Enabled: true, SwitchOnSuccess: true, MaxEntries: 1000, DefaultTTLSeconds: 3600,
			Rules: json.RawMessage(`[{"name":"codex","model_regex":["^gpt-"],"path_regex":["/v1/responses"],"key_sources":[{"type":"gjson","path":"prompt_cache_key"}],"value_regex":"","ttl_seconds":0,"skip_retry_on_failure":true,"include_using_group":true,"include_model_name":false,"include_rule_name":true}]`),
		},
	}
}

func timeNowUnix() int64 {
	return time.Now().UTC().Unix()
}
