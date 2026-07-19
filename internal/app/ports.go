package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/onebot"
	"ai-upstream-monitor/internal/store"
)

// CardRepository：模型卡片（探测 / 号池绑定）的持久化端口。
// 生产接线使用 *store.Store（见下方编译期检查）。
type CardRepository interface {
	Card(ctx context.Context, id string) (domain.ModelCard, error)
	ListCards(ctx context.Context) ([]domain.ModelCard, error)
	CreateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error)
	UpdateCard(ctx context.Context, c domain.ModelCard) (domain.ModelCard, error)
	DeleteCard(ctx context.Context, id string) error
	UpdateCardOrder(ctx context.Context, ids []string) error
	UpdateCardProbeState(ctx context.Context, id, lastError string, failureCount int) error
	UpdateCardSchedulerAutoDisabled(ctx context.Context, id string, disabled bool) error
}

// Notifier：出站通知端口（当前为 Telegram）。
type Notifier interface {
	Send(ctx context.Context, message string) error
}

// ProbeRunner：出站模型探测端口。
type ProbeRunner interface {
	Probe(ctx context.Context, baseURL, key, model string) monitor.ProbeResult
}

// OneBotClient：QQ 群查询的 OneBot HTTP 出站端口。
type OneBotClient interface {
	GetLoginInfo(ctx context.Context, baseURL, token string) (onebot.LoginInfo, error)
	SendGroupMessage(ctx context.Context, baseURL, token, groupID, text string) error
}

// 编译期检查：*store.Store 实现 CardRepository。
var _ CardRepository = (*store.Store)(nil)

// telegramNotifier 实现 Notifier；闭包引用 Service，Settings/HTTP 保持最新。
type telegramNotifier struct {
	send func(context.Context, string) error
}

func (n *telegramNotifier) Send(ctx context.Context, message string) error {
	return n.send(ctx, message)
}

// liveProbeRunner 始终调用当前 monitor.Client，便于测试替换 Client。
type liveProbeRunner struct {
	svc *Service
}

func (r *liveProbeRunner) Probe(ctx context.Context, baseURL, key, model string) monitor.ProbeResult {
	return r.svc.Client.Probe(ctx, baseURL, key, model)
}
