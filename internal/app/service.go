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

	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"
)

const (
	minPasswordLength = 8
	checkConcurrency  = 3
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
	Cards  CardRepository
	Notify Notifier
	Prober ProbeRunner

	// 限界上下文门面（同包）。对外 API 仍经 Service 方法转发。
	Scheduler *SchedulerService
	ProfitSvc *ProfitService
	Probe     *ProbeService
	CLIProxy  *CLIProxyService
	TG        *TGService
}

// SchedulerService 负责调度配置、渠道/分组应用、成本快照与自动化。
type SchedulerService struct {
	app *Service
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
	time.Local = appLocation()
	mediaDir := os.Getenv("TG_MEDIA_DIR")
	if mediaDir == "" {
		mediaDir = "/app/data/tg_media"
	}
	probeMode := monitor.ProbeModeCLI
	if strings.EqualFold(os.Getenv("AUM_PROBE_MODE"), monitor.ProbeModeHTTP) {
		probeMode = monitor.ProbeModeHTTP
	}
	s := &Service{Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}, ProbeMode: probeMode}, TGMediaDir: mediaDir}
	s.Cards = st
	s.Notify = &telegramNotifier{send: s.sendTelegram}
	s.Prober = &liveProbeRunner{svc: s}
	s.Scheduler = &SchedulerService{app: s}
	s.ProfitSvc = &ProfitService{app: s}
	s.Probe = &ProbeService{app: s}
	s.CLIProxy = &CLIProxyService{app: s}
	s.TG = &TGService{app: s}
	return s
}

func (s *Service) StartScheduler(ctx context.Context) {
	if err := s.SeedSchedulerSnapshots(ctx); err != nil {
		log.Printf("scheduler: seed snapshots: %v", err)
	}
	// 启动时跑一次数据保留清理，让长期闲置主机立刻腾出空间。
	if stats, err := s.Store.CleanupExpiredData(ctx); err != nil {
		log.Printf("scheduler: retention cleanup: %v", err)
	} else if stats.DeletedTotal() > 0 {
		log.Printf("scheduler: retention deleted %d rows (probes=%d balances=%d sessions=%d)",
			stats.DeletedTotal(), stats.ProbeRuns, stats.BalanceSnapshots, stats.ExpiredSessions)
	}
	t := time.NewTicker(time.Minute)
	go func() {
		defer t.Stop()
		var lastRetention time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tickCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
				if err := s.CheckDue(tickCtx); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("scheduler: check due: %v", err)
				}
				if err := s.RefreshTGMessagesDue(tickCtx); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("scheduler: tg refresh: %v", err)
				}
				// 保留清理开销小；按小时跑，不必每分钟。
				if lastRetention.IsZero() || time.Since(lastRetention) >= time.Hour {
					if stats, err := s.Store.CleanupExpiredData(tickCtx); err != nil {
						log.Printf("scheduler: retention cleanup: %v", err)
					} else if stats.DeletedTotal() > 0 {
						log.Printf("scheduler: retention deleted %d rows", stats.DeletedTotal())
					}
					lastRetention = time.Now()
				}
				cancel()
			}
		}
	}()
}
