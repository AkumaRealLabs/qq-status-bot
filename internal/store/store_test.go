package store

import (
	"path/filepath"
	"testing"

	"qq-status-bot/internal/domain"
)

func TestStorePersistsSettingsLogsAndAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	defaults := domain.Settings{QQBotAppID: "app", QQBotAppSecret: "secret", Commands: []string{"状态"}}
	s, err := Open(path, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Setup("admin", "password8"); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login("admin", "password8")
	if err != nil || !s.Authenticated(token) {
		t.Fatalf("登录失败 token=%q err=%v", token, err)
	}
	if err := s.AppendLog(domain.EventLog{Direction: "receive", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if s2.Settings().QQBotAppSecret != "secret" || len(s2.Logs(10)) != 1 || s2.SetupStatus() != true {
		t.Fatalf("持久化结果错误: settings=%+v logs=%+v", s2.Settings(), s2.Logs(10))
	}
}

func TestStoreSupportsPasswordOnlyAuthentication(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Setup("", "password8"); err != nil {
		t.Fatal(err)
	}
	if token, err := s.Login("", "password8"); err != nil || token == "" {
		t.Fatalf("仅密码登录失败: token=%q err=%v", token, err)
	}
}

func TestStoreLogsUsesEmptyListInsteadOfNull(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if logs := s.Logs(100); logs == nil || len(logs) != 0 {
		t.Fatalf("空日志应返回非 nil 空列表: %#v", logs)
	}
}

func TestStorePersistsDiscoveredGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path, domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []domain.EventLog{
		{Direction: "receive", GroupOpenID: "group-a"},
		{Direction: "receive", GroupOpenID: " group-a "},
		{Direction: "send", GroupOpenID: "group-b"},
	} {
		if err := s.AppendLog(item); err != nil {
			t.Fatal(err)
		}
	}
	if groups := s.DiscoveredGroups(); len(groups) != 1 || groups[0] != "group-a" {
		t.Fatalf("已发现群去重错误: %v", groups)
	}
	reopened, err := Open(path, domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if groups := reopened.DiscoveredGroups(); len(groups) != 1 || groups[0] != "group-a" {
		t.Fatalf("已发现群未持久化: %v", groups)
	}
}

func TestStorePersistsAlertStateAndDefaultsNewSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path, domain.Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Settings().AlertFailureSamples != 2 || s.Settings().AlertRecoverySamples != 2 || s.Settings().AlertGroups == nil {
		t.Fatalf("新告警设置默认值错误: %+v", s.Settings())
	}
	state := domain.AlertState{Enabled: true, SourceKey: "source", Nodes: map[string]domain.AlertNodeState{
		"7": {LastHeartbeat: "heartbeat", OfflineSamples: 1, OfflineAttempts: map[string]int{"group": 2}},
	}}
	if err := s.UpdateAlertState(state); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	loaded := s2.AlertState()
	if loaded.SourceKey != "source" || loaded.Nodes["7"].OfflineAttempts["group"] != 2 {
		t.Fatalf("告警状态未持久化: %+v", loaded)
	}
}
