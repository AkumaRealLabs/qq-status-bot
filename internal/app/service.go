package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/epay"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"

	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	Store      *store.Store
	Client     monitor.Client
	TGMediaDir string
	mu         sync.Mutex
	running    bool
	lastRun    time.Time
	tgRunning  bool
	tgLastRun  time.Time
}

func New(st *store.Store) *Service {
	mediaDir := os.Getenv("TG_MEDIA_DIR")
	if mediaDir == "" {
		mediaDir = "/app/data/tg_media"
	}
	probeMode := monitor.ProbeModeCLI
	if strings.EqualFold(os.Getenv("AUM_PROBE_MODE"), monitor.ProbeModeHTTP) {
		probeMode = monitor.ProbeModeHTTP
	}
	return &Service{Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}, ProbeMode: probeMode}, TGMediaDir: mediaDir}
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
				_ = s.RefreshTGMessagesDue(context.Background())
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
		return domain.User{}, ErrBadRequest("username and password are required")
	}
	n, err := s.Store.UserCount(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if n > 0 {
		return domain.User{}, ErrBadRequest("setup already completed")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	u, err := s.Store.CreateInitialUser(ctx, username, string(hash))
	if err != nil {
		if errors.Is(err, store.ErrInitialUserExists) {
			return domain.User{}, ErrBadRequest("setup already completed")
		}
		if n, countErr := s.Store.UserCount(ctx); countErr == nil && n > 0 {
			return domain.User{}, ErrBadRequest("setup already completed")
		}
	}
	return u, err
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
		return domain.Upstream{}, ErrBadRequest("name, type and base_url are required")
	}
	if in.Type != "newapi" && in.Type != "sub2api" {
		return domain.Upstream{}, ErrBadRequest("type must be newapi or sub2api")
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

func (s *Service) BalanceRechargeCapabilities(ctx context.Context, upstreamID string) (monitor.RechargeCapabilities, error) {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return monitor.RechargeCapabilities{}, err
	}
	mu := toMonitorUpstream(u)
	out, err := s.Client.RechargeCapabilities(ctx, &mu)
	if saveErr := s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken); saveErr != nil && err == nil {
		err = saveErr
	}
	return out, err
}

func (s *Service) RedeemBalance(ctx context.Context, upstreamID, code string) (monitor.RechargeOrderResult, error) {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return monitor.RechargeOrderResult{}, err
	}
	mu := toMonitorUpstream(u)
	out, err := s.Client.Redeem(ctx, &mu, code)
	_ = s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
	status, msg := rechargeStatus(err, out)
	_, logErr := s.Store.SaveBalanceRechargeLog(ctx, domain.BalanceRechargeLog{
		UpstreamID: u.ID, Method: "redeem", PaymentType: "code:" + store.HashToken(code)[:12], Status: status, Message: msg,
	})
	if err == nil && logErr != nil {
		err = logErr
	}
	if err == nil {
		_ = s.CheckUpstream(ctx, u.ID)
	}
	return out, err
}

func (s *Service) CreateBalanceRechargeOrder(ctx context.Context, upstreamID string, req monitor.RechargeOrderRequest) (monitor.RechargeOrderResult, error) {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return monitor.RechargeOrderResult{}, err
	}
	mu := toMonitorUpstream(u)
	out, err := s.Client.CreateRechargeOrder(ctx, &mu, req)
	_ = s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
	status, msg := rechargeOrderStatus(err, out)
	_, logErr := s.Store.SaveBalanceRechargeLog(ctx, domain.BalanceRechargeLog{
		UpstreamID: u.ID, Method: "order", Amount: req.Amount, PaymentType: req.PaymentType,
		RemoteOrderID: out.RemoteOrderID, Status: status, Message: msg, RawStatus: out.Status,
	})
	if err == nil && logErr != nil {
		err = logErr
	}
	return out, err
}

func (s *Service) BalanceRechargeLogs(ctx context.Context, upstreamID string) ([]domain.BalanceRechargeLog, error) {
	if _, err := s.Store.Upstream(ctx, upstreamID); err != nil {
		return nil, err
	}
	return s.Store.BalanceRechargeLogs(ctx, upstreamID, 50)
}

