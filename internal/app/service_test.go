package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	image             []byte
	text              string
	messageID         string
	eventID           string
	keyboard          qqbot.Keyboard
	interactionID     string
	interactionCode   int
	interactionCalled int
}

func (f *fakeReplier) ReplyGroupImage(_ context.Context, _, _ string, image []byte) error {
	f.image = image
	return nil
}
func (f *fakeReplier) ReplyGroupText(_ context.Context, _, _, content string, _ int) error {
	f.text = content
	return nil
}
func (f *fakeReplier) ReplyGroupImageWithKeyboard(_ context.Context, _, messageID, eventID string, image []byte, keyboard qqbot.Keyboard) error {
	f.image, f.messageID, f.eventID, f.keyboard = image, messageID, eventID, keyboard
	return nil
}
func (f *fakeReplier) ReplyGroupImageWithTarget(_ context.Context, _, messageID, eventID string, image []byte) error {
	f.image, f.messageID, f.eventID = image, messageID, eventID
	return nil
}
func (f *fakeReplier) ReplyGroupTextWithKeyboard(_ context.Context, _, messageID, eventID, content string, _ int, keyboard qqbot.Keyboard) error {
	f.text, f.messageID, f.eventID, f.keyboard = content, messageID, eventID, keyboard
	return nil
}
func (f *fakeReplier) RespondInteraction(_ context.Context, interactionID string, code int) error {
	f.interactionID, f.interactionCode = interactionID, code
	f.interactionCalled++
	return nil
}

type activeTestReplier struct {
	fakeReplier
	activeGroup string
	activeImage []byte
	activeTexts []string
}

func (r *activeTestReplier) SendGroupImage(_ context.Context, group string, image []byte) error {
	r.activeGroup = group
	r.activeImage = image
	return nil
}

func (r *activeTestReplier) SendGroupImageWithKeyboard(_ context.Context, group string, image []byte, keyboard qqbot.Keyboard) error {
	r.activeGroup, r.activeImage, r.keyboard = group, image, keyboard
	return nil
}

func (r *activeTestReplier) SendGroupText(_ context.Context, group, content string) error {
	r.activeGroup = group
	r.activeTexts = append(r.activeTexts, content)
	return nil
}

func (r *activeTestReplier) SendGroupTextWithKeyboard(_ context.Context, group, content string, keyboard qqbot.Keyboard) error {
	r.activeGroup, r.keyboard = group, keyboard
	r.activeTexts = append(r.activeTexts, content)
	return nil
}

func TestProcessMessageUploadsGeneratedPNG(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 15}}
	generator := &fakeGenerator{image: []byte("png")}
	replier := &fakeReplier{}
	service := New(store, generator, replier, 3)
	service.processMessage(t.Context(), domain.GroupMessage{ID: "message", GroupOpenID: "group"})
	if string(replier.image) != "png" || replier.text != "请选择操作：" || generator.calls != 1 {
		t.Fatalf("成功流程错误: image=%q text=%q calls=%d", replier.image, replier.text, generator.calls)
	}
	if replier.keyboard.Empty() || replier.messageID != "message" || replier.eventID != "" {
		t.Fatalf("状态图应携带按钮并使用原消息回复: message=%q event=%q keyboard=%+v", replier.messageID, replier.eventID, replier.keyboard)
	}
	if settings := store.settings; settings.GGAPIBalanceEnabled {
		rows := replier.keyboard.Content.Rows
		if len(rows) < 2 || rows[1].Buttons[0].Action.Permission.Type != qqbot.ButtonPermissionAll {
			t.Fatalf("主菜单账号按钮应允许群成员使用: %+v", replier.keyboard)
		}
	}
	if len(store.logs) != 1 || store.logs[0].Status != "sent" {
		t.Fatalf("成功日志错误: %+v", store.logs)
	}
}

func TestInteractionStatusAcknowledgesAndQueuesEventReply(t *testing.T) {
	settings := domain.Settings{AllowedGroups: []string{"group-a"}, Commands: []string{"状态"}}
	store := &fakeSettingsStore{settings: settings}
	replier := &fakeReplier{}
	service := New(store, &fakeGenerator{image: []byte("png")}, replier, 2)
	interaction := qqbot.Interaction{
		ID: "interaction-1", Type: qqbot.InteractionTypeMessageButton, Scene: "group",
		GroupOpenID: "group-a", GroupMemberOpenID: "member-a",
		Data: qqbot.InteractionData{Resolved: qqbot.InteractionResolved{ButtonData: interactionDataPrefix + interactionStatus, ButtonID: "status"}},
	}
	data, err := json.Marshal(interaction)
	if err != nil {
		t.Fatal(err)
	}
	ack := service.handleDispatchContext(t.Context(), qqbot.Payload{ID: "event-1", Type: qqbot.EventInteractionCreate, Data: data}, settings)
	if !strings.Contains(string(ack), `"d":0`) || replier.interactionCalled != 1 || replier.interactionID != "event-1" || replier.interactionCode != 0 {
		t.Fatalf("互动确认错误: ack=%s replier=%+v", ack, replier)
	}
	if len(service.jobs) != 1 {
		t.Fatalf("状态按钮未进入状态队列: %d", len(service.jobs))
	}
	message := <-service.jobs
	if message.ID != "event-1" || message.EventID != "event-1" || message.GroupOpenID != "group-a" || message.Author.MemberOpenID != "member-a" {
		t.Fatalf("互动任务字段错误: %+v", message)
	}
	service.processMessage(t.Context(), message)
	if replier.messageID != "" || replier.eventID != "event-1" || replier.keyboard.Empty() {
		t.Fatalf("互动结果应使用 event_id 并携带按钮: message=%q event=%q keyboard=%+v", replier.messageID, replier.eventID, replier.keyboard)
	}
}

