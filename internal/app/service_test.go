package app

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"

	"github.com/gotd/td/tg"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSetupOnlyOnce(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.Setup(t.Context(), "admin", "secret12"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Setup(t.Context(), "admin2", "secret12"); err == nil {
		t.Fatal("second setup succeeded")
	}
}

func TestSetupConcurrentCreatesOneUser(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	start := make(chan struct{})
	var wg sync.WaitGroup
	success := make(chan struct{}, 8)
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if _, err := svc.Setup(t.Context(), "admin"+string(rune('0'+i)), "secret12"); err == nil {
				success <- struct{}{}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatalf("successful setups = %d, want 1", len(success))
	}
	n, err := st.UserCount(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func TestUpstreamRowsReturnsEmptyKeysArray(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.SaveUpstream(t.Context(), "", domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.UpstreamRows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "null" || !json.Valid(body) {
		t.Fatalf("bad json: %s", body)
	}
	if !strings.Contains(string(body), `"keys":[]`) {
		t.Fatalf("keys not encoded as empty array: %s", body)
	}
}

func TestEffectiveRatioUsesBalanceRate(t *testing.T) {
	if got := domain.EffectiveRatio("0.045", 2); got != "0.09" {
		t.Fatalf("ratio = %q, want 0.09", got)
	}
	if got := domain.EffectiveRatio("custom", 2); got != "custom" {
		t.Fatalf("non-numeric ratio = %q, want custom", got)
	}
}

func TestProfitRequiresSchedulerConfig(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	out, err := New(st).Profit(t.Context(), "24h")
	if err == nil || out.Window != "24h" {
		t.Fatalf("profit=%+v err=%v", out, err)
	}
}

func TestTodayStartUsesConfiguredTimezone(t *testing.T) {
	oldLocal := time.Local
	t.Cleanup(func() { time.Local = oldLocal })
	t.Setenv("TZ", "Asia/Shanghai")
	time.Local = appLocation()

	start := todayStart()
	_, offset := start.Zone()
	now := time.Now().In(start.Location())
	if start.Location().String() != "Asia/Shanghai" || offset != 8*60*60 || start.Hour() != 0 || start.YearDay() != now.YearDay() {
		t.Fatalf("todayStart = %s, want Asia/Shanghai midnight", start)
	}
}

func TestNewDefaultsToCodexCLIProbeWithHTTPFallback(t *testing.T) {
	t.Setenv("AUM_PROBE_MODE", "")
	if got := New(nil).Client.ProbeMode; got != monitor.ProbeModeCLI {
		t.Fatalf("default probe mode = %q", got)
	}
	t.Setenv("AUM_PROBE_MODE", "http")
	if got := New(nil).Client.ProbeMode; got != monitor.ProbeModeHTTP {
		t.Fatalf("fallback probe mode = %q", got)
	}
}

func TestCheckBrowserCDPFallsBackToJSONList(t *testing.T) {
	var sawVersion, sawList bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			sawVersion = true
			http.Error(w, "cdp version unavailable", http.StatusInternalServerError)
		case "/json":
			sawList = true
			_, _ = w.Write([]byte("[]"))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer ts.Close()

	got := checkBrowserCDP(t.Context(), ts.Client(), ts.URL)
	if got.Status != "ok" || !sawVersion || !sawList {
		t.Fatalf("got=%+v sawVersion=%v sawList=%v", got, sawVersion, sawList)
	}
}

func TestCheckBrowserCDPUsesLocalChromeHostHeader(t *testing.T) {
	var gotHost, gotPath string
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotHost = r.Host
		gotPath = r.URL.Path
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: http.NoBody, Header: make(http.Header), Request: r}, nil
	})}

	got := checkBrowserCDP(t.Context(), hc, "http://browser:9222")
	if got.Status != "ok" || gotPath != "/json/version" || gotHost != "127.0.0.1:19222" {
		t.Fatalf("got=%+v path=%q host=%q", got, gotPath, gotHost)
	}
}

func TestSendTelegramIncludesUpstreamError(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.TelegramBotToken = " token "
	cfg.TelegramChatID = " 100 "
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}

	var gotPath, gotBody string
	svc := New(st)
	svc.Client = monitor.Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)),
			Request:    r,
		}, nil
	})}}
	err = svc.sendTelegram(t.Context(), "hi")
	if err == nil || !strings.Contains(err.Error(), "Bad Request: chat not found") {
		t.Fatalf("err = %v", err)
	}
	if gotPath != "/bottoken/sendMessage" || !strings.Contains(gotBody, "chat_id=100") {
		t.Fatalf("request path=%q body=%q", gotPath, gotBody)
	}
}

