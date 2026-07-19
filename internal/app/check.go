package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

func (s *ProbeService) CheckDue(ctx context.Context) error {
	cfg, err := s.app.Store.Settings(ctx)
	if err != nil {
		return err
	}
	interval := time.Duration(cfg.CheckIntervalMinutes) * time.Minute
	s.app.mu.Lock()
	if s.app.running || (!s.app.lastRun.IsZero() && time.Since(s.app.lastRun) < interval) {
		s.app.mu.Unlock()
		return nil
	}
	s.app.running = true
	s.app.lastRun = time.Now()
	s.app.mu.Unlock()
	defer func() {
		s.app.mu.Lock()
		s.app.running = false
		s.app.mu.Unlock()
	}()
	return s.CheckAll(ctx)
}

func (s *ProbeService) CheckAll(ctx context.Context) error {
	batch := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	upstreams, err := s.app.Store.ListUpstreams(ctx)
	if err != nil {
		return err
	}
	var enabledUpstreams []domain.Upstream
	for _, u := range upstreams {
		if u.Enabled {
			enabledUpstreams = append(enabledUpstreams, u)
		}
	}
	upOK, upFail := s.runLimited(ctx, len(enabledUpstreams), checkConcurrency, func(i int) error {
		u := enabledUpstreams[i]
		if err := s.checkUpstream(ctx, u.ID, false); err != nil {
			return fmt.Errorf("batch=%s upstream_id=%s upstream=%q: %w", batch, u.ID, u.Name, err)
		}
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(enabledUpstreams) > 0 {
		if err := s.app.ReconcileAvailability(ctx); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
			log.Printf("scheduler: availability reconcile after balance refresh: %v", err)
		}
		s.app.syncSchedulerGroupsBestEffort(ctx)
	}
	cards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	var enabledCards []domain.ModelCard
	for _, c := range cards {
		if c.Enabled {
			enabledCards = append(enabledCards, c)
		}
	}
	cardProcessed, cardInternalErrors := s.runLimited(ctx, len(enabledCards), checkConcurrency, func(i int) error {
		card := enabledCards[i]
		if err := s.CheckCard(ctx, card.ID); err != nil {
			return fmt.Errorf("batch=%s card_id=%s card=%q upstream_id=%s: %w", batch, card.ID, card.Name, card.UpstreamID, err)
		}
		return nil
	})
	latestCards, err := s.app.Cards.ListCards(ctx)
	if err != nil {
		return err
	}
	cardProbeOK, cardProbeFailed := cardProbeStateCounts(latestCards)
	log.Printf("scheduler: check finished batch=%s upstreams_ok=%d upstreams_fail=%d cards_processed=%d cards_internal_errors=%d cards_probe_ok=%d cards_probe_failed=%d", batch, upOK, upFail, cardProcessed, cardInternalErrors, cardProbeOK, cardProbeFailed)
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	return nil
}

func cardProbeStateCounts(cards []domain.ModelCard) (ok, failed int) {
	for _, card := range cards {
		if !card.Enabled {
			continue
		}
		if card.FailureCount > 0 || card.LastError != "" {
			failed++
		} else {
			ok++
		}
	}
	return ok, failed
}

// runLimited 以最多 limit 个并发 worker 执行 n 个任务。
// 返回成功/失败计数。单个任务错误只记日志，不向上返回。
func (s *ProbeService) runLimited(ctx context.Context, n, limit int, fn func(i int) error) (ok, fail int) {
	if n == 0 {
		return 0, 0
	}
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			mu.Lock()
			fail += n - i
			mu.Unlock()
			break
		}
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				fail++
				mu.Unlock()
				return
			}
			if err := fn(i); err != nil {
				mu.Lock()
				fail++
				mu.Unlock()
				if !errors.Is(err, context.Canceled) {
					log.Printf("scheduler: check failed: %v", err)
				}
				return
			}
			mu.Lock()
			ok++
			mu.Unlock()
		}()
	}
	wg.Wait()
	return ok, fail
}

func (s *ProbeService) CheckUpstream(ctx context.Context, upstreamID string) error {
	return s.checkUpstream(ctx, upstreamID, true)
}

func (s *ProbeService) checkUpstream(ctx context.Context, upstreamID string, syncGroups bool) error {
	u, err := s.app.Store.Upstream(ctx, upstreamID)
	if err != nil {
		return err
	}
	start := time.Now()
	mu := toMonitorUpstream(u)
	result, err := s.app.Client.Check(ctx, &mu, "", "")
	if err != nil {
		failures := u.FailureCount + 1
		_ = s.app.Store.SaveUpstreamError(ctx, u.ID, err.Error(), failures)
		failing := domain.UpstreamAlerting(failures, s.app.alertFailureThreshold(ctx))
		if monitor.IsAuthError(err) {
			_ = s.app.alert(ctx, u, "credential", failing, u.Name+" 凭据失效: "+err.Error())
		} else {
			_ = s.app.alert(ctx, u, "balance_query", failing, u.Name+" 额度查询失败: "+err.Error())
		}
		return err
	}
	if err := s.app.Store.SaveUpstreamTokens(ctx, u.ID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken); err != nil {
		return err
	}
	if err := s.app.Store.SaveKeys(ctx, u.ID, result.Keys); err != nil {
		return err
	}
	if err := s.app.recordCurrentCostSnapshots(ctx); err != nil {
		return err
	}
	if syncGroups {
		s.app.syncSchedulerGroupsBestEffort(ctx)
	}
	snap, err := s.app.Store.SaveBalance(ctx, u.ID, result.Balance, "", int(time.Since(start).Milliseconds()))
	if err != nil {
		return err
	}
	_ = s.app.Store.SaveUpstreamError(ctx, u.ID, "", 0)
	_ = s.app.alert(ctx, u, "credential", false, u.Name+" 凭据已恢复")
	_ = s.app.alert(ctx, u, "balance_query", false, u.Name+" 额度查询已恢复")
	if err := s.app.ReconcileAvailability(ctx); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		log.Printf("scheduler: availability reconcile upstream_id=%s: %v", u.ID, err)
	}
	return s.app.alert(ctx, u, "balance", domain.LowBalance(u, snap), fmt.Sprintf("%s 余额低于阈值", u.Name))
}