func TestInteractionRejectsUnknownUnauthorizedBusyAndDuplicate(t *testing.T) {
	settings := domain.Settings{AllowedGroups: []string{"group-a"}}
	store := &fakeSettingsStore{settings: settings}
	replier := &fakeReplier{}
	service := New(store, &fakeGenerator{}, replier, 1)
	dispatch := func(id, group, data string) int {
		t.Helper()
		interaction := qqbot.Interaction{
			ID: id, Type: qqbot.InteractionTypeMessageButton, Scene: "group",
			GroupOpenID: group, GroupMemberOpenID: "member-a",
			Data: qqbot.InteractionData{Resolved: qqbot.InteractionResolved{ButtonData: data}},
		}
		encoded, err := json.Marshal(interaction)
		if err != nil {
			t.Fatal(err)
		}
		service.handleDispatchContext(t.Context(), qqbot.Payload{Type: qqbot.EventInteractionCreate, Data: encoded}, settings)
		return replier.interactionCode
	}
	if code := dispatch("unknown", "group-a", "untrusted"); code != 1 {
		t.Fatalf("未知按钮应操作失败: %d", code)
	}
	if code := dispatch("blocked", "group-b", interactionDataPrefix+interactionStatus); code != 4 {
		t.Fatalf("白名单外按钮应无权限: %d", code)
	}
	if code := dispatch("first", "group-a", interactionDataPrefix+interactionStatus); code != 0 {
		t.Fatalf("首个状态按钮应成功: %d", code)
	}
	if code := dispatch("busy", "group-a", interactionDataPrefix+interactionStatus); code != 2 {
		t.Fatalf("队列满应返回操作频繁: %d", code)
	}
	<-service.jobs
	if code := dispatch("first", "group-a", interactionDataPrefix+interactionStatus); code != 3 {
		t.Fatalf("重复互动应返回重复操作: %d", code)
	}
}

func TestInteractionIgnoresEventsThatDoNotRequireResponse(t *testing.T) {
	store := &fakeSettingsStore{}
	replier := &fakeReplier{}
	service := New(store, &fakeGenerator{}, replier, 1)
	interaction := qqbot.Interaction{ID: "authorize", Type: 18, Scene: "group", GroupOpenID: "group-a", GroupMemberOpenID: "member-a"}
	data, err := json.Marshal(interaction)
	if err != nil {
		t.Fatal(err)
	}
	service.handleDispatchContext(t.Context(), qqbot.Payload{Type: qqbot.EventInteractionCreate, Data: data}, domain.Settings{})
	if replier.interactionCalled != 0 || len(service.jobs) != 0 || len(service.accountJobs) != 0 {
		t.Fatalf("授权等非按钮事件不应确认或进入业务队列: replier=%+v", replier)
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

func TestProcessMessageFailureIncludesRetryExampleForStatusCommand(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 15}}
	generator := &fakeGenerator{err: errors.New("upstream failed")}
	replier := &fakeReplier{}
	service := New(store, generator, replier, 3)
	message := domain.GroupMessage{ID: "message", GroupOpenID: "group", Content: "状态"}
	message.Author.MemberOpenID = "member"
	service.processMessage(t.Context(), message)
	if !strings.Contains(replier.text, "重试示例：@机器人 状态") {
		t.Fatalf("状态命令错误提示缺少重试示例: %q", replier.text)
	}
}

