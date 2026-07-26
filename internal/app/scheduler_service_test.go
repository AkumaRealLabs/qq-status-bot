package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"ai-upstream-monitor/internal/domain"
)

// 没配 GGAPI 连接时读分组/渠道要报 400，而不是打空地址。
func TestSchedulerReadsRequireGGAPIConfig(t *testing.T) {
	svc, _ := newOpsTestService(t)
	if _, err := svc.Scheduler.SchedulerGroups(t.Context()); !IsBadRequest(err) {
		t.Fatalf("SchedulerGroups 应报 400，实际 %v", err)
	}
	if _, err := svc.Scheduler.SchedulerChannels(t.Context(), ""); !IsBadRequest(err) {
		t.Fatalf("SchedulerChannels 应报 400，实际 %v", err)
	}
}

// AxonHub 模式下分组是固定的托管标签，不该去打 GGAPI。
func TestSchedulerGroupsReturnsAxonHubTags(t *testing.T) {
	svc, st := newOpsTestService(t)
	cfg, err := st.SchedulerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Provider = domain.SchedulerProviderAxonHub
	if _, err := st.UpdateSchedulerConfig(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	groups, err := svc.Scheduler.SchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].Name != domain.AxonHubTagLow || groups[1].Name != domain.AxonHubTagStable {
		t.Fatalf("groups = %+v", groups)
	}
}

// /api/user/self/groups 失败时要回落到 /api/user/groups。
func TestSchedulerGroupsFallsBackToLegacyPath(t *testing.T) {
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		if r.URL.Path == "/api/user/self/groups" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"vip": 1.5}})
	}))
	defer server.Close()

	svc, _ := newCostSyncTestService(t, server)
	groups, err := svc.Scheduler.SchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "vip" || groups[0].Ratio != "1.5" {
		t.Fatalf("groups = %+v", groups)
	}
	if len(hits) != 2 || hits[0] != "/api/user/self/groups" || hits[1] != "/api/user/groups" {
		t.Fatalf("请求路径 = %+v", hits)
	}
}

// success:false 要把上游文案带出来，而不是当成空结果。
func TestSchedulerGroupsSurfacesUpstreamMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "令牌无效"})
	}))
	defer server.Close()

	svc, _ := newCostSyncTestService(t, server)
	_, err := svc.Scheduler.SchedulerGroups(t.Context())
	if err == nil || err.Error() != "令牌无效" {
		t.Fatalf("err = %v", err)
	}
}

// 渠道列表满页要继续翻页，不足 100 条才停。
func TestFetchSchedulerChannelsPaginates(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("p")
		pages = append(pages, page)
		items := []any{}
		count := 100
		if page == "2" {
			count = 3
		}
		for i := 0; i < count; i++ {
			items = append(items, map[string]any{"id": page + "-" + strconv.Itoa(i), "name": "ch"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": items})
	}))
	defer server.Close()

	svc, _ := newCostSyncTestService(t, server)
	channels, err := svc.Scheduler.SchedulerChannels(t.Context(), "关键字")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 103 {
		t.Fatalf("应取满两页共 103 条，实际 %d", len(channels))
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Fatalf("翻页 = %+v", pages)
	}
}

func TestSchedulerConfigRedactsSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	svc, _ := newCostSyncTestService(t, server)

	cfg, err := svc.Scheduler.SchedulerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessToken != "" {
		t.Fatalf("AccessToken 不应出现在响应里：%q", cfg.AccessToken)
	}
	if !cfg.AccessTokenSet {
		t.Fatal("已配置时应返回 AccessTokenSet=true")
	}
}

// 档位非法要在落库前拒绝。
func TestSaveSchedulerConfigRejectsInvalidTiers(t *testing.T) {
	svc, _ := newOpsTestService(t)
	_, err := svc.Scheduler.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: "http://x", UserID: "1", AccessToken: "t",
		Tiers: []domain.SchedulerTier{{Tag: "", Group: ""}},
	})
	if !IsBadRequest(err) {
		t.Fatalf("非法档位应报 400，实际 %v", err)
	}
}

func TestSchedulerLogsScopedToProvider(t *testing.T) {
	svc, st := newOpsTestService(t)
	if err := st.CreateSchedulerLog(t.Context(), domain.SchedulerLog{Action: "group_sync", Status: "ok", Provider: "ggapi"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSchedulerLog(t.Context(), domain.SchedulerLog{Action: "group_sync", Status: "ok", Provider: "axonhub"}); err != nil {
		t.Fatal(err)
	}
	logs, err := svc.Scheduler.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if l.Provider != "ggapi" {
			t.Fatalf("默认 provider 为 ggapi，不应返回 %q", l.Provider)
		}
	}
	if len(logs) != 1 {
		t.Fatalf("应只返回 ggapi 的 1 条，实际 %d", len(logs))
	}
}
