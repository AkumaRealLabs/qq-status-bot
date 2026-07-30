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
	"qq-status-bot/internal/browsercdp"
	"qq-status-bot/internal/config"
	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/httpapi"
	"qq-status-bot/internal/qqbot"
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
		ScreenshotSelector: cfg.ScreenshotSelector, ScreenshotWidth: cfg.ScreenshotWidth,
		ScreenshotHeight: cfg.ScreenshotHeight, ScreenshotWait: maxInt(1, int(cfg.ScreenshotWait/time.Second)),
		ScreenshotTimeout: maxInt(15, int(cfg.ScreenshotTimeout/time.Second)), QueueSize: cfg.ScreenshotQueueSize,
	}
	state, err := store.Open(cfg.DataPath, defaults)
	if err != nil {
		log.Fatal(err)
	}
	defer state.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	screenshotter := browsercdp.Client{DebugURL: cfg.BrowserDebugURL, HostHeader: cfg.BrowserHostHeader, Width: cfg.ScreenshotWidth, Height: cfg.ScreenshotHeight, Wait: cfg.ScreenshotWait}
	replier := &qqbot.Client{
		APIBaseURL: cfg.QQBotAPIBaseURL, TokenURL: cfg.QQBotTokenURL,
		Credentials: func() (string, string) {
			settings := state.Settings()
			return settings.QQBotAppID, settings.QQBotAppSecret
		},
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
	service := app.New(state, screenshotter, replier, defaults.QueueSize)
	service.Start(ctx)
	staticFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: (&httpapi.Server{App: service, Static: staticFS}).Routes(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
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