func TestSendStatusRequiresKnownGroupAndSendsGeneratedImage(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{
		AlertGroups: []string{"alert-group"}, StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 15,
	}}
	generator := &fakeGenerator{image: []byte("png")}
	replier := &activeTestReplier{}
	service := New(store, generator, replier, 3)
	if err := service.SendStatus(context.Background(), "unknown-group"); !errors.Is(err, ErrActiveGroupNotAvailable) {
		t.Fatalf("未知群应被拒绝: %v", err)
	}
	if err := service.SendStatus(context.Background(), "alert-group"); err != nil {
		t.Fatal(err)
	}
	if replier.activeGroup != "alert-group" || string(replier.activeImage) != "png" || generator.calls != 1 {
		t.Fatalf("主动状态发送错误: group=%q image=%q calls=%d", replier.activeGroup, replier.activeImage, generator.calls)
	}
	if len(replier.activeTexts) != 1 || replier.activeTexts[0] != "请选择操作：" || replier.keyboard.Empty() {
		t.Fatalf("主动状态菜单发送错误: texts=%q keyboard=%+v", replier.activeTexts, replier.keyboard)
	}
	if len(store.logs) != 1 || store.logs[0].EventType != "STATUS_ACTIVE" || store.logs[0].Status != "sent" {
		t.Fatalf("主动状态日志错误: %+v", store.logs)
	}
}

func TestSimulateAlertSendsMarkedMessagesWithoutChangingState(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{AlertGroups: []string{"alert-group"}}}
	replier := &activeTestReplier{}
	service := New(store, &fakeGenerator{}, replier, 3)
	if err := service.SimulateAlert(context.Background(), "alert-group", "offline"); err != nil {
		t.Fatal(err)
	}
	if err := service.SimulateAlert(context.Background(), "alert-group", "recovery"); err != nil {
		t.Fatal(err)
	}
	if len(replier.activeTexts) != 2 || !strings.Contains(replier.activeTexts[0], "[模拟测试] [故障通知]") || !strings.Contains(replier.activeTexts[1], "[模拟测试] [恢复通知]") {
		t.Fatalf("模拟消息错误: %q", replier.activeTexts)
	}
	if len(store.logs) != 2 || store.logs[0].EventType != "ALERT_SIMULATED_OFFLINE" || store.logs[1].EventType != "ALERT_SIMULATED_RECOVERY" {
		t.Fatalf("模拟日志错误: %+v", store.logs)
	}
	if state := service.getAlertState(); state.Enabled || len(state.Nodes) != 0 {
		t.Fatalf("模拟操作不应修改告警状态: %+v", state)
	}
	if err := service.SimulateAlert(context.Background(), "alert-group", "invalid"); !errors.Is(err, ErrInvalidAlertSimulation) {
		t.Fatalf("无效模拟类型应被拒绝: %v", err)
	}
}

func TestHandleWebhookValidationRequiresEventSignature(t *testing.T) {
	store := &fakeSettingsStore{settings: domain.Settings{QQBotAppSecret: "test-secret"}}
	service := New(store, &fakeGenerator{}, &fakeReplier{}, 3)
	body := []byte(`{"op":13,"d":{"plain_token":"plain","event_ts":"1725442341"}}`)
	timestamp := "1725442341"

	if _, err := service.HandleWebhook("", "", body); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("未签名的回调验证应被拒绝: %v", err)
	}
	signature := webhookSignature(t, "test-secret", timestamp, body)
	response, err := service.HandleWebhook(timestamp, signature, body)
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
	tampered := []byte(`{"op":13,"d":{"plain_token":"other","event_ts":"1725442341"}}`)
	if _, err := service.HandleWebhook(timestamp, signature, tampered); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("被篡改的回调验证应被拒绝: %v", err)
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

func webhookSignature(t *testing.T, secret, timestamp string, body []byte) string {
	t.Helper()
	response, err := qqbot.ValidationResponse(secret, qqbot.ValidationRequest{PlainToken: string(body), EventTS: timestamp})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded.Signature
}

func TestStatusDispatchRequiresRealBotMentionInFullMode(t *testing.T) {
	settings := domain.Settings{
		AllowedGroups: []string{"group-a", "group-b"}, Commands: []string{"状态"},
	}
	store := &fakeSettingsStore{settings: settings}
	service := New(store, &fakeGenerator{}, &fakeReplier{}, 4)

	dispatch := func(id, group string, mentions []domain.GroupMention) {
		t.Helper()
		message := domain.GroupMessage{ID: id, GroupOpenID: group, Content: "状态", Mentions: mentions}
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupMessage, Data: data}, settings)
	}

	dispatch("no-mention", "group-a", nil)
	dispatch("other-bot", "group-b", []domain.GroupMention{{ID: "another-bot", Bot: true}})
	if got := len(service.jobs); got != 0 {
		t.Fatalf("未真实提及机器人的全量状态命令不应入队: %d", got)
	}

	dispatch("bot-flag-present", "group-a", []domain.GroupMention{{Bot: true, IsYou: true}})
	dispatch("bot-flag-absent", "group-b", []domain.GroupMention{{IsYou: true}})
	if got := len(service.jobs); got != 2 {
		t.Fatalf("不同白名单群的真实提及都应入队: %d", got)
	}
}