func (s *Service) RefreshBalanceRechargeLog(ctx context.Context, upstreamID, logID string) (domain.BalanceRechargeLog, error) {
	u, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return domain.BalanceRechargeLog{}, err
	}
	log, err := s.Store.BalanceRechargeLog(ctx, upstreamID, logID)
	if err != nil {
		return domain.BalanceRechargeLog{}, err
	}
	if log.Method != "order" || strings.TrimSpace(log.RemoteOrderID) == "" {
		return log, ErrBadRequest("该记录没有可刷新的订单号")
	}
	mu := toMonitorUpstream(u)
	out, err := s.Client.RefreshRechargeOrder(ctx, &mu, log.RemoteOrderID)
	_ = s.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
	if err != nil {
		log.Message = err.Error()
		if saveErr := s.Store.UpdateBalanceRechargeLog(ctx, log); saveErr != nil {
			return log, saveErr
		}
		return log, err
	}
	log.Status, log.Message = rechargeOrderStatus(nil, out)
	log.RawStatus = out.Status
	if err := s.Store.UpdateBalanceRechargeLog(ctx, log); err != nil {
		return log, err
	}
	if log.Status == "success" {
		_ = s.CheckUpstream(ctx, u.ID)
	}
	return log, nil
}

func (s *Service) DeleteBalanceRechargeLog(ctx context.Context, upstreamID, logID string) error {
	if _, err := s.Store.Upstream(ctx, upstreamID); err != nil {
		return err
	}
	return s.Store.DeleteBalanceRechargeLog(ctx, upstreamID, logID)
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
		failures := u.FailureCount + 1
		_ = s.Store.SaveUpstreamError(ctx, u.ID, err.Error(), failures)
		failing := failures >= s.alertFailureThreshold(ctx)
		if monitor.IsAuthError(err) {
			_ = s.alert(ctx, u, "credential", failing, u.Name+" 凭据失效: "+err.Error())
		} else {
			_ = s.alert(ctx, u, "balance_query", failing, u.Name+" 额度查询失败: "+err.Error())
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
	_ = s.alert(ctx, u, "balance_query", false, u.Name+" 额度查询已恢复")
	return s.alert(ctx, u, "balance", domain.LowBalance(u, snap), fmt.Sprintf("%s 余额低于阈值", u.Name))
}

func (s *Service) SaveCard(ctx context.Context, id string, in domain.ModelCard) (domain.ModelCard, error) {
	card, err := s.normalizeCard(ctx, in)
	if err != nil {
		return domain.ModelCard{}, err
	}
	if card.SchedulerChannelID != "" {
		cards, err := s.Store.ListCards(ctx)
		if err != nil {
			return domain.ModelCard{}, err
		}
		for _, item := range cards {
			if item.ID != id && item.SchedulerChannelID == card.SchedulerChannelID {
				return domain.ModelCard{}, ErrBadRequest("scheduler channel already bound to another card")
			}
		}
	}
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
	card.SortOrder = old.SortOrder
	card.CreatedAt = old.CreatedAt
	return s.Store.UpdateCard(ctx, card)
}

func (s *Service) normalizeCard(ctx context.Context, in domain.ModelCard) (domain.ModelCard, error) {
	card := domain.ModelCard{
		Name:                  strings.TrimSpace(in.Name),
		BaseURL:               strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		APIKey:                strings.TrimSpace(in.APIKey),
		UpstreamID:            strings.TrimSpace(in.UpstreamID),
		KeyID:                 strings.TrimSpace(in.KeyID),
		Model:                 domain.ProbeModel,
		DisplayGroup:          strings.TrimSpace(in.DisplayGroup),
		SchedulerGroup:        strings.TrimSpace(in.SchedulerGroup),
		SchedulerChannelID:    strings.TrimSpace(in.SchedulerChannelID),
		SchedulerChannelName:  strings.TrimSpace(in.SchedulerChannelName),
		SchedulerAutoDisabled: in.SchedulerAutoDisabled,
		Enabled:               in.Enabled,
		PublicEnabled:         in.PublicEnabled,
		SortOrder:             in.SortOrder,
	}
	custom := card.BaseURL != "" || card.APIKey != ""
	if custom {
		if card.Name == "" || card.BaseURL == "" || card.APIKey == "" {
			return card, ErrBadRequest("name, base_url and api_key are required")
		}
		card.UpstreamID, card.KeyID = "", ""
		return card, nil
	}
	if card.UpstreamID == "" || card.KeyID == "" {
		return card, ErrBadRequest("upstream_id and key_id are required")
	}
	u, err := s.Store.Upstream(ctx, card.UpstreamID)
	if err != nil {
		return card, err
	}
	k, err := s.Store.Key(ctx, card.KeyID)
	if err != nil {
		return card, err
	}
	if k.UpstreamID != card.UpstreamID {
		return card, ErrBadRequest("key does not belong to upstream")
	}
	if card.Name == "" {
		card.Name = domain.CardName(u, &k)
	}
	return card, nil
}

func (s *Service) SortCards(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return ErrBadRequest("ids are required")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return ErrBadRequest("card id is required")
		}
		if _, ok := seen[id]; ok {
			return ErrBadRequest("duplicate card id")
		}
		seen[id] = struct{}{}
	}
	return s.Store.UpdateCardOrder(ctx, ids)
}

