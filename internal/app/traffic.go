package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/domain"
)

const (
	trafficOverlap       = 30 * time.Second
	trafficStartupReplay = 5 * time.Minute
	trafficPageSize      = 100
	trafficMaxPages      = 200
)

type trafficFetchResult struct {
	cursor domain.TrafficCursor
	events []domain.TrafficEvent
	err    error
}

func (s *SchedulerService) ReconcileTraffic(ctx context.Context) error {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if cfg.TrafficMode == domain.TrafficModeOff {
		return nil
	}
	if !schedulerConfigured(cfg) {
		return errSchedulerNotConfigured
	}
	now := time.Now().UTC()
	affinitySkipRules := map[string]bool{}
	if settings, settingsErr := s.GGAPISettings(ctx); settingsErr == nil {
		affinitySkipRules = ggapiAffinitySkipRules(settings)
	}
	sources := []struct {
		name    string
		logType int
	}{{"consumption", 2}, {"error", 5}}
	results := make([]trafficFetchResult, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		cursor, _, cursorErr := s.app.Store.TrafficCursor(ctx, source.name)
		if cursorErr != nil {
			return cursorErr
		}
		wg.Add(1)
		go func(i int, source string, logType int, cursor domain.TrafficCursor) {
			defer wg.Done()
			results[i] = s.fetchTrafficSource(ctx, cfg, source, logType, cursor, now, affinitySkipRules)
		}(i, source.name, source.logType, cursor)
	}
	wg.Wait()
	var pollErr error
	for _, result := range results {
		for _, event := range result.events {
			if _, err := s.app.Store.SaveTrafficEvent(ctx, event); err != nil {
				return err
			}
		}
		if err := s.app.Store.SaveTrafficCursor(ctx, result.cursor); err != nil {
			return err
		}
		if result.err != nil {
			pollErr = errors.Join(pollErr, result.err)
		}
	}
	if pollErr != nil {
		return pollErr
	}
	events, err := s.app.Store.TrafficEventsSince(ctx, now.Add(-trafficStartupReplay))
	if err != nil {
		return err
	}
	if err := s.saveTrafficAggregates(ctx, events, now); err != nil {
		return err
	}
	status, err := s.trafficStatus(ctx, cfg, events, now)
	if err != nil {
		return err
	}
	if cfg.TrafficMode == domain.TrafficModeActive && !status.Frozen {
		return s.applyTrafficControl(ctx, cfg, status.Channels, now)
	}
	return nil
}

func (s *SchedulerService) fetchTrafficSource(ctx context.Context, cfg domain.SchedulerConfig, source string, logType int, cursor domain.TrafficCursor, now time.Time, affinitySkipRules map[string]bool) trafficFetchResult {
	result := trafficFetchResult{cursor: cursor}
	result.cursor.Source = source
	start, end, firstPage := cursor.ScanStartAt, cursor.ScanEndAt, cursor.NextPage
	if firstPage <= 0 || start.IsZero() || end.IsZero() {
		start, end, firstPage = cursor.CursorAt.Add(-trafficOverlap), now, 1
		if cursor.CursorAt.IsZero() {
			start = now.Add(-trafficStartupReplay)
		}
	}
	latest := cursor.LastEventAt
	lastPage, pageFull := 0, false
	for page := firstPage; page < firstPage+trafficMaxPages; page++ {
		values := url.Values{}
		values.Set("type", strconv.Itoa(logType))
		values.Set("start_timestamp", strconv.FormatInt(start.Unix(), 10))
		values.Set("end_timestamp", strconv.FormatInt(end.Unix(), 10))
		values.Set("p", strconv.Itoa(page))
		values.Set("page_size", strconv.Itoa(trafficPageSize))
		var raw map[string]any
		if err := s.schedulerJSON(ctx, cfg, http.MethodGet, "/api/log/?"+values.Encode(), nil, &raw); err != nil {
			result.err = err
			result.cursor.LastError = trafficSafeError(err)
			break
		}
		if ok, exists := raw["success"].(bool); exists && !ok {
			result.err = errors.New(schedulerMessage(raw))
			result.cursor.LastError = trafficSafeError(result.err)
			break
		}
		items := profitLogItems(raw)
		lastPage, pageFull = page, len(items) == trafficPageSize
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			event, ok := parseTrafficEvent(source, logType, m, affinitySkipRules)
			if !ok || event.OccurredAt.Before(start) || event.OccurredAt.After(end.Add(time.Minute)) {
				continue
			}
			result.events = append(result.events, event)
			if event.OccurredAt.After(latest) {
				latest = event.OccurredAt
			}
		}
		if len(items) < trafficPageSize {
			break
		}
	}
	result.cursor.LastPollAt = now
	result.cursor.UpdatedAt = now
	result.cursor.BacklogPages = 0
	if lastPage == firstPage+trafficMaxPages-1 && pageFull {
		result.cursor.BacklogPages = 1
		result.cursor.ScanStartAt, result.cursor.ScanEndAt = start, end
		result.cursor.NextPage = lastPage + 1
	}
	if result.err == nil {
		if result.cursor.BacklogPages == 0 {
			result.cursor.CursorAt = end
			result.cursor.ScanStartAt, result.cursor.ScanEndAt, result.cursor.NextPage = time.Time{}, time.Time{}, 0
		}
		result.cursor.LastEventAt = latest
		result.cursor.LastError = ""
	}
	return result
}

