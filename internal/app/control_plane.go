package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

var (
	errControlPlaneExternalTakeover = errors.New("渠道已由 GGAPI 后台外部接管，请先在 AUM 重新接管")
	errControlPlaneOwnedByGGAPI     = errors.New("GGAPI 自动关闭状态 3 由 GGAPI 独占恢复")
)

func (s *SchedulerService) SeedControlPlaneBaseline(ctx context.Context) error {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if !schedulerConfigured(cfg) {
		return errSchedulerNotConfigured
	}
	_, err = s.fetchSchedulerChannels(ctx, cfg, "")
	return err
}

func (s *SchedulerService) observeSchedulerChannels(ctx context.Context, channels []domain.SchedulerChannel) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	for _, channel := range channels {
		s.observeSchedulerChannelLocked(ctx, channel)
	}
}

func (s *SchedulerService) observeSchedulerChannelLocked(ctx context.Context, channel domain.SchedulerChannel) {
	now := time.Now().UTC()
	row, found, err := s.app.Store.SchedulerChannelLifecycle(ctx, channel.ID)
	if err != nil {
		return
	}
	if !found {
		row = domain.SchedulerChannelLifecycle{
			ChannelID: channel.ID, ChannelName: channel.Name, RemoteStatus: channel.Status,
			RemotePriority: channel.Priority, RemoteWeight: channel.Weight, Owner: domain.ControlOwnerAUM,
			TrafficSince: now, UpdatedAt: now,
		}
		if channel.Status == 2 {
			row.Owner, row.ExternalTakeover = domain.ControlOwnerExternal, true
			row.LastReason = "首次观察到远端手动关闭"
		} else if channel.Status == 3 {
			row.Owner = domain.ControlOwnerGGAPI
			row.LastReason = "GGAPI 自动关闭"
		}
		_ = s.app.Store.SaveSchedulerChannelLifecycle(ctx, row)
		return
	}

	previousStatus := row.RemoteStatus
	statusChanged := previousStatus != 0 && previousStatus != channel.Status
	row.ChannelName = domain.FirstNonEmpty(channel.Name, row.ChannelName)
	row.RemoteStatus, row.RemotePriority, row.RemoteWeight = channel.Status, channel.Priority, channel.Weight
	row.UpdatedAt = now

	switch {
	case channel.Status == 3:
		row.Owner = domain.ControlOwnerGGAPI
		row.ExternalTakeover = false
		row.AUMDisabled = false
		row.LastReason = "GGAPI 自动关闭，等待 GGAPI 被动恢复"
	case previousStatus == 3 && channel.Status == 1:
		row.Owner = domain.ControlOwnerAUM
		row.ExternalTakeover = false
		row.AUMDisabled = false
		row.TrafficSince = now
		row.LastSource = domain.ControlSourceControlPlane
		row.LastReason = "GGAPI 已被动恢复，仅评估恢复后的新流量"
		s.resetTrafficAfterGGAPIRecovery(ctx, channel, now)
	case statusChanged && (row.LastAUMWriteAt.IsZero() || row.LastAUMStatus != channel.Status):
		row.Owner = domain.ControlOwnerExternal
		row.ExternalTakeover = true
		row.AUMDisabled = false
		row.LastSource = domain.ControlOwnerExternal
		row.LastReason = fmt.Sprintf("GGAPI 后台将远端状态改为 %d", channel.Status)
	case channel.Status == 2 && !row.AUMDisabled && row.LastAUMStatus != 2:
		row.Owner = domain.ControlOwnerExternal
		row.ExternalTakeover = true
		row.LastReason = "远端手动关闭"
	}
	if err := s.app.Store.SaveSchedulerChannelLifecycle(ctx, row); err != nil {
		return
	}
	if statusChanged && (row.ExternalTakeover || channel.Status == 3 || previousStatus == 3) {
		s.recordControlPlaneTransition(ctx, row)
	}
}

func (s *SchedulerService) resetTrafficAfterGGAPIRecovery(ctx context.Context, channel domain.SchedulerChannel, now time.Time) {
	control, found, err := s.app.Store.TrafficControl(ctx, channel.ID)
	if err != nil || !found {
		return
	}
	control.State, control.Reason = "healthy", "GGAPI 恢复后重新建立流量窗口"
	control.FailureWindows, control.RecoveryStage, control.RecoverySuccesses = 0, 0, 0
	control.CooldownUntil, control.RetryAt, control.StageChangedAt = time.Time{}, time.Time{}, time.Time{}
	control.RetryCount = 0
	control.DesiredStatus, control.ActualStatus = 1, 1
	control.ActualPriority, control.ActualWeight = channel.Priority, channel.Weight
	control.UpdatedAt = now
	_ = s.app.Store.SaveTrafficControl(ctx, control)
}

