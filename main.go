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
	"qq-status-bot/internal/ggapi"
	"qq-status-bot/internal/httpapi"
	"qq-status-bot/internal/mailer"
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
		GGAPIBalanceEnabled: cfg.GGAPIBalanceEnabled, GGAPIBaseURL: cfg.GGAPIBaseURL,
		GGAPIAdminToken: cfg.GGAPIAdminToken, GGAPISmtpHost: cfg.GGAPISmtpHost,
		GGAPISmtpPort: cfg.GGAPISmtpPort, GGAPISmtpUsername: cfg.GGAPISmtpUsername,
		GGAPISmtpPassword: cfg.GGAPISmtpPassword, GGAPISmtpFrom: cfg.GGAPISmtpFrom,
		GGAPISmtpFromName: cfg.GGAPISmtpFromName, GGAPISmtpTLSMode: cfg.GGAPISmtpTLSMode,
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
	service.ConfigureAccounts(dynamicGGAPI{settings: state, http: &http.Client{Timeout: 15 * time.Second}}, dynamicMailer{settings: state})
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

type dynamicGGAPI struct {
	settings interface{ Settings() domain.Settings }
	http     *http.Client
}

func (d dynamicGGAPI) client() ggapi.Client {
	settings := d.settings.Settings()
	return ggapi.Client{BaseURL: settings.GGAPIBaseURL, AdminToken: settings.GGAPIAdminToken, HTTP: d.http, Timeout: 15 * time.Second}
}

func (d dynamicGGAPI) VerifyEmail(ctx context.Context, email string) (ggapi.User, error) {
	return d.client().VerifyEmail(ctx, email)
}

func (d dynamicGGAPI) GetUser(ctx context.Context, id string) (ggapi.User, error) {
	return d.client().GetUser(ctx, id)
}

func (d dynamicGGAPI) Balance(ctx context.Context, user ggapi.User) (ggapi.Balance, error) {
	return d.client().Balance(ctx, user)
}

type dynamicMailer struct {
	settings interface{ Settings() domain.Settings }
}

func (d dynamicMailer) SendVerificationCode(ctx context.Context, recipient, code string, expiresAt time.Time) error {
	settings := d.settings.Settings()
	return (mailer.SMTPMailer{Host: settings.GGAPISmtpHost, Port: settings.GGAPISmtpPort,
		Username: settings.GGAPISmtpUsername, Password: settings.GGAPISmtpPassword,
		From: settings.GGAPISmtpFrom, FromName: settings.GGAPISmtpFromName,
		TLSMode: settings.GGAPISmtpTLSMode, Timeout: 15 * time.Second}).SendVerificationCode(ctx, recipient, code, expiresAt)
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
