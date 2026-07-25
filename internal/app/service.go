package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"
	_ "time/tzdata"

	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/onebot"
	"ai-upstream-monitor/internal/store"
)

const (
	minPasswordLength = 8
	checkConcurrency  = 3

	schedulerTickInterval      = time.Minute
	schedulerGroupSyncBudget   = 45 * time.Second
	schedulerRetentionTimeout  = time.Minute
	schedulerRevenueTimeout    = 3 * time.Minute
	schedulerRechargeTimeout   = 4 * time.Minute
	schedulerRetentionInterval = time.Hour
	schedulerRevenueInterval   = 15 * time.Minute
	schedulerRechargeInterval  = 15 * time.Minute
)

type Service struct {
	Store          *store.Store
	Client         monitor.Client
	mu             sync.Mutex
	running        bool
	lastBalanceRun time.Time

	// 最小 ports（Phase 3）。默认接到 Store / Telegram / monitor.Client。
	Notify       Notifier
	OneBotClient OneBotClient

	// 限界上下文门面（同包）。对外 API 仍经 Service 方法转发。
	Scheduler *SchedulerService
	OneBot    *OneBotService
}

// SchedulerService 负责调度配置、渠道/分组应用与成本同步。
type SchedulerService struct {
	app                    *Service
	controlMu              sync.Mutex
	axonHubAuthMu          sync.Mutex
	axonHubToken           string
	axonHubTokenExpiresAt  time.Time
	axonHubTokenBaseURL    string
	axonHubTokenAdminEmail string
}

func New(st *store.Store) *Service {
	s := &Service{
		Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}},
		OneBotClient: &onebot.Client{HTTP: &http.Client{Timeout: 10 * time.Second}},
	}
	s.Notify = &telegramNotifier{send: s.sendTelegram}
	s.Scheduler = &SchedulerService{app: s}
	s.OneBot = &OneBotService{app: s}
	return s
}

func (s *Service) StartScheduler(ctx context.Context) {
	// 启动时跑一次数据保留清理，让长期闲置主机立刻腾出空间。
	retentionCtx, cancel := context.WithTimeout(ctx, schedulerRetentionTimeout)
	stats, err := s.Store.CleanupExpiredData(retentionCtx)
	cancel()
	if err != nil {
		log.Printf("scheduler: retention cleanup: %v", err)
	} else if stats.DeletedTotal() > 0 {
		log.Printf("scheduler: retention deleted %d rows (balances=%d sessions=%d)",
			stats.DeletedTotal(), stats.BalanceSnapshots, stats.ExpiredSessions)
	}
	state := schedulerState{}
	if err == nil {
		state.lastRetention = time.Now()
	}
	t := time.NewTicker(schedulerTickInterval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runSchedulerTick(ctx, time.Now(), &state)
			}
		}
	}()
}

type schedulerState struct {
	lastRetention time.Time
	lastRevenue   time.Time
	lastRecharge  time.Time
}

func (s *Service) runSchedulerTick(ctx context.Context, now time.Time, state *schedulerState) {
	if s.balanceRefreshDue(ctx, now) {
		runSchedulerTask(ctx, "balance refresh", 4*time.Minute, func(taskCtx context.Context) error {
			_, err := s.RefreshBalances(taskCtx)
			return err
		})
		s.mu.Lock()
		s.lastBalanceRun = now
		s.mu.Unlock()
	}
	if schedulerTaskDue(state.lastRetention, now, schedulerRetentionInterval) {
		runSchedulerTask(ctx, "retention cleanup", schedulerRetentionTimeout, func(taskCtx context.Context) error {
			stats, err := s.Store.CleanupExpiredData(taskCtx)
			if err == nil && stats.DeletedTotal() > 0 {
				log.Printf("scheduler: retention deleted %d rows", stats.DeletedTotal())
			}
			return err
		})
		state.lastRetention = now
	}
	if schedulerTaskDue(state.lastRevenue, now, schedulerRevenueInterval) {
		runSchedulerTask(ctx, "revenue refresh", schedulerRevenueTimeout, func(taskCtx context.Context) error {
			_, err := s.TodayRevenue(taskCtx)
			return err
		})
		state.lastRevenue = now
	}
	if schedulerTaskDue(state.lastRecharge, now, schedulerRechargeInterval) {
		runSchedulerTask(ctx, "pending recharge refresh", schedulerRechargeTimeout, s.RefreshPendingBalanceRechargeLogs)
		state.lastRecharge = now
	}
}

func (s *Service) balanceRefreshDue(ctx context.Context, now time.Time) bool {
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return false
	}
	interval := time.Duration(cfg.CheckIntervalMinutes) * time.Minute
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBalanceRun.IsZero() || now.Sub(s.lastBalanceRun) >= interval
}

func runSchedulerTask(parent context.Context, name string, timeout time.Duration, fn func(context.Context) error) {
	taskCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := fn(taskCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("scheduler: %s: %v", name, err)
	}
}

func schedulerTaskDue(last, now time.Time, interval time.Duration) bool {
	return last.IsZero() || now.Sub(last) >= interval
}
