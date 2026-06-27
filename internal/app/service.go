package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"

	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	Store   *store.Store
	Client  monitor.Client
	mu      sync.Mutex
	running bool
	lastRun time.Time
}

func New(st *store.Store) *Service {
	return &Service{Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}}}
}

func (s *Service) StartScheduler(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = s.CheckDue(context.Background())
			}
		}
	}()
}

func (s *Service) SetupStatus(ctx context.Context) (map[string]bool, error) {
	n, err := s.Store.UserCount(ctx)
	return map[string]bool{"initialized": n > 0}, err
}

func (s *Service) Setup(ctx context.Context, username, password string) (domain.User, error) {
	if username == "" || password == "" {
		return domain.User{}, errors.New("username and password are required")
	}
	n, err := s.Store.UserCount(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if n > 0 {
		return domain.User{}, errors.New("setup already completed")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	return s.Store.CreateUser(ctx, username, string(hash))
}

func (s *Service) Login(ctx context.Context, username, password string) (string, domain.User, error) {
	u, err := s.Store.UserByUsername(ctx, username)
	if err != nil {
		return "", u, errors.New("invalid username or password")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", u, errors.New("invalid username or password")
	}
	token, err := s.Store.CreateSession(ctx, u.ID, 30*24*time.Hour)
	return token, u, err
}

func (s *Service) Me(ctx context.Context, token string) (domain.User, error) {
	return s.Store.UserBySessionToken(ctx, token)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.Store.DeleteSession(ctx, token)
}

func (s *Service) SaveUpstream(ctx context.Context, id string, in domain.Upstream) (domain.Upstream, error) {
	if in.Name == "" || in.Type == "" || in.BaseURL == "" {
		return domain.Upstream{}, errors.New("name, type and base_url are required")
	}
	if in.Type != "newapi" && in.Type != "sub2api" {
		return domain.Upstream{}, errors.New("type must be newapi or sub2api")
	}
	if in.BalanceRate <= 0 {
		in.BalanceRate = 1
	}
	if id == "" {
		if !in.Enabled {
			in.Enabled = true
		}
		return s.Store.CreateUpstream(ctx, in)
	}
	old, err := s.Store.Upstream(ctx, id)
	if err != nil {
		return domain.Upstream{}, err
	}
	in.ID = id
	in.LastError = old.LastError
	in.FailureCount = old.FailureCount
	in.CreatedAt = old.CreatedAt
	return s.Store.UpdateUpstream(ctx, in)
}

func (s *Service) SyncKeys(ctx context.Context, upstreamID string) error {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return err
	}
	mu := toMonitorUpstream(u)
	result, err := s.Client.Check(ctx, &mu, "", "")
	if err != nil {
		return err
	}
	if err := s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken); err != nil {
		return err
	}
	return s.Store.SaveKeys(ctx, u.ID, result.Keys)
}

func (s *Service) CheckDue(ctx context.Context) error {
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return err
	}
	interval := time.Duration(cfg.CheckIntervalMinutes) * time.Minute
	s.mu.Lock()
	if s.running || (!s.lastRun.IsZero() && time.Since(s.lastRun) < interval) {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.lastRun = time.Now()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	return s.CheckAll(ctx)
}

func (s *Service) CheckAll(ctx context.Context) error {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return err
	}
	for _, u := range upstreams {
		if u.Enabled {
			_ = s.CheckUpstream(ctx, u.ID)
		}
	}
	cards, err := s.Store.ListCards(ctx)
	if err != nil {
		return err
	}
	for _, c := range cards {
		if c.Enabled {
			_ = s.CheckCard(ctx, c.ID)
		}
	}
	return nil
}