func (s *SchedulerService) recordControlPlaneTransition(ctx context.Context, row domain.SchedulerChannelLifecycle) {
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
		ChannelID: row.ChannelID, ChannelName: row.ChannelName, Action: "control_plane", Status: "skipped",
		Reason: row.Owner, Message: row.LastReason,
	})
	_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{
		Type: "scheduler_control_owner_changed", Severity: "warning", Title: "渠道控制权变更",
		Message:    domain.FirstNonEmpty(row.ChannelName, row.ChannelID) + " " + row.LastReason,
		TargetType: "scheduler_channel", TargetID: row.ChannelID, Actions: []string{"scheduler_control_plane"},
	})
}

func (s *SchedulerService) ReconcileControlPlane(ctx context.Context) error {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if !schedulerConfigured(cfg) {
		return errSchedulerNotConfigured
	}
	if _, err := s.fetchSchedulerChannels(ctx, cfg, ""); err != nil {
		return err
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	return s.retryAffinityCleanupLocked(ctx, cfg, time.Now().UTC())
}

func (s *SchedulerService) retryAffinityCleanupLocked(ctx context.Context, cfg domain.SchedulerConfig, now time.Time) error {
	rows, err := s.app.Store.SchedulerChannelLifecycles(ctx)
	if err != nil {
		return err
	}
	due := make([]domain.SchedulerChannelLifecycle, 0)
	for _, row := range rows {
		if row.AffinityCleanupPending && (row.AffinityCleanupRetryAt.IsZero() || !now.Before(row.AffinityCleanupRetryAt)) {
			due = append(due, row)
		}
	}
	if len(due) == 0 {
		return nil
	}
	cleanupErr := s.clearSchedulerChannelAffinityCache(ctx, cfg)
	for _, row := range due {
		s.finishAffinityCleanup(ctx, &row, cleanupErr, now)
	}
	return cleanupErr
}

func (s *SchedulerService) finishAffinityCleanup(ctx context.Context, row *domain.SchedulerChannelLifecycle, cleanupErr error, now time.Time) {
	if cleanupErr == nil {
		row.AffinityCleanupPending = false
		row.AffinityCleanupRetryAt = time.Time{}
		row.AffinityCleanupRetries = 0
		row.AffinityCleanupError = ""
		row.UpdatedAt = now
		_ = s.app.Store.SaveSchedulerChannelLifecycle(ctx, *row)
		return
	}
	row.AffinityCleanupPending = true
	row.AffinityCleanupRetries++
	row.AffinityCleanupRetryAt = controlPlaneRetryAt(now, row.AffinityCleanupRetries)
	row.AffinityCleanupError = affinityCleanupSafeError(cleanupErr)
	row.UpdatedAt = now
	_ = s.app.Store.SaveSchedulerChannelLifecycle(ctx, *row)
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
		ChannelID: row.ChannelID, ChannelName: row.ChannelName, Action: "affinity_cleanup", Status: "error",
		Reason: "cleanup_retry", Message: row.AffinityCleanupError,
	})
}

func controlPlaneRetryAt(now time.Time, count int) time.Time {
	delays := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute}
	if count < 1 {
		count = 1
	}
	if count > len(delays) {
		count = len(delays)
	}
	return now.Add(delays[count-1])
}

func affinityCleanupSafeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "亲和性缓存清理超时"
	}
	return "亲和性缓存清理失败，等待自动重试"
}