func parseTrafficEvent(source string, logType int, raw map[string]any, skipRuleSets ...map[string]bool) (domain.TrafficEvent, bool) {
	other := profitMap(firstScheduler(raw, "other", "metadata", "meta", "details"))
	adminInfo := profitMap(firstScheduler(other, "admin_info"))
	affinity := profitMap(firstScheduler(adminInfo, "channel_affinity"))
	lookup := func(keys ...string) any {
		if value := firstScheduler(raw, keys...); value != nil {
			return value
		}
		return firstScheduler(other, keys...)
	}
	channelID, channelName := trafficChannel(lookup("channel", "channel_id", "channelId"), lookup("channel_name", "channelName"))
	occurred := profitTime(lookup("created_at", "createdAt", "created_time", "createdTime", "timestamp", "time", "created"))
	if channelID == "" || occurred.IsZero() {
		return domain.TrafficEvent{}, false
	}
	status := schedulerInt(lookup("upstream_status_code", "upstreamStatusCode", "status_code", "statusCode", "http_status", "httpStatus"))
	errorType := trafficErrorField(schedulerString(lookup("error_type", "errorType")))
	if errorType == "" {
		// GGAPI 顶层 type 是日志类型（2=消费、5=错误），不能当成上游错误类型。
		errorType = trafficErrorField(schedulerString(firstScheduler(other, "type")))
	}
	errorCode := trafficErrorField(schedulerString(lookup("error_code", "errorCode", "code")))
	errorText := schedulerString(lookup("error_message", "errorMessage", "message", "error"))
	kind := domain.TrafficEventSuccess
	if logType == 5 || status >= 400 || errorType != "" || errorCode != "" {
		kind = domain.ClassifyTrafficError(status, errorType, errorCode, errorText)
	}
	duration := trafficMilliseconds(lookup("duration_ms", "durationMs", "latency_ms", "latencyMs"), false)
	if duration == 0 {
		duration = trafficMilliseconds(lookup("use_time", "useTime"), true)
	}
	// new-api 把首响应耗时以毫秒写入日志 other.frt；同时兼容其他调度器的显式 TTFT 字段。
	ttft := trafficMilliseconds(lookup("ttft_ms", "ttftMs", "first_token_ms", "firstTokenMs", "time_to_first_token", "timeToFirstToken", "frt"), false)
	streamEnded := trafficBool(lookup("stream_ended", "streamEnded", "stream_completed", "streamCompleted"), kind == domain.TrafficEventSuccess)
	if !streamEnded && kind == domain.TrafficEventSuccess {
		kind = domain.TrafficEventSoftFailure
	}
	tokens := int64(schedulerInt(lookup("tokens", "total_tokens", "totalTokens")))
	if tokens == 0 {
		tokens = int64(schedulerInt(lookup("prompt_tokens", "promptTokens", "input_tokens", "inputTokens")) + schedulerInt(lookup("completion_tokens", "completionTokens", "output_tokens", "outputTokens")))
	}
	retrySucceeded := trafficBool(lookup("retry_succeeded", "retrySucceeded"), false)
	if retrySucceeded && kind == domain.TrafficEventSuccess {
		// 本渠道失败后由其他渠道重试成功，仍应惩罚原渠道的软失败率。
		kind = domain.TrafficEventSoftFailure
	}
	affinityRule := compactTrafficField(schedulerString(firstScheduler(affinity, "rule_name", "reason")))
	affinityGroup := compactTrafficField(schedulerString(firstScheduler(affinity, "selected_group", "using_group")))
	affinityHit := affinityRule != ""
	if rawHit := firstScheduler(affinity, "hit", "cache_hit"); rawHit != nil {
		switch value := rawHit.(type) {
		case bool:
			affinityHit = value
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(value))
			if err == nil {
				affinityHit = parsed
			}
		default:
			affinityHit = schedulerInt(rawHit) != 0
		}
	}
	sessionScoped := false
	if affinityRule != "" && kind != domain.TrafficEventSuccess {
		for _, skipRules := range skipRuleSets {
			if skipRules[affinityRule] {
				sessionScoped = true
				break
			}
		}
	}
	event := domain.TrafficEvent{
		Source: source, OccurredAt: occurred, ChannelID: channelID, ChannelName: channelName,
		Model: domain.NormalizeProbeModel(schedulerString(lookup("model", "model_name", "modelName"))), Group: schedulerString(lookup("group", "group_name", "groupName")),
		RequestID:         compactTrafficField(schedulerString(lookup("request_id", "requestId", "request-id"))),
		UpstreamRequestID: compactTrafficField(schedulerString(lookup("upstream_request_id", "upstreamRequestId", "upstream-id"))),
		Kind:              kind, HTTPStatus: status, ErrorType: errorType, ErrorCode: errorCode, DurationMS: duration, TTFTMS: ttft, StreamEnded: streamEnded,
		Tokens: tokens, RetryCount: schedulerInt(lookup("retry_count", "retryCount", "retries")),
		RetrySucceeded: retrySucceeded,
		AffinityRule:   affinityRule, AffinityGroup: affinityGroup, AffinityHit: affinityHit, SessionScoped: sessionScoped,
	}
	event.DedupeKey = domain.TrafficDedupeKey(event)
	sum := sha256.Sum256([]byte(event.DedupeKey))
	event.ID = hex.EncodeToString(sum[:16])
	return event, true
}

