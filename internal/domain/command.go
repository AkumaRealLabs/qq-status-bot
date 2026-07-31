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
}

const (
	CommandBind    = "绑定"
	CommandBalance = "余额"
	CommandUnbind  = "解绑"
	CommandCancel  = "取消"
	CommandResend  = "重发"
)

func IsAccountCommand(content string) bool {
	switch NormalizeContent(content) {
	case CommandBind, CommandBalance, CommandUnbind, CommandCancel, CommandResend:
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
