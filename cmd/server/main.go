// Command server is the PipelineBuilder backend: HTTP API + background workers.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // embed zoneinfo so America/New_York resolves in minimal containers

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/api"
	"github.com/zainclaude/goutreach/internal/auth"
	"github.com/zainclaude/goutreach/internal/config"
	"github.com/zainclaude/goutreach/internal/crypto"
	"github.com/zainclaude/goutreach/internal/db"
	"github.com/zainclaude/goutreach/internal/googleoauth"
	"github.com/zainclaude/goutreach/internal/mailauth"
	"github.com/zainclaude/goutreach/internal/research"
	"github.com/zainclaude/goutreach/internal/sender"
	"github.com/zainclaude/goutreach/internal/store"
	"github.com/zainclaude/goutreach/internal/tracking"
	"github.com/zainclaude/goutreach/internal/warmup"
)

func main() {
	logger := log.New(os.Stdout, "goutreach ", log.LstdFlags)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		logger.Fatalf("migrate: %v", err)
	}
	logger.Println("migrations applied")

	st := store.New(pool)
	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		logger.Fatalf("crypto: %v", err)
	}
	authSvc := auth.New(cfg.JWTSecret)

	tiktok := research.New(st, cipher, logger)
	gen := ai.New(cfg.AnthropicKey, cfg.AIModel, tiktok, logger)
	if !gen.Enabled() {
		logger.Println("warning: ANTHROPIC_API_KEY not set — AI generation disabled")
	} else {
		logger.Printf("ai: generating emails with %s", cfg.AIModel)
	}

	google := googleoauth.New(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)
	if google.Enabled() {
		logger.Println("Google OAuth enabled")
	}
	resolver := mailauth.New(cipher, google)

	snd := sender.New(st, resolver, gen, cfg.AppURL, logger)
	poller := tracking.NewPoller(st, resolver, gen, cfg.AppURL, logger)
	warm := warmup.New(st, resolver, logger)

	// Background workers.
	go snd.Run(ctx)
	go poller.Run(ctx)
	go warm.Run(ctx)

	server := api.New(cfg, st, authSvc, cipher, resolver, google, snd, gen, logger)
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