// trafficSafeError 只把可操作的错误类别写入游标/控制态，避免把调度端响应原文落库。
func trafficSafeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "调度器请求超时"
	}
	if errors.Is(err, context.Canceled) {
		return "调度器请求已取消"
	}
	message := strings.ToLower(err.Error())
	for _, status := range []string{"401", "403", "429", "500", "502", "503", "504"} {
		if strings.Contains(message, "http "+status) {
			return "调度器 HTTP " + status
		}
	}
	if strings.Contains(message, "no permission") || strings.Contains(message, "forbidden") {
		return "调度器日志权限不足"
	}
	return "调度器日志请求失败"
}

func trafficChannel(value, name any) (string, string) {
	if object, ok := value.(map[string]any); ok {
		return schedulerString(firstScheduler(object, "id", "channel_id", "channelId")), domain.FirstNonEmpty(schedulerString(firstScheduler(object, "name", "channel_name", "channelName")), schedulerString(name))
	}
	return schedulerString(value), schedulerString(name)
}

func compactTrafficField(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func trafficErrorField(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_-.:/", r) {
			continue
		}
		return ""
	}
	return value
}

func trafficMilliseconds(value any, seconds bool) int {
	multiplier := 1.0
	if seconds {
		multiplier = 1000
	}
	switch x := value.(type) {
	case float64:
		return int(x * multiplier)
	case int:
		return int(float64(x) * multiplier)
	case int64:
		return int(float64(x) * multiplier)
	case json.Number:
		f, _ := x.Float64()
		return trafficMilliseconds(f, seconds)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return trafficMilliseconds(f, seconds)
	default:
		return schedulerInt(value)
	}
}

