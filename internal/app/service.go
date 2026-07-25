package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
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
	schedulerTGTimeout         = 2 * time.Minute
	schedulerRetentionTimeout  = time.Minute
	schedulerRevenueTimeout    = 3 * time.Minute
	schedulerRechargeTimeout   = 4 * time.Minute
	schedulerCLIProxyTimeout   = 4 * time.Minute
	schedulerRetentionInterval = time.Hour
	schedulerRevenueInterval   = 15 * time.Minute
	schedulerRechargeInterval  = 15 * time.Minute
	schedulerCLIProxyInterval  = 30 * time.Minute
)

type Service struct {
	Store          *store.Store
	Client         monitor.Client
	TGMediaDir     string
	mu             sync.Mutex
	running        bool
	lastBalanceRun time.Time
	tgRunning      bool
	tgLastRun      time.Time

	// 最小 ports（Phase 3）。默认接到 Store / Telegram / monitor.Client。
	Notify       Notifier
	OneBotClient OneBotClient

	// 限界上下文门面（同包）。对外 API 仍经 Service 方法转发。
	Scheduler *SchedulerService
	ProfitSvc *ProfitService
	CLIProxy  *CLIProxyService
	TG        *TGService
	OneBot    *OneBotService
}

// SchedulerService 负责调度配置、渠道/分组应用、成本快照与自动化。
type SchedulerService struct {
	app                    *Service
	controlMu              sync.Mutex
	axonHubAuthMu          sync.Mutex
	axonHubToken           string
	axonHubTokenExpiresAt  time.Time
	axonHubTokenBaseURL    string
	axonHubTokenAdminEmail string
}

// ProfitService 负责调度号池利润汇总。
type ProfitService struct {
	app *Service
}

// CLIProxyService 负责 CLIProxyAPI 管理与配额快照。
type CLIProxyService struct {
	app *Service
}

// TGService 负责 Telegram 会话、频道与消息同步。
type TGService struct {
	app *Service
}

func New(st *store.Store) *Service {
	mediaDir := os.Getenv("TG_MEDIA_DIR")
	if mediaDir == "" {
		mediaDir = "/app/data/tg_media"
	}
	s := &Service{
		Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}}, TGMediaDir: mediaDir,
		OneBotClient: &onebot.Client{HTTP: &http.Client{Timeout: 10 * time.Second}},
	}
	s.Notify = &telegramNotifier{send: s.sendTelegram}
	s.Scheduler = &SchedulerService{app: s}
	s.ProfitSvc = &ProfitService{app: s}
	s.CLIProxy = &CLIProxyService{app: s}
	s.TG = &TGService{app: s}
	s.OneBot = &OneBotService{app: s}
	return s
}

func (s *Service) StartScheduler(ctx context.Context) {
	if err := s.SeedSchedulerSnapshots(ctx); err != nil {
		log.Printf("scheduler: seed snapshots: %v", err)
	}
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
	lastCLIProxy  time.Time
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
	runSchedulerTask(ctx, "TG refresh", schedulerTGTimeout, s.RefreshTGMessagesDue)

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
	if schedulerTaskDue(state.lastCLIProxy, now, schedulerCLIProxyInterval) {
		attempted := false
		runSchedulerTask(ctx, "CLIProxy quota refresh", schedulerCLIProxyTimeout, func(taskCtx context.Context) error {
			var err error
			attempted, err = s.CLIProxy.refreshCLIProxyQuotas(taskCtx)
			return err
		})
		if attempted {
			state.lastCLIProxy = now
		}
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
