package domain

import "strings"

type GroupMessage struct {
	ID          string `json:"id"`
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
	Bot bool `json:"bot"`
}

// MentionsBot 判断全量群消息是否明确提及了机器人。
func (m GroupMessage) MentionsBot() bool {
	for _, mention := range m.Mentions {
		if mention.Bot {
			return true
		}
	}
	return false
}

const (
	CommandBind    = "绑定"
	CommandBalance = "余额"
	CommandUnbind  = "解绑"
	CommandCancel  = "取消"
	CommandResend  = "重发"
	CommandHelp    = "帮助"
)

func IsAccountCommand(content string) bool {
	switch NormalizeContent(content) {
	case CommandBind, CommandBalance, CommandUnbind, CommandCancel, CommandResend, CommandHelp:
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