func trafficBool(value any, fallback bool) bool {
	switch x := value.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(x))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func (s *SchedulerService) saveTrafficAggregates(ctx context.Context, events []domain.TrafficEvent, now time.Time) error {
	for _, spec := range []struct {
		table    string
		duration time.Duration
	}{{"scheduler_traffic_10s", 10 * time.Second}, {"scheduler_traffic_1m", time.Minute}} {
		start := now.Add(-trafficStartupReplay).Truncate(spec.duration)
		for bucket := start; bucket.Before(now); bucket = bucket.Add(spec.duration) {
			for _, row := range domain.AggregateTraffic(events, bucket, bucket.Add(spec.duration)) {
				if err := s.app.Store.SaveTrafficWindow(ctx, spec.table, row); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *SchedulerService) TrafficStatus(ctx context.Context) (domain.TrafficStatus, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.TrafficStatus{}, err
	}
	events, err := s.app.Store.TrafficEventsSince(ctx, time.Now().UTC().Add(-trafficStartupReplay))
	if err != nil {
		return domain.TrafficStatus{}, err
	}
	return s.trafficStatus(ctx, cfg, events, time.Now().UTC())
}

func (s *SchedulerService) TrafficRows(ctx context.Context) ([]domain.TrafficChannelState, error) {
	status, err := s.TrafficStatus(ctx)
	return status.Channels, err
}

func (s *SchedulerService) trafficStatus(ctx context.Context, cfg domain.SchedulerConfig, events []domain.TrafficEvent, now time.Time) (domain.TrafficStatus, error) {
	out := domain.TrafficStatus{Mode: cfg.TrafficMode, Profile: cfg.TrafficProfile, Channels: []domain.TrafficChannelState{}}
	cursors, err := s.app.Store.TrafficCursors(ctx)
	if err != nil {
		return out, err
	}
	out.Connected = len(cursors) == 2
	for _, cursor := range cursors {
		if cursor.LastPollAt.After(out.LastPollAt) {
			out.LastPollAt = cursor.LastPollAt
		}
		if cursor.LastEventAt.After(out.LastEventAt) {
			out.LastEventAt = cursor.LastEventAt
		}
		out.BacklogPages += cursor.BacklogPages
		if cursor.LastError != "" {
			out.Connected = false
			out.FreezeReason = cursor.LastError
		}
	}
	if !out.LastPollAt.IsZero() {
		out.LagSeconds = int(now.Sub(out.LastPollAt).Seconds())
		if out.LagSeconds < 0 {
			out.LagSeconds = 0
		}
	}
	if cfg.TrafficMode != domain.TrafficModeOff && (out.LastPollAt.IsZero() || now.Sub(out.LastPollAt) > 15*time.Second || out.BacklogPages > 0 || !out.Connected) {
		out.Frozen = true
		if out.FreezeReason == "" {
			if out.BacklogPages > 0 {
				out.FreezeReason = "日志分页仍有积压"
			} else {
				out.FreezeReason = "遥测超过 15 秒未更新"
			}
		}
	}
	if !schedulerConfigured(cfg) {
		return out, nil
	}
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		if cfg.TrafficMode == domain.TrafficModeOff {
			return out, nil
		}
		return out, err
	}
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return out, err
	}
	managed := map[string]bool{}
	for _, card := range cards {
		if card.PoolEnabled && strings.TrimSpace(card.SchedulerChannelID) != "" {
			managed[card.SchedulerChannelID] = true
		}
	}
	byChannel := map[string][]domain.TrafficEvent{}
	filteredEvents := make([]domain.TrafficEvent, 0, len(events))
	for _, event := range events {
		if lifecycle, found, lifecycleErr := s.app.Store.SchedulerChannelLifecycle(ctx, event.ChannelID); lifecycleErr == nil && found && !lifecycle.TrafficSince.IsZero() && event.OccurredAt.Before(lifecycle.TrafficSince) {
			continue
		}
		filteredEvents = append(filteredEvents, event)
		if event.SessionScoped {
			out.SessionFailures++
		}
		byChannel[event.ChannelID] = append(byChannel[event.ChannelID], event)
	}
	for _, channel := range channels {
		row := s.trafficChannelView(ctx, channel, byChannel[channel.ID], filteredEvents, managed[channel.ID], now)
		out.Channels = append(out.Channels, row)
	}
	sort.Slice(out.Channels, func(i, j int) bool {
		return trafficStateRank(out.Channels[i].State) < trafficStateRank(out.Channels[j].State)
	})
	return out, nil
}

func (s *SchedulerService) trafficChannelView(ctx context.Context, channel domain.SchedulerChannel, own, all []domain.TrafficEvent, managed bool, now time.Time) domain.TrafficChannelState {
	control, found, _ := s.app.Store.TrafficControl(ctx, channel.ID)
	if !found {
		control = domain.TrafficControlState{ChannelID: channel.ID, BasePriority: channel.Priority, BaseWeight: channel.Weight, DesiredPriority: channel.Priority, DesiredWeight: channel.Weight, ActualPriority: channel.Priority, ActualWeight: channel.Weight, DesiredStatus: channel.Status, ActualStatus: channel.Status, State: "healthy"}
	}
	// desired_status=2 可能只是上一轮可用性策略关闭渠道留下的控制态；
	// 当前是否应关闭由本轮流量决策和可用性覆盖共同决定，不能沿用旧值阻塞恢复。
	row := domain.TrafficChannelState{ChannelID: channel.ID, ChannelName: channel.Name, Managed: managed, State: "healthy", DesiredStatus: 1,
		ActualStatus: channel.Status, BasePriority: control.BasePriority, ActualPriority: channel.Priority, BaseWeight: control.BaseWeight, ActualWeight: channel.Weight, HealthScore: 100, UpdatedAt: control.UpdatedAt}
	if row.DesiredStatus == 0 {
		row.DesiredStatus = 1
	}
	models := map[string]bool{}
	for _, event := range own {
		models[domain.NormalizeProbeModel(event.Model)] = true
	}
	worstRank := -1
	for model := range models {
		w15 := firstTrafficWindow(domain.AggregateTraffic(own, now.Add(-15*time.Second), now), channel.ID, model, now.Add(-15*time.Second), now)
		w1 := firstTrafficWindow(domain.AggregateTraffic(own, now.Add(-time.Minute), now), channel.ID, model, now.Add(-time.Minute), now)
		w5 := firstTrafficWindow(domain.AggregateTraffic(own, now.Add(-5*time.Minute), now), channel.ID, model, now.Add(-5*time.Minute), now)
		alternative := healthyTrafficAlternative(all, channel, model, now)
		baseline := trafficHealthyBaseline(all, channel, model, now)
		decisionW5 := w5
		if baseline > 0 {
			decisionW5.P95TTFTMS = baseline
		}
		state, status, offset, percent, reason, score := domain.TrafficDecision(w15, w1, decisionW5, alternative, now)
		rank := trafficStateRank(state)
		if rank > worstRank {
			worstRank = rank
			row.State, row.Reason, row.DesiredStatus, row.Model, row.HealthScore = state, reason, status, model, score
			row.Window15s, row.Window1m, row.Window5m = &w15, &w1, &w5
			row.HealthyBaselineTTFTMS = baseline
			row.BasePriority = control.BasePriority
			_ = offset
			_ = percent
		}
	}
	if !managed {
		row.State, row.Reason = "unmanaged", "未显式绑定，仅展示遥测"
	} else if channel.Status == 3 {
		row.State, row.Reason, row.DesiredStatus = domain.TrafficStateExternalOff, "调度器自动禁用，等待调度器自身恢复", 3
	} else if row.State == "healthy" && (control.State == "recovering" || control.State == "hard_recovering") {
		row.State, row.Reason, row.DesiredStatus = control.State, control.Reason, control.DesiredStatus
	}
	return row
}