func (s *SchedulerService) coordinateSchedulerStatus(ctx context.Context, channelID string, status int, source, reason string, confirmedRestore bool) error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if !schedulerConfigured(cfg) {
		return errSchedulerNotConfigured
	}
	now := time.Now().UTC()
	row, found, err := s.app.Store.SchedulerChannelLifecycle(ctx, channelID)
	if err != nil {
		return err
	}
	if !found {
		row = domain.SchedulerChannelLifecycle{ChannelID: channelID, Owner: domain.ControlOwnerAUM, TrafficSince: now}
		if confirmedRestore {
			row.RemoteStatus, row.AUMDisabled, row.LastAUMStatus = 2, true, 2
		}
	}
	if row.Owner == domain.ControlOwnerGGAPI || row.RemoteStatus == 3 {
		return BadRequest(errControlPlaneOwnedByGGAPI)
	}
	if row.ExternalTakeover {
		return BadRequest(errControlPlaneExternalTakeover)
	}
	if status == 1 && row.RemoteStatus == 2 && !row.AUMDisabled && !confirmedRestore {
		row.Owner, row.ExternalTakeover = domain.ControlOwnerExternal, true
		row.LastReason, row.UpdatedAt = "远端状态 2 未经 AUM 写入确认", now
		_ = s.app.Store.SaveSchedulerChannelLifecycle(ctx, row)
		return BadRequest(errControlPlaneExternalTakeover)
	}
	needsAffinityCleanup := status == 2 && (row.RemoteStatus != 2 || !row.AUMDisabled || row.AffinityCleanupPending)
	if row.RemoteStatus != status {
		if err := s.writeSchedulerChannelStatus(ctx, cfg, channelID, status); err != nil {
			return err
		}
	}
	row.Owner = domain.ControlOwnerAUM
	row.ExternalTakeover = false
	row.RemoteStatus = status
	row.LastAUMStatus = status
	row.LastAUMWriteAt = now
	row.LastSource = source
	row.LastReason = reason
	row.AUMDisabled = status == 2
	row.UpdatedAt = now
	if status == 2 && needsAffinityCleanup {
		row.AffinityCleanupPending = true
	}
	if err := s.app.Store.SaveSchedulerChannelLifecycle(ctx, row); err != nil {
		return err
	}
	if status == 2 && row.AffinityCleanupPending && (row.AffinityCleanupRetryAt.IsZero() || !now.Before(row.AffinityCleanupRetryAt)) {
		cleanupErr := s.clearSchedulerChannelAffinityCache(ctx, cfg)
		s.finishAffinityCleanup(ctx, &row, cleanupErr, now)
		// 关渠已被远端确认，缓存清理失败由生命周期状态独立重试。
	}
	return nil
}

func (s *SchedulerService) writeSchedulerChannelStatus(ctx context.Context, cfg domain.SchedulerConfig, channelID string, status int) error {
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodPost, "/api/channel/"+url.PathEscape(channelID)+"/status", map[string]int{"status": status}, &raw); err != nil {
		return err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return errors.New(schedulerMessage(raw))
	}
	return nil
}

func (s *SchedulerService) coordinateSchedulerFields(ctx context.Context, cfg domain.SchedulerConfig, current domain.SchedulerChannel, group string, priority int64, weight uint, writeWeight bool, source, reason string) error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	now := time.Now().UTC()
	row, found, err := s.app.Store.SchedulerChannelLifecycle(ctx, current.ID)
	if err != nil {
		return err
	}
	if !found {
		row = domain.SchedulerChannelLifecycle{
			ChannelID: current.ID, ChannelName: current.Name, RemoteStatus: current.Status, RemotePriority: current.Priority,
			RemoteWeight: current.Weight, Owner: domain.ControlOwnerAUM, TrafficSince: now,
		}
	}
	if row.Owner == domain.ControlOwnerGGAPI || current.Status == 3 {
		return errControlPlaneOwnedByGGAPI
	}
	if row.ExternalTakeover {
		return errControlPlaneExternalTakeover
	}
	id, err := strconv.Atoi(strings.TrimSpace(current.ID))
	if err != nil || id <= 0 {
		return ErrBadRequest("invalid scheduler channel id")
	}
	body := map[string]any{"id": id, "group": group, "priority": priority}
	if writeWeight {
		body["weight"] = weight
	}
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodPut, "/api/channel/", body, &raw); err != nil {
		return err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return errors.New(schedulerMessage(raw))
	}
	row.ChannelName, row.RemotePriority = domain.FirstNonEmpty(current.Name, row.ChannelName), priority
	if writeWeight {
		row.RemoteWeight = weight
	}
	row.LastSource, row.LastReason, row.UpdatedAt = source, reason, now
	return s.app.Store.SaveSchedulerChannelLifecycle(ctx, row)
}

