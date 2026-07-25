package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *SchedulerService) CostBindings(ctx context.Context) ([]domain.SchedulerCostBinding, error) {
	rows, err := s.app.Store.ListCostBindings(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i] = costBindingProjection(rows[i])
		if ownership, found, _ := s.app.Store.CostFieldOwnership(ctx, domain.SchedulerProviderGGAPI, rows[i].SchedulerChannelID); found {
			rows[i].GGAPIExternalTakeover = ownership.ExternalTakeover
			rows[i].GGAPIOwnershipReason = ownership.LastReason
		}
		if ownership, found, _ := s.app.Store.CostFieldOwnership(ctx, domain.SchedulerProviderAxonHub, rows[i].AxonHubChannelID); found {
			rows[i].AxonHubExternalTakeover = ownership.ExternalTakeover
			rows[i].AxonHubOwnershipReason = ownership.LastReason
		}
	}
	return rows, nil
}

func (s *SchedulerService) CostBinding(ctx context.Context, id string) (domain.SchedulerCostBinding, error) {
	row, err := s.app.Store.CostBinding(ctx, id)
	if err != nil {
		return row, err
	}
	return costBindingProjection(row), nil
}

func (s *SchedulerService) SaveCostBinding(ctx context.Context, id string, in domain.SchedulerCostBinding) (domain.SchedulerCostBinding, error) {
	in = domain.NormalizeCostBinding(in)
	if err := domain.ValidateCostBinding(in); err != nil {
		return in, BadRequest(err)
	}
	if in.SourceType == domain.CostSourceUpstreamKey {
		upstream, err := s.app.Store.Upstream(ctx, in.UpstreamID)
		if err != nil {
			return in, err
		}
		key, err := s.app.Store.Key(ctx, in.KeyID)
		if err != nil {
			return in, err
		}
		if key.UpstreamID != upstream.ID {
			return in, ErrBadRequest("Key 不属于所选上游")
		}
	}
	rows, err := s.app.Store.ListCostBindings(ctx)
	if err != nil {
		return in, err
	}
	for _, row := range rows {
		if row.ID == id {
			continue
		}
		if in.SchedulerChannelID != "" && in.SchedulerChannelID == row.SchedulerChannelID {
			return in, ErrBadRequest("GGAPI 渠道已绑定到其他成本项")
		}
		if in.AxonHubChannelID != "" && in.AxonHubChannelID == row.AxonHubChannelID {
			return in, ErrBadRequest("AxonHub 渠道已绑定到其他成本项")
		}
	}
	var out domain.SchedulerCostBinding
	if id == "" {
		out, err = s.app.Store.CreateCostBinding(ctx, in)
	} else {
		old, oldErr := s.app.Store.CostBinding(ctx, id)
		if oldErr != nil {
			return in, oldErr
		}
		in.ID, in.CreatedAt = old.ID, old.CreatedAt
		out, err = s.app.Store.UpdateCostBinding(ctx, in)
		if err == nil {
			s.recordInactiveBindingSnapshot(ctx, old, out)
		}
	}
	if err != nil {
		return out, err
	}
	out = costBindingProjection(out)
	if err := s.recordCostBindingSnapshot(ctx, out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *SchedulerService) recordInactiveBindingSnapshot(ctx context.Context, old, next domain.SchedulerCostBinding) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return
	}
	oldChannel, nextChannel := old.SchedulerChannelID, next.SchedulerChannelID
	oldName := old.SchedulerChannelName
	if cfg.Provider == domain.SchedulerProviderAxonHub {
		oldChannel, nextChannel, oldName = old.AxonHubChannelID, next.AxonHubChannelID, old.AxonHubChannelName
	}
	if oldChannel == "" || (oldChannel == nextChannel && next.Enabled) {
		return
	}
	_, _ = s.app.Store.SaveSchedulerChannelCostSnapshot(ctx, domain.SchedulerChannelCostSnapshot{
		Provider: cfg.Provider, ChannelID: oldChannel, ChannelName: oldName, CardID: old.ID, CardName: old.Name,
		Active: false, MissingReason: "成本绑定已变更", EffectiveAt: time.Now().UTC(),
	})
}

func (s *SchedulerService) DeleteCostBinding(ctx context.Context, id string) error {
	old, err := s.app.Store.CostBinding(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		s.recordInactiveBindingSnapshot(ctx, old, domain.SchedulerCostBinding{})
	}
	return s.app.Store.DeleteCostBinding(ctx, id)
}

func (s *SchedulerService) AdoptCostBinding(ctx context.Context, id, provider string) (domain.CostFieldOwnership, error) {
	binding, err := s.app.Store.CostBinding(ctx, id)
	if err != nil {
		return domain.CostFieldOwnership{}, err
	}
	provider = domain.NormalizeSchedulerProvider(provider)
	channelID := binding.SchedulerChannelID
	if provider == domain.SchedulerProviderAxonHub {
		channelID = binding.AxonHubChannelID
	}
	if strings.TrimSpace(channelID) == "" {
		return domain.CostFieldOwnership{}, ErrBadRequest("该 provider 未绑定渠道")
	}
	channels, err := s.SchedulerChannelsForProvider(ctx, provider, "")
	if err != nil {
		return domain.CostFieldOwnership{}, err
	}
	channel, found := channelByID(channels, channelID)
	if !found {
		return domain.CostFieldOwnership{}, ErrBadRequest("远端找不到绑定渠道")
	}
	groups, weight := domain.SplitGroups(channel.Group), int(channel.Weight)
	priority := channel.Priority
	if provider == domain.SchedulerProviderAxonHub {
		groups, weight, priority = channel.Tags, channel.OrderingWeight, 0
	}
	row := domain.CostFieldOwnership{Provider: provider, ChannelID: channel.ID, ChannelName: channel.Name, RemoteGroups: groups, RemotePriority: priority, RemoteWeight: weight, Managed: true, ExternalTakeover: false, LastReason: "管理员重新接管成本字段", UpdatedAt: time.Now().UTC()}
	if err := s.app.Store.SaveCostFieldOwnership(ctx, row); err != nil {
		return row, err
	}
	return row, nil
}