func (s *Service) RefreshBalances(ctx context.Context) error {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, u := range upstreams {
		if !u.Enabled {
			continue
		}
		upstreamID := u.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}
			if err := s.CheckUpstream(ctx, upstreamID); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func (s *Service) CheckUpstream(ctx context.Context, upstreamID string) error {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return err
	}
	start := time.Now()
	mu := toMonitorUpstream(u)
	result, err := s.Client.Check(ctx, &mu, "", "")
	if err != nil {
		_ = s.Store.SaveUpstreamError(ctx, u.ID, err.Error(), u.FailureCount+1)
		if monitor.IsAuthError(err) {
			_ = s.alert(ctx, u, "credential", true, u.Name+" 凭据失效: "+err.Error())
		}
		return err
	}
	if err := s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken); err != nil {
		return err
	}
	if err := s.Store.SaveKeys(ctx, u.ID, result.Keys); err != nil {
		return err
	}
	snap, err := s.Store.SaveBalance(ctx, u.ID, result.Balance, "", int(time.Since(start).Milliseconds()))
	if err != nil {
		return err
	}
	_ = s.Store.SaveUpstreamError(ctx, u.ID, "", 0)
	_ = s.alert(ctx, u, "credential", false, u.Name+" 凭据已恢复")
	return s.alert(ctx, u, "balance", domain.LowBalance(u, snap), fmt.Sprintf("%s 余额低于阈值", u.Name))
}

func (s *Service) SaveCard(ctx context.Context, id string, upstreamID, keyID string, enabled bool) (domain.ModelCard, error) {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return domain.ModelCard{}, err
	}
	var key *domain.APIKey
	if keyID != "" {
		k, err := s.Store.Key(ctx, keyID)
		if err != nil {
			return domain.ModelCard{}, err
		}
		if k.UpstreamID != upstreamID {
			return domain.ModelCard{}, errors.New("key does not belong to upstream")
		}
		key = &k
	}
	card := domain.ModelCard{Name: domain.CardName(u, key), UpstreamID: upstreamID, KeyID: keyID, Model: domain.ProbeModel, Enabled: enabled}
	if id == "" {
		return s.Store.CreateCard(ctx, card)
	}
	old, err := s.Store.Card(ctx, id)
	if err != nil {
		return domain.ModelCard{}, err
	}
	card.ID = old.ID
	card.LastError = old.LastError
	card.FailureCount = old.FailureCount
	card.CreatedAt = old.CreatedAt
	return s.Store.UpdateCard(ctx, card)
}

func (s *Service) CheckCard(ctx context.Context, cardID string) error {
	card, err := s.Store.Card(ctx, cardID)
	if err != nil {
		return err
	}
	u, err := s.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		return err
	}
	if card.KeyID == "" {
		msg := "未选择 Key"
		if _, err := s.Store.SaveProbe(ctx, u.ID, card.ID, monitor.ProbeResult{Status: monitor.StatusFailed, Error: msg}); err != nil {
			return err
		}
		return s.Store.UpdateCardProbeState(ctx, card.ID, msg, card.FailureCount+1)
	}
	key, err := s.Store.Key(ctx, card.KeyID)
	if err != nil {
		return err
	}
	probe := s.Client.Probe(ctx, u.BaseURL, key.Key, domain.ProbeModel)
	if _, err := s.Store.SaveProbe(ctx, u.ID, card.ID, probe); err != nil {
		return err
	}
	failures := 0
	lastErr := ""
	if !probe.Success {
		failures = card.FailureCount + 1
		lastErr = probe.Error
	}
	if err := s.Store.UpdateCardProbeState(ctx, card.ID, lastErr, failures); err != nil {
		return err
	}
	return s.alert(ctx, u, "ping:"+card.ID, !probe.Success && failures >= 2, card.Name+" 探测失败: "+probe.Error)
}

func (s *Service) MonitorStatus(ctx context.Context, window string) (map[string]any, error) {
	since, label := windowSince(window)
	cards, err := s.enrichedCards(ctx, since)
	if err != nil {
		return nil, err
	}
	total, ok, failed, latency, samples := 0, 0, 0, 0, 0
	for _, c := range cards {
		for _, p := range c.History {
			total++
			if p.Success {
				ok++
			} else {
				failed++
			}
			if p.LatencyMS > 0 {
				latency += p.LatencyMS
				samples++
			}
		}
	}
	return map[string]any{
		"window": label, "rows": cards, "requests": total, "success": ok, "failed": failed,
		"success_rate": percent(ok, total), "avg_latency": avg(latency, samples),
	}, nil
}

