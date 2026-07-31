package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/qqbot"
	"qq-status-bot/internal/statusapi"
)

const alertPollInterval = 3 * time.Minute

type StatusFetcher interface {
	Fetch(context.Context, string, string, string) (statusapi.StatusPage, error)
}

type ActiveMessageSender interface {
	SendGroupText(context.Context, string, string) error
}

type alertSample struct {
	key       string
	groupName string
	nodeName  string
	status    int
	heartbeat string
	incident  string
	recovery  string
}

type alertDelivery struct {
	group string
	kind  string
	nodes []alertSample
}

func (s *Service) PollAlerts(ctx context.Context) {
	s.pollAlerts(ctx)
}

func (s *Service) startAlerts(ctx context.Context) {
	if s.alertFetcher == nil {
		return
	}
	go func() {
		s.pollAlerts(ctx)
		ticker := time.NewTicker(s.alertInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pollAlerts(ctx)
			}
		}
	}()
}

func (s *Service) pollAlerts(ctx context.Context) {
	if s.alertFetcher == nil {
		return
	}
	settings := s.settings.Settings()
	if settings.AlertFailureSamples <= 0 {
		settings.AlertFailureSamples = 2
	}
	if settings.AlertRecoverySamples <= 0 {
		settings.AlertRecoverySamples = 2
	}
	state := s.getAlertState()
	if !settings.AlertsEnabled {
		if state.Enabled || state.PollFailed {
			state.Enabled = false
			state.PollFailed = false
			_ = s.saveAlertState(state)
		}
		return
	}
	page, err := s.alertFetcher.Fetch(ctx, settings.StatusURL, settings.StatusPageID, settings.StatusPeriod)
	if err != nil {
		if !state.PollFailed {
			s.appendAlertLog("receive", "ALERT_POLL", "failed", "上游状态数据读取失败："+trimLog(err.Error()), "")
		}
		state.PollFailed = true
		if saveErr := s.saveAlertState(state); saveErr != nil {
			log.Printf("保存告警轮询状态失败: %v", saveErr)
		}
		return
	}
	if state.PollFailed {
		s.appendAlertLog("receive", "ALERT_POLL", "recovered", "状态数据源已恢复", "")
	}
	state.PollFailed = false
	samplesByNode := heartbeatSamples(page)
	samples := latestSamples(page)
	sourceKey := strings.Join([]string{strings.TrimRight(settings.StatusURL, "/"), strings.TrimSpace(settings.StatusPageID), strings.TrimSpace(settings.StatusPeriod)}, "|")
	baseline := !state.Enabled || state.SourceKey != sourceKey || state.Nodes == nil
	if baseline {
		state = domain.AlertState{Enabled: true, SourceKey: sourceKey, Nodes: make(map[string]domain.AlertNodeState)}
	}
	if state.Nodes == nil {
		state.Nodes = make(map[string]domain.AlertNodeState)
	}
	syncAlertGroups(&state, settings.AlertGroups)
	for key, nodeSamples := range samplesByNode {
		node := state.Nodes[key]
		if baseline && len(nodeSamples) > 1 {
			nodeSamples = nodeSamples[len(nodeSamples)-1:]
		}
		for _, sample := range nodeSamples {
			if isDuplicateHeartbeat(node.LastHeartbeat, sample.heartbeat) {
				continue
			}
			applySample(&node, sample, settings, baseline)
			node.LastHeartbeat = sample.heartbeat
			node.LastStatus = sample.status
		}
		state.Nodes[key] = node
	}
	state.Enabled = true
	state.SourceKey = sourceKey
	if err := s.saveAlertState(state); err != nil {
		log.Printf("保存告警状态失败: %v", err)
		return
	}
	if baseline {
		s.appendAlertLog("receive", "ALERT_POLL", "ok", "已建立告警基线", "")
		return
	}
	deliveries := collectAlertDeliveries(state, samples)
	s.sendAlertDeliveries(ctx, &state, deliveries)
	if err := s.saveAlertState(state); err != nil {
		log.Printf("保存告警发送状态失败: %v", err)
	}
	s.appendAlertLog("receive", "ALERT_POLL", "ok", fmt.Sprintf("已处理 %d 个监控心跳", len(samples)), "")
}

