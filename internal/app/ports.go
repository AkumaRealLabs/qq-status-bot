package app

import (
	"context"

	"ai-upstream-monitor/internal/onebot"
)

// Notifier：出站通知端口（当前为 Telegram）。
type Notifier interface {
	Send(ctx context.Context, message string) error
}

// OneBotClient：OneBot 通用文本发送的 HTTP 出站端口。
type OneBotClient interface {
	GetLoginInfo(ctx context.Context, baseURL, token string) (onebot.LoginInfo, error)
	SendGroupMessage(ctx context.Context, baseURL, token, groupID, text string) error
}

// telegramNotifier 实现 Notifier；闭包引用 Service，Settings/HTTP 保持最新。
type telegramNotifier struct {
	send func(context.Context, string) error
}

func (n *telegramNotifier) Send(ctx context.Context, message string) error {
	return n.send(ctx, message)
}
