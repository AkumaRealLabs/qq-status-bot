package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/qqbot"
)

const interactionDataPrefix = "qq-status-bot:"

const (
	interactionStatus  = "status"
	interactionHelp    = "help"
	interactionBind    = "bind"
	interactionBalance = "balance"
	interactionUnbind  = "unbind"
	interactionCancel  = "cancel"
	interactionResend  = "resend"
)

type interactiveGroupReplier interface {
	ReplyGroupImageWithTarget(context.Context, string, string, string, []byte) error
	ReplyGroupTextWithKeyboard(context.Context, string, string, string, string, int, qqbot.Keyboard) error
	RespondInteraction(context.Context, string, int) error
}

type activeInteractiveImageSender interface {
	SendGroupImageWithKeyboard(context.Context, string, []byte, qqbot.Keyboard) error
}

type activeInteractiveTextSender interface {
	SendGroupTextWithKeyboard(context.Context, string, string, qqbot.Keyboard) error
}

func (s *Service) sendActiveText(ctx context.Context, groupOpenID, content string) error {
	if sender, ok := s.replier.(activeInteractiveTextSender); ok {
		return sender.SendGroupTextWithKeyboard(ctx, groupOpenID, content, mainKeyboard(s.settings.Settings(), ""))
	}
	sender, ok := s.replier.(ActiveMessageSender)
	if !ok {
		return errors.New("QQ 客户端不支持主动消息")
	}
	return sender.SendGroupText(ctx, groupOpenID, content)
}

func (s *Service) handleInteraction(ctx context.Context, payload qqbot.Payload, settings domain.Settings) []byte {
	var interaction qqbot.Interaction
	if err := json.Unmarshal(payload.Data, &interaction); err != nil {
		return qqbot.CallbackACK(true)
	}
	interactionID := firstNonempty(strings.TrimSpace(payload.ID), strings.TrimSpace(interaction.ID))
	if interactionID == "" {
		return qqbot.CallbackACK(true)
	}
	message := domain.GroupMessage{
		ID:          interactionID,
		EventID:     interactionID,
		GroupOpenID: strings.TrimSpace(interaction.GroupOpenID),
	}
	message.Author.MemberOpenID = strings.TrimSpace(interaction.GroupMemberOpenID)
	if interaction.Type != qqbot.InteractionTypeMessageButton {
		if interaction.Type == qqbot.InteractionTypeQuickMenu {
			s.respondInteraction(ctx, interactionID, 1, message)
		}
		return qqbot.CallbackACK(true)
	}

	resultCode := 0
	action, ok := interactionAction(interaction.Data.Resolved.ButtonData)
	switch {
	case interaction.Scene != "group" || message.GroupOpenID == "" || message.Author.MemberOpenID == "":
		resultCode = 1
	case !groupAllowed(message.GroupOpenID, settings.AllowedGroups):
		resultCode = 4
	case !ok:
		resultCode = 1
	case s.duplicate(message.ID):
		resultCode = 3
	default:
		message.Content = action.command
		if action.help {
			resultCode = s.enqueueInteractionHelp(message)
		} else if action.account {
			resultCode = s.enqueueInteractionAccount(message)
		} else {
			resultCode = s.enqueueInteractionStatus(message)
		}
	}

	status := "accepted"
	if resultCode != 0 {
		status = "rejected"
	}
	actionName := action.name
	if actionName == "" {
		actionName = "unknown"
	}
	s.logEvent(qqbot.EventInteractionCreate, message, status, "按钮动作 "+actionName)
	s.respondInteraction(ctx, interactionID, resultCode, message)
	return qqbot.CallbackACK(true)
}

type buttonAction struct {
	name    string
	command string
	account bool
	help    bool
}

func interactionAction(data string) (buttonAction, bool) {
	name := strings.TrimPrefix(strings.TrimSpace(data), interactionDataPrefix)
	if name == strings.TrimSpace(data) {
		return buttonAction{}, false
	}
	actions := map[string]buttonAction{
		interactionStatus:  {name: interactionStatus, command: domain.CommandStatus},
		interactionHelp:    {name: interactionHelp, command: domain.CommandHelp, help: true},
		interactionBind:    {name: interactionBind, command: domain.CommandBind, account: true},
		interactionBalance: {name: interactionBalance, command: domain.CommandBalance, account: true},
		interactionUnbind:  {name: interactionUnbind, command: domain.CommandUnbind, account: true},
		interactionCancel:  {name: interactionCancel, command: domain.CommandCancel, account: true},
		interactionResend:  {name: interactionResend, command: domain.CommandResend, account: true},
	}
	action, ok := actions[name]
	return action, ok
}

func (s *Service) enqueueInteractionHelp(message domain.GroupMessage) int {
	select {
	case s.helpJobs <- message:
		s.markSeen(message.ID)
		return 0
	default:
		return 2
	}
}

func (s *Service) enqueueInteractionStatus(message domain.GroupMessage) int {
	select {
	case s.jobs <- message:
		s.markSeen(message.ID)
		return 0
	default:
		return 2
	}
}

