package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

var errSchedulerNotConfigured = errors.New("scheduler not configured")

func (s *Service) SchedulerConfig(ctx context.Context) (domain.SchedulerConfig, error) {
	return s.Store.SchedulerConfig(ctx)
}

func (s *Service) SaveSchedulerConfig(ctx context.Context, cfg domain.SchedulerConfig) (domain.SchedulerConfig, error) {
	return s.Store.UpdateSchedulerConfig(ctx, cfg)
}

func (s *Service) SchedulerChannels(ctx context.Context, keyword string) ([]domain.SchedulerChannel, error) {
	cfg, err := s.Store.SchedulerConfig(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, errors.New("请先配置调度器连接")
	}
	values := url.Values{}
	if strings.TrimSpace(keyword) != "" {
		values.Set("keyword", strings.TrimSpace(keyword))
	}
	path := "/api/channel/"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return nil, errors.New(schedulerMessage(raw))
	}
	return schedulerChannels(raw), nil
}

func (s *Service) SchedulerLogs(ctx context.Context, limit int) ([]domain.SchedulerLog, error) {
	return s.Store.SchedulerLogs(ctx, limit)
}

func (s *Service) applySchedulerAutomation(ctx context.Context, card domain.ModelCard, success bool, failures int) error {
	if card.SchedulerChannelID == "" {
		return nil
	}
	if !success && failures >= 2 && !card.SchedulerAutoDisabled {
		if err := s.setSchedulerChannelStatus(ctx, card.SchedulerChannelID, 2); err != nil {
			if errors.Is(err, errSchedulerNotConfigured) {
				s.logSchedulerAction(ctx, card, "disable", "skipped", "调度器未配置")
				return nil
			}
			s.logSchedulerAction(ctx, card, "disable", "error", err.Error())
			return err
		}
		if err := s.Store.UpdateCardSchedulerAutoDisabled(ctx, card.ID, true); err != nil {
			s.logSchedulerAction(ctx, card, "disable", "error", err.Error())
			return err
		}
		s.logSchedulerAction(ctx, card, "disable", "success", "连续失败 2 次，已关闭调度器渠道")
		return nil
	}
	if success && card.SchedulerAutoDisabled && s.lastTwoProbesSucceeded(ctx, card.ID) {
		if err := s.setSchedulerChannelStatus(ctx, card.SchedulerChannelID, 1); err != nil {
			if errors.Is(err, errSchedulerNotConfigured) {
				s.logSchedulerAction(ctx, card, "restore", "skipped", "调度器未配置")
				return nil
			}
			s.logSchedulerAction(ctx, card, "restore", "error", err.Error())
			return err
		}
		if err := s.Store.UpdateCardSchedulerAutoDisabled(ctx, card.ID, false); err != nil {
			s.logSchedulerAction(ctx, card, "restore", "error", err.Error())
			return err
		}
		s.logSchedulerAction(ctx, card, "restore", "success", "连续成功 2 次，已恢复调度器渠道")
		return nil
	}
	return nil
}

func (s *Service) logSchedulerAction(ctx context.Context, card domain.ModelCard, action, status, message string) {
	_ = s.Store.CreateSchedulerLog(ctx, domain.SchedulerLog{
		CardID:      card.ID,
		CardName:    card.Name,
		ChannelID:   card.SchedulerChannelID,
		ChannelName: card.SchedulerChannelName,
		Action:      action,
		Status:      status,
		Message:     message,
	})
}

func (s *Service) lastTwoProbesSucceeded(ctx context.Context, cardID string) bool {
	probes, err := s.Store.RecentProbesForCard(ctx, cardID, 2)
	if err != nil || len(probes) < 2 {
		return false
	}
	return probes[0].Success && probes[1].Success
}

func (s *Service) setSchedulerChannelStatus(ctx context.Context, channelID string, status int) error {
	cfg, err := s.Store.SchedulerConfig(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return errSchedulerNotConfigured
	}
	var raw map[string]any
	if err := s.schedulerJSON(ctx, cfg, http.MethodPost, "/api/channel/"+url.PathEscape(channelID)+"/status", map[string]int{"status": status}, &raw); err != nil {
		return err
	}
	if ok, exists := raw["success"].(bool); exists && !ok {
		return errors.New(schedulerMessage(raw))
	}
	return nil
}

func (s *Service) schedulerJSON(ctx context.Context, cfg domain.SchedulerConfig, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, joinSchedulerURL(cfg.BaseURL, path), r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", cfg.AccessToken)
	req.Header.Set("New-Api-User", cfg.UserID)
	hc := s.Client.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("调度器 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if len(b) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return err
	}
	return nil
}

func joinSchedulerURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func schedulerMessage(raw map[string]any) string {
	for _, key := range []string{"message", "error", "msg"} {
		if v := strings.TrimSpace(fmt.Sprint(raw[key])); v != "" && v != "<nil>" {
			return v
		}
	}
	return "调度器返回失败"
}

func schedulerChannels(raw map[string]any) []domain.SchedulerChannel {
	items := schedulerArray(raw["data"])
	if len(items) == 0 {
		items = schedulerArray(raw["channels"])
	}
	out := make([]domain.SchedulerChannel, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ch := domain.SchedulerChannel{
			ID:     schedulerString(firstScheduler(m, "id")),
			Name:   schedulerString(firstScheduler(m, "name", "channel_name")),
			Status: schedulerInt(firstScheduler(m, "status")),
			Tag:    schedulerString(firstScheduler(m, "tag")),
			Type:   schedulerString(firstScheduler(m, "type")),
			Group:  schedulerString(firstScheduler(m, "group")),
			Models: schedulerStrings(firstScheduler(m, "models")),
		}
		if ch.ID != "" {
			out = append(out, ch)
		}
	}
	return out
}

func schedulerArray(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, key := range []string{"items", "channels", "data"} {
			if a, ok := x[key].([]any); ok {
				return a
			}
		}
	}
	return nil
}

func firstScheduler(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func schedulerString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func schedulerInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func schedulerStrings(v any) []string {
	items := schedulerArray(v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := schedulerString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}