func trafficHealthyBaseline(events []domain.TrafficEvent, current domain.SchedulerChannel, model string, now time.Time) int {
	groups := map[string]bool{}
	for _, group := range domain.SplitGroups(current.Group) {
		groups[group] = true
	}
	byChannel := map[string][]domain.TrafficEvent{}
	for _, event := range events {
		if event.ChannelID == current.ID || event.Model != model || !groups[event.Group] || event.Kind != domain.TrafficEventSuccess || !event.OccurredAt.After(now.Add(-time.Minute)) {
			continue
		}
		byChannel[event.ChannelID] = append(byChannel[event.ChannelID], event)
	}
	baseline := 0
	for _, candidate := range byChannel {
		rows := domain.AggregateTraffic(candidate, now.Add(-time.Minute), now)
		for _, row := range rows {
			if row.P95TTFTMS > 0 && (baseline == 0 || row.P95TTFTMS < baseline) {
				baseline = row.P95TTFTMS
			}
		}
	}
	return baseline
}

func firstTrafficWindow(rows []domain.TrafficWindow, channelID, model string, start, end time.Time) domain.TrafficWindow {
	for _, row := range rows {
		if row.ChannelID == channelID && row.Model == model {
			return row
		}
	}
	return domain.TrafficWindow{ChannelID: channelID, Model: model, WindowStart: start, WindowEnd: end}
}

func healthyTrafficAlternative(events []domain.TrafficEvent, current domain.SchedulerChannel, model string, now time.Time) bool {
	groups := map[string]bool{}
	for _, group := range domain.SplitGroups(current.Group) {
		groups[group] = true
	}
	for _, event := range events {
		if event.ChannelID != current.ID && event.Model == model && groups[event.Group] && event.Kind == domain.TrafficEventSuccess && event.OccurredAt.After(now.Add(-time.Minute)) {
			return true
		}
	}
	return false
}

func trafficStateRank(state string) int {
	switch state {
	case "unmanaged":
		return 0
	case "healthy":
		return 1
	case "warning":
		return 2
	case "probe_required":
		return 3
	case "degraded":
		return 4
	case "recovering", "hard_recovering":
		return 5
	case domain.TrafficStateExternalOff:
		return 6
	case "soft_blocked":
		return 7
	case "hard_blocked":
		return 8
	default:
		return 1
	}
}

