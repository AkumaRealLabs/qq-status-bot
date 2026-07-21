package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
)

func TestTrafficActiveSoftCircuitWritesAndVerifiesRemote(t *testing.T) {
	type remoteChannel struct {
		Status   int
		Priority int64
		Weight   uint
	}
	channels := map[string]*remoteChannel{"9": {Status: 1, Priority: 100, Weight: 100}, "10": {Status: 1, Priority: 99, Weight: 100}}
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/log/" && r.Method == http.MethodGet:
			items := []map[string]any{}
			if r.URL.Query().Get("type") == "5" {
				for i := 0; i < 10; i++ {
					items = append(items, map[string]any{"created_at": now.Unix(), "channel_id": 9, "group": "gpt", "model": "m", "request_id": "fail-" + strconv.Itoa(i), "status_code": 502})
				}
			} else {
				items = append(items, map[string]any{"created_at": now.Unix(), "channel_id": 10, "group": "gpt", "model": "m", "request_id": "healthy", "status_code": 200})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
		case r.URL.Path == "/api/channel/" && r.Method == http.MethodGet:
			items := []map[string]any{}
			for _, id := range []string{"9", "10"} {
				ch := channels[id]
				items = append(items, map[string]any{"id": id, "name": "渠道 " + id, "status": ch.Status, "priority": ch.Priority, "weight": ch.Weight, "group": "gpt"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
		case r.URL.Path == "/api/channel/" && r.Method == http.MethodPut:
			var body struct {
				ID       int   `json:"id"`
				Priority int64 `json:"priority"`
				Weight   uint  `json:"weight"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			ch := channels[strconv.Itoa(body.ID)]
			ch.Priority, ch.Weight = body.Priority, body.Weight
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case r.URL.Path == "/api/channel/9/status" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var status struct {
				Status int `json:"status"`
			}
			_ = json.Unmarshal(body, &status)
			channels["9"].Status = status.Status
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "active.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "卡片", BaseURL: "https://upstream.invalid", APIKey: "secret", Model: "m", PoolEnabled: true, SchedulerChannelID: "9", SchedulerChannelName: "渠道 9", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "候选", BaseURL: "https://upstream.invalid", APIKey: "secret", Model: "m", PoolEnabled: true, SchedulerChannelID: "10", SchedulerChannelName: "渠道 10", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client.HTTP = server.Client()
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", TrafficMode: domain.TrafficModeActive}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTraffic(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := channels["9"]; got.Status != 2 || got.Priority != -1900 || got.Weight != 0 {
		t.Fatalf("remote=%+v", got)
	}
	control, found, err := st.TrafficControl(t.Context(), "9")
	if err != nil || !found {
		t.Fatalf("control found=%v err=%v", found, err)
	}
	if control.State != "soft_blocked" || control.DesiredStatus != 2 || control.DesiredPriority != -1900 || control.ActualWeight != 0 {
		t.Fatalf("control=%+v", control)
	}
}

func TestTrafficReconcilePaginationOverlapAndDedup(t *testing.T) {
	var mu sync.Mutex
	starts := []int64{}
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/log/":
			logType, _ := strconv.Atoi(r.URL.Query().Get("type"))
			page, _ := strconv.Atoi(r.URL.Query().Get("p"))
			if logType == 2 && page == 1 {
				start, _ := strconv.ParseInt(r.URL.Query().Get("start_timestamp"), 10, 64)
				mu.Lock()
				starts = append(starts, start)
				mu.Unlock()
			}
			items := []map[string]any{}
			if logType == 2 && page == 1 {
				for i := 0; i < 100; i++ {
					items = append(items, map[string]any{"created_at": now.Unix(), "channel_id": 9, "channel_name": "渠道 9", "model": "gpt-test", "request_id": "ok-" + strconv.Itoa(i), "status_code": 200, "ttft_ms": 200})
				}
			} else if logType == 2 && page == 2 {
				items = append(items, map[string]any{"created_at": now.Unix(), "channel_id": 9, "model": "gpt-test", "request_id": "ok-last", "status_code": 200})
			} else if logType == 5 && page == 1 {
				items = append(items, map[string]any{"created_at": now.Unix(), "channel_id": 9, "model": "gpt-test", "request_id": "err-1", "status_code": 429, "error_type": "rate_limit", "error_message": "raw secret must not persist"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items}})
		case "/api/channel/":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{"id": 9, "name": "渠道 9", "status": 1, "priority": 100, "weight": 100, "group": "gpt"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "traffic.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client.HTTP = server.Client()
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", TrafficMode: domain.TrafficModeObserve, TrafficProfile: domain.TrafficProfileBalanced, TrafficPollSecs: 5}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTraffic(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTraffic(t.Context()); err != nil {
		t.Fatal(err)
	}
	events, err := st.TrafficEventsSince(t.Context(), now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 102 {
		t.Fatalf("events=%d want=102", len(events))
	}
	for _, event := range events {
		if event.ErrorType == "raw secret must not persist" || event.ErrorCode == "raw secret must not persist" {
			t.Fatal("raw error persisted")
		}
	}
	mu.Lock()
	gotStarts := append([]int64(nil), starts...)
	mu.Unlock()
	if len(gotStarts) < 2 {
		t.Fatalf("start timestamps=%v", gotStarts)
	}
	secondOverlap := time.Now().Unix() - gotStarts[len(gotStarts)-1]
	if secondOverlap < 25 || secondOverlap > 35 {
		t.Fatalf("second overlap=%ds", secondOverlap)
	}
}

func TestTrafficReconcilePermissionFailureFreezes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/log/" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []any{}}})
	}))
	defer server.Close()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "traffic.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	svc.Client.HTTP = server.Client()
	if _, err := svc.SaveSchedulerConfig(t.Context(), domain.SchedulerConfig{BaseURL: server.URL, UserID: "1", AccessToken: "token", UnassignedGroup: "unassigned", TrafficMode: domain.TrafficModeObserve}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTraffic(t.Context()); err == nil {
		t.Fatal("permission failure succeeded")
	}
	status, err := svc.TrafficStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.Connected || !status.Frozen || status.FreezeReason == "" {
		t.Fatalf("status=%+v", status)
	}
	cursors, err := st.TrafficCursors(t.Context())
	if err != nil || len(cursors) != 2 {
		t.Fatalf("cursors=%+v err=%v", cursors, err)
	}
	for _, cursor := range cursors {
		if cursor.LastError == "forbidden" || cursor.LastError == "" {
			t.Fatalf("unsafe cursor error=%q", cursor.LastError)
		}
	}
}

func TestParseTrafficEventRetrySuccessIsSoftFailure(t *testing.T) {
	event, ok := parseTrafficEvent("error", 5, map[string]any{
		"created_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"channel_id":      "9",
		"model":           "m",
		"status_code":     200,
		"retry_succeeded": true,
	})
	if !ok {
		t.Fatal("event was not parsed")
	}
	if event.Kind != domain.TrafficEventSoftFailure || !event.RetrySucceeded {
		t.Fatalf("event=%+v", event)
	}
}
