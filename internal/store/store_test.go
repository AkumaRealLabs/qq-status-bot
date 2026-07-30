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