func syncAlertGroups(state *domain.AlertState, groups []string) {
	allowed := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			allowed[group] = struct{}{}
		}
	}
	for key, node := range state.Nodes {
		if node.ConfirmedOffline {
			if node.OfflineAttempts == nil {
				node.OfflineAttempts = make(map[string]int)
			}
			for group := range allowed {
				if !contains(node.NotifiedGroups, group) {
					if _, exists := node.OfflineAttempts[group]; !exists {
						node.OfflineAttempts[group] = 0
					}
				}
			}
			for group := range node.OfflineAttempts {
				if _, exists := allowed[group]; !exists {
					delete(node.OfflineAttempts, group)
				}
			}
		}
		state.Nodes[key] = node
	}
}

func latestSamples(page statusapi.StatusPage) []alertSample {
	byNode := heartbeatSamples(page)
	var samples []alertSample
	for _, nodeSamples := range byNode {
		if len(nodeSamples) > 0 {
			samples = append(samples, nodeSamples[len(nodeSamples)-1])
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].key < samples[j].key })
	return samples
}

func heartbeatSamples(page statusapi.StatusPage) map[string][]alertSample {
	byNode := make(map[string][]alertSample)
	for _, group := range page.Groups {
		for _, monitor := range group.Monitors {
			heartbeats := page.Heartbeats[monitor.ID]
			for _, heartbeat := range heartbeats {
				if strings.TrimSpace(heartbeat.Time) == "" {
					continue
				}
				byNode[fmt.Sprint(monitor.ID)] = append(byNode[fmt.Sprint(monitor.ID)], alertSample{
					key: fmt.Sprint(monitor.ID), groupName: group.Name, nodeName: monitor.Name,
					status: heartbeat.Status, heartbeat: strings.TrimSpace(heartbeat.Time),
				})
			}
		}
	}
	for key, samples := range byNode {
		sort.SliceStable(samples, func(i, j int) bool {
			left, leftOK := parseHeartbeatTime(samples[i].heartbeat)
			right, rightOK := parseHeartbeatTime(samples[j].heartbeat)
			if leftOK && rightOK {
				return left.Before(right)
			}
			return false
		})
		byNode[key] = samples
	}
	return byNode
}

func isDuplicateHeartbeat(previous, current string) bool {
	previous, current = strings.TrimSpace(previous), strings.TrimSpace(current)
	if previous == "" || current == "" || previous == current {
		return previous != ""
	}
	oldTime, oldOK := parseHeartbeatTime(previous)
	newTime, newOK := parseHeartbeatTime(current)
	return oldOK && newOK && !newTime.After(oldTime)
}

func applySample(node *domain.AlertNodeState, sample alertSample, settings domain.Settings, baseline bool) {
	if sample.status == 0 {
		node.OnlineSamples = 0
		if node.ConfirmedOffline {
			return
		}
		if node.OfflineSamples == 0 {
			node.IncidentStarted = sample.heartbeat
		}
		node.OfflineSamples++
		if baseline {
			return
		}
		if node.OfflineSamples >= settings.AlertFailureSamples {
			node.ConfirmedOffline = true
			node.IncidentStarted = chooseIncidentStart(node.IncidentStarted, sample.heartbeat)
			node.OfflineAttempts = make(map[string]int, len(settings.AlertGroups))
			for _, group := range settings.AlertGroups {
				node.OfflineAttempts[group] = 0
			}
		}
		return
	}
	if sample.status == 1 {
		node.OfflineSamples = 0
		if !node.ConfirmedOffline {
			node.IncidentStarted = ""
			node.OnlineSamples++
			return
		}
		node.OnlineSamples++
		if baseline || node.OnlineSamples < settings.AlertRecoverySamples {
			return
		}
		node.RecoveryTime = sample.heartbeat
		node.RecoveryAttempts = make(map[string]int, len(node.NotifiedGroups))
		for _, group := range node.NotifiedGroups {
			node.RecoveryAttempts[group] = 0
		}
		node.ConfirmedOffline = false
		node.OfflineSamples = 0
		if len(node.RecoveryAttempts) == 0 {
			clearIncident(node)
		}
		return
	}
	// 重试、维护和未知状态会打断连续计数，但不会把已确认故障判定为恢复。
	node.OfflineSamples = 0
	node.OnlineSamples = 0
	if !node.ConfirmedOffline {
		node.IncidentStarted = ""
	}
}