func (s *Service) CheckCard(ctx context.Context, cardID string) error {
	card, err := s.Store.Card(ctx, cardID)
	if err != nil {
		return err
	}
	if card.BaseURL != "" {
		return s.checkCustomCard(ctx, card)
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
		failures := card.FailureCount + 1
		if err := s.Store.UpdateCardProbeState(ctx, card.ID, msg, failures); err != nil {
			return err
		}
		return s.applySchedulerAutomation(ctx, card, false, failures)
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
	_ = s.applySchedulerAutomation(ctx, card, probe.Success, failures)
	return s.alert(ctx, u, "ping:"+card.ID, !probe.Success && failures >= s.alertFailureThreshold(ctx), card.Name+" 探测失败: "+probe.Error)
}

func (s *Service) checkCustomCard(ctx context.Context, card domain.ModelCard) error {
	if card.APIKey == "" {
		msg := "未填写 Key"
		if _, err := s.Store.SaveProbe(ctx, "", card.ID, monitor.ProbeResult{Status: monitor.StatusFailed, Error: msg}); err != nil {
			return err
		}
		failures := card.FailureCount + 1
		if err := s.Store.UpdateCardProbeState(ctx, card.ID, msg, failures); err != nil {
			return err
		}
		return s.applySchedulerAutomation(ctx, card, false, failures)
	}
	probe := s.Client.Probe(ctx, card.BaseURL, card.APIKey, domain.ProbeModel)
	if _, err := s.Store.SaveProbe(ctx, "", card.ID, probe); err != nil {
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
	if !probe.Success && failures >= s.alertFailureThreshold(ctx) {
		_, _ = s.Store.CreateOpsEvent(ctx, domain.OpsEvent{
			Type: "probe_failed", Severity: "warning", Title: "探测失败", Message: card.Name + " 探测失败: " + probe.Error,
			TargetType: "card", TargetID: card.ID, Actions: []string{"check_card"},
		})
	}
	return s.applySchedulerAutomation(ctx, card, probe.Success, failures)
}

func rechargeStatus(err error, out monitor.RechargeOrderResult) (string, string) {
	if err != nil {
		return "failed", err.Error()
	}
	return "success", nonEmptyText(out.Message, out.ResultType)
}

func rechargeOrderStatus(err error, out monitor.RechargeOrderResult) (string, string) {
	if err != nil {
		return "failed", err.Error()
	}
	status := strings.ToLower(strings.TrimSpace(out.Status))
	switch status {
	case "success", "paid", "completed":
		return "success", nonEmptyText(out.Status, out.Message)
	case "failed", "expired", "cancelled", "canceled", "refund_failed":
		return "failed", nonEmptyText(out.Status, out.Message)
	case "pending", "recharging", "processing", "paying", "":
		return "pending", nonEmptyText(out.Status, "pending")
	default:
		return "pending", nonEmptyText(out.Status, out.Message)
	}
}

func nonEmptyText(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func (s *Service) MonitorStatus(ctx context.Context, window string) (map[string]any, error) {
	return s.monitorStatus(ctx, window, false)
}

func (s *Service) PublicMonitorStatus(ctx context.Context, window string) (map[string]any, error) {
	return s.monitorStatus(ctx, window, true)
}

func (s *Service) monitorStatus(ctx context.Context, window string, publicOnly bool) (map[string]any, error) {
	since, label, _ := windowSince(window)
	cards, err := s.enrichedCards(ctx, since, 0)
	if err != nil {
		return nil, err
	}
	if publicOnly {
		public := publicCards(cards)
		total, ok, failed, latency, samples := statusSummary(public)
		return map[string]any{
			"window": label, "rows": public, "requests": total, "success": ok, "failed": failed,
			"success_rate": percent(ok, total), "avg_latency": avg(latency, samples),
		}, nil
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

func statusSummary(cards []domain.PublicModelCard) (total, ok, failed, latency, samples int) {
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
	return total, ok, failed, latency, samples
}

func publicCards(cards []domain.ModelCard) []domain.PublicModelCard {
	out := []domain.PublicModelCard{}
	for _, c := range cards {
		if !c.PublicEnabled {
			continue
		}
		card := domain.PublicModelCard{Name: c.Name, DisplayGroup: c.DisplayGroup}
		for i := range c.History {
			p := c.History[i]
			p.Status = probeStatusLabel(p.Status)
			p.Error = publicProbeError(p)
			card.History = append(card.History, domain.PublicProbeRun{
				CheckedAt: p.CheckedAt, Status: p.Status, Input: p.Input, ExpectedAnswer: p.ExpectedAnswer,
				Output: p.Output, HTTPStatus: p.HTTPStatus, LatencyMS: p.LatencyMS, Success: p.Success, Error: p.Error,
			})
		}
		card.LastError = publicLastError(card)
		out = append(out, card)
	}
	return out
}

func publicLastError(c domain.PublicModelCard) string {
	if len(c.History) == 0 || c.History[len(c.History)-1].Success {
		return ""
	}
	return c.History[len(c.History)-1].Error
}

func publicProbeError(p domain.ProbeRun) string {
	switch p.Status {
	case monitor.StatusValidationFailed, "验证失败":
		return "验证失败"
	case monitor.StatusError, "探测异常":
		return "探测异常"
	case monitor.StatusFailed, "请求失败":
		if p.HTTPStatus > 0 {
			return fmt.Sprintf("HTTP %d", p.HTTPStatus)
		}
		return "请求失败"
	default:
		return ""
	}
}

func probeStatusLabel(status string) string {
	switch status {
	case monitor.StatusOperational:
		return "正常"
	case monitor.StatusDegraded:
		return "延迟偏高"
	case monitor.StatusValidationFailed:
		return "验证失败"
	case monitor.StatusFailed:
		return "请求失败"
	case monitor.StatusError:
		return "探测异常"
	default:
		return status
	}
}

func (s *Service) ListCards(ctx context.Context) ([]domain.ModelCard, error) {
	return s.enrichedCards(ctx, time.Now().Add(-time.Hour), 60)
}

func (s *Service) enrichedCards(ctx context.Context, since time.Time, probeLimit int) ([]domain.ModelCard, error) {
	cards, err := s.Store.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		var u domain.Upstream
		if cards[i].UpstreamID != "" {
			var err error
			u, err = s.Store.Upstream(ctx, cards[i].UpstreamID)
			if err == nil {
				cards[i].UpstreamName = u.Name
				cards[i].Type = u.Type
			}
		}
		if cards[i].KeyID != "" {
			if k, err := s.Store.Key(ctx, cards[i].KeyID); err == nil {
				cards[i].KeyName = k.Name
				cards[i].KeyGroup = k.Group
				cards[i].KeyRatio = k.GroupRatio
				cards[i].EffectiveRatio = effectiveRatio(k.GroupRatio, domain.BalanceRate(u))
			}
		}
		history, err := s.Store.ProbesForCardSince(ctx, cards[i].ID, since, probeLimit)
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

func (s *Service) SaveRevenueCard(ctx context.Context, id string, in domain.RevenueCard) (domain.RevenueCard, error) {
	card, err := s.normalizeRevenueCard(ctx, in)
	if err != nil {
		return domain.RevenueCard{}, err
	}
	if id == "" {
		return s.Store.CreateRevenueCard(ctx, card)
	}
	old, err := s.Store.RevenueCard(ctx, id)
	if err != nil {
		return domain.RevenueCard{}, err
	}
	card.ID = old.ID
	card.SortOrder = old.SortOrder
	card.CreatedAt = old.CreatedAt
	return s.Store.UpdateRevenueCard(ctx, card)
}

func (s *Service) normalizeRevenueCard(ctx context.Context, in domain.RevenueCard) (domain.RevenueCard, error) {
	card := domain.RevenueCard{
		Name:        strings.TrimSpace(in.Name),
		SourceType:  strings.TrimSpace(in.SourceType),
		BaseURL:     strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		UserID:      strings.TrimSpace(in.UserID),
		AccessToken: strings.TrimSpace(in.AccessToken),
		AdminAPIKey: strings.TrimSpace(in.AdminAPIKey),
		EpayPID:     strings.TrimSpace(in.EpayPID),
		EpayKey:     strings.TrimSpace(in.EpayKey),
		UpstreamID:  strings.TrimSpace(in.UpstreamID),
		Enabled:     in.Enabled,
		SortOrder:   in.SortOrder,
	}
	switch card.SourceType {
	case "epay_total":
		if card.Name == "" {
			card.Name = "今日收入"
		}
		card.UpstreamID = ""
		if card.BaseURL == "" || card.EpayPID == "" || card.EpayKey == "" {
			cfg, _ := s.Store.Settings(ctx)
			if card.BaseURL == "" {
				card.BaseURL = cfg.EpayBaseURL
			}
			if card.EpayPID == "" {
				card.EpayPID = cfg.EpayPID
			}
			if card.EpayKey == "" {
				card.EpayKey = cfg.EpayKey
			}
		}
		if card.BaseURL == "" || card.EpayPID == "" || card.EpayKey == "" {
			return card, ErrBadRequest("易支付 Base URL、PID、Key 是必填")
		}
	case "newapi_orders", "sub2api_orders":
		want := strings.TrimSuffix(card.SourceType, "_orders")
		if card.UpstreamID != "" && card.BaseURL == "" {
			u, err := s.Store.Upstream(ctx, card.UpstreamID)
			if err != nil {
				return card, err
			}
			if u.Type != want {
				return card, ErrBadRequest("upstream type does not match revenue card")
			}
			if card.Name == "" {
				card.Name = u.Name
			}
		}
		if card.BaseURL == "" && card.UpstreamID == "" {
			return card, ErrBadRequest("Base URL 是必填")
		}
		if want == "newapi" && card.UpstreamID == "" && (card.UserID == "" || card.AccessToken == "") {
			return card, ErrBadRequest("new-api 用户 ID 和 Access Token 是必填")
		}
		if want == "sub2api" && card.AdminAPIKey == "" && card.UpstreamID == "" {
			return card, ErrBadRequest("sub2api 管理员 API Key 是必填")
		}
		if card.Name == "" {
			card.Name = sourceTypeLabel(card.SourceType)
		}
	default:
		return card, ErrBadRequest("source_type must be epay_total, newapi_orders or sub2api_orders")
	}
	return card, nil
}

func (s *Service) ListRevenueCards(ctx context.Context) ([]domain.RevenueCard, error) {
	cards, err := s.Store.ListRevenueCards(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichRevenueCards(ctx, cards), nil
}

func (s *Service) TodayRevenue(ctx context.Context) ([]domain.RevenueRow, error) {
	cards, err := s.Store.ListRevenueCards(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	cards = s.enrichRevenueCards(ctx, cards)
	out := []domain.RevenueRow{}
	start := todayStart()
	for _, card := range cards {
		row := domain.RevenueRow{RevenueCard: card}
		if !card.Enabled {
			out = append(out, row)
			continue
		}
		row.CheckedAt = time.Now().UTC()
		switch card.SourceType {
		case "epay_total":
			balance := (epay.Client{HTTP: s.Client.HTTP}).MerchantBalance(ctx, epay.Config{BaseURL: firstNonEmpty(card.BaseURL, cfg.EpayBaseURL), PID: firstNonEmpty(card.EpayPID, cfg.EpayPID), Key: firstNonEmpty(card.EpayKey, cfg.EpayKey)})
			row.Revenue, row.CheckedAt, row.Error = balance.Balance, balance.CheckedAt, balance.Error
			if row.Error != "" {
				row.Revenue = 0
			}
		case "newapi_orders", "sub2api_orders":
			mu, upstreamID, err := s.revenueMonitorUpstream(ctx, card)
			if err != nil {
				row.Error = err.Error()
				break
			}
			total, err := s.Client.TodayOrderRevenue(ctx, &mu, start)
			if upstreamID != "" {
				_ = s.Store.SaveUpstreamTokens(ctx, upstreamID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
			}
			row.Revenue, row.CheckedAt = total.Revenue, total.CheckedAt
			if err != nil {
				row.Revenue = 0
				row.Error = err.Error()
			}
		default:
			row.Error = "unsupported revenue card type"
		}
		_ = s.Store.SaveRevenueSnapshot(ctx, domain.RevenueSnapshot{
			SourceID: card.ID, SourceName: card.Name, SourceType: card.SourceType,
			CheckedAt: row.CheckedAt, Revenue: row.Revenue, Error: row.Error,
		})
		out = append(out, row)
	}
	return out, nil
}

func (s *Service) RevenueCardOrders(ctx context.Context, id string) ([]monitor.RevenueOrder, error) {
	card, err := s.Store.RevenueCard(ctx, id)
	if err != nil {
		return nil, err
	}
	if !card.Enabled {
		return []monitor.RevenueOrder{}, nil
	}
	if card.SourceType == "epay_total" {
		cfg, err := s.Store.Settings(ctx)
		if err != nil {
			return nil, err
		}
		orders, err := (epay.Client{HTTP: s.Client.HTTP}).TodayOrders(ctx, epay.Config{BaseURL: firstNonEmpty(card.BaseURL, cfg.EpayBaseURL), PID: firstNonEmpty(card.EpayPID, cfg.EpayPID), Key: firstNonEmpty(card.EpayKey, cfg.EpayKey)}, todayStart())
		out := make([]monitor.RevenueOrder, 0, len(orders))
		for _, order := range orders {
			out = append(out, monitor.RevenueOrder{
				RemoteID:    order.RemoteID,
				Amount:      order.Amount,
				Status:      order.Status,
				PaymentType: order.PaymentType,
				PaidAt:      order.PaidAt,
			})
		}
		return out, err
	}
	mu, upstreamID, err := s.revenueMonitorUpstream(ctx, card)
	if err != nil {
		return nil, err
	}
	orders, err := s.Client.TodayRevenueOrders(ctx, &mu, todayStart())
	if upstreamID != "" {
		_ = s.Store.SaveUpstreamTokens(ctx, upstreamID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
	}
	return orders, err
}

func (s *Service) SortRevenueCards(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return ErrBadRequest("ids are required")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return ErrBadRequest("card id is required")
		}
		if _, ok := seen[id]; ok {
			return ErrBadRequest("duplicate card id")
		}
		seen[id] = struct{}{}
	}
	return s.Store.UpdateRevenueCardOrder(ctx, ids)
}

func (s *Service) enrichRevenueCards(ctx context.Context, cards []domain.RevenueCard) []domain.RevenueCard {
	cfg, _ := s.Store.Settings(ctx)
	for i := range cards {
		if cards[i].SourceType == "epay_total" {
			if cards[i].BaseURL == "" {
				cards[i].BaseURL = cfg.EpayBaseURL
			}
			if cards[i].EpayPID == "" {
				cards[i].EpayPID = cfg.EpayPID
			}
			if cards[i].EpayKey == "" {
				cards[i].EpayKey = cfg.EpayKey
			}
		}
		if cards[i].UpstreamID != "" {
			if u, err := s.Store.Upstream(ctx, cards[i].UpstreamID); err == nil {
				cards[i].UpstreamName = u.Name
			}
		}
	}
	return cards
}

func (s *Service) revenueMonitorUpstream(ctx context.Context, card domain.RevenueCard) (monitor.Upstream, string, error) {
	typ := strings.TrimSuffix(card.SourceType, "_orders")
	if card.UpstreamID != "" && card.BaseURL == "" {
		u, err := s.Store.Upstream(ctx, card.UpstreamID)
		if err != nil {
			return monitor.Upstream{}, "", err
		}
		return toMonitorUpstream(u), u.ID, nil
	}
	if card.BaseURL == "" {
		return monitor.Upstream{}, "", ErrBadRequest("Base URL 是必填")
	}
	return monitor.Upstream{
		Name:        card.Name,
		Type:        typ,
		BaseURL:     card.BaseURL,
		UserID:      card.UserID,
		AccessToken: card.AccessToken,
		AdminAPIKey: card.AdminAPIKey,
	}, "", nil
}

func sourceTypeLabel(sourceType string) string {
	switch sourceType {
	case "newapi_orders":
		return "new-api 订单"
	case "sub2api_orders":
		return "sub2api 订单"
	default:
		return "今日收入"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func todayStart() time.Time {
	now := time.Now()
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
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
	s.createAlertOpsEvent(ctx, u, kind, dec.Recover, dec.Message)
	rules, err := s.Store.NotificationRules(ctx)
	if err != nil {
		return err
	}
	eventType, _, _ := alertOpsType(kind, dec.Recover)
	shouldSend := rules.Enabled && rules.EventTypes[eventType] && (!dec.Recover || rules.Recovery)
	sent := false
	if shouldSend {
		sent = s.sendTelegram(ctx, dec.Message) == nil
	}
	return s.Store.SaveAlert(ctx, u.ID, dec, sent)
}

func (s *Service) alertFailureThreshold(ctx context.Context) int {
	rules, err := s.Store.NotificationRules(ctx)
	if err != nil || rules.FailureThreshold <= 0 {
		return 2
	}
	return rules.FailureThreshold
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
	hc := s.Client.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
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