func TestSchedulerConfigAndChannelsProxy(t *testing.T) {
	var sawAuth, sawUser, sawQuery, sawPageSize, sawPage string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth, sawUser, sawQuery = r.Header.Get("Authorization"), r.Header.Get("New-Api-User"), r.URL.Query().Get("keyword")
		sawPageSize = r.URL.Query().Get("page_size")
		sawPage = r.URL.Query().Get("p")
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{
			"id": 9, "name": "Claude", "status": 1, "priority": 12, "weight": 34, "tag": "fast", "type": "openai", "group": "vip", "models": []string{"gpt-5.5"},
		}}}})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	cfg, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL + "/", UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != ts.URL {
		t.Fatalf("base url = %q", cfg.BaseURL)
	}
	channels, err := svc.SchedulerChannels(t.Context(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "token" || sawUser != "42" || sawQuery != "claude" || sawPageSize != "100" || sawPage != "1" {
		t.Fatalf("headers/query = auth=%q user=%q keyword=%q page_size=%q p=%q", sawAuth, sawUser, sawQuery, sawPageSize, sawPage)
	}
	if len(channels) != 1 || channels[0].ID != "9" || channels[0].Name != "Claude" || channels[0].Status != 1 || channels[0].Priority != 12 || channels[0].Weight != 34 || channels[0].Models[0] != "gpt-5.5" {
		t.Fatalf("channels = %+v", channels)
	}
}

func TestApplySchedulerGroupsUsesPriceTiers(t *testing.T) {
	currentGroups := map[int]string{9: "", 10: ""}
	currentPriorities := map[int]int64{9: 1, 10: 1}
	weights := map[int]uint{9: 7, 10: 3}
	updated := map[int]string{}
	updatedPriorities := map[int]int64{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("page_size") != "100" || r.URL.Query().Get("p") != "1" {
				t.Fatalf("bad channel page query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"id": 9, "group": currentGroups[9], "priority": currentPriorities[9], "weight": weights[9]},
				{"id": 10, "group": currentGroups[10], "priority": currentPriorities[10], "weight": weights[10]},
			}}})
			return
		}
		if r.Method != http.MethodPut {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		var body struct {
			ID       int    `json:"id"`
			Group    string `json:"group"`
			Priority *int64 `json:"priority"`
			Weight   *uint  `json:"weight"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Priority == nil || body.Weight != nil {
			t.Fatalf("priority/weight body = %+v", body)
		}
		currentGroups[body.ID] = body.Group
		currentPriorities[body.ID] = *body.Priority
		updated[body.ID] = body.Group
		updatedPriorities[body.ID] = *body.Priority
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{
		{RemoteID: "low", Name: "low", GroupRatio: "0.025"},
		{RemoteID: "stable", Name: "stable", GroupRatio: "0.06"},
	}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	keyID := map[string]string{}
	for _, key := range keys {
		keyID[key.RemoteID] = key.ID
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "low", UpstreamID: u.ID, KeyID: keyID["low"], SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "stable", UpstreamID: u.ID, KeyID: keyID["stable"], SchedulerChannelID: "10", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{
			{Tag: "gpt_low", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1},
			{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0, PriceMax: 0.15},
		},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApplySchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 2 || out.Unchanged != 0 || updated[9] != "gpt_low,gpt_stable" || updated[10] != "gpt_stable" || updatedPriorities[9] != 100 || updatedPriorities[10] != 99 || weights[9] != 7 || weights[10] != 3 {
		t.Fatalf("out=%+v updated=%+v priorities=%+v weights=%+v", out, updated, updatedPriorities, weights)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != "group_sync" || logs[0].Status != "success" || !strings.Contains(logs[0].Message, "更新 2 个") || !strings.Contains(logs[0].Message, "优先级 1 -> 100") {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestApplySchedulerGroupsUsesCustomManualCostAndSkipsMonitorOnly(t *testing.T) {
	currentGroups := map[int]string{9: "", 10: "", 11: ""}
	currentPriorities := map[int]int64{9: 7, 10: 8, 11: 9}
	updated := map[int]string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"id": 9, "group": currentGroups[9], "priority": currentPriorities[9]},
				{"id": 10, "group": currentGroups[10], "priority": currentPriorities[10]},
				{"id": 11, "group": currentGroups[11], "priority": currentPriorities[11]},
			}}})
		case http.MethodPut:
			var body struct {
				ID       int    `json:"id"`
				Group    string `json:"group"`
				Priority int64  `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			currentGroups[body.ID] = body.Group
			currentPriorities[body.ID] = body.Priority
			updated[body.ID] = body.Group
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "纯监控", BaseURL: "https://monitor.example.test", APIKey: "sk", PoolEnabled: false, PoolEnabledSet: true, SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "无成本", BaseURL: "https://missing.example.test", APIKey: "sk", SchedulerChannelID: "10", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "手动成本", BaseURL: "https://manual.example.test", APIKey: "sk", ManualCostRatio: "0.14", SchedulerChannelID: "11", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{
			{Tag: "gpt_low", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1, SalePrice: 0.1},
			{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.25, SalePrice: 0.25},
		},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApplySchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 1 || out.Skipped != 1 || updated[11] != "gpt_stable" || currentPriorities[11] != 100 {
		t.Fatalf("out=%+v updated=%+v", out, updated)
	}
	if _, ok := updated[9]; ok {
		t.Fatalf("pure monitor was updated: %+v", updated)
	}
}

func TestApplySchedulerGroupsKeepsManualGroupsAndCountsUnchanged(t *testing.T) {
	currentGroups := map[int]string{9: "gpt_low,gpt_stable", 10: "gpt_low,manual", 11: "manual,gpt_stable"}
	currentPriorities := map[int]int64{9: 100, 10: 99, 11: 100}
	updated := map[int]string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"id": 9, "group": currentGroups[9], "priority": currentPriorities[9]},
				{"id": 10, "group": currentGroups[10], "priority": currentPriorities[10]},
				{"id": 11, "group": currentGroups[11], "priority": currentPriorities[11]},
			}}})
		case http.MethodPut:
			var body struct {
				ID       int    `json:"id"`
				Group    string `json:"group"`
				Priority int64  `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			currentGroups[body.ID] = body.Group
			currentPriorities[body.ID] = body.Priority
			updated[body.ID] = body.Group
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{
		{RemoteID: "stable", Name: "stable", GroupRatio: "0.14"},
		{RemoteID: "expensive", Name: "expensive", GroupRatio: "0.5"},
	}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	keyID := map[string]string{}
	for _, key := range keys {
		keyID[key.RemoteID] = key.ID
	}
	for _, row := range []struct {
		name      string
		keyID     string
		channelID string
	}{
		{"stable-old-both", keyID["stable"], "9"},
		{"expensive-manual", keyID["expensive"], "10"},
		{"stable-correct", keyID["stable"], "11"},
	} {
		if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: row.name, UpstreamID: u.ID, KeyID: row.keyID, SchedulerChannelID: row.channelID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{
			{Tag: "gpt_low", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1, SalePrice: 0.1},
			{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.25, SalePrice: 0.25},
		},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApplySchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 2 || out.Unchanged != 1 || out.Skipped != 0 || updated[9] != "gpt_stable" || updated[10] != "manual" {
		t.Fatalf("out=%+v updated=%+v", out, updated)
	}
	if _, ok := updated[11]; ok {
		t.Fatalf("unchanged channel was updated: %+v", updated)
	}
}

func TestApplySchedulerGroupsMovesOutOfRangeToUnassigned(t *testing.T) {
	currentGroups := map[int]string{9: "gpt_stable"}
	currentPriority := int64(1)
	var puts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"id": 9, "name": "expensive", "group": currentGroups[9], "priority": currentPriority},
			}}})
		case http.MethodPut:
			var body struct {
				ID       int    `json:"id"`
				Group    string `json:"group"`
				Priority int64  `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			currentGroups[body.ID] = body.Group
			currentPriority = body.Priority
			puts++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "expensive", Name: "expensive", GroupRatio: "0.5"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "expensive", UpstreamID: u.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.15, SalePrice: 0.25}},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApplySchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 1 || out.Skipped != 0 || puts != 1 || currentGroups[9] != "unassigned" || len(logs) != 1 || !strings.Contains(logs[0].Message, "分组 gpt_stable -> unassigned") {
		t.Fatalf("out=%+v puts=%d groups=%+v logs=%+v", out, puts, currentGroups, logs)
	}
}

func TestApplySchedulerGroupsLogsWhenWriteIsIgnored(t *testing.T) {
	var puts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			// 模拟 new-api：PUT success 但 group 不变
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"id": 9, "name": "expensive", "group": "gpt_stable", "priority": 1, "weight": 1},
			}}})
		case http.MethodPut:
			puts++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "expensive", Name: "expensive", GroupRatio: "0.5"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "expensive", UpstreamID: u.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0.1, PriceMax: 0.15, SalePrice: 0.25}},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ApplySchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if out.Updated != 0 || out.Skipped != 1 || puts != 1 || len(logs) != 1 || logs[0].Status != "error" || !strings.Contains(logs[0].Message, "校验失败") {
		t.Fatalf("out=%+v puts=%d logs=%+v", out, puts, logs)
	}
}