func chooseIncidentStart(previous, heartbeat string) string {
	if strings.TrimSpace(previous) != "" {
		return previous
	}
	if strings.TrimSpace(heartbeat) != "" {
		return heartbeat
	}
	return shanghaiNow().Format(time.RFC3339)
}

func collectAlertDeliveries(state domain.AlertState, samples []alertSample) []alertDelivery {
	byKey := make(map[string]alertSample, len(samples))
	for _, sample := range samples {
		byKey[sample.key] = sample
	}
	groupKinds := make(map[string]map[string][]alertSample)
	for key, node := range state.Nodes {
		sample, ok := byKey[key]
		if !ok {
			continue
		}
		if node.ConfirmedOffline {
			for group, attempt := range node.OfflineAttempts {
				if attempt < 3 && !contains(node.NotifiedGroups, group) {
					sample.incident = node.IncidentStarted
					addDeliveryNode(groupKinds, group, "offline", sample)
				}
			}
		}
		for group, attempt := range node.RecoveryAttempts {
			if attempt < 3 {
				sample.incident = node.IncidentStarted
				sample.recovery = node.RecoveryTime
				addDeliveryNode(groupKinds, group, "recovery", sample)
			}
		}
	}
	var out []alertDelivery
	for group, kinds := range groupKinds {
		for kind, nodes := range kinds {
			sort.Slice(nodes, func(i, j int) bool { return nodes[i].key < nodes[j].key })
			out = append(out, alertDelivery{group: group, kind: kind, nodes: nodes})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].group == out[j].group {
			return out[i].kind < out[j].kind
		}
		return out[i].group < out[j].group
	})
	return out
}

func addDeliveryNode(groups map[string]map[string][]alertSample, group, kind string, sample alertSample) {
	if groups[group] == nil {
		groups[group] = make(map[string][]alertSample)
	}
	for _, existing := range groups[group][kind] {
		if existing.key == sample.key {
			return
		}
	}
	groups[group][kind] = append(groups[group][kind], sample)
}

func (s *Service) sendAlertDeliveries(ctx context.Context, state *domain.AlertState, deliveries []alertDelivery) {
	sender, ok := s.replier.(ActiveMessageSender)
	if !ok {
		for _, delivery := range deliveries {
			s.appendAlertLog("send", alertEventType(delivery.kind), "failed", "QQ 客户端不支持主动消息", delivery.group)
		}
		return
	}
	for _, delivery := range deliveries {
		content := formatAlertMessage(delivery.kind, delivery.nodes)
		err := sender.SendGroupText(ctx, delivery.group, content)
		if err == nil {
			for _, sample := range delivery.nodes {
				node := state.Nodes[sample.key]
				if delivery.kind == "offline" {
					node.NotifiedGroups = appendUnique(node.NotifiedGroups, delivery.group)
					delete(node.OfflineAttempts, delivery.group)
				} else {
					delete(node.RecoveryAttempts, delivery.group)
					if len(node.RecoveryAttempts) == 0 {
						clearIncident(&node)
					}
				}
				state.Nodes[sample.key] = node
			}
			s.appendAlertLog("send", alertEventType(delivery.kind), "sent", content, delivery.group)
			if saveErr := s.saveAlertState(*state); saveErr != nil {
				log.Printf("保存告警发送状态失败: %v", saveErr)
			}
			continue
		}
		permission := isPermissionError(err)
		for _, sample := range delivery.nodes {
			node := state.Nodes[sample.key]
			if delivery.kind == "offline" {
				if node.OfflineAttempts == nil {
					node.OfflineAttempts = make(map[string]int)
				}
				node.OfflineAttempts[delivery.group]++
				if permission {
					node.OfflineAttempts[delivery.group] = 3
				}
			} else {
				if node.RecoveryAttempts == nil {
					node.RecoveryAttempts = make(map[string]int)
				}
				node.RecoveryAttempts[delivery.group]++
				if permission {
					node.RecoveryAttempts[delivery.group] = 3
				}
				if recoveryAttemptsExhausted(node.RecoveryAttempts) {
					clearIncident(&node)
				}
			}
			state.Nodes[sample.key] = node
		}
		status := "retry"
		if permission {
			status = "permission_denied"
		}
		s.appendAlertLog("send", alertEventType(delivery.kind), status, trimLog(err.Error()), delivery.group)
		if saveErr := s.saveAlertState(*state); saveErr != nil {
			log.Printf("保存告警重试状态失败: %v", saveErr)
		}
	}
}

