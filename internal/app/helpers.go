package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

func appLocation() *time.Location {
	if name := strings.TrimSpace(os.Getenv("TZ")); name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.Local
}

func (s *Service) alert(ctx context.Context, u domain.Upstream, kind string, failing bool, msg string) error {
	prev, err := s.Store.AlertState(ctx, u.ID, kind)
	if err != nil {
		return err
	}
	dec, send := domain.DecideAlert(time.Now(), kind, failing, msg, prev)
	if !send {
		return nil
	}
	if dec.Recover {
		dec.Message = u.Name + " " + kind + " 已恢复"
	}
	s.createAlertOpsEvent(ctx, u, kind, dec.Recover, dec.Message)
	rules, err := s.Store.NotificationRules(ctx)
	if err != nil {
		return err
	}
	eventType, _, _ := domain.AlertEventType(kind, dec.Recover)
	sent := false
	if domain.ShouldNotify(rules, eventType, dec.Recover) {
		sent = s.Notify.Send(ctx, dec.Message) == nil
	}
	return s.Store.SaveAlert(ctx, u.ID, dec, sent)
}

func (s *Service) alertFailureThreshold(ctx context.Context) int {
	rules, err := s.Store.NotificationRules(ctx)
	if err != nil {
		return domain.EffectiveFailureThreshold(domain.NotificationRules{})
	}
	return domain.EffectiveFailureThreshold(rules)
}

func (s *Service) probeMuteFailureThreshold(ctx context.Context) int {
	rules, err := s.Store.NotificationRules(ctx)
	if err != nil {
		return domain.EffectiveMuteThreshold(domain.NotificationRules{})
	}
	return domain.EffectiveMuteThreshold(rules)
}

func (s *Service) probeInternalRetryPolicy(ctx context.Context) (retries int, interval time.Duration) {
	rules, err := s.Store.NotificationRules(ctx)
	if err != nil {
		return domain.EffectiveInternalRetry(domain.NotificationRules{})
	}
	return domain.EffectiveInternalRetry(rules)
}

func (s *Service) sendTelegram(ctx context.Context, message string) error {
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(cfg.TelegramBotToken)
	chatID := strings.TrimSpace(cfg.TelegramChatID)
	if token == "" || chatID == "" {
		return nil
	}
	form := url.Values{"chat_id": {chatID}, "text": {message}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := s.Client.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg != "" {
			return fmt.Errorf("telegram status %d: %s", resp.StatusCode, msg)
		}
		return fmt.Errorf("telegram status %d", resp.StatusCode)
	}
	return nil
}

func toMonitorUpstream(u domain.Upstream) monitor.Upstream {
	return monitor.Upstream{
		ID: u.ID, Name: u.Name, Type: u.Type, BaseURL: u.BaseURL, Enabled: u.Enabled, UserID: u.UserID,
		AccessToken: u.AccessToken, Email: u.Email, Password: u.Password, Sub2APIAccessToken: u.Sub2APIAccessToken,
		Sub2APIRefreshToken: u.Sub2APIRefreshToken, LowBalanceThreshold: u.LowBalanceThreshold, FailureCount: u.FailureCount,
	}
}

func windowSince(window string) (time.Time, string, time.Duration) {
	windows := map[string]time.Duration{
		"1h": time.Hour, "3h": 3 * time.Hour, "5h": 5 * time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "15d": 15 * 24 * time.Hour,
	}
	if _, ok := windows[window]; !ok {
		window = "1h"
	}
	duration := windows[window]
	return time.Now().UTC().Add(-duration), window, duration
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func avg(total, count int) int {
	if count == 0 {
		return 0
	}
	return total / count
}

func reverse[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func IgnoreNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