func TestApplySchedulerGroupsRequiresUnassignedGroup(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	// 直写 store 绕过 Save 校验，模拟旧库未配置 unassigned
	if _, err := st.UpdateSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: "https://scheduler.example.test", UserID: "1", AccessToken: "token",
		Tiers: []domain.SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 0.1, SalePrice: 0.1}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplySchedulerGroups(t.Context()); err == nil {
		t.Fatal("expected error when unassigned group missing")
	}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: "https://scheduler.example.test", UserID: "1", AccessToken: "token", UnassignedGroup: "",
		Tiers: []domain.SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 0.1, SalePrice: 0.1}},
	}); err == nil {
		t.Fatal("expected save to require unassigned group")
	}
}

func TestCheckAllSyncsSchedulerGroupsOnce(t *testing.T) {
	var channelGets, channelPuts, tokenHits int
	currentGroup := ""
	currentPriority := int64(1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota": 100}})
		case "/api/token/":
			tokenHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "k", "key": "sk", "name": "K", "group": "g"}}})
		case "/api/user/self/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"g": map[string]any{"ratio": "0.05"}}})
		case "/api/channel/":
			if r.Method == http.MethodPut {
				channelPuts++
				var body struct {
					ID       int    `json:"id"`
					Group    string `json:"group"`
					Priority int64  `json:"priority"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				currentGroup = body.Group
				currentPriority = body.Priority
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
				return
			}
			channelGets++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "group": currentGroup, "priority": currentPriority}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u1, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: ts.URL, BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "B", Type: "newapi", BaseURL: ts.URL, BalanceRate: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u1.ID, []monitor.APIKey{{RemoteID: "k", Name: "K", Key: "sk", GroupRatio: "0.05"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", UpstreamID: u1.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	// 1 次列表 GET + 写后校验 1 次 GET；PUT 仅 1 次（整轮 CheckAll 同步一次）
	if tokenHits != 2 || channelGets != 2 || channelPuts != 1 {
		t.Fatalf("tokenHits=%d channelGets=%d channelPuts=%d", tokenHits, channelGets, channelPuts)
	}
}

func TestCheckAllGroupSyncFailureDoesNotStopCardProbe(t *testing.T) {
	var probeHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"quota": 100}})
		case "/api/token/":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "k", "key": "sk", "name": "K", "group": "g"}}})
		case "/api/user/self/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"g": map[string]any{"ratio": "0.05"}}})
		case "/api/channel/":
			http.Error(w, "scheduler down", http.StatusBadGateway)
		case "/v1/responses":
			probeHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "pong"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: ts.URL, BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "k", Name: "K", Key: "sk", GroupRatio: "0.05"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", UpstreamID: u.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if probeHits != 1 || len(logs) != 1 || logs[0].Action != "group_sync" || logs[0].Status != "error" {
		t.Fatalf("probeHits=%d logs=%+v", probeHits, logs)
	}
}

func TestSaveUpstreamSyncsGroupsOnlyWhenBalanceRateChanges(t *testing.T) {
	currentGroup := "gpt_low"
	currentPriority := int64(100)
	var gets, puts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			gets++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "group": currentGroup, "priority": currentPriority}}}})
			return
		}
		puts++
		var body struct {
			Group    string `json:"group"`
			Priority int64  `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		currentGroup = body.Group
		currentPriority = body.Priority
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "k", Name: "K", GroupRatio: "0.06"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", UpstreamID: u.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{
			{Tag: "gpt_low", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1, SalePrice: 0.1},
			{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0, PriceMax: 0.25, SalePrice: 0.25},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveUpstream(t.Context(), u.ID, domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if gets != 0 || puts != 0 {
		t.Fatalf("unchanged rate synced: gets=%d puts=%d", gets, puts)
	}
	if _, err := svc.SaveUpstream(t.Context(), u.ID, domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 2, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// 列表 1 次 + 写后校验 1 次
	if gets != 2 || puts != 1 || currentGroup != "gpt_stable" {
		t.Fatalf("gets=%d puts=%d group=%q", gets, puts, currentGroup)
	}
}

func TestProfitUsageUnitsUsesOriginalQuota(t *testing.T) {
	got, ok := domain.UsageUnits(50000, 0.1)
	if !ok || got != 1 {
		t.Fatalf("usage=%v ok=%v", got, ok)
	}
}

func TestProfitFromSchedulerLogs(t *testing.T) {
	var logTime time.Time
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/log/" || r.URL.Query().Get("type") != "2" || r.URL.Query().Get("p") != "1" || r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("bad log request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		switch r.URL.Query().Get("group") {
		case "gpt_low":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"quota": 100 * 500000 * 0.1, "channel": 9, "group": "gpt_low", "created_at": logTime.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
				{"quota": 10 * 500000 * 0.1, "channel": 99, "group": "gpt_low", "created_at": logTime.Format(time.RFC3339Nano), "other": `{"group_ratio":0.1}`},
			}}})
		case "gpt_stable":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
				{"quota": 100 * 500000 * 0.2, "channel": "10", "group": "gpt_stable", "created_at": logTime.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.2}},
			}}})
		default:
			t.Fatalf("unexpected group %q", r.URL.Query().Get("group"))
		}
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	low, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "low-up", Type: "newapi", BaseURL: "https://low.example.test", BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	stable, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "stable-up", Type: "newapi", BaseURL: "https://stable.example.test", BalanceRate: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), low.ID, []monitor.APIKey{{RemoteID: "low", Name: "low-key", GroupRatio: "0.08"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), stable.ID, []monitor.APIKey{{RemoteID: "stable", Name: "stable-key", GroupRatio: "0.15"}}); err != nil {
		t.Fatal(err)
	}
	lowKeys, _ := st.ListKeys(t.Context(), low.ID)
	stableKeys, _ := st.ListKeys(t.Context(), stable.ID)
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "low-card", UpstreamID: low.ID, KeyID: lowKeys[0].ID, SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "stable-card", UpstreamID: stable.ID, KeyID: stableKeys[0].ID, SchedulerChannelID: "10", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{
		BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",
		Tiers: []domain.SchedulerTier{
			{Tag: "gpt_low", Group: "gpt_low", PriceMin: 0, PriceMax: 0.1, SalePrice: 0.1},
			{Tag: "gpt_stable", Group: "gpt_stable", PriceMin: 0, PriceMax: 0.25, SalePrice: 0.25},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedSchedulerSnapshots(t.Context()); err != nil {
		t.Fatal(err)
	}
	logTime = time.Now().UTC().Add(time.Millisecond)
	out, err := svc.Profit(t.Context(), "24h")
	if err != nil {
		t.Fatal(err)
	}
	lowPool := profitTestPool(out.Pools, "gpt_low")
	stablePool := profitTestPool(out.Pools, "gpt_stable")
	if out.Complete || !closeEnough(out.Revenue, 35) || !closeEnough(out.Cost, 23) || !closeEnough(out.Profit, 12) || !closeEnough(out.MissingRevenue, 1) {
		t.Fatalf("profit = %+v", out)
	}
	if lowPool.Complete || !closeEnough(lowPool.Usage, 110) || !closeEnough(lowPool.Revenue, 10) || !closeEnough(lowPool.Cost, 8) || !closeEnough(lowPool.Profit, 2) {
		t.Fatalf("low pool = %+v", lowPool)
	}
	if !stablePool.Complete || !closeEnough(stablePool.Usage, 100) || !closeEnough(stablePool.Revenue, 25) || !closeEnough(stablePool.Cost, 15) || !closeEnough(stablePool.Profit, 10) {
		t.Fatalf("stable pool = %+v", stablePool)
	}
	missing := profitTestChannel(lowPool.Channels, "99")
	if missing.Complete || !closeEnough(missing.Revenue, 1) || missing.Cost != 0 || missing.MissingReason == "" {
		t.Fatalf("missing channel = %+v", missing)
	}
}

func TestSeedSchedulerSnapshotsIsIdempotentAndSkipsMonitorOnlyActiveCost(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "U", Type: "newapi", BaseURL: "https://api.example.test", BalanceRate: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "k", Name: "K", GroupRatio: "0.05"}}); err != nil {
		t.Fatal(err)
	}
	keys, _ := st.ListKeys(t.Context(), u.ID)
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "池子", UpstreamID: u.ID, KeyID: keys[0].ID, SchedulerChannelID: "9", PoolEnabled: true, PoolEnabledSet: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "监控", BaseURL: "https://monitor.example.test", APIKey: "sk", SchedulerChannelID: "10", PoolEnabled: false, PoolEnabledSet: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: "https://scheduler.example.test", UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", Tiers: []domain.SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 0.1, SalePrice: 0.1}}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedSchedulerSnapshots(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedSchedulerSnapshots(t.Context()); err != nil {
		t.Fatal(err)
	}
	var activeCost, monitorActive, saleRows int
	if err := st.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM scheduler_channel_cost_snapshots WHERE active=1`).Scan(&activeCost); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM scheduler_channel_cost_snapshots WHERE channel_id='10' AND active=1`).Scan(&monitorActive); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM scheduler_group_sale_snapshots WHERE active=1`).Scan(&saleRows); err != nil {
		t.Fatal(err)
	}
	if activeCost != 1 || monitorActive != 0 || saleRows != 1 {
		t.Fatalf("activeCost=%d monitorActive=%d saleRows=%d", activeCost, monitorActive, saleRows)
	}
}

func TestProfitUsesManualCostSnapshotsByLogTime(t *testing.T) {
	var oldLog, newLog time.Time
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/log/" {
			t.Fatalf("bad path: %s", r.URL.Path)
		}
		items := []map[string]any{}
		if r.URL.Query().Get("group") == "gpt_low" {
			items = []map[string]any{
				{"quota": 10 * 500000 * 0.1, "channel": "9", "group": "gpt_low", "created_at": oldLog.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
				{"quota": 10 * 500000 * 0.1, "channel": "9", "group": "gpt_low", "created_at": newLog.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	// 固定整秒时间轴，避免 RFC3339Nano 小数位数不固定导致 SQLite 字符串比较
	// 在 1ns 边界上漏选新成本快照（CI 偶发 Cost=2 而非 2.4）。
	first := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	if _, err := st.SaveSchedulerChannelCostSnapshot(t.Context(), domain.SchedulerChannelCostSnapshot{ChannelID: "9", ChannelName: "ch", CardName: "自建", SourceType: "manual_cost_ratio", CostPerUnit: 0.10, Active: true, EffectiveAt: first}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerChannelCostSnapshot(t.Context(), domain.SchedulerChannelCostSnapshot{ChannelID: "9", ChannelName: "ch", CardName: "自建", SourceType: "manual_cost_ratio", CostPerUnit: 0.14, Active: true, EffectiveAt: second}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerGroupSaleSnapshot(t.Context(), domain.SchedulerGroupSaleSnapshot{Group: "gpt_low", Tag: "low", SalePrice: 0.2, Active: true, EffectiveAt: first.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	oldLog, newLog = first.Add(time.Second), second.Add(time.Second)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", Tiers: []domain.SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 1, SalePrice: 0.2}}}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Profit(t.Context(), "24h")
	if err != nil {
		t.Fatal(err)
	}
	pool := profitTestPool(out.Pools, "gpt_low")
	row := profitTestChannel(pool.Channels, "9")
	if !out.Complete || !closeEnough(out.Revenue, 4) || !closeEnough(out.Cost, 2.4) || !closeEnough(out.Profit, 1.6) || row.CostEffective != "mixed" {
		t.Fatalf("profit=%+v row=%+v", out, row)
	}
}

func TestProfitUsesFirstSnapshotsForEarlierLogs(t *testing.T) {
	logTime := time.Now().UTC().Add(-time.Hour)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
			{"quota": 10 * 500000 * 0.1, "channel": "9", "group": "gpt_low", "created_at": logTime.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
		}}})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.SaveCard(t.Context(), "", domain.ModelCard{Name: "自建", BaseURL: "https://api.example.test", APIKey: "sk", ManualCostRatio: "0.10", SchedulerChannelID: "9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", Tiers: []domain.SchedulerTier{{Tag: "low", Group: "gpt_low", PriceMax: 1, SalePrice: 0.2}}}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Profit(t.Context(), "24h")
	if err != nil {
		t.Fatal(err)
	}
	row := profitTestChannel(profitTestPool(out.Pools, "gpt_low").Channels, "9")
	if !out.Complete || !row.Complete || !closeEnough(out.Revenue, 2) || !closeEnough(out.Cost, 1) || !closeEnough(out.Profit, 1) {
		t.Fatalf("profit=%+v row=%+v", out, row)
	}
}

func TestProfitIncludesDeletedGroupFromSaleSnapshots(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	logTime := base.Add(time.Minute)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("group") != "old_group" {
			t.Fatalf("unexpected group %q", r.URL.Query().Get("group"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{
			{"quota": 5 * 500000 * 0.1, "channel": "9", "group": "old_group", "created_at": logTime.Format(time.RFC3339Nano), "other": map[string]any{"group_ratio": 0.1}},
		}}})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerGroupSaleSnapshot(t.Context(), domain.SchedulerGroupSaleSnapshot{Group: "old_group", Tag: "old", SalePrice: 0.2, Active: true, EffectiveAt: base}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerGroupSaleSnapshot(t.Context(), domain.SchedulerGroupSaleSnapshot{Group: "old_group", Tag: "old", Active: false, EffectiveAt: base.Add(30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSchedulerChannelCostSnapshot(t.Context(), domain.SchedulerChannelCostSnapshot{ChannelID: "9", ChannelName: "old", CardName: "old-card", SourceType: "manual_cost_ratio", CostPerUnit: 0.1, Active: true, EffectiveAt: base}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", Tiers: []domain.SchedulerTier{}}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Profit(t.Context(), "24h")
	if err != nil {
		t.Fatal(err)
	}
	pool := profitTestPool(out.Pools, "old_group")
	if !out.Complete || pool.Group != "old_group" || !closeEnough(out.Revenue, 1) || !closeEnough(out.Cost, 0.5) {
		t.Fatalf("profit=%+v pool=%+v", out, pool)
	}
}

func profitTestPool(rows []domain.ProfitPoolRow, group string) domain.ProfitPoolRow {
	for _, row := range rows {
		if row.Group == group {
			return row
		}
	}
	return domain.ProfitPoolRow{}
}

func profitTestChannel(rows []domain.ProfitChannelRow, id string) domain.ProfitChannelRow {
	for _, row := range rows {
		if row.ChannelID == id {
			return row
		}
	}
	return domain.ProfitChannelRow{}
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestSchedulerProfitLogsUsesNewAPICappedPageSize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("page_size = %q", r.URL.Query().Get("page_size"))
		}
		items := []map[string]any{}
		count := 1
		if r.URL.Query().Get("p") == "1" {
			count = 100
		}
		for range count {
			items = append(items, map[string]any{"quota": 500000, "channel": 9, "other": map[string]any{"group_ratio": 1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
	}))
	defer ts.Close()

	svc := New(nil)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	logs, err := svc.ProfitSvc.schedulerProfitLogs(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned",}, time.Unix(1, 0), time.Unix(2, 0), "gpt_low")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 101 {
		t.Fatalf("logs = %d", len(logs))
	}
}

func TestSchedulerAutomationDisableAndRestore(t *testing.T) {
	var statuses []int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/9/status" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body map[string]int
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, body["status"])
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",}); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: "https://api.example.test", APIKey: "sk", SchedulerChannelID: "9", SchedulerChannelName: "C", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 1); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Fatalf("first failure changed scheduler: %+v", statuses)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 2); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Fatalf("second failure changed scheduler: %+v", statuses)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 3); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Fatalf("third failure changed scheduler: %+v", statuses)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 4); err != nil {
		t.Fatal(err)
	}
	card, _ = st.Card(t.Context(), card.ID)
	if len(statuses) != 1 || statuses[0] != 2 || !card.SchedulerAutoDisabled {
		t.Fatalf("disable statuses=%v card=%+v", statuses, card)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 5); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("retry disable statuses=%v", statuses)
	}
	if _, err := st.SaveProbe(t.Context(), "", card.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, true, 0); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("first success restored scheduler: %+v", statuses)
	}
	if _, err := st.SaveProbe(t.Context(), "", card.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, true, 0); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("second success restored too early: %+v", statuses)
	}
	out, err := svc.MonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	if rows := out["rows"].([]domain.ModelCard); len(rows) != 1 || !rows[0].ProbeMuted {
		t.Fatalf("auto disabled card should stay muted before restore: %+v", rows)
	}
	old := time.Now().Add(-16 * time.Minute)
	card.SchedulerAutoDisabledAt = &old
	if _, err := st.SaveProbe(t.Context(), "", card.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, true, 0); err != nil {
		t.Fatal(err)
	}
	card, _ = st.Card(t.Context(), card.ID)
	if len(statuses) != 2 || statuses[1] != 1 || card.SchedulerAutoDisabled {
		t.Fatalf("restore statuses=%v card=%+v", statuses, card)
	}
	out, err = svc.MonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	if rows := out["rows"].([]domain.ModelCard); len(rows) != 1 || rows[0].ProbeMuted {
		t.Fatalf("restored card should exit mute: %+v", rows)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Action != "restore" || logs[1].Action != "disable" || logs[0].Status != "success" {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestSetCardSchedulerChannelStatus(t *testing.T) {
	var statuses []int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/9/status" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body map[string]int
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, body["status"])
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",}); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: "https://api.example.test", APIKey: "sk", SchedulerChannelID: "9", SchedulerChannelName: "C", SchedulerAutoDisabled: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCardSchedulerChannelStatus(t.Context(), card.ID, 1); err != nil {
		t.Fatal(err)
	}
	card, _ = st.Card(t.Context(), card.ID)
	if len(statuses) != 1 || statuses[0] != 1 || card.SchedulerAutoDisabled {
		t.Fatalf("enable statuses=%v card=%+v", statuses, card)
	}
	if _, err := svc.SetCardSchedulerChannelStatus(t.Context(), card.ID, 2); err != nil {
		t.Fatal(err)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[1] != 2 || len(logs) != 2 || logs[0].Message != "手动关闭调度器渠道" {
		t.Fatalf("statuses=%v logs=%+v", statuses, logs)
	}
}

func TestSchedulerNoConfigNoBindingAndSuccessFalse(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	unconfigured, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "U", BaseURL: "https://api.example.test", APIKey: "sk", SchedulerChannelID: "9", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.applySchedulerAutomation(t.Context(), unconfigured, false, 4); err != nil {
		t.Fatal(err)
	}
	unconfigured, _ = st.Card(t.Context(), unconfigured.ID)
	if unconfigured.SchedulerAutoDisabled {
		t.Fatalf("unconfigured scheduler marked card disabled: %+v", unconfigured)
	}
	logs, err := svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Status != "skipped" {
		t.Fatalf("unconfigured logs = %+v", logs)
	}
	if err := svc.applySchedulerAutomation(t.Context(), unconfigured, false, 5); err != nil {
		t.Fatal(err)
	}
	logs, err = svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("unconfigured retry logs = %+v", logs)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "blocked"})
	}))
	defer ts.Close()
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",}); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: "https://api.example.test", APIKey: "sk", SchedulerChannelID: "9", SchedulerChannelName: "C", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 4); err == nil {
		t.Fatal("success:false should fail")
	}
	got, err := st.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchedulerAutoDisabled {
		t.Fatalf("card was marked auto disabled after remote failure: %+v", got)
	}
	logs, err = svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Status != "error" {
		t.Fatalf("remote failure logs = %+v", logs)
	}
	if err := svc.applySchedulerAutomation(t.Context(), card, false, 5); err != nil {
		t.Fatal(err)
	}
	logs, err = svc.SchedulerLogs(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("remote retry logs = %+v", logs)
	}
	if _, err := svc.SchedulerChannels(t.Context(), ""); err == nil {
		t.Fatal("success:false channel list should fail")
	}
}

func TestQuotaProbeUsesQuotaEventAndCooldown(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "insufficient_quota: not enough balance"}})
	}))
	defer ts.Close()
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: ts.URL, APIKey: "sk", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client(), ProbeMode: monitor.ProbeModeHTTP}
	for i := 0; i < 4; i++ {
		if err := svc.CheckCard(t.Context(), card.ID); err != nil {
			t.Fatal(err)
		}
	}
	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "quota_exhausted", TargetType: "card", TargetID: card.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Title != "余额不足/成本池不可用" {
		t.Fatalf("quota events = %+v", events)
	}
	if events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "probe_failed", TargetType: "card", TargetID: card.ID, Limit: 10}); err != nil || len(events) != 0 {
		t.Fatalf("probe events = %+v err=%v", events, err)
	}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	events, err = st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "quota_exhausted", TargetType: "card", TargetID: card.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("quota cooldown failed: %+v", events)
	}
}

func TestCheckCardMutesLongFailingProbeNoise(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream broken"}}`))
	}))
	defer ts.Close()
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: ts.URL, APIKey: "sk", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client(), ProbeMode: monitor.ProbeModeHTTP}
	for i := 0; i < 4; i++ {
		if err := svc.CheckCard(t.Context(), card.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FailureCount != 4 {
		t.Fatalf("failure_count = %d", got.FailureCount)
	}
	out, err := svc.MonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	rows := out["rows"].([]domain.ModelCard)
	if len(rows) != 1 || !rows[0].ProbeMuted {
		t.Fatalf("rows = %+v", rows)
	}
	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "probe_failed", TargetType: "card", TargetID: card.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events after mute threshold = %+v", events)
	}
	var probes int
	if err := st.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM probe_runs WHERE card_id=?`, card.ID).Scan(&probes); err != nil {
		t.Fatal(err)
	}
	if probes != 4 {
		t.Fatalf("probe count = %d", probes)
	}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	events, err = st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "probe_failed", TargetType: "card", TargetID: card.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM probe_runs WHERE card_id=?`, card.ID).Scan(&probes); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || probes != 5 {
		t.Fatalf("events=%+v probes=%d", events, probes)
	}
}