func (s *SchedulerService) applyTrafficControl(ctx context.Context, cfg domain.SchedulerConfig, views []domain.TrafficChannelState, now time.Time) error {
	channels, err := s.fetchSchedulerChannels(ctx, cfg, "")
	if err != nil {
		return err
	}
	byID := map[string]domain.SchedulerChannel{}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	cardsByChannel := map[string]domain.ModelCard{}
	for _, card := range cards {
		if card.PoolEnabled && card.SchedulerChannelID != "" {
			cardsByChannel[card.SchedulerChannelID] = card
		}
	}
	var actionErrors error
	sustainedWindows := (30 + domain.NormalizeTrafficPollSeconds(cfg.TrafficPollSecs) - 1) / domain.NormalizeTrafficPollSeconds(cfg.TrafficPollSecs)
	if sustainedWindows < 2 {
		sustainedWindows = 2
	}
	for _, view := range views {
		if !view.Managed {
			continue
		}
		current, ok := byID[view.ChannelID]
		if !ok {
			continue
		}
		control, found, err := s.app.Store.TrafficControl(ctx, view.ChannelID)
		if err != nil {
			return err
		}
		if !found {
			control = domain.TrafficControlState{ChannelID: view.ChannelID, BasePriority: current.Priority, BaseWeight: current.Weight, DesiredPriority: current.Priority, DesiredWeight: current.Weight, ActualPriority: current.Priority, ActualWeight: current.Weight, DesiredStatus: current.Status, ActualStatus: current.Status, State: "healthy"}
		}
		if current.Status == 3 {
			allowExplicitOverride := false
			if availability, found, _ := s.app.Store.ChannelAvailability(ctx, view.ChannelID); found {
				forceEnable := availability.Override == domain.OverrideForceEnable && (availability.OverrideUntil == nil || now.Before(*availability.OverrideUntil))
				allowExplicitOverride = forceEnable || availability.Override == domain.OverrideManualHold
			}
			if !allowExplicitOverride {
				control.State, control.Reason = domain.TrafficStateExternalOff, "调度器自动禁用，等待调度器自身恢复"
				control.DesiredStatus, control.ActualStatus = 3, 3
				control.DesiredPriority, control.ActualPriority = current.Priority, current.Priority
				control.DesiredWeight, control.ActualWeight = current.Weight, current.Weight
				control.RetryCount, control.RetryAt = 0, time.Time{}
				control.UpdatedAt = now
				if err := s.app.Store.SaveTrafficControl(ctx, control); err != nil {
					return err
				}
				continue
			}
		}
		if !control.RetryAt.IsZero() && now.Before(control.RetryAt) {
			continue
		}
		previousState := control.State
		wasDegraded := view.State == "degraded"
		status, priority, weight := view.DesiredStatus, control.BasePriority, control.BaseWeight
		if view.Window15s != nil && view.Window1m != nil && view.Window5m != nil {
			alternative := healthyTrafficAlternativeForView(views, view)
			decisionW5 := *view.Window5m
			if view.HealthyBaselineTTFTMS > 0 {
				decisionW5.P95TTFTMS = view.HealthyBaselineTTFTMS
			}
			var offset int64
			var percent uint
			_, status, offset, percent, _, _ = domain.TrafficDecision(*view.Window15s, *view.Window1m, decisionW5, alternative, now)
			priority += offset
			if percent == 0 {
				weight = 0
			} else if weight > 0 {
				weight = uint((uint64(weight)*uint64(percent) + 99) / 100)
				if weight == 0 {
					weight = 1
				}
			}
		}
		if view.State == "warning" || view.State == "degraded" {
			control.FailureWindows++
		} else if view.State != "soft_blocked" && view.State != "hard_blocked" {
			control.FailureWindows = 0
		}
		if view.State == "warning" && control.FailureWindows >= sustainedWindows {
			view.State, view.Reason = "degraded", "连续两个窗口退化"
			status, priority, weight = 1, control.BasePriority-2000, trafficWeight(control.BaseWeight, 10)
		}
		if wasDegraded && view.State == "degraded" && control.FailureWindows >= sustainedWindows && healthyTrafficAlternativeForView(views, view) {
			view.State, view.Reason = "soft_blocked", "严重退化持续 30 秒"
			status, priority, weight = 2, control.BasePriority-2000, 0
		}
		if view.State == "probe_required" {
			probeOK, attempted := s.trafficProbeDue(ctx, cardsByChannel[view.ChannelID], &control, now)
			if attempted && !probeOK {
				if healthyTrafficAlternativeForView(views, view) {
					view.State, view.Reason, status, priority, weight = "soft_blocked", "低流量主动探测确认失败", 2, control.BasePriority-2000, 0
					control.CooldownUntil = now.Add(time.Minute)
				} else {
					view.State, view.Reason, status, priority = "degraded", "末路保护：主动探测失败但保留最后候选", 1, control.BasePriority-2000
					weight = trafficWeight(control.BaseWeight, 10)
				}
			} else if attempted {
				view.State, view.Reason, status, priority, weight = "healthy", "主动探测确认健康", 1, control.BasePriority, control.BaseWeight
			}
		}
		if view.State == "soft_blocked" {
			control.RecoveryStage, control.RecoverySuccesses = 0, 0
			if control.CooldownUntil.IsZero() || previousState != "soft_blocked" {
				control.CooldownUntil = now.Add(time.Minute)
			}
		}
		if view.State == "hard_blocked" {
			control.RecoveryStage, control.RecoverySuccesses = -1, 0
			if control.CooldownUntil.IsZero() || previousState != "hard_blocked" {
				control.CooldownUntil = now.Add(15 * time.Minute)
			}
		}
		if (view.State == "healthy" || view.State == "hard_recovering") && (previousState == "hard_blocked" || previousState == "hard_recovering" || control.RecoveryStage == -1) {
			status, priority, weight = 2, control.BasePriority-2000, 0
			view.State, view.Reason = "hard_recovering", "硬阻断恢复确认中"
			if !control.CooldownUntil.IsZero() && !now.Before(control.CooldownUntil) {
				probeOK, attempted := s.trafficProbeDue(ctx, cardsByChannel[view.ChannelID], &control, now)
				if attempted {
					if probeOK {
						control.RecoverySuccesses++
					} else {
						control.RecoverySuccesses = 0
					}
				}
				if control.RecoverySuccesses >= 3 {
					view.State, view.Reason, status, priority, weight = "healthy", "硬阻断恢复完成", 1, control.BasePriority, control.BaseWeight
					control.RecoveryStage, control.RecoverySuccesses = 0, 0
					control.CooldownUntil = time.Time{}
				}
			}
		}
		if (view.State == "healthy" || view.State == "recovering") && (previousState == "soft_blocked" || previousState == "recovering" || control.RecoveryStage > 0) {
			status, priority, weight = 2, control.BasePriority-2000, 0
			view.State, view.Reason = "recovering", "软熔断冷却中"
			if !control.CooldownUntil.IsZero() && !now.Before(control.CooldownUntil) {
				probeOK, attempted := s.trafficProbeDue(ctx, cardsByChannel[view.ChannelID], &control, now)
				if attempted {
					if probeOK {
						control.RecoverySuccesses++
					} else {
						control.RecoverySuccesses = 0
					}
				}
				if control.RecoverySuccesses >= 2 {
					oldStage := control.RecoveryStage
					nextStage, percent, complete := domain.TrafficRecoveryTarget(oldStage, control.StageChangedAt, now)
					if oldStage == 0 || nextStage != oldStage {
						control.StageChangedAt = now
					}
					control.RecoveryStage = nextStage
					status, weight = 1, trafficWeight(control.BaseWeight, percent)
					if nextStage >= 3 {
						priority = control.BasePriority - 1000
					}
					view.Reason = fmt.Sprintf("恢复阶梯 %d%%", percent)
					if complete {
						view.State, view.Reason, priority, weight = "healthy", "恢复完成", control.BasePriority, control.BaseWeight
						control.RecoveryStage, control.RecoverySuccesses = 0, 0
						control.CooldownUntil, control.StageChangedAt = time.Time{}, time.Time{}
					}
				}
			}
		}
		if availability, found, _ := s.app.Store.ChannelAvailability(ctx, view.ChannelID); found {
			if availability.Override == domain.OverrideManualHold || availability.DesiredStatus == 2 {
				status, weight = 2, 0
			}
			if availability.Override == domain.OverrideForceEnable && (availability.OverrideUntil == nil || now.Before(*availability.OverrideUntil)) {
				status, priority, weight = 1, control.BasePriority, control.BaseWeight
			}
		}
		control.State, control.Reason, control.DesiredStatus, control.UpdatedAt = view.State, view.Reason, status, now
		control.DesiredPriority, control.DesiredWeight = priority, weight
		stateChanged := previousState != control.State
		if status == 2 && view.State == "soft_blocked" && control.CooldownUntil.IsZero() {
			control.CooldownUntil = now.Add(time.Minute)
		}
		if err := s.app.Store.SaveTrafficControl(ctx, control); err != nil {
			return err
		}
		// 关闭请求可能已写入远端，但亲和性缓存清理失败；到达重试时间后
		// 即使远端字段已经吻合，也要重放关闭动作完成清理。
		retryClosedWrite := status == 2 && control.RetryCount > 0
		if current.Status == status && current.Priority == priority && current.Weight == weight && !retryClosedWrite {
			if stateChanged {
				s.recordTrafficTransition(ctx, current, view)
			}
			continue
		}
		if err := s.writeTrafficChannel(ctx, cfg, current, status, priority, weight); err != nil {
			control.RetryCount++
			control.RetryAt = *retryAt(now, control.RetryCount)
			control.Reason = trafficSafeError(err)
			_ = s.app.Store.SaveTrafficControl(ctx, control)
			actionErrors = errors.Join(actionErrors, err)
			continue
		}
		actual, found, err := s.schedulerChannel(ctx, cfg, current.ID)
		if err != nil || !found || actual.Status != status || actual.Priority != priority || actual.Weight != weight {
			if err == nil {
				err = fmt.Errorf("流量调度写后校验失败：期望状态/优先级/权重 %d/%d/%d", status, priority, weight)
			}
			control.RetryCount++
			control.RetryAt = *retryAt(now, control.RetryCount)
			control.Reason = trafficSafeError(err)
			_ = s.app.Store.SaveTrafficControl(ctx, control)
			actionErrors = errors.Join(actionErrors, err)
			continue
		}
		control.ActualStatus, control.ActualPriority, control.ActualWeight = actual.Status, actual.Priority, actual.Weight
		control.RetryCount, control.RetryAt = 0, time.Time{}
		_ = s.app.Store.SaveTrafficControl(ctx, control)
		if stateChanged {
			s.recordTrafficTransition(ctx, current, view)
		}
	}
	return actionErrors
}

