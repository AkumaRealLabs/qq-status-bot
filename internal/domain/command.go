package domain

import "strings"

type GroupMessage struct {
	ID          string `json:"id"`
	EventID     string `json:"-"`
	GroupOpenID string `json:"group_openid"`
	Content     string `json:"content"`
	Author      struct {
		Bot          bool   `json:"bot"`
		MemberOpenID string `json:"member_openid"`
	} `json:"author"`
	Mentions []GroupMention `json:"mentions,omitempty"`
}

// GroupMention 是 QQ 全量群消息中的被提及用户摘要。
type GroupMention struct {
	ID    string `json:"id"`
	Bot   bool   `json:"bot"`
	IsYou bool   `json:"is_you"`
}

// MentionsBot 判断全量群消息的结构化提及列表是否明确包含当前机器人。
func (m GroupMessage) MentionsBot() bool {
	for _, mention := range m.Mentions {
		if mention.IsYou {
			return true
		}
	}
	return false
}

const (
	CommandStatus   = "状态"
	CommandBind     = "绑定"
	CommandBalance  = "余额"
	CommandUnbind   = "解绑"
	CommandCancel   = "取消"
	CommandResend   = "重发"
	CommandHelp     = "帮助"
	CommandMenu     = "菜单"
	CommandHowTo    = "怎么用"
	CommandChatHelp = "聊天帮助"
)

func IsAccountCommand(content string) bool {
	switch NormalizeContent(content) {
	case CommandBind, CommandBalance, CommandUnbind, CommandCancel, CommandResend,
		CommandHelp, CommandMenu, CommandHowTo, CommandChatHelp:
		return true
	default:
		return false
	}
}

func IsHelpCommand(content string) bool {
	switch NormalizeContent(content) {
	case CommandHelp, CommandMenu, CommandHowTo, CommandChatHelp:
		return true
	default:
		return false
	}
}

// NormalizeContent 去掉 QQ 事件正文开头的机器人提及，保留业务命令或流程输入。
func NormalizeContent(content string) string {
	content = strings.TrimSpace(content)
	for {
		switch {
		case strings.HasPrefix(content, "<@"):
			if end := strings.IndexByte(content, '>'); end >= 0 {
				content = strings.TrimSpace(content[end+1:])
				continue
			}
		case strings.HasPrefix(content, "@机器人"):
			content = strings.TrimSpace(strings.TrimPrefix(content, "@机器人"))
			continue
		}
		return content
	}
}

func IsCommand(content string, commands []string) bool {
	content = NormalizeContent(content)
	for _, command := range commands {
		if strings.EqualFold(content, strings.TrimSpace(command)) {
			return true
		}
	}
	return false
}