func recoveryAttemptsExhausted(attempts map[string]int) bool {
	if len(attempts) == 0 {
		return true
	}
	for _, attempt := range attempts {
		if attempt < 3 {
			return false
		}
	}
	return true
}

func alertEventType(kind string) string {
	if kind == "recovery" {
		return "ALERT_RECOVERY"
	}
	return "ALERT_OFFLINE"
}

func formatAlertMessage(kind string, nodes []alertSample) string {
	now := shanghaiNow().Format("2006-01-02 15:04:05 -0700")
	var b strings.Builder
	if kind == "recovery" {
		b.WriteString("[恢复通知] ")
	} else {
		b.WriteString("[故障通知] ")
	}
	b.WriteString(now)
	for _, node := range nodes {
		b.WriteString("\n分组：")
		b.WriteString(node.groupName)
		b.WriteString("\n节点：")
		b.WriteString(node.nodeName)
		if kind == "offline" {
			b.WriteString("\n首次离线：")
			b.WriteString(formatDisplayTime(node.incident))
		} else {
			b.WriteString("\n恢复时间：")
			b.WriteString(formatDisplayTime(node.recovery))
			if started, ok := parseHeartbeatTime(node.incident); ok {
				if recovered, recoveredOK := parseHeartbeatTime(node.recovery); recoveredOK {
					b.WriteString(fmt.Sprintf("\n故障持续：%s", formatDuration(recovered.Sub(started))))
				} else {
					b.WriteString("\n故障持续：未知")
				}
			} else {
				b.WriteString("\n故障持续：未知")
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func formatDisplayTime(raw string) string {
	parsed, ok := parseHeartbeatTime(raw)
	if !ok {
		return raw
	}
	return parsed.In(shanghaiLocation).Format("2006-01-02 15:04:05 -0700")
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	days := duration / (24 * time.Hour)
	duration %= 24 * time.Hour
	hours := duration / time.Hour
	duration %= time.Hour
	minutes := duration / time.Minute
	if days > 0 {
		return fmt.Sprintf("%d天%d小时%d分钟", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	}
	return fmt.Sprintf("%d分钟", minutes)
}

func parseHeartbeatTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func isPermissionError(err error) bool {
	var apiErr *qqbot.APIError
	return errors.As(err, &apiErr) && apiErr.Code == 40034102
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func appendUnique(items []string, target string) []string {
	if contains(items, target) {
		return items
	}
	return append(items, target)
}

func clearIncident(node *domain.AlertNodeState) {
	node.IncidentStarted = ""
	node.ConfirmedOffline = false
	node.OfflineSamples = 0
	node.OnlineSamples = 0
	node.OfflineAttempts = nil
	node.NotifiedGroups = nil
	node.RecoveryTime = ""
	node.RecoveryAttempts = nil
}

func (s *Service) appendAlertLog(direction, eventType, status, message, group string) {
	if err := s.settings.AppendLog(domain.EventLog{Direction: direction, EventType: eventType, GroupOpenID: group, Status: status, Message: trimLog(message)}); err != nil {
		log.Printf("写入告警日志失败: %v", err)
	}
}

func (s *Service) getAlertState() domain.AlertState {
	if store, ok := s.settings.(AlertStateStore); ok {
		return store.AlertState()
	}
	s.alertStateMu.Lock()
	defer s.alertStateMu.Unlock()
	return cloneAlertState(s.alertState)
}

func (s *Service) saveAlertState(next domain.AlertState) error {
	if store, ok := s.settings.(AlertStateStore); ok {
		return store.UpdateAlertState(next)
	}
	s.alertStateMu.Lock()
	s.alertState = cloneAlertState(next)
	s.alertStateMu.Unlock()
	return nil
}

func cloneAlertState(in domain.AlertState) domain.AlertState {
	out := in
	out.Nodes = make(map[string]domain.AlertNodeState, len(in.Nodes))
	for key, node := range in.Nodes {
		copyNode := node
		copyNode.NotifiedGroups = append([]string{}, node.NotifiedGroups...)
		copyNode.OfflineAttempts = cloneAttempts(node.OfflineAttempts)
		copyNode.RecoveryAttempts = cloneAttempts(node.RecoveryAttempts)
		out.Nodes[key] = copyNode
	}
	return out
}

func cloneAttempts(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