func (s *Service) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	return s.enrichedCards(ctx, time.Now().Add(-time.Hour))
}

func (s *Service) enrichedCards(ctx context.Context, since time.Time) ([]domain.ModelCard, error) {
	cards, err := s.Store.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		u, err := s.Store.Upstream(ctx, cards[i].UpstreamID)
		if err == nil {
			cards[i].UpstreamName = u.Name
			cards[i].Type = u.Type
		}
		if cards[i].KeyID != "" {
			if k, err := s.Store.Key(ctx, cards[i].KeyID); err == nil {
				cards[i].KeyName = k.Name
				cards[i].KeyGroup = k.Group
				cards[i].KeyRatio = k.GroupRatio
				cards[i].EffectiveRatio = effectiveRatio(k.GroupRatio, domain.BalanceRate(u))
			}
		}
		history, err := s.Store.ProbesForCardSince(ctx, cards[i].ID, since, 60)
		if err != nil {
			return nil, err
		}
		reverse(history)
		cards[i].History = history
	}
	return cards, nil
}

func effectiveRatio(groupRatio string, balanceRate float64) string {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(groupRatio), 64)
	if err != nil {
		return groupRatio
	}
	out := fmt.Sprintf("%.6f", ratio*balanceRate)
	return strings.TrimRight(strings.TrimRight(out, "0"), ".")
}

func (s *Service) BalanceRows(ctx context.Context) ([]map[string]any, error) {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(upstreams))
	for _, u := range upstreams {
		row := map[string]any{
			"id": u.ID, "name": u.Name, "type": u.Type, "enabled": u.Enabled,
			"balance_rate": domain.BalanceRate(u), "low_balance_threshold": u.LowBalanceThreshold,
		}
		if b, err := s.Store.LatestBalance(ctx, u.ID); err == nil {
			balance, used, remain := domain.ConvertedBalanceValues(u.Type, domain.BalanceRate(u), b.Balance, b.Used, b.Remain)
			sourceBalance, sourceUsed, sourceRemain := domain.NormalizedBalanceValues(u.Type, b.Balance, b.Used, b.Remain)
			row["balance"], row["used"], row["remain"] = balance, used, remain
			row["source_balance"], row["source_used"], row["source_remain"] = sourceBalance, sourceUsed, sourceRemain
			row["requests"], row["last_check"], row["error"] = b.Requests, b.CheckedAt, b.Error
			row["low_balance"] = domain.LowBalance(u, b)
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *Service) UpstreamRows(ctx context.Context) ([]map[string]any, error) {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(upstreams))
	for _, u := range upstreams {
		keys, err := s.Store.ListKeys(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		row := map[string]any{"upstream": u, "keys": keys}
		if b, err := s.Store.LatestBalance(ctx, u.ID); err == nil {
			row["balance"] = b
		}
		out = append(out, row)
	}
	return out, nil
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
	sent := s.sendTelegram(ctx, dec.Message) == nil
	return s.Store.SaveAlert(ctx, u.ID, dec, sent)
}

func (s *Service) sendTelegram(ctx context.Context, message string) error {
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return err
	}
	if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		return nil
	}
	form := url.Values{"chat_id": {cfg.TelegramChatID}, "text": {message}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+cfg.TelegramBotToken+"/sendMessage", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

func windowSince(window string) (time.Time, string) {
	windows := map[string]time.Duration{
		"1h": time.Hour, "3h": 3 * time.Hour, "5h": 5 * time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "15d": 15 * 24 * time.Hour,
	}
	if _, ok := windows[window]; !ok {
		window = "1h"
	}
	return time.Now().UTC().Add(-windows[window]), window
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
