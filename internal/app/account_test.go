package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/ggapi"
	"qq-status-bot/internal/qqbot"
	"qq-status-bot/internal/store"
)

type accountMailer struct {
	codes []string
	err   error
}

func TestAccountDispatchRequiresBotMentionAndMemberOpenID(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true, Commands: []string{"状态"}})
	if err != nil {
		t.Fatal(err)
	}
	workflow := NewAccountService(state, state, accountVerifier{user: ggapi.User{ID: "7", Email: "name@example.com", Role: "user", Status: "active"}}, &accountMailer{})
	service := New(state, &fakeGenerator{}, &fakeReplier{}, 2)
	service.ConfigureAccounts(workflow.verify, workflow.mailer)
	data := func(member string) json.RawMessage {
		message := accountMessage("group-a", member, "绑定")
		encoded, _ := json.Marshal(message)
		return encoded
	}
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupMessage, Data: data("member-1")}, state.Settings()); string(ack) == "" || len(service.accountJobs) != 0 {
		t.Fatalf("非提及消息不应进入账号队列: ack=%s queue=%d", ack, len(service.accountJobs))
	}
	fullMode := accountMessage("group-a", "member-1", "绑定")
	fullMode.ID = "full-mode-account"
	fullMode.Mentions = []domain.GroupMention{{Bot: true}}
	fullModeData, _ := json.Marshal(fullMode)
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupMessage, Data: fullModeData}, state.Settings()); len(ack) == 0 || len(service.accountJobs) != 1 {
		t.Fatalf("全量模式下提及机器人应进入账号队列: ack=%s queue=%d", ack, len(service.accountJobs))
	}
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupAtMessage, Data: data("")}, state.Settings()); string(ack) == "" || len(service.accountJobs) != 1 {
		t.Fatalf("缺少成员 OpenID 不应新增账号队列任务: ack=%s queue=%d", ack, len(service.accountJobs))
	}
	valid := accountMessage("group-a", "member-1", "绑定")
	valid.ID = "at-account"
	validData, _ := json.Marshal(valid)
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupAtMessage, Data: validData}, state.Settings()); len(ack) == 0 || len(service.accountJobs) != 2 {
		t.Fatalf("有效账号消息未入队: ack=%s queue=%d", ack, len(service.accountJobs))
	}
	status := accountMessage("group-a", "member-1", "状态")
	status.ID = "status-message"
	statusData, _ := json.Marshal(status)
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupMessage, Data: statusData}, state.Settings()); len(ack) == 0 || len(service.jobs) != 1 {
		t.Fatalf("现有状态命令应继续入队: ack=%s queue=%d", ack, len(service.jobs))
	}
}

func TestAccountDispatchHonorsWhitelistAndRetriesWhenQueueIsFull(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{
		GGAPIBalanceEnabled: true, AllowedGroups: []string{"group-a"}, Commands: []string{"状态"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := New(state, &fakeGenerator{}, &fakeReplier{}, 1)
	encode := func(message domain.GroupMessage) json.RawMessage {
		encoded, _ := json.Marshal(message)
		return encoded
	}
	blocked := accountMessage("group-b", "member-1", "绑定")
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupAtMessage, Data: encode(blocked)}, state.Settings()); len(ack) == 0 || len(service.accountJobs) != 0 {
		t.Fatalf("白名单外账号消息不应入队: ack=%s queue=%d", ack, len(service.accountJobs))
	}
	first := accountMessage("group-a", "member-1", "绑定")
	first.ID = "account-1"
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupAtMessage, Data: encode(first)}, state.Settings()); !strings.Contains(string(ack), `"d":0`) {
		t.Fatalf("首条账号消息应成功入队: %s", ack)
	}
	second := accountMessage("group-a", "member-2", "余额")
	second.ID = "account-2"
	if ack := service.handleDispatch(qqbot.Payload{Type: qqbot.EventGroupAtMessage, Data: encode(second)}, state.Settings()); !strings.Contains(string(ack), `"d":1`) {
		t.Fatalf("账号队列满时应要求 QQ 重试: %s", ack)
	}
}

func (m *accountMailer) SendVerificationCode(_ context.Context, _ string, code string, _ time.Time) error {
	if m.err != nil {
		return m.err
	}
	m.codes = append(m.codes, code)
	return nil
}

type accountVerifier struct {
	user       ggapi.User
	verifyErr  error
	getErr     error
	balanceErr error
	balance    ggapi.Balance
}

