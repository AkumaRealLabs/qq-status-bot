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
	return &Service{Store: st, Client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}, ProbeMode: probeMode}, TGMediaDir: mediaDir}
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
