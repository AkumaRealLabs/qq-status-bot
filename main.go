package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/browsercdp"
	"ai-upstream-monitor/internal/httpapi"
	"ai-upstream-monitor/internal/store"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		dsn = "/app/data/monitor.sqlite"
	}
	st, err := store.Open(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	if err := st.MigratePocketBase(ctx, os.Getenv("PB_DATA_DB")); err != nil {
		log.Printf("pocketbase migration skipped: %v", err)
	}

	svc := app.New(st)
	svc.Client.Browser = browsercdp.Client{
		DebugURL:   os.Getenv("BROWSER_DEBUG_URL"),
		HostHeader: os.Getenv("BROWSER_DEBUG_HOST_HEADER"),
	}
	svc.StartScheduler(ctx)
	server := &http.Server{
		Addr:              env("HTTP_ADDR", "0.0.0.0:8090"),
		Handler:           (&httpapi.Server{App: svc, Static: frontendFS}).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
