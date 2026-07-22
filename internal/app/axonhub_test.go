package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

type axonHubRemote struct {
	mu           sync.Mutex
	status       string
	tags         []string
	weight       int
	fieldWrites  int
	statusWrites int
}

func newAxonHubTestServer(t *testing.T, remote *axonHubRemote) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/graphql" || r.Header.Get("Authorization") != "Bearer control" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		remote.mu.Lock()
		defer remote.mu.Unlock()
		channel := func() map[string]any {
			return map[string]any{"id": "9", "name": "GPT 渠道", "type": "openai", "status": remote.status, "orderingWeight": remote.weight, "tags": remote.tags, "allModelEntries": []map[string]string{{"requestModel": "gpt-5.6-sol"}}}
		}
		data := map[string]any{}
		switch {
		case strings.Contains(request.Query, "updateChannelStatus"):
			remote.status, _ = request.Variables["status"].(string)
			remote.statusWrites++
			data["updateChannelStatus"] = channel()
		case strings.Contains(request.Query, "updateChannel"):
			input, _ := request.Variables["input"].(map[string]any)
			if tags, ok := input["tags"].([]any); ok {
				remote.tags = make([]string, 0, len(tags))
				for _, tag := range tags {
					remote.tags = append(remote.tags, tag.(string))
				}
			}
			if weight, ok := input["orderingWeight"].(float64); ok {
				remote.weight = int(weight)
			}
			remote.fieldWrites++
			data["updateChannel"] = channel()
		default:
			data["allChannelSummarys"] = []map[string]any{channel()}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func configureAxonHub(t *testing.T, svc *Service, baseURL, mode string) {
	t.Helper()
	if _, err := svc.Store.UpdateAxonHubConfig(t.Context(), domain.AxonHubConfig{BaseURL: baseURL, APIKey: "control", ControlMode: mode}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.UpdateSchedulerProvider(t.Context(), domain.SchedulerProviderAxonHub); err != nil {
		t.Fatal(err)
	}
}

func TestAxonHubObserveAndExternalTakeoverNeverWrite(t *testing.T) {
	remote := &axonHubRemote{status: domain.AxonHubStatusEnabled, tags: []string{"manual"}, weight: 55}
	server := newAxonHubTestServer(t, remote)
	defer server.Close()
	svc := testBackgroundService(t)
	configureAxonHub(t, svc, server.URL, domain.AxonHubControlObserve)
	if _, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{Name: "低价", BaseURL: "https://upstream.invalid", APIKey: "secret", Model: "gpt-5.6-sol", ManualCostRatio: "0.05", PoolEnabled: true, PoolEnabledSet: true, Enabled: true, AxonHubChannelID: "9"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	if remote.fieldWrites != 0 || remote.statusWrites != 0 {
		t.Fatalf("observe writes fields=%d status=%d", remote.fieldWrites, remote.statusWrites)
	}
	if _, err := svc.AdoptAxonHubControlPlaneChannel(t.Context(), "9"); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.weight = 77
	remote.mu.Unlock()
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	life, found, err := svc.Store.AxonHubChannelLifecycle(t.Context(), "9")
	if err != nil || !found || !life.ExternalTakeover || life.Owner != domain.ControlOwnerExternal {
		t.Fatalf("life=%+v found=%v err=%v", life, found, err)
	}
	if remote.fieldWrites != 0 || remote.statusWrites != 0 {
		t.Fatalf("external takeover wrote fields=%d status=%d", remote.fieldWrites, remote.statusWrites)
	}
}

func TestAxonHubActiveWritesOnlyAfterAdoptionAndBalancesRecover(t *testing.T) {
	remote := &axonHubRemote{status: domain.AxonHubStatusEnabled, tags: []string{"manual"}, weight: 30}
	server := newAxonHubTestServer(t, remote)
	defer server.Close()
	svc := testBackgroundService(t)
	configureAxonHub(t, svc, server.URL, domain.AxonHubControlActive)
	upstream, err := svc.Store.CreateUpstream(t.Context(), domain.Upstream{ID: "u1", Name: "上游", Type: "newapi", BaseURL: "https://upstream.invalid", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.SaveKeys(t.Context(), upstream.ID, []monitor.APIKey{{RemoteID: "k1", Name: "key", GroupRatio: "0.05"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := svc.Store.ListKeys(t.Context(), upstream.ID)
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys=%+v err=%v", keys, err)
	}
	if _, err := svc.Store.UpdateAvailabilityPolicy(t.Context(), upstream.ID, domain.AvailabilityPolicy{BalanceGuardMode: domain.BalanceGuardActive, BalanceCloseThreshold: 1, BalanceRecoverThreshold: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.SaveBalance(t.Context(), upstream.ID, monitor.Balance{Remain: 0.5 * 500000}, "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.CreateCard(t.Context(), domain.ModelCard{Name: "低价", UpstreamID: upstream.ID, KeyID: keys[0].ID, Model: "gpt-5.6-sol", PoolEnabled: true, PoolEnabledSet: true, Enabled: true, AxonHubChannelID: "9"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	if remote.fieldWrites != 0 || remote.statusWrites != 0 {
		t.Fatalf("first baseline wrote fields=%d status=%d", remote.fieldWrites, remote.statusWrites)
	}
	if _, err := svc.AdoptAxonHubControlPlaneChannel(t.Context(), "9"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	gotTags, gotStatus, gotWeight := append([]string(nil), remote.tags...), remote.status, remote.weight
	remote.mu.Unlock()
	if !domain.SameGroups(gotTags, []string{"manual", domain.AxonHubTagLow}) || gotWeight != 100 || gotStatus != domain.AxonHubStatusDisabled {
		t.Fatalf("tags=%v weight=%d status=%s", gotTags, gotWeight, gotStatus)
	}
	life, _, err := svc.Store.AxonHubChannelLifecycle(t.Context(), "9")
	if err != nil || !life.AUMDisabled {
		t.Fatalf("life=%+v err=%v", life, err)
	}
	life.AUMDisabledAt = time.Now().UTC().Add(-16 * time.Minute)
	if err := svc.Store.SaveAxonHubChannelLifecycle(t.Context(), life); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.SaveBalance(t.Context(), upstream.ID, monitor.Balance{Remain: 3 * 500000}, "", 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	gotStatus = remote.status
	remote.mu.Unlock()
	if gotStatus != domain.AxonHubStatusEnabled {
		t.Fatalf("recovered status=%s", gotStatus)
	}
	remote.mu.Lock()
	remote.status = domain.AxonHubStatusDisabled
	writesBefore := remote.statusWrites
	remote.mu.Unlock()
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Scheduler.ReconcileAxonHub(t.Context()); err != nil {
		t.Fatal(err)
	}
	life, _, err = svc.Store.AxonHubChannelLifecycle(t.Context(), "9")
	remote.mu.Lock()
	statusWrites := remote.statusWrites
	remote.mu.Unlock()
	if err != nil || !life.ExternalTakeover || life.AUMDisabled || statusWrites != writesBefore {
		t.Fatalf("external life=%+v writes=%d before=%d err=%v", life, statusWrites, writesBefore, err)
	}
}
