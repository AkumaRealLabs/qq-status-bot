package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/statusapi"
	"qq-status-bot/internal/store"
)

type sequenceFetcher struct {
	pages []statusapi.StatusPage
	index int
}

func (f *sequenceFetcher) Fetch(context.Context, string, string, string) (statusapi.StatusPage, error) {
	if f.index >= len(f.pages) {
		return f.pages[len(f.pages)-1], nil
	}
	page := f.pages[f.index]
	f.index++
	return page, nil
}

type alertReplier struct {
	messages []string
	groups   []string
	err      error
}

func (r *alertReplier) ReplyGroupImage(context.Context, string, string, []byte) error { return nil }
func (r *alertReplier) ReplyGroupText(context.Context, string, string, string, int) error {
	return nil
}
func (r *alertReplier) SendGroupText(_ context.Context, group, content string) error {
	if r.err != nil {
		return r.err
	}
	r.groups = append(r.groups, group)
	r.messages = append(r.messages, content)
	return nil
}

func TestAlertPollUsesNewHeartbeatsAndSendsRecovery(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{
		AlertsEnabled: true, AlertGroups: []string{"alert-group"}, AlertFailureSamples: 2, AlertRecoverySamples: 2,
		StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y",
	}}
	replier := &alertReplier{}
	fetcher := &sequenceFetcher{pages: []statusapi.StatusPage{
		alertPage(0, "2026-07-31T10:00:00Z"),
		alertPage(0, "2026-07-31T10:00:00Z"),
		alertPage(0, "2026-07-31T10:03:00Z"),
		alertPage(1, "2026-07-31T10:06:00Z"),
		alertPage(1, "2026-07-31T10:06:00Z"),
		alertPage(1, "2026-07-31T10:09:00Z"),
	}}
	service := New(store, &fakeGenerator{}, replier, 3, fetcher)
	for range 6 {
		service.PollAlerts(context.Background())
	}
	if len(replier.messages) != 2 || replier.groups[0] != "alert-group" || replier.groups[1] != "alert-group" {
		t.Fatalf("告警发送次数错误: groups=%v messages=%v", replier.groups, replier.messages)
	}
	if want := "首次离线：2026-07-31 18:00:00 +0800"; !containsText(replier.messages[0], want) {
		t.Fatalf("故障消息缺少首次离线时间: %q", replier.messages[0])
	}
	if want := "恢复时间：2026-07-31 18:09:00 +0800"; !containsText(replier.messages[1], want) {
		t.Fatalf("恢复消息缺少恢复时间: %q", replier.messages[1])
	}
}

func TestAlertPollCountsAllNewHeartbeatsReturnedTogether(t *testing.T) {
	settings := domain.Settings{AlertsEnabled: true, AlertGroups: []string{"alert-group"}, AlertFailureSamples: 2, AlertRecoverySamples: 2, StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y"}
	store := &fakeSettingsStore{settings: settings}
	replier := &alertReplier{}
	fetcher := &sequenceFetcher{pages: []statusapi.StatusPage{
		alertPage(0, "2026-07-31T10:00:00Z"),
		alertPageWithHeartbeats([]statusapi.Heartbeat{{Status: 0, Time: "2026-07-31T10:00:00Z"}, {Status: 0, Time: "2026-07-31T10:03:00Z"}}),
	}}
	service := New(store, &fakeGenerator{}, replier, 3, fetcher)
	service.PollAlerts(context.Background())
	service.PollAlerts(context.Background())
	if len(replier.messages) != 1 {
		t.Fatalf("同一轮返回两个新心跳时应告警: %v", replier.messages)
	}
}

func TestAlertPollSourceChangeOnlyRebaselines(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{
		AlertsEnabled: true, AlertGroups: []string{"alert-group"}, AlertFailureSamples: 2, AlertRecoverySamples: 2,
		StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y",
	}}
	replier := &alertReplier{}
	fetcher := &sequenceFetcher{pages: []statusapi.StatusPage{
		alertPage(0, "2026-07-31T10:00:00Z"), alertPage(0, "2026-07-31T10:03:00Z"),
		alertPage(0, "2026-07-31T10:06:00Z"), alertPage(0, "2026-07-31T10:09:00Z"),
	}}
	service := New(store, &fakeGenerator{}, replier, 3, fetcher)
	service.PollAlerts(context.Background())
	store.settings.StatusURL = "https://new-status.example"
	service.PollAlerts(context.Background())
	if len(replier.messages) != 0 {
		t.Fatalf("换源后旧历史不应立即告警: %v", replier.messages)
	}
	service.PollAlerts(context.Background())
	if len(replier.messages) != 1 {
		t.Fatalf("换源基线后的第二个新样本应告警: %v", replier.messages)
	}
}

func TestAlertPollFailureDoesNotMarkNodeOffline(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{AlertsEnabled: true, AlertGroups: []string{"alert-group"}, StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y"}}
	fetcher := &errorFetcher{}
	service := New(store, &fakeGenerator{}, &alertReplier{}, 3, fetcher)
	service.PollAlerts(context.Background())
	if state := service.getAlertState(); len(state.Nodes) != 0 || !state.PollFailed {
		t.Fatalf("上游失败不应写节点离线状态: %+v", state)
	}
}

func TestAlertStatePreventsDuplicateAfterStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	settings := domain.Settings{AlertsEnabled: true, AlertGroups: []string{"alert-group"}, AlertFailureSamples: 2, AlertRecoverySamples: 2, StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y"}
	state, err := store.Open(path, settings)
	if err != nil {
		t.Fatal(err)
	}
	firstSender := &alertReplier{}
	fetcher := &sequenceFetcher{pages: []statusapi.StatusPage{alertPage(0, "2026-07-31T10:00:00Z"), alertPage(0, "2026-07-31T10:03:00Z")}}
	service := New(state, &fakeGenerator{}, firstSender, 3, fetcher)
	service.PollAlerts(context.Background())
	service.PollAlerts(context.Background())
	if len(firstSender.messages) != 1 {
		t.Fatalf("首次故障未发送: %v", firstSender.messages)
	}
	reopened, err := store.Open(path, domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	secondSender := &alertReplier{}
	serviceAfterRestart := New(reopened, &fakeGenerator{}, secondSender, 3, &sequenceFetcher{pages: []statusapi.StatusPage{alertPage(0, "2026-07-31T10:03:00Z")}})
	serviceAfterRestart.PollAlerts(context.Background())
	if len(secondSender.messages) != 0 {
		t.Fatalf("重启后重复心跳不应再次告警: %v", secondSender.messages)
	}
}

type errorFetcher struct{}

func (*errorFetcher) Fetch(context.Context, string, string, string) (statusapi.StatusPage, error) {
	return statusapi.StatusPage{}, errors.New("upstream")
}

func alertPage(status int, heartbeat string) statusapi.StatusPage {
	return alertPageWithHeartbeats([]statusapi.Heartbeat{{Status: status, Time: heartbeat}})
}

func alertPageWithHeartbeats(heartbeats []statusapi.Heartbeat) statusapi.StatusPage {
	return statusapi.StatusPage{
		Groups:     []statusapi.MonitorGroup{{Name: "服务", Monitors: []statusapi.Monitor{{ID: 7, Name: "节点"}}}},
		Heartbeats: map[int][]statusapi.Heartbeat{7: heartbeats},
	}
}

func containsText(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
