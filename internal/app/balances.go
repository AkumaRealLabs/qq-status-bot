package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

const pendingRechargeRefreshBatchSize = 100
const balanceRefreshConcurrency = 3

func (s *Service) RefreshBalances(ctx context.Context) (domain.BalanceRefreshResult, error) {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return domain.BalanceRefreshResult{}, err
	}
	enabled := make([]domain.Upstream, 0, len(upstreams))
	for _, upstream := range upstreams {
		if upstream.Enabled {
			enabled = append(enabled, upstream)
		}
	}
	out := domain.BalanceRefreshResult{Total: len(enabled), Results: make([]domain.BalanceRefreshItem, len(enabled))}
	var wg sync.WaitGroup
	sem := make(chan struct{}, balanceRefreshConcurrency)
	for i, upstream := range enabled {
		i, upstream := i, upstream
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := domain.BalanceRefreshItem{UpstreamID: upstream.ID, UpstreamName: upstream.Name}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				item.Error = ctx.Err().Error()
				out.Results[i] = item
				return
			}
			if err := s.refreshUpstreamBalance(ctx, upstream); err != nil {
				item.Error = err.Error()
			} else {
				item.Success = true
			}
			out.Results[i] = item
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	for _, item := range out.Results {
		if item.Success {
			out.Succeeded++
		} else {
			out.Failed++
		}
	}
	if err := s.Scheduler.recordCurrentCostSnapshots(ctx); err != nil {
		return out, err
	}
	s.syncSchedulerGroupsBestEffort(ctx)
	return out, nil
}

func (s *Service) RefreshUpstreamBalance(ctx context.Context, upstreamID string) error {
	upstream, err := s.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return err
	}
	if err := s.refreshUpstreamBalance(ctx, upstream); err != nil {
		return err
	}
	if err := s.Scheduler.recordCurrentCostSnapshots(ctx); err != nil {
		return err
	}
	s.syncSchedulerGroupsBestEffort(ctx)
	return nil
}

func (s *Service) refreshUpstreamBalance(ctx context.Context, upstream domain.Upstream) error {
	start := time.Now()
	remote := toMonitorUpstream(upstream)
	result, err := s.Client.Check(ctx, &remote)
	if err != nil {
		failures := upstream.FailureCount + 1
		_ = s.Store.SaveUpstreamError(ctx, upstream.ID, err.Error(), failures)
		kind := "balance_query"
		message := upstream.Name + " 额度查询失败: " + err.Error()
		if monitor.IsAuthError(err) {
			kind, message = "credential", upstream.Name+" 凭据失效: "+err.Error()
		}
		_ = s.alert(ctx, upstream, kind, domain.UpstreamAlerting(failures, s.alertFailureThreshold(ctx)), message)
		return err
	}
	if err := s.Store.SaveUpstreamTokens(ctx, upstream.ID, remote.Sub2APIAccessToken, remote.Sub2APIRefreshToken); err != nil {
		return err
	}
	if err := s.Store.SaveKeys(ctx, upstream.ID, result.Keys); err != nil {
		return err
	}
	snapshot, err := s.Store.SaveBalance(ctx, upstream.ID, result.Balance, "", int(time.Since(start).Milliseconds()))
	if err != nil {
		return err
	}
	_ = s.Store.SaveUpstreamError(ctx, upstream.ID, "", 0)
	_ = s.alert(ctx, upstream, "credential", false, upstream.Name+" 凭据已恢复")
	_ = s.alert(ctx, upstream, "balance_query", false, upstream.Name+" 额度查询已恢复")
	return s.alert(ctx, upstream, "balance", domain.LowBalance(upstream, snapshot), fmt.Sprintf("%s 余额低于阈值", upstream.Name))
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
		_ = s.RefreshUpstreamBalance(ctx, u.ID)
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
	return s.refreshBalanceRechargeLog(ctx, upstreamID, logID, true)
}

func (s *Service) refreshBalanceRechargeLog(ctx context.Context, upstreamID, logID string, refreshBalance bool) (domain.BalanceRechargeLog, error) {
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
	if refreshBalance && log.Status == "success" {
		_ = s.RefreshUpstreamBalance(ctx, u.ID)
	}
	return log, nil
}

// RefreshPendingBalanceRechargeLogs 定期核验未完成的充值订单；单条失败不阻塞其余订单。
func (s *Service) RefreshPendingBalanceRechargeLogs(ctx context.Context) error {
	logs, err := s.Store.PendingBalanceRechargeLogs(ctx, pendingRechargeRefreshBatchSize)
	if err != nil {
		return err
	}
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]domain.Upstream, len(upstreams))
	for _, upstream := range upstreams {
		byID[upstream.ID] = upstream
	}
	var firstErr error
	refreshUpstreams := map[string]domain.Upstream{}
	for _, recharge := range logs {
		if err := ctx.Err(); err != nil {
			return err
		}
		upstream, ok := byID[recharge.UpstreamID]
		if !ok || !upstream.Enabled {
			continue
		}
		updated, err := s.refreshBalanceRechargeLog(ctx, recharge.UpstreamID, recharge.ID, false)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("scheduler: pending recharge refresh upstream_id=%s upstream=%q recharge_id=%s: %v", upstream.ID, upstream.Name, recharge.ID, err)
			continue
		}
		if updated.Status == "success" {
			refreshUpstreams[upstream.ID] = upstream
		}
	}
	for upstreamID, upstream := range refreshUpstreams {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.RefreshUpstreamBalance(ctx, upstreamID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("scheduler: pending recharge balance refresh upstream_id=%s upstream=%q: %v", upstream.ID, upstream.Name, err)
		}
	}
	if firstErr != nil {
		return fmt.Errorf("刷新待处理充值订单: %w", firstErr)
	}
	return nil
}

func (s *Service) DeleteBalanceRechargeLog(ctx context.Context, upstreamID, logID string) error {
	if _, err := s.Store.Upstream(ctx, upstreamID); err != nil {
		return err
	}
	return s.Store.DeleteBalanceRechargeLog(ctx, upstreamID, logID)
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

func (s *Service) BalanceRows(ctx context.Context) ([]map[string]any, error) {
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(upstreams))
	for _, u := range upstreams {
		row := map[string]any{
			"id": u.ID, "name": u.Name, "type": u.Type, "enabled": u.Enabled,
			"balance_rate": domain.BalanceRate(u), "low_balance_threshold": u.LowBalanceThreshold, "error": u.LastError,
		}
		if b, err := s.Store.LatestBalance(ctx, u.ID); err == nil {
			balance, used, remain := domain.ConvertedBalanceValues(u.Type, domain.BalanceRate(u), b.Balance, b.Used, b.Remain)
			sourceBalance, sourceUsed, sourceRemain := domain.NormalizedBalanceValues(u.Type, b.Balance, b.Used, b.Remain)
			row["balance"], row["used"], row["remain"] = balance, used, remain
			row["source_balance"], row["source_used"], row["source_remain"] = sourceBalance, sourceUsed, sourceRemain
			row["requests"], row["last_check"] = b.Requests, b.CheckedAt
			if u.LastError == "" {
				row["error"] = b.Error
			}
			row["low_balance"] = domain.LowBalance(u, b)
		}
		out = append(out, row)
	}
	return out, nil
}
