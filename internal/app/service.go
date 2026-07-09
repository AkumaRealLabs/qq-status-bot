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

	// Minimal ports (Phase 3). Defaults wire to Store / Telegram / monitor.Client.
	Cards  CardRepository
	Notify Notifier
	Prober ProbeRunner

	// Bounded-context facades (same package). Public APIs still forward via Service methods.
	Scheduler *SchedulerService
	ProfitSvc *ProfitService
	Probe     *ProbeService
	CLIProxy  *CLIProxyService
	TG        *TGService
}

// SchedulerService owns scheduler config, channel/group apply, cost snapshots, and automation.
type SchedulerService struct {
	app *Service
}

// ProfitService owns scheduler-pool profit aggregation.
type ProfitService struct {
	app *Service
}

// ProbeService owns model cards, probe runs, upstream checks, and monitor status.
type ProbeService struct {
	app *Service
}

// CLIProxyService owns CLIProxyAPI management and quota snapshots.
type CLIProxyService struct {
	app *Service
}

// TGService owns Telegram session, channels, and message sync.
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
	// Run retention once at boot so long-idle hosts free space immediately.
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
				// Retention is cheap; run hourly rather than every minute.
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