func (v accountVerifier) VerifyEmail(context.Context, string) (ggapi.User, error) {
	return v.user, v.verifyErr
}
func (v accountVerifier) GetUser(context.Context, string) (ggapi.User, error) {
	return v.user, v.getErr
}
func (v accountVerifier) Balance(context.Context, ggapi.User) (ggapi.Balance, error) {
	return v.balance, v.balanceErr
}

func accountMessage(group, member, content string) domain.GroupMessage {
	var message domain.GroupMessage
	message.ID = group + "-message"
	message.GroupOpenID = group
	message.Content = content
	message.Author.MemberOpenID = member
	return message
}

func TestAccountServiceBindsAndReusesMemberAcrossGroups(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	mailer := &accountMailer{}
	verifier := accountVerifier{user: ggapi.User{ID: "7", Email: "name@example.com", Username: "name", Role: "user", Status: "active"}, balance: ggapi.Balance{Amount: 12.5, Currency: "USD"}}
	workflow := NewAccountService(state, state, verifier, mailer)
	message := accountMessage("group-a", "member-1", "绑定")
	if handled, response := workflow.Handle(context.Background(), message); !handled || !strings.Contains(response, "name@example.com") {
		t.Fatalf("开始绑定响应错误: handled=%v response=%q", handled, response)
	}
	message.ID = "email"
	message.Content = "name@example.com"
	if _, response := workflow.Handle(context.Background(), message); !strings.Contains(response, "验证码已发送") || len(mailer.codes) != 1 {
		t.Fatalf("发送验证码响应错误: %q codes=%v", response, mailer.codes)
	}
	message.ID = "code"
	message.Content = mailer.codes[0]
	if _, response := workflow.Handle(context.Background(), message); !strings.Contains(response, "绑定成功") {
		t.Fatalf("完成绑定响应错误: %q", response)
	}
	if binding, ok := state.AccountBinding("member-1"); !ok || binding.GGAPIUserID != "7" {
		t.Fatalf("绑定未保存: %+v ok=%v", binding, ok)
	}
	message = accountMessage("group-b", "member-1", "余额")
	if _, response := workflow.Handle(context.Background(), message); !strings.Contains(response, "12.50") || !strings.Contains(response, "再次查询：@机器人 余额") {
		t.Fatalf("跨群余额响应错误: %q", response)
	}
}

func TestAccountServiceHandlesHelpWithoutGGAPIConfiguration(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	workflow := NewAccountService(state, state, nil, nil)
	handled, response := workflow.Handle(context.Background(), accountMessage("group-a", "member-1", "@机器人 帮助"))
	if !handled || !strings.Contains(response, "绑定示例：@机器人 绑定") || !strings.Contains(response, "余额示例：@机器人 余额") {
		t.Fatalf("帮助响应错误: handled=%v response=%q", handled, response)
	}
}

func TestAccountServiceTestEmailDoesNotCreateBinding(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	mailer := &accountMailer{}
	workflow := NewAccountService(state, state, nil, mailer)
	if err := workflow.TestEmail(context.Background(), "name@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(mailer.codes) != 1 || !sixDigits.MatchString(mailer.codes[0]) {
		t.Fatalf("测试邮件验证码错误: %v", mailer.codes)
	}
	if bindings := state.AccountBindings(); len(bindings) != 0 {
		t.Fatalf("测试邮件不应创建绑定: %+v", bindings)
	}
}

func TestAccountServiceCodeAttemptsAndCancelKeepOldBinding(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.UpsertAccountBinding(domain.AccountBinding{ID: "old", MemberOpenID: "member-1", Email: "old@example.com", GGAPIUserID: "old-user", FirstGroupOpenID: "group-a"}); err != nil {
		t.Fatal(err)
	}
	mailer := &accountMailer{}
	workflow := NewAccountService(state, state, accountVerifier{verifyErr: errors.New("rejected")}, mailer)
	message := accountMessage("group-a", "member-1", "绑定")
	workflow.Handle(context.Background(), message)
	message.Content = "new@example.com"
	workflow.Handle(context.Background(), message)
	message.Content = "000000"
	for attempt := 0; attempt < 5; attempt++ {
		_, response := workflow.Handle(context.Background(), message)
		if attempt == 4 && !strings.Contains(response, "重新绑定示例") {
			t.Fatalf("验证码耗尽响应错误: %q", response)
		}
	}
	if binding, ok := state.AccountBinding("member-1"); !ok || binding.Email != "old@example.com" {
		t.Fatalf("失败重绑不应覆盖旧绑定: %+v ok=%v", binding, ok)
	}
	message.Content = "绑定"
	workflow.Handle(context.Background(), message)
	message.Content = "取消"
	if _, response := workflow.Handle(context.Background(), message); !strings.Contains(response, "旧绑定未受影响") {
		t.Fatalf("取消响应错误: %q", response)
	}
}