func (s *SchedulerService) AdoptControlPlaneChannel(ctx context.Context, channelID string) (domain.SchedulerChannelLifecycle, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.SchedulerChannelLifecycle{}, err
	}
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return domain.SchedulerChannelLifecycle{}, err
	}
	var channel domain.SchedulerChannel
	found := false
	for _, candidate := range channels {
		if candidate.ID == channelID {
			channel, found = candidate, true
			break
		}
	}
	if !found {
		return domain.SchedulerChannelLifecycle{}, ErrBadRequest("GGAPI 中找不到该渠道")
	}
	if channel.Status == 3 {
		return domain.SchedulerChannelLifecycle{}, BadRequest(errControlPlaneOwnedByGGAPI)
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	now := time.Now().UTC()
	row, _, err := s.app.Store.SchedulerChannelLifecycle(ctx, channelID)
	if err != nil {
		return row, err
	}
	row.ChannelID, row.ChannelName = channel.ID, channel.Name
	row.RemoteStatus, row.RemotePriority, row.RemoteWeight = channel.Status, channel.Priority, channel.Weight
	row.Owner, row.ExternalTakeover = domain.ControlOwnerAUM, false
	row.AUMDisabled = channel.Status == 2
	row.LastAUMStatus = channel.Status
	row.LastAUMWriteAt = now
	row.LastSource, row.LastReason = domain.ControlSourceManual, "在 AUM 重新接管"
	row.TrafficSince, row.UpdatedAt = now, now
	if err := s.app.Store.SaveSchedulerChannelLifecycle(ctx, row); err != nil {
		return row, err
	}
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{ChannelID: row.ChannelID, ChannelName: row.ChannelName, Action: "control_plane", Status: "success", Reason: "adopt", Message: row.LastReason})
	return row, nil
}

func (s *SchedulerService) ControlPlane(ctx context.Context) (domain.SchedulerControlPlane, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.SchedulerControlPlane{}, err
	}
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return domain.SchedulerControlPlane{}, err
	}
	traffic, err := s.TrafficStatus(ctx)
	if err != nil {
		return domain.SchedulerControlPlane{}, err
	}
	availability, err := s.AvailabilityRows(ctx, "", "")
	if err != nil {
		return domain.SchedulerControlPlane{}, err
	}
	events, err := s.app.Store.TrafficEventsSince(ctx, time.Now().UTC().Add(-trafficStartupReplay))
	if err != nil {
		return domain.SchedulerControlPlane{}, err
	}
	logs, err := s.app.Store.SchedulerLogs(ctx, 50)
	if err != nil {
		return domain.SchedulerControlPlane{}, err
	}
	availabilityByID := map[string]domain.AvailabilityView{}
	for _, row := range availability {
		availabilityByID[row.ChannelID] = row
	}
	trafficByID := map[string]domain.TrafficChannelState{}
	for _, row := range traffic.Channels {
		trafficByID[row.ChannelID] = row
	}
	out := domain.SchedulerControlPlane{Traffic: traffic, Channels: []domain.SchedulerControlPlaneChannel{}, Logs: logs}
	for _, channel := range channels {
		life, _, err := s.app.Store.SchedulerChannelLifecycle(ctx, channel.ID)
		if err != nil {
			return out, err
		}
		row := domain.SchedulerControlPlaneChannel{
			ChannelID: channel.ID, ChannelName: channel.Name, RemoteStatus: channel.Status, RemotePriority: channel.Priority, RemoteWeight: channel.Weight,
			Owner: life.Owner, ExternalTakeover: life.ExternalTakeover, AUMDisabled: life.AUMDisabled, CloseSource: life.LastSource,
			CloseReason: life.LastReason, TrafficSince: life.TrafficSince, AffinityCleanupPending: life.AffinityCleanupPending,
			AffinityCleanupRetryAt: life.AffinityCleanupRetryAt, AffinityCleanupError: life.AffinityCleanupError, UpdatedAt: life.UpdatedAt,
		}
		if value, ok := availabilityByID[channel.ID]; ok {
			copy := value
			row.Availability = &copy
			row.Managed = row.Managed || value.Managed
		}
		if value, ok := trafficByID[channel.ID]; ok {
			copy := value
			row.Traffic = &copy
			row.Managed = row.Managed || value.Managed
		}
		if !row.Managed && channel.Status != 3 && life.LastAUMWriteAt.IsZero() && !life.AUMDisabled {
			row.Owner = domain.ControlOwnerObserved
			row.ExternalTakeover = false
			row.CloseSource = ""
			row.CloseReason = "未纳入 AUM 管理，仅观察远端状态"
		}
		for _, event := range events {
			if event.ChannelID != channel.ID || (!life.TrafficSince.IsZero() && event.OccurredAt.Before(life.TrafficSince)) {
				continue
			}
			if event.SessionScoped {
				row.SessionFailures++
			} else {
				row.NewTrafficRequests++
			}
		}
		out.Channels = append(out.Channels, row)
	}
	sort.Slice(out.Channels, func(i, j int) bool {
		if out.Channels[i].ExternalTakeover != out.Channels[j].ExternalTakeover {
			return out.Channels[i].ExternalTakeover
		}
		return out.Channels[i].ChannelID < out.Channels[j].ChannelID
	})
	return out, nil
}