func TestAutoDisabledCardSuppressesProbeFailureNoise(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient_quota secret detail"}}`))
	}))
	defer ts.Close()
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: ts.URL, APIKey: "sk", SchedulerAutoDisabled: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client(), ProbeMode: monitor.ProbeModeHTTP}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	for _, eventType := range []string{"probe_failed", "quota_exhausted"} {
		events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: eventType, TargetType: "card", TargetID: card.ID, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Fatalf("%s events = %+v", eventType, events)
		}
	}
}

func TestInternalProbeErrorRetriesWithoutFailureCountOrTelegram(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	fake := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nprintf x >> " + countPath + "\necho 'model instructions file is empty /tmp/aum-codex-probe' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "C", BaseURL: "https://api.example.test", APIKey: "sk", SchedulerChannelID: "9", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{ProbeMode: monitor.ProbeModeCLI, CodexPath: fake}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "xx" {
		t.Fatalf("retry count = %q", count)
	}
	got, err := st.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FailureCount != 0 || got.LastError != "" || got.SchedulerAutoDisabled {
		t.Fatalf("card state = %+v", got)
	}
	events, err := st.OpsEvents(t.Context(), domain.OpsEventFilter{Type: "probe_internal_error", TargetType: "card", TargetID: card.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("internal events = %+v", events)
	}
	if logs, err := svc.SchedulerLogs(t.Context(), 10); err != nil || len(logs) != 0 {
		t.Fatalf("scheduler logs = %+v err=%v", logs, err)
	}
}

func TestSchedulerGroupsFallbackAndParse(t *testing.T) {
	selfHit := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token" || r.Header.Get("New-Api-User") != "42" {
			t.Fatalf("bad scheduler headers: %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/user/self/groups":
			selfHit = true
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/api/user/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
				"gpt_stable": map[string]any{"ratio": 1.2, "description": "稳定"},
				"gpt_low":    map[string]any{"rate_multiplier": "0.5x"},
				"bugteam":    "",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",}); err != nil {
		t.Fatal(err)
	}
	groups, err := svc.SchedulerGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !selfHit || len(groups) != 3 || groups[0].Name != "bugteam" || groups[1].Name != "gpt_low" || groups[1].Ratio != "0.5" || groups[2].Ratio != "1.2" {
		t.Fatalf("groups = %+v selfHit=%v", groups, selfHit)
	}
}

func TestTodayRevenueMapsEpayOrders(t *testing.T) {
	now := todayStart().Add(time.Hour).Format("2006-01-02 15:04:05")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api.php" || q.Get("act") != "orders" || q.Get("pid") != "1000" || q.Get("key") != "secret" {
			t.Fatalf("bad epay request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "data": []map[string]any{
			{"trade_no": "E-1", "money": "10", "status": "1", "endtime": now},
			{"trade_no": "E-2", "money": "99", "status": "0", "endtime": now},
		}})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cards, err := st.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cards[0].BaseURL = ts.URL
	cards[0].EpayPID = "1000"
	cards[0].EpayKey = "secret"
	if _, err := st.UpdateRevenueCard(t.Context(), cards[0]); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	out, err := svc.TodayRevenue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].SourceType != "epay_total" || out[0].Revenue != 10 {
		t.Fatalf("revenue = %+v", out)
	}
}