func TestAccountServiceExpiresCodeAndLimitsResendInterval(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	mailer := &accountMailer{}
	workflow := NewAccountService(state, state, accountVerifier{user: ggapi.User{ID: "7", Email: "name@example.com", Role: "user", Status: "active"}}, mailer)
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	workflow.now = func() time.Time { return now }
	message := accountMessage("group-a", "member-1", "绑定")
	workflow.Handle(context.Background(), message)
	message.Content = "name@example.com"
	workflow.Handle(context.Background(), message)
	message.Content = "重发"
	if _, response := workflow.Handle(context.Background(), message); !strings.Contains(response, "60 秒") && !strings.Contains(response, "61 秒") {
		t.Fatalf("重发间隔响应错误: %q", response)
	}
	now = now.Add(11 * time.Minute)
	message.Content = mailer.codes[0]
	if _, response := workflow.Handle(context.Background(), message); !strings.Contains(response, "已过期") {
		t.Fatalf("过期验证码响应错误: %q", response)
	}
	if _, ok := state.AccountBinding("member-1"); ok {
		t.Fatal("过期验证码不应创建绑定")
	}
}

func TestAccountServiceLimitsCodeSendsByMemberAndEmail(t *testing.T) {
	t.Run("同一成员不能通过更换邮箱绕过限制", func(t *testing.T) {
		state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		mailer := &accountMailer{}
		workflow := NewAccountService(state, state, accountVerifier{}, mailer)
		now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
		workflow.now = func() time.Time { return now }
		message := accountMessage("group-a", "member-1", "绑定")
		workflow.Handle(context.Background(), message)
		for index := 0; index < 6; index++ {
			message.Content = fmt.Sprintf("name%d@example.com", index)
			_, response := workflow.Handle(context.Background(), message)
			if index < 5 && !strings.Contains(response, "验证码已发送") {
				t.Fatalf("第 %d 次发送应成功: %q", index+1, response)
			}
			if index == 5 && !strings.Contains(response, "每小时上限") {
				t.Fatalf("第 6 次发送应触发成员限制: %q", response)
			}
		}
		if len(mailer.codes) != 5 {
			t.Fatalf("SMTP 调用次数 = %d，期望 5", len(mailer.codes))
		}
	})

	t.Run("同一邮箱不能通过更换成员绕过限制", func(t *testing.T) {
		state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		mailer := &accountMailer{}
		workflow := NewAccountService(state, state, accountVerifier{}, mailer)
		now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
		workflow.now = func() time.Time { return now }
		for index := 0; index < 6; index++ {
			message := accountMessage("group-a", fmt.Sprintf("member-%d", index), "绑定")
			workflow.Handle(context.Background(), message)
			message.Content = "shared@example.com"
			_, response := workflow.Handle(context.Background(), message)
			if index < 5 && !strings.Contains(response, "验证码已发送") {
				t.Fatalf("第 %d 次发送应成功: %q", index+1, response)
			}
			if index == 5 && !strings.Contains(response, "每小时上限") {
				t.Fatalf("第 6 次发送应触发邮箱限制: %q", response)
			}
		}
		if len(mailer.codes) != 5 {
			t.Fatalf("SMTP 调用次数 = %d，期望 5", len(mailer.codes))
		}
	})
}

func TestEnabledUserRequiresExplicitActiveIdentityAndKnownRole(t *testing.T) {
	tests := []struct {
		name string
		user ggapi.User
		want bool
	}{
		{name: "数值普通用户", user: ggapi.User{Role: "1", Status: "1"}, want: true},
		{name: "文本普通用户", user: ggapi.User{Role: "user", Status: "active"}, want: true},
		{name: "缺少角色", user: ggapi.User{Status: "1"}},
		{name: "缺少状态", user: ggapi.User{Role: "1"}},
		{name: "管理员", user: ggapi.User{Role: "10", Status: "1"}, want: true},
		{name: "禁用", user: ggapi.User{Role: "1", Status: "2"}},
		{name: "已删除", user: ggapi.User{Role: "1", Status: "1", Deleted: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := enabledUser(test.user); got != test.want {
				t.Fatalf("enabledUser() = %v，期望 %v", got, test.want)
			}
		})
	}
}