func (s *Service) enqueueInteractionAccount(message domain.GroupMessage) int {
	s.accountMu.Lock()
	account := s.account
	accountJobs := s.accountJobs
	s.accountMu.Unlock()
	if account == nil || accountJobs == nil {
		return 1
	}
	select {
	case accountJobs <- message:
		s.markSeen(message.ID)
		return 0
	default:
		return 2
	}
}

func (s *Service) respondInteraction(parent context.Context, interactionID string, code int, message domain.GroupMessage) {
	replier, ok := s.replier.(interactiveGroupReplier)
	if !ok {
		s.logEvent(qqbot.EventInteractionCreate, message, "failed", "QQ 客户端不支持互动确认")
		return
	}
	ctx, cancel := context.WithTimeout(parent, 2500*time.Millisecond)
	defer cancel()
	if err := replier.RespondInteraction(ctx, interactionID, code); err != nil {
		log.Printf("QQ 按钮互动确认失败 group=%s: %v", message.GroupOpenID, err)
		s.logEvent(qqbot.EventInteractionCreate, message, "failed", "互动确认失败")
	}
}

func (s *Service) replyImageWithMenu(ctx context.Context, message domain.GroupMessage, image []byte) error {
	keyboard := mainKeyboard(s.settings.Settings(), message.Author.MemberOpenID)
	if replier, ok := s.replier.(interactiveGroupReplier); ok {
		messageID := message.ID
		if message.EventID != "" {
			messageID = ""
		}
		if err := replier.ReplyGroupImageWithTarget(ctx, message.GroupOpenID, messageID, message.EventID, image); err != nil {
			return err
		}
		return replier.ReplyGroupTextWithKeyboard(ctx, message.GroupOpenID, messageID, message.EventID, "请选择操作：", 2, keyboard)
	}
	if message.EventID != "" {
		return errors.New("QQ 客户端不支持互动事件图片回复")
	}
	return s.replier.ReplyGroupImage(ctx, message.GroupOpenID, message.ID, image)
}

func (s *Service) replyTextWithMenu(ctx context.Context, message domain.GroupMessage, content string, seq int, keyboard qqbot.Keyboard) error {
	if replier, ok := s.replier.(interactiveGroupReplier); ok {
		messageID := message.ID
		if message.EventID != "" {
			messageID = ""
		}
		return replier.ReplyGroupTextWithKeyboard(ctx, message.GroupOpenID, messageID, message.EventID, content, seq, keyboard)
	}
	if message.EventID != "" {
		return errors.New("QQ 客户端不支持互动事件文本回复")
	}
	return s.replier.ReplyGroupText(ctx, message.GroupOpenID, message.ID, content, seq)
}

func mainKeyboard(settings domain.Settings, _ string) qqbot.Keyboard {
	rows := []qqbot.KeyboardRow{{Buttons: []qqbot.KeyboardButton{
		callbackButton("status", "查看状态", interactionStatus, 1, ""),
		callbackButton("help", "使用帮助", interactionHelp, 0, ""),
	}}}
	if settings.GGAPIBalanceEnabled {
		rows = append(rows,
			qqbot.KeyboardRow{Buttons: []qqbot.KeyboardButton{
				callbackButton("balance", "查询余额", interactionBalance, 1, ""),
				callbackButton("bind", "绑定账号", interactionBind, 0, ""),
			}},
			qqbot.KeyboardRow{Buttons: []qqbot.KeyboardButton{
				callbackButton("unbind", "解绑账号", interactionUnbind, 0, ""),
			}},
		)
	}
	return qqbot.Keyboard{Content: &qqbot.KeyboardContent{Rows: rows}}
}

func pendingKeyboard(memberOpenID string, canResend bool) qqbot.Keyboard {
	flowButtons := []qqbot.KeyboardButton{callbackButton("cancel", "取消绑定", interactionCancel, 0, memberOpenID)}
	if canResend {
		flowButtons = []qqbot.KeyboardButton{
			callbackButton("resend", "重发验证码", interactionResend, 1, memberOpenID),
			callbackButton("cancel", "取消绑定", interactionCancel, 0, memberOpenID),
		}
	}
	return qqbot.Keyboard{Content: &qqbot.KeyboardContent{Rows: []qqbot.KeyboardRow{
		{Buttons: flowButtons},
		{Buttons: []qqbot.KeyboardButton{
			callbackButton("status", "查看状态", interactionStatus, 0, ""),
			callbackButton("help", "使用帮助", interactionHelp, 0, ""),
		}},
	}}}
}

func callbackButton(id, label, action string, style int, memberOpenID string) qqbot.KeyboardButton {
	permission := qqbot.ButtonPermission{Type: qqbot.ButtonPermissionAll}
	if strings.TrimSpace(memberOpenID) != "" {
		permission = qqbot.ButtonPermission{Type: qqbot.ButtonPermissionUser, SpecifyUserIDs: []string{strings.TrimSpace(memberOpenID)}}
	}
	return qqbot.KeyboardButton{
		ID:         id,
		RenderData: qqbot.ButtonRenderData{Label: label, VisitedLabel: label, Style: style},
		Action: qqbot.KeyboardButtonAction{
			Type: qqbot.ButtonActionCallback, Permission: permission,
			Data: interactionDataPrefix + action, UnsupportTips: "当前 QQ 版本不支持此按钮，请发送文字命令。",
		},
	}
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
