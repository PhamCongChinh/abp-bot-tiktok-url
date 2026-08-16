package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"abp-bot-tiktok-url/internal/crawler"
	"abp-bot-tiktok-url/internal/repository"
	"abp-bot-tiktok-url/internal/scheduler"
	"abp-bot-tiktok-url/pkg/config"
	"abp-bot-tiktok-url/pkg/database"
	"abp-bot-tiktok-url/pkg/logger"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: config load failed: %v\n", err)
		os.Exit(1)
	}

	logFilePath := filepath.Join(cfg.OutputDir, "logs", "bot.log")
	log, err := logger.New(logger.Config{
		Level:      cfg.LogLevel,
		FilePath:   logFilePath,
		MaxSizeMB:  cfg.LogMaxSizeMB,
		MaxAgeDays: cfg.LogMaxAgeDays,
		MaxBackups: cfg.LogMaxBackups,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = log.Sync() }()

	log.Info("Starting abp-bot-tiktok-url...")
	log.Sugar().Infof("DEBUG=%v | BotName=%s", cfg.Debug, cfg.BotName)

	// Top-level context for graceful shutdown.
	// All subsystems (MongoDB, scheduler, crawler) share this context so
	// a single cancel propagates cleanly through the entire call chain.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Wire OS signals to context cancellation.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Sugar().Infof("Received signal %v — initiating graceful shutdown", sig)
		cancel()
	}()

	// Connect MongoDB (source of `tiktok_url`) and PostgreSQL (source of
	// active org_ids via the `org` table). The bot resolves which URLs to
	// crawl dynamically every cycle: active orgs from Postgres, then their
	// URLs from Mongo's `tiktok_url` collection — see Crawler.loadURLs().
	dbCtx, dbCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dbCancel()

	mongoDB, err := database.NewMongoDB(dbCtx, cfg.MongoURI, cfg.MongoDB,
		uint64(cfg.MongoMaxPoolSize), uint64(cfg.MongoMinPoolSize), log)
	if err != nil {
		log.Fatal("Failed to connect MongoDB", zap.Error(err))
	}
	defer func() { _ = mongoDB.Close() }()

	pgDB, err := database.NewPostgresDB(dbCtx, cfg.PGDSN, log)
	if err != nil {
		log.Fatal("Failed to connect PostgreSQL", zap.Error(err))
	}
	defer pgDB.Close()

	orgRepo := repository.NewOrgRepository(pgDB.Pool)
	urlRepo := repository.NewURLRepository(mongoDB.Database(), log)

	// Init Prometheus metrics for observability (in-process counters only —
	// no HTTP /metrics endpoint is exposed).
	metrics := crawler.NewCrawlerMetrics(prometheus.DefaultRegisterer)

	// Init crawler
	c := crawler.New(&cfg, log, nil, orgRepo, urlRepo, metrics)

	log.Info("Crawler initialized - will query active orgs/URLs from MongoDB every 30-45 minutes")

	// Run crawler with the top-level context.
	// If SIGTERM/SIGINT is received, ctx is cancelled and the entire
	// call chain (scheduler → crawler → GPM) drains gracefully.
	runCrawler(ctx, &cfg, log, c)

	log.Info("Shutdown complete")
}

func runCrawler(pctx context.Context, cfg *config.Config, log *zap.Logger, c *crawler.Crawler) {
	// Derive a child context from the parent so that cancelling pctx (via
	// SIGTERM/SIGINT) propagates cleanly through scheduler and crawler.
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	if cfg.Debug {
		log.Info("DEBUG mode: running crawler immediately")
		c.Run(ctx)
		log.Info("Done.")
		return
	}

	s := scheduler.New(cfg, log, c)
	s.Start(ctx)
	defer s.Stop()

	// Block until the parent context is cancelled (signal received).
	<-ctx.Done()
	log.Info("Shutting down — closing all profiles...")
}
