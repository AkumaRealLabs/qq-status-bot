package app

import (
	"context"

	"ai-upstream-monitor/internal/axonhub"
	"ai-upstream-monitor/internal/domain"
)

type SchedulerCapabilities struct {
	Tags            bool
	OrderingWeight  bool
	NumericPriority bool
	AffinityCache   bool
	TrafficLogs     bool
}

// SchedulerBackend 是调度器防腐层的最小端口，只暴露 AUM 实际需要的渠道能力。
type SchedulerBackend interface {
	Channels(context.Context) ([]domain.SchedulerChannel, error)
	UpdateFields(context.Context, domain.SchedulerChannel, []string, int) (domain.SchedulerChannel, error)
	UpdateStatus(context.Context, domain.SchedulerChannel, string) (domain.SchedulerChannel, error)
	Capabilities() SchedulerCapabilities
}

type axonHubBackend struct {
	client axonhub.Client
}

func (b axonHubBackend) Channels(ctx context.Context) ([]domain.SchedulerChannel, error) {
	rows, err := b.client.Channels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.SchedulerChannel, 0, len(rows))
	for _, row := range rows {
		status := 0
		if row.Status == domain.AxonHubStatusEnabled {
			status = 1
		} else if row.Status == domain.AxonHubStatusDisabled {
			status = 2
		}
		out = append(out, domain.SchedulerChannel{
			ID: row.ID, Name: row.Name, Type: row.Type, Status: status, RemoteStatus: row.Status,
			Tags: row.Tags, OrderingWeight: row.OrderingWeight, Models: row.Models,
			Archived: row.Status == domain.AxonHubStatusArchived,
		})
	}
	return out, nil
}

func (b axonHubBackend) UpdateFields(ctx context.Context, current domain.SchedulerChannel, tags []string, weight int) (domain.SchedulerChannel, error) {
	row, err := b.client.UpdateFields(ctx, current.ID, tags, weight)
	if err != nil {
		return domain.SchedulerChannel{}, err
	}
	return axonHubDomainChannel(row), nil
}

func (b axonHubBackend) UpdateStatus(ctx context.Context, current domain.SchedulerChannel, status string) (domain.SchedulerChannel, error) {
	row, err := b.client.UpdateStatus(ctx, current.ID, status)
	if err != nil {
		return domain.SchedulerChannel{}, err
	}
	return axonHubDomainChannel(row), nil
}

func (axonHubBackend) Capabilities() SchedulerCapabilities {
	return SchedulerCapabilities{Tags: true, OrderingWeight: true}
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

type ggapiBackend struct {
	service *SchedulerService
	cfg     domain.SchedulerConfig
}

func (b ggapiBackend) Channels(ctx context.Context) ([]domain.SchedulerChannel, error) {
	return b.service.fetchSchedulerChannels(ctx, b.cfg, "")
}

func (b ggapiBackend) UpdateFields(ctx context.Context, current domain.SchedulerChannel, tags []string, weight int) (domain.SchedulerChannel, error) {
	group := domain.JoinGroups(tags)
	if err := b.service.coordinateSchedulerFields(ctx, b.cfg, current, group, current.Priority, uint(weight), true, domain.ControlSourceCost, "成本分组基线"); err != nil {
		return domain.SchedulerChannel{}, err
	}
	row, _, err := b.service.schedulerChannel(ctx, b.cfg, current.ID)
	return row, err
}

func (b ggapiBackend) UpdateStatus(ctx context.Context, current domain.SchedulerChannel, status string) (domain.SchedulerChannel, error) {
	value := 2
	if status == domain.AxonHubStatusEnabled {
		value = 1
	}
	if err := b.service.coordinateSchedulerStatus(ctx, current.ID, value, domain.ControlSourceManual, "调度器后端状态写入", false); err != nil {
		return domain.SchedulerChannel{}, err
	}
	row, _, err := b.service.schedulerChannel(ctx, b.cfg, current.ID)
	return row, err
}

func (ggapiBackend) Capabilities() SchedulerCapabilities {
	return SchedulerCapabilities{NumericPriority: true, AffinityCache: true, TrafficLogs: true}
}
