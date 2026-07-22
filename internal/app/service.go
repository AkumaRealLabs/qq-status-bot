package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/onebot"
	"ai-upstream-monitor/internal/store"
)

const (
	minPasswordLength = 8
	checkConcurrency  = 3

	schedulerTickInterval      = time.Minute
	schedulerMinCheckTimeout   = 4 * time.Minute
	schedulerCheckOverhead     = 30 * time.Second
	upstreamCheckBudget        = 4 * 45 * time.Second
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
	schedulerTrafficTimeout    = 90 * time.Second
	schedulerAxonHubInterval   = 10 * time.Second
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

	// 最小 ports（Phase 3）。默认接到 Store / Telegram / monitor.Client。
	Cards                     CardRepository
	Notify                    Notifier
	Prober                    ProbeRunner
	OneBotClient              OneBotClient
	OneBotStatusImageRenderer OneBotStatusImageRenderer
	oneBotRuntime             oneBotRuntime

	// 限界上下文门面（同包）。对外 API 仍经 Service 方法转发。
	Scheduler *SchedulerService
	ProfitSvc *ProfitService
	Probe     *ProbeService
	CLIProxy  *CLIProxyService
	TG        *TGService
	OneBot    *OneBotService
}

// SchedulerService 负责调度配置、渠道/分组应用、成本快照与自动化。
type SchedulerService struct {
	app       *Service
	controlMu sync.Mutex
}

// ProfitService 负责调度号池利润汇总。
type ProfitService struct {
	app *Service
}

// ProbeService 负责模型卡片、探测、上游检查与监控状态。
type ProbeService struct {
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
	probeMode := monitor.ProbeModeCLI
	if strings.EqualFold(os.Getenv("AUM_PROBE_MODE"), monitor.ProbeModeHTTP) {
		probeMode = monitor.ProbeModeHTTP
	}
	s := &Service{
		Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}, ProbeMode: probeMode}, TGMediaDir: mediaDir,
		OneBotClient: &onebot.Client{HTTP: &http.Client{Timeout: 10 * time.Second}}, oneBotRuntime: newOneBotRuntime(),
	}
	s.Cards = st
	s.Notify = &telegramNotifier{send: s.sendTelegram}
	s.Prober = &liveProbeRunner{svc: s}
	s.Scheduler = &SchedulerService{app: s}
	s.ProfitSvc = &ProfitService{app: s}
	s.Probe = &ProbeService{app: s}
	s.CLIProxy = &CLIProxyService{app: s}
	s.TG = &TGService{app: s}
	s.OneBot = &OneBotService{app: s}
	return s
}

func (s *Service) StartScheduler(ctx context.Context) {
	if err := s.SeedSchedulerSnapshots(ctx); err != nil {
		log.Printf("scheduler: seed snapshots: %v", err)
	}
	if err := s.Scheduler.SeedControlPlaneBaseline(ctx); err != nil && !errors.Is(err, errSchedulerNotConfigured) {
		log.Printf("scheduler: seed control plane: %v", err)
	}
	// 启动时跑一次数据保留清理，让长期闲置主机立刻腾出空间。
	retentionCtx, cancel := context.WithTimeout(ctx, schedulerRetentionTimeout)
	stats, err := s.Store.CleanupExpiredData(retentionCtx)
	cancel()
	if err != nil {
		log.Printf("scheduler: retention cleanup: %v", err)
	} else if stats.DeletedTotal() > 0 {
		log.Printf("scheduler: retention deleted %d rows (probes=%d balances=%d sessions=%d)",
			stats.DeletedTotal(), stats.ProbeRuns, stats.BalanceSnapshots, stats.ExpiredSessions)
	}
	state := schedulerState{}
	if err == nil {
		state.lastRetention = time.Now()
	}
	t := time.NewTicker(schedulerTickInterval)
	s.startTrafficScheduler(ctx)
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

func (s *Service) startTrafficScheduler(ctx context.Context) {
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			interval := 5 * time.Second
			cfg, err := s.Store.SchedulerConfig(ctx)
			if err == nil {
				if cfg.Provider == domain.SchedulerProviderAxonHub {
					runSchedulerTask(ctx, "AxonHub reconcile", schedulerGroupSyncBudget, s.Scheduler.ReconcileAxonHub)
					timer.Reset(schedulerAxonHubInterval)
					continue
				}
				interval = time.Duration(domain.NormalizeTrafficPollSeconds(cfg.TrafficPollSecs)) * time.Second
				if cfg.TrafficMode != domain.TrafficModeOff {
					runSchedulerTask(ctx, "traffic reconcile", schedulerTrafficTimeout, s.ReconcileTraffic)
				}
			}
			timer.Reset(interval)
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
	runSchedulerTask(ctx, "control plane reconcile", schedulerGroupSyncBudget, s.Scheduler.ReconcileControlPlane)
	runSchedulerTask(ctx, "check due", s.checkCycleTimeout(ctx), s.CheckDue)
	runSchedulerTask(ctx, "availability reconcile", schedulerGroupSyncBudget, s.ReconcileAvailability)
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

// checkCycleTimeout 按并发批次为整轮检查预留时间，不能让排队卡片因总 deadline 被误判失败。
func (s *Service) checkCycleTimeout(ctx context.Context) time.Duration {
	cards, cardErr := s.Cards.ListCards(ctx)
	upstreams, upstreamErr := s.Store.ListUpstreams(ctx)
	if cardErr != nil || upstreamErr != nil {
		return schedulerMinCheckTimeout
	}
	cardCount, upstreamCount := 0, 0
	for _, card := range cards {
		if card.Enabled {
			cardCount++
		}
	}
	for _, upstream := range upstreams {
		if upstream.Enabled {
			upstreamCount++
		}
	}
	// 受可用性控制的普通失败会额外串行确认两次，每次沿用单卡 45 秒预算。
	timeout := schedulerCheckOverhead + time.Duration(limitedBatches(cardCount))*cardProbeTimeout*availabilityProbeAttempts + time.Duration(limitedBatches(upstreamCount))*upstreamCheckBudget
	if upstreamCount > 0 {
		timeout += schedulerGroupSyncBudget
	}
	if timeout < schedulerMinCheckTimeout {
		return schedulerMinCheckTimeout
	}
	return timeout
}

func limitedBatches(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + checkConcurrency - 1) / checkConcurrency
}
