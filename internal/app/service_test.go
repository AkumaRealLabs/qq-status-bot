package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/qqbot"
)

type fakeSettingsStore struct {
	settings domain.Settings
	logs     []domain.EventLog
}

func (f *fakeSettingsStore) Settings() domain.Settings { return f.settings }
func (f *fakeSettingsStore) UpdateSettings(next domain.Settings) (domain.Settings, error) {
	f.settings = next.MergeUpdate(f.settings)
	return f.settings, nil
}
func (*fakeSettingsStore) Setup(string, string) error           { return nil }
func (*fakeSettingsStore) SetupStatus() bool                    { return true }
func (*fakeSettingsStore) Login(string, string) (string, error) { return "token", nil }
func (*fakeSettingsStore) Authenticated(string) bool            { return true }
func (*fakeSettingsStore) Logout(string)                        {}
func (f *fakeSettingsStore) AppendLog(item domain.EventLog) error {
	f.logs = append(f.logs, item)
	return nil
}
func (f *fakeSettingsStore) Logs(int) []domain.EventLog { return f.logs }

type fakeGenerator struct {
	image []byte
	err   error
	calls int
}

func (f *fakeGenerator) Generate(context.Context, string, string, string) ([]byte, error) {
	f.calls++
	return f.image, f.err
}

type fakeReplier struct {
	image []byte
	text  string
}

func (f *fakeReplier) ReplyGroupImage(_ context.Context, _, _ string, image []byte) error {
	f.image = image
	return nil
}
func (f *fakeReplier) ReplyGroupText(_ context.Context, _, _, content string, _ int) error {
	f.text = content
	return nil
}

func TestProcessMessageUploadsGeneratedPNG(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 15}}
	generator := &fakeGenerator{image: []byte("png")}
	replier := &fakeReplier{}
	service := New(store, generator, replier, 3)
	service.processMessage(t.Context(), domain.GroupMessage{ID: "message", GroupOpenID: "group"})
	if string(replier.image) != "png" || replier.text != "" || generator.calls != 1 {
		t.Fatalf("成功流程错误: image=%q text=%q calls=%d", replier.image, replier.text, generator.calls)
	}
	if len(store.logs) != 1 || store.logs[0].Status != "sent" {
		t.Fatalf("成功日志错误: %+v", store.logs)
	}
}

func TestProcessMessageLogsFailureAndRepliesText(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 15}}
	generator := &fakeGenerator{err: errors.New("upstream failed")}
	replier := &fakeReplier{}
	service := New(store, generator, replier, 3)
	service.processMessage(t.Context(), domain.GroupMessage{ID: "message", GroupOpenID: "group"})
	if replier.text != "状态图生成失败，请稍后再试。" {
		t.Fatalf("错误提示错误: %q", replier.text)
	}
	if len(store.logs) != 1 || store.logs[0].Status != "failed" || store.logs[0].Message != "upstream failed" {
		t.Fatalf("失败日志错误: %+v", store.logs)
	}
}

func TestHandleWebhookValidationDoesNotRequireEventSignature(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{QQBotAppSecret: "test-secret"}}
	service := New(store, &fakeGenerator{}, &fakeReplier{}, 3)
	body := []byte(`{"op":13,"d":{"plain_token":"plain","event_ts":"1725442341"}}`)

	response, err := service.HandleWebhook("", "", body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		PlainToken string `json:"plain_token"`
		Signature  string `json:"signature"`
	}
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PlainToken != "plain" || decoded.Signature == "" {
		t.Fatalf("回调验证响应错误: %+v", decoded)
	}
	if len(store.logs) != 1 || store.logs[0].EventType != "CALLBACK_VALIDATION" {
		t.Fatalf("回调验证日志错误: %+v", store.logs)
	}
}

func TestHandleWebhookDispatchStillRequiresEventSignature(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{QQBotAppSecret: "test-secret"}}
	service := New(store, &fakeGenerator{}, &fakeReplier{}, 3)
	body := []byte(`{"op":0,"d":{},"t":"` + qqbot.EventGroupAtMessage + `"}`)

	if _, err := service.HandleWebhook("", "", body); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("未签名事件应被拒绝: %v", err)
	}
}