func TestAccountServiceConversationRepliesAlwaysIncludeNextAction(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{GGAPIBalanceEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	mailer := &accountMailer{}
	verifier := accountVerifier{
		user:    ggapi.User{ID: "7", Email: "name@example.com", Username: "name", Role: "1", Status: "1"},
		balance: ggapi.Balance{Amount: 12.5, Currency: "USD"},
	}
	workflow := NewAccountService(state, state, verifier, mailer)
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	workflow.now = func() time.Time { return now }
	message := accountMessage("group-a", "member-1", "")
	handle := func(content string) string {
		t.Helper()
		message.Content = content
		handled, response := workflow.Handle(context.Background(), message)
		if !handled || response == "" {
			t.Fatalf("账号输入未产生回复: content=%q handled=%v", content, handled)
		}
		requireAccountAction(t, response)
		return response
	}

	for _, command := range []string{"余额", "解绑", "重发", "取消"} {
		handle(command)
	}
	handle("绑定")
	handle("abc")
	handle("name@example.com")
	handle("abc")
	wrongCode := "000000"
	if mailer.codes[len(mailer.codes)-1] == wrongCode {
		wrongCode = "999999"
	}
	handle(wrongCode)
	handle("重发")
	now = now.Add(61 * time.Second)
	handle("重发")
	handle(mailer.codes[len(mailer.codes)-1])
	handle("余额")
	handle("绑定")
	handle("取消")
	handle("解绑")
	handle("余额")
	handle("绑定")
	handle("name@example.com")
	expiredCode := mailer.codes[len(mailer.codes)-1]
	now = now.Add(11 * time.Minute)
	handle(expiredCode)

	t.Run("服务未配置", func(t *testing.T) {
		disabledState, openErr := store.Open(filepath.Join(t.TempDir(), "state.json"), domain.Settings{})
		if openErr != nil {
			t.Fatal(openErr)
		}
		disabled := NewAccountService(disabledState, disabledState, verifier, mailer)
		_, response := disabled.Handle(context.Background(), accountMessage("group-a", "member-2", "绑定"))
		requireAccountAction(t, response)
	})

	t.Run("邮件发送失败", func(t *testing.T) {
		failedMailer := &accountMailer{err: errors.New("smtp failed")}
		failed := NewAccountService(state, state, verifier, failedMailer)
		failed.Handle(context.Background(), accountMessage("group-a", "member-2", "绑定"))
		_, response := failed.Handle(context.Background(), accountMessage("group-a", "member-2", "name@example.com"))
		requireAccountAction(t, response)
	})

	t.Run("账号核验失败", func(t *testing.T) {
		codes := &accountMailer{}
		failed := NewAccountService(state, state, accountVerifier{verifyErr: errors.New("rejected")}, codes)
		failed.Handle(context.Background(), accountMessage("group-a", "member-3", "绑定"))
		failed.Handle(context.Background(), accountMessage("group-a", "member-3", "name@example.com"))
		_, response := failed.Handle(context.Background(), accountMessage("group-a", "member-3", codes.codes[0]))
		requireAccountAction(t, response)
	})

	t.Run("上游查询失败", func(t *testing.T) {
		if saveErr := state.UpsertAccountBinding(domain.AccountBinding{ID: "query-failed", MemberOpenID: "member-4", Email: "name@example.com", GGAPIUserID: "7"}); saveErr != nil {
			t.Fatal(saveErr)
		}
		failed := NewAccountService(state, state, accountVerifier{getErr: errors.New("upstream failed")}, mailer)
		_, response := failed.Handle(context.Background(), accountMessage("group-a", "member-4", "余额"))
		requireAccountAction(t, response)
		if _, ok := state.AccountBinding("member-4"); !ok {
			t.Fatal("临时上游失败不应删除绑定")
		}
	})

	t.Run("身份变化自动解绑", func(t *testing.T) {
		if saveErr := state.UpsertAccountBinding(domain.AccountBinding{ID: "identity-changed", MemberOpenID: "member-5", Email: "name@example.com", GGAPIUserID: "7"}); saveErr != nil {
			t.Fatal(saveErr)
		}
		changed := NewAccountService(state, state, accountVerifier{user: ggapi.User{ID: "7", Email: "other@example.com", Role: "1", Status: "1"}}, mailer)
		_, response := changed.Handle(context.Background(), accountMessage("group-a", "member-5", "余额"))
		requireAccountAction(t, response)
		if _, ok := state.AccountBinding("member-5"); ok {
			t.Fatal("身份变化后应自动删除绑定")
		}
	})
}

func requireAccountAction(t *testing.T, response string) {
	t.Helper()
	if !strings.Contains(response, "@机器人") {
		t.Fatalf("账号回复缺少下一步操作示例: %q", response)
	}
}
