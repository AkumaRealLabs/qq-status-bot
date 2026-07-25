package app

import (
	"context"
	"path/filepath"
	"testing"

	"ai-upstream-monitor/internal/store"
)

type recordingNotifier struct {
	messages []string
}

func (n *recordingNotifier) Send(_ context.Context, message string) error {
	n.messages = append(n.messages, message)
	return nil
}

func TestTelegramBotTestNotificationIsRetained(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "notifications.sqlite"))
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
	cfg.TelegramBotToken = "bot-token"
	cfg.TelegramChatID = "chat-id"
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	svc := New(st)
	svc.Notify = notifier
	if err := svc.TestNotification(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.messages) != 1 || notifier.messages[0] != "通知规则测试" {
		t.Fatalf("messages=%v", notifier.messages)
	}
}
