package app

import (
	"context"
	"errors"

	"ai-upstream-monitor/internal/axonhub"
	"ai-upstream-monitor/internal/domain"
)

// SchedulerBackend 是调度器防腐层的最小端口，只暴露 AUM 实际需要的渠道能力。
type SchedulerBackend interface {
	Channels(context.Context) ([]domain.SchedulerChannel, error)
	UpdateFields(context.Context, domain.SchedulerChannel, []string, int) (domain.SchedulerChannel, error)
}

type axonHubBackend struct {
	service *SchedulerService
	cfg     domain.AxonHubConfig
}

func (b axonHubBackend) Channels(ctx context.Context) ([]domain.SchedulerChannel, error) {
	client, err := b.service.axonHubClient(ctx, b.cfg)
	if err != nil {
		return nil, err
	}
	rows, err := client.Channels(ctx)
	if errors.Is(err, axonhub.ErrUnauthorized) {
		b.service.resetAxonHubSession()
		client, err = b.service.axonHubClient(ctx, b.cfg)
		if err == nil {
			rows, err = client.Channels(ctx)
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]domain.SchedulerChannel, 0, len(rows))
	for _, row := range rows {
		out = append(out, axonHubDomainChannel(row))
	}
	return out, nil
}

func (b axonHubBackend) UpdateFields(ctx context.Context, current domain.SchedulerChannel, tags []string, weight int) (domain.SchedulerChannel, error) {
	client, err := b.service.axonHubClient(ctx, b.cfg)
	if err != nil {
		return domain.SchedulerChannel{}, err
	}
	row, err := client.UpdateFields(ctx, current.ID, tags, weight)
	if errors.Is(err, axonhub.ErrUnauthorized) {
		b.service.resetAxonHubSession()
		client, err = b.service.axonHubClient(ctx, b.cfg)
		if err == nil {
			row, err = client.UpdateFields(ctx, current.ID, tags, weight)
		}
	}
	if err != nil {
		return domain.SchedulerChannel{}, err
	}
	return axonHubDomainChannel(row), nil
}

func axonHubDomainChannel(row axonhub.Channel) domain.SchedulerChannel {
	status := 0
	if row.Status == domain.AxonHubStatusEnabled {
		status = 1
	} else if row.Status == domain.AxonHubStatusDisabled {
		status = 2
	}
	return domain.SchedulerChannel{
		ID: row.ID, Name: row.Name, Type: row.Type, Status: status, RemoteStatus: row.Status,
		Tags: row.Tags, OrderingWeight: row.OrderingWeight, Models: row.Models,
		Archived: row.Status == domain.AxonHubStatusArchived,
	}
}