func (s *SchedulerService) recordTrafficTransition(ctx context.Context, channel domain.SchedulerChannel, view domain.TrafficChannelState) {
	message := domain.FirstNonEmpty(view.Reason, view.State)
	_ = s.app.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{ChannelID: channel.ID, ChannelName: channel.Name, Action: "traffic_control", Status: "success", Reason: view.State, Message: message})
	severity := "warning"
	if view.State == "healthy" {
		severity = "success"
	}
	_, _ = s.app.Store.CreateOpsEvent(ctx, domain.OpsEvent{Type: "availability_changed", Severity: severity, Title: "真实流量调度变更", Message: channel.Name + " " + message, TargetType: "channel", TargetID: channel.ID, Actions: []string{"scheduler_availability"}})
	s.notifyAvailability(ctx, "availability_changed", channel.Name+" "+message, view.State == "healthy")
}

func (s *SchedulerService) trafficProbeDue(ctx context.Context, card domain.ModelCard, control *domain.TrafficControlState, now time.Time) (success, attempted bool) {
	if card.ID == "" || !control.LastProbeAt.IsZero() && now.Sub(control.LastProbeAt) < 15*time.Second {
		return false, false
	}
	control.LastProbeAt = now
	if err := s.app.Probe.CheckCard(ctx, card.ID); err != nil {
		return false, true
	}
	probes, err := s.app.Store.RecentProbesForCard(ctx, card.ID, 1)
	return err == nil && len(probes) == 1 && probes[0].Success, true
}

