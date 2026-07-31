package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"qq-status-bot/internal/app"
	"qq-status-bot/internal/config"
	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/httpapi"
	"qq-status-bot/internal/qqbot"
	"qq-status-bot/internal/statusapi"
	"qq-status-bot/internal/statusimage"
	"qq-status-bot/internal/store"
)

//go:embed frontend/dist
var frontendFS embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	defaults := domain.Settings{
		QQBotAppID: cfg.QQBotAppID, QQBotAppSecret: cfg.QQBotAppSecret,
		AllowedGroups: cfg.AllowedGroups, Commands: cfg.Commands, StatusURL: cfg.StatusURL,
		StatusPageID: cfg.StatusPageID, StatusPeriod: cfg.StatusPeriod,
		ScreenshotTimeout: maxInt(15, int(cfg.ScreenshotTimeout/time.Second)), QueueSize: cfg.ScreenshotQueueSize,
		AlertFailureSamples: 2, AlertRecoverySamples: 2, AlertGroups: []string{},
	}
	state, err := store.Open(cfg.DataPath, defaults)
	if err != nil {
		log.Fatal(err)
	}
	defer state.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	generator := statusimage.Generator{}
	replier := &qqbot.Client{
		APIBaseURL: cfg.QQBotAPIBaseURL, TokenURL: cfg.QQBotTokenURL,
		Credentials: func() (string, string) {
			settings := state.Settings()
			return settings.QQBotAppID, settings.QQBotAppSecret
		},
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
	service := app.New(state, generator, replier, state.Settings().QueueSize, statusapi.Client{HTTP: &http.Client{Timeout: 15 * time.Second}})
	service.Start(ctx)
	staticFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: (&httpapi.Server{App: service, Static: staticFS}).Routes(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 4*time.Minute + 5*time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("QQ 状态机器人监听 %s，回调路径 /qqbot/events", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
