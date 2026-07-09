package app

import (
	"context"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

// CardRepository is the persistence port for model cards (probe / pool binding).
// Production wiring uses *store.Store (see compile-time check below).
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

// Notifier is the outbound notification port (Telegram today).
type Notifier interface {
	Send(ctx context.Context, message string) error
}

// ProbeRunner is the outbound model-probe port.
type ProbeRunner interface {
	Probe(ctx context.Context, baseURL, key, model string) monitor.ProbeResult
}

// Compile-time: *store.Store implements CardRepository.
var _ CardRepository = (*store.Store)(nil)

// telegramNotifier implements Notifier; closes over Service so Settings/HTTP stay live.
type telegramNotifier struct {
	send func(context.Context, string) error
}

func (n *telegramNotifier) Send(ctx context.Context, message string) error {
	return n.send(ctx, message)
}

// liveProbeRunner always calls the current monitor.Client so tests can swap Client.
type liveProbeRunner struct {
	svc *Service
}

func (r *liveProbeRunner) Probe(ctx context.Context, baseURL, key, model string) monitor.ProbeResult {
	return r.svc.Client.Probe(ctx, baseURL, key, model)
}