func trafficWeight(base uint, percent uint) uint {
	if base == 0 || percent == 0 {
		return 0
	}
	weight := uint((uint64(base)*uint64(percent) + 99) / 100)
	if weight == 0 {
		return 1
	}
	return weight
}

func healthyTrafficAlternativeForView(views []domain.TrafficChannelState, current domain.TrafficChannelState) bool {
	for _, view := range views {
		currentGroup, otherGroup := "", ""
		if current.Window1m != nil {
			currentGroup = current.Window1m.Group
		}
		if view.Window1m != nil {
			otherGroup = view.Window1m.Group
		}
		if view.ChannelID != current.ChannelID && view.Model == current.Model && currentGroup != "" && currentGroup == otherGroup && view.State == "healthy" {
			return true
		}
	}
	return false
}

func (s *SchedulerService) writeTrafficChannel(ctx context.Context, cfg domain.SchedulerConfig, current domain.SchedulerChannel, status int, priority int64, weight uint) error {
	if current.Status != status || status == 2 {
		confirmedRestore := false
		if lifecycle, found, _ := s.app.Store.SchedulerChannelLifecycle(ctx, current.ID); found {
			confirmedRestore = lifecycle.AUMDisabled
		}
		if err := s.setSchedulerChannelStatus(ctx, current.ID, status, domain.ControlSourceTraffic, "真实流量协调", confirmedRestore); err != nil {
			return err
		}
	}
	if current.Priority == priority && current.Weight == weight {
		return nil
	}
	current.Status = status
	return s.coordinateSchedulerFields(ctx, cfg, current, current.Group, priority, weight, true, domain.ControlSourceTraffic, "真实流量优先级与权重")
}

func (s *SchedulerService) AdoptTrafficBaseline(ctx context.Context, channelID string) (domain.TrafficControlState, error) {
	cfg, err := s.app.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.TrafficControlState{}, err
	}
	channel, found, err := s.schedulerChannel(ctx, cfg, channelID)
	if err != nil {
		return domain.TrafficControlState{}, err
	}
	if !found {
		return domain.TrafficControlState{}, ErrBadRequest("调度器中找不到该渠道")
	}
	row, _, err := s.app.Store.TrafficControl(ctx, channelID)
	if err != nil {
		return row, err
	}
	row.ChannelID, row.BasePriority, row.BaseWeight = channelID, channel.Priority, channel.Weight
	row.DesiredPriority, row.DesiredWeight = channel.Priority, channel.Weight
	row.ActualPriority, row.ActualWeight, row.ActualStatus = channel.Priority, channel.Weight, channel.Status
	if row.State == "" {
		row.State, row.DesiredStatus = "healthy", channel.Status
	}
	row.UpdatedAt = time.Now().UTC()
	if err := s.app.Store.SaveTrafficControl(ctx, row); err != nil {
		return row, err
	}
	return row, nil
}
