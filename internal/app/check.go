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
	var enabledUpstreams []domain.Upstream
	for _, u := range upstreams {
		if u.Enabled {
			enabledUpstreams = append(enabledUpstreams, u)
		}
	}
	upOK, upFail := s.runLimited(ctx, len(enabledUpstreams), checkConcurrency, func(i int) error {
		return s.checkUpstream(ctx, enabledUpstreams[i].ID, false)
	})
	if len(enabledUpstreams) > 0 {
		s.syncSchedulerGroupsBestEffort(ctx)
	}
	cards, err := s.Store.ListCards(ctx)
	if err != nil {
		return err
	}
	var enabledCards []domain.ModelCard
	for _, c := range cards {
		if c.Enabled {
			enabledCards = append(enabledCards, c)
		}
	}
	cardOK, cardFail := s.runLimited(ctx, len(enabledCards), checkConcurrency, func(i int) error {
		return s.CheckCard(ctx, enabledCards[i].ID)
	})
	log.Printf("scheduler: check finished upstreams ok=%d fail=%d cards ok=%d fail=%d", upOK, upFail, cardOK, cardFail)
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	return nil
}

// runLimited executes n tasks with at most limit concurrent workers.
// Returns success/failure counts. Individual task errors are logged, not returned.
func (s *Service) runLimited(ctx context.Context, n, limit int, fn func(i int) error) (ok, fail int) {
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
					log.Printf("scheduler: task %d failed: %v", i, err)
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

func (s *Service) CheckUpstream(ctx context.Context, upstreamID string) error {
	return s.checkUpstream(ctx, upstreamID, true)
}

func (s *Service) checkUpstream(ctx context.Context, upstreamID string, syncGroups bool) error {
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
	if err := s.recordCurrentCostSnapshots(ctx); err != nil {
		return err
	}
	if syncGroups {
		s.syncSchedulerGroupsBestEffort(ctx)
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