func TestTodayRevenueReturnsIndependentCardRows(t *testing.T) {
	now := todayStart().Add(time.Hour).Format(time.RFC3339)
	old := todayStart().Add(-time.Second).Format(time.RFC3339)
	var sub2AdminKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.php":
			if r.URL.Query().Get("pid") != "1000" || r.URL.Query().Get("key") != "secret" {
				t.Fatalf("bad epay query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "data": []map[string]any{
				{"money": "88.66", "status": "1", "endtime": now},
			}})
		case "/api/user/topup":
			if r.Header.Get("Authorization") != "new-token" || r.Header.Get("New-Api-User") != "new-user" {
				t.Fatalf("bad newapi auth: auth=%q user=%q", r.Header.Get("Authorization"), r.Header.Get("New-Api-User"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": []map[string]any{
				{"amount": 12.5, "status": "success", "created_at": now},
				{"amount": 99, "status": "pending", "created_at": now},
				{"amount": 100, "status": "success", "created_at": old},
			}}})
		case "/api/v1/admin/payment/orders":
			sub2AdminKey = r.Header.Get("x-api-key")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"orders": []map[string]any{
				{"amount": 7.25, "status": "COMPLETED", "created_at": now},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cards, err := st.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cards[0].BaseURL = ts.URL
	cards[0].EpayPID = "1000"
	cards[0].EpayKey = "secret"
	if _, err := st.UpdateRevenueCard(t.Context(), cards[0]); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveRevenueCard(t.Context(), "", domain.RevenueCard{Name: "N 订单", SourceType: "newapi_orders", BaseURL: ts.URL, UserID: "new-user", AccessToken: "new-token", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRevenueCard(t.Context(), "", domain.RevenueCard{Name: "S 订单", SourceType: "sub2api_orders", BaseURL: ts.URL, AdminAPIKey: "admin-secret", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.TodayRevenue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	amounts := map[string]float64{}
	for _, row := range rows {
		amounts[row.SourceType] = row.Revenue
	}
	if amounts["epay_total"] != 88.66 || amounts["newapi_orders"] != 12.5 || amounts["sub2api_orders"] != 7.25 {
		t.Fatalf("rows = %+v", rows)
	}
	if sub2AdminKey != "admin-secret" {
		t.Fatalf("sub2 admin key = %q", sub2AdminKey)
	}
}

func TestRevenueCardOrdersReturnsEpayOrders(t *testing.T) {
	now := todayStart().Add(time.Hour).Format("2006-01-02 15:04:05")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api.php" || q.Get("act") != "orders" || q.Get("pid") != "1000" || q.Get("key") != "secret" {
			t.Fatalf("bad epay request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "data": []map[string]any{
			{"trade_no": "E-1", "money": "12.34", "type": "alipay", "status": "1", "endtime": now},
		}})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cards, err := st.ListRevenueCards(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cards[0].BaseURL = ts.URL
	cards[0].EpayPID = "1000"
	cards[0].EpayKey = "secret"
	if _, err := st.UpdateRevenueCard(t.Context(), cards[0]); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	orders, err := svc.RevenueCardOrders(t.Context(), cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].RemoteID != "E-1" || orders[0].Amount != 12.34 || orders[0].PaymentType != "alipay" {
		t.Fatalf("orders = %+v", orders)
	}
}

func TestSaveRevenueCardValidatesCardCredentials(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	if _, err := svc.SaveRevenueCard(t.Context(), "", domain.RevenueCard{Name: "bad", SourceType: "newapi_orders", BaseURL: "https://n.example.test", Enabled: true}); err == nil {
		t.Fatal("missing new-api credentials should fail")
	}
	if _, err := svc.SaveRevenueCard(t.Context(), "", domain.RevenueCard{Name: "bad", SourceType: "sub2api_orders", BaseURL: "https://s.example.test", Enabled: true}); err == nil {
		t.Fatal("missing sub2api admin key should fail")
	}
	if _, err := svc.SaveRevenueCard(t.Context(), "", domain.RevenueCard{SourceType: "epay_total", Enabled: true}); err == nil {
		t.Fatal("missing epay config should fail")
	}
	card, err := svc.SaveRevenueCard(t.Context(), "", domain.RevenueCard{SourceType: "epay_total", BaseURL: "https://pay.example.test", EpayPID: "1000", EpayKey: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "今日收入" || card.UpstreamID != "" || card.BaseURL != "https://pay.example.test" {
		t.Fatalf("card = %+v", card)
	}
}

func TestTGMediaDownloadFailureKeepsMessageData(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.TGMediaDir = filepath.Join(parent, "media")
	ch, err := st.CreateTGChannel(t.Context(), domain.TGChannel{DisplayName: "频道", PeerID: 1, AccessHash: 2, Enabled: true, MessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	msg := &tg.Message{
		ID:      7,
		Date:    int(time.Now().Unix()),
		Message: "text",
		Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{
			ID: 1, AccessHash: 2, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "m", W: 10, H: 10}},
		}},
	}
	typ, path, url, cached := svc.TG.cacheTGMedia(t.Context(), nil, ch.ID, msg)
	if typ != "photo" || path != "" || url != "" || cached {
		t.Fatalf("media = %q %q %q %v", typ, path, url, cached)
	}
	if _, err := st.SaveTGMessage(t.Context(), domain.TGMessage{ChannelID: ch.ID, RemoteID: msg.ID, PublishedAt: time.Now(), Text: msg.Message, MediaType: typ, MediaCached: cached}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.TGMessages(t.Context(), ch.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != "text" || rows[0].MediaType != "photo" || rows[0].MediaCached {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestMonitorStatusCountsProbeStatuses(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://example.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "A", UpstreamID: u.ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	muted, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "Muted", UpstreamID: u.ID, Enabled: true, FailureCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{monitor.StatusOperational, monitor.StatusDegraded, monitor.StatusFailed} {
		if _, err := st.SaveProbe(t.Context(), u.ID, card.ID, domain.ProbeModel, monitor.ProbeResult{Status: status, Latency: time.Millisecond}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SaveProbe(t.Context(), u.ID, muted.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusFailed, Latency: 10 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	out, err := New(st).MonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	if out["success"] != 2 || out["failed"] != 1 || out["requests"] != 3 {
		t.Fatalf("status = %#v", out)
	}
}

func TestSaveCardSupportsCustomAndUpstreamKey(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	custom, err := svc.SaveCard(t.Context(), "", domain.ModelCard{
		Name: "自定义", BaseURL: " https://api.example.test/ ", APIKey: " sk-test ", DisplayGroup: " 生产 ", Enabled: true, PublicEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.BaseURL != "https://api.example.test" || custom.APIKey != "" || !custom.APIKeySet || custom.Model != domain.ProbeModel || custom.DisplayGroup != "生产" || !custom.PoolEnabled || !custom.PublicEnabled {
		t.Fatalf("custom = %+v", custom)
	}
	stored, err := st.Card(t.Context(), custom.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.APIKey != "sk-test" {
		t.Fatalf("stored api key = %q", stored.APIKey)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: "https://upstream.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveKeys(t.Context(), u.ID, []monitor.APIKey{{RemoteID: "k1", Name: "主 Key", Key: "sk-up"}}); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	card, err := svc.SaveCard(t.Context(), "", domain.ModelCard{UpstreamID: u.ID, KeyID: keys[0].ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "A · 主 Key" || card.UpstreamID != u.ID || card.KeyID != keys[0].ID {
		t.Fatalf("card = %+v", card)
	}
}

func TestSaveCardMonitorOnlyClearsSchedulerBinding(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	card, err := svc.SaveCard(t.Context(), "", domain.ModelCard{
		Name: "监控", BaseURL: "https://api.example.test", APIKey: "sk", PoolEnabled: false, PoolEnabledSet: true,
		ManualCostRatio: "0.14", SchedulerGroup: "gpt_low", SchedulerChannelID: "9", SchedulerChannelName: "通道", SchedulerAutoDisabled: true, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.PoolEnabled || card.ManualCostRatio != "" || card.SchedulerGroup != "" || card.SchedulerChannelID != "" || card.SchedulerChannelName != "" || card.SchedulerAutoDisabled {
		t.Fatalf("card = %+v", card)
	}
}

func TestSchedulerAutomationSkipsMonitorOnlyCards(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer ts.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: ts.URL, UserID: "42", AccessToken: "token", UnassignedGroup: "unassigned",}); err != nil {
		t.Fatal(err)
	}
	err = svc.applySchedulerAutomation(t.Context(), domain.ModelCard{ID: "c", Name: "监控", PoolEnabled: false, SchedulerChannelID: "9"}, false, 2)
	if err != nil || hits != 0 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

func TestSaveCardRejectsDuplicateSchedulerChannel(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	first, err := svc.SaveCard(t.Context(), "", domain.ModelCard{Name: "A", BaseURL: "https://api-a.example.test", APIKey: "sk-a", SchedulerChannelID: "ch-1", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveCard(t.Context(), "", domain.ModelCard{Name: "B", BaseURL: "https://api-b.example.test", APIKey: "sk-b", SchedulerChannelID: "ch-1", Enabled: true}); err == nil {
		t.Fatal("expected duplicate scheduler channel error")
	}
	if _, err := svc.SaveCard(t.Context(), first.ID, domain.ModelCard{Name: "A", BaseURL: "https://api-a.example.test", APIKey: "sk-a", SchedulerChannelID: "ch-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCustomCardUsesOwnURLKeyAndConfiguredModel(t *testing.T) {
	var auth, model string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		model, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "pong"})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	card, err := svc.SaveCard(t.Context(), "", domain.ModelCard{Name: "自定义", BaseURL: ts.URL, APIKey: "sk-custom", Model: " grok-4 ", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-custom" || model != "grok-4" {
		t.Fatalf("auth=%q model=%q", auth, model)
	}
	runs, err := st.RecentProbesForCard(t.Context(), card.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Model != "grok-4" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestCheckCardFallsBackToDefaultModel(t *testing.T) {
	var model string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		model, _ = body["model"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "pong"})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "旧卡", BaseURL: ts.URL, APIKey: "sk-custom", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(t.Context(), `UPDATE model_cards SET model='' WHERE id=?`, card.ID); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client(), ProbeMode: monitor.ProbeModeHTTP}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	if model != domain.ProbeModel {
		t.Fatalf("model = %q", model)
	}
}

func TestCheckCardStoresCodexCLIFailure(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'ERROR: cli unavailable' >&2\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{ProbeMode: monitor.ProbeModeCLI, CodexPath: fake}
	card, err := svc.SaveCard(t.Context(), "", domain.ModelCard{Name: "自定义", BaseURL: "https://api.example.test", APIKey: "sk-custom", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckCard(t.Context(), card.ID); err != nil {
		t.Fatal(err)
	}
	card, err = st.Card(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.FailureCount != 1 || !strings.Contains(card.LastError, "cli unavailable") {
		t.Fatalf("card = %+v", card)
	}
	runs, err := st.ProbesForCardSince(t.Context(), card.ID, time.Now().Add(-time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != monitor.StatusError || !strings.Contains(runs[0].Error, "cli unavailable") {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestSortCardsRejectsDuplicateIDs(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	card, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "A", BaseURL: "https://a.example.test", APIKey: "sk-a", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(st).SortCards(t.Context(), []string{card.ID, card.ID}); err == nil {
		t.Fatal("duplicate ids should fail")
	}
}

func TestRedeemBalanceAuditsWithoutPlainCode(t *testing.T) {
	var sawCode bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/topup" {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		sawCode = body["key"] == "secret-code"
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": 10})
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "A", Type: "newapi", BaseURL: ts.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	if _, err := svc.RedeemBalance(t.Context(), u.ID, "secret-code"); err != nil {
		t.Fatal(err)
	}
	if !sawCode {
		t.Fatal("redeem code was not sent upstream")
	}
	logs, err := st.BalanceRechargeLogs(t.Context(), u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Status != "success" || strings.Contains(logs[0].PaymentType+logs[0].Message, "secret-code") {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestRefreshAndDeleteBalanceRechargeLog(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": "token", "refresh_token": "refresh"}})
		case "/api/v1/payment/orders/verify":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"out_trade_no": "ORD-1", "payment_type": "alipay", "status": "COMPLETED"}})
		case "/api/v1/user/profile", "/api/v1/keys", "/api/v1/groups/available":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUpstream(t.Context(), domain.Upstream{Name: "S", Type: "sub2api", BaseURL: ts.URL, Enabled: true, Sub2APIRefreshToken: "refresh"})
	if err != nil {
		t.Fatal(err)
	}
	log, err := st.SaveBalanceRechargeLog(t.Context(), domain.BalanceRechargeLog{UpstreamID: u.ID, Method: "order", PaymentType: "alipay", RemoteOrderID: "ORD-1", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client = monitor.Client{HTTP: ts.Client()}
	updated, err := svc.RefreshBalanceRechargeLog(t.Context(), u.ID, log.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "success" || updated.RawStatus != "COMPLETED" {
		t.Fatalf("updated = %+v", updated)
	}
	if err := svc.DeleteBalanceRechargeLog(t.Context(), u.ID, log.ID); err != nil {
		t.Fatal(err)
	}
	logs, err := st.BalanceRechargeLogs(t.Context(), u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestPublicMonitorStatusFiltersAndRedacts(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	public, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "公开", BaseURL: "https://api.example.test", APIKey: "sk-public", Enabled: true, PublicEnabled: true, FailureCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	private, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "私有", BaseURL: "https://private.example.test", APIKey: "sk-private", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "暂停", BaseURL: "https://paused.example.test", APIKey: "sk-paused", PublicEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", public.ID, domain.ProbeModel, monitor.ProbeResult{
		Status: monitor.StatusFailed, Input: "ping", Output: "", Error: "secret upstream detail",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", private.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", paused.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusOperational}); err != nil {
		t.Fatal(err)
	}
	out, err := New(st).PublicMonitorStatus(t.Context(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"name":"公开"`) || !strings.Contains(text, `"name":"暂停"`) || strings.Contains(text, "私有") || strings.Contains(text, "sk-public") {
		t.Fatalf("public body = %s", body)
	}
	for _, visible := range []string{"ping", "请求失败"} {
		if !strings.Contains(text, visible) {
			t.Fatalf("public body missing %s: %s", visible, body)
		}
	}
	if !strings.Contains(text, `"probe_muted":true`) {
		t.Fatalf("public body missing probe_muted: %s", body)
	}
	if !strings.Contains(text, `"auto_probe_paused":true`) {
		t.Fatalf("public body missing auto_probe_paused: %s", body)
	}
	for _, hidden := range []string{`"id"`, "api_key", "model", "enabled", "public_enabled", "sort_order", "failure_count", "created_at", "updated_at", "secret upstream detail"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("public body leaked %s: %s", hidden, body)
		}
	}
	var parsed struct {
		Rows []struct {
			History []map[string]any `json:"history"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Rows[0].History[0]["success"]; ok {
		t.Fatalf("public history leaked success: %s", body)
	}
	if !strings.Contains(text, "请求失败") || strings.Contains(text, `"status":"`+monitor.StatusFailed+`"`) {
		t.Fatalf("bad public status summary: %s", body)
	}
}
