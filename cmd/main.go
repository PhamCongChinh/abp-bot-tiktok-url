package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"abp-bot-tiktok-url/internal/crawler"
	// "abp-bot-tiktok-url/internal/repository"
	"abp-bot-tiktok-url/internal/scheduler"
	"abp-bot-tiktok-url/pkg/config"
	// "abp-bot-tiktok-url/pkg/database"
	"abp-bot-tiktok-url/pkg/logger"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// TEMP: MongoDB connection disabled while testing GPM locally.
	// Uncomment this whole block (and the "internal/repository" + "pkg/database"
	// imports above) once GPM works — cfg.URLs is populated from the MongoDB
	// tiktok_url collection this way. Until then, set URLS=... in .env as a
	// fallback so cfg.URLs (loaded by config.Load()) isn't empty.
	//
	// dbCtx, dbCancel := context.WithTimeout(ctx, 30*time.Second)
	// defer dbCancel()
	//
	// mongoDB, err := database.NewMongoDB(dbCtx, cfg.MongoURI, cfg.MongoDB,
	// 	uint64(cfg.MongoMaxPoolSize), uint64(cfg.MongoMinPoolSize), log)
	// if err != nil {
	// 	log.Fatal("Failed to connect MongoDB", zap.Error(err))
	// }
	// defer func() { _ = mongoDB.Close() }()
	//
	// // Init repositories
	// urlRepo := repository.NewURLRepository(mongoDB.Database(), log)
	//
	// // Load URLs from MongoDB by org_ids from .env
	// log.Info("Loading URLs from MongoDB", zap.Ints("org_ids", cfg.OrgIDs))
	//
	// urlEntries, err := urlRepo.FindByOrgIDs(cfg.OrgIDs)
	// if err != nil {
	// 	log.Fatal("Failed to load URLs from MongoDB", zap.Error(err))
	// }
	//
	// log.Info("URLs loaded from MongoDB",
	// 	zap.Int("count", len(urlEntries)),
	// 	zap.Ints("org_ids", cfg.OrgIDs),
	// )
	//
	// // Build URL list, org map, and group by org_id
	// orgURLCount := make(map[int]int)
	// var urlList []string
	// urlOrgMap := make(map[string]int)
	// for _, u := range urlEntries {
	// 	urlList = append(urlList, u.URL)
	// 	urlOrgMap[u.URL] = u.OrgID
	// 	orgURLCount[u.OrgID]++
	// }
	//
	// log.Info("URL distribution by organization:")
	// for orgID, count := range orgURLCount {
	// 	log.Info("", zap.Int("org_id", orgID), zap.Int("urls", count))
	// }
	//
	// if len(urlList) == 0 {
	// 	log.Warn("No URLs found for org_ids, exiting", zap.Ints("org_ids", cfg.OrgIDs))
	// 	return
	// }
	//
	// // Set URLs to config (will be reused for all crawl cycles, shuffled each cycle in Run())
	// cfg.URLs = urlList
	// cfg.URLOrgMap = urlOrgMap

	if len(cfg.URLs) == 0 {
		log.Warn("No URLs configured — MongoDB loading is disabled, set URLS=... in .env")
		return
	}

	// Init Prometheus metrics for observability.
	metrics := crawler.NewCrawlerMetrics(prometheus.DefaultRegisterer)

	// Start metrics HTTP server on configured address (e.g. :9090).
	metricsSrv := startMetricsServer(cfg.MetricsAddr, log)
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = metricsSrv.Shutdown(shutdownCtx)
	}()

	// Init crawler
	c := crawler.New(&cfg, log, nil, metrics)

	log.Info("Crawler initialized - will crawl same URLs every 1-1.5 hours")

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

// startMetricsServer starts an HTTP server on addr that exposes /metrics for
// Prometheus scraping. Returns the *http.Server so the caller can shut it down
// gracefully. Logs a warning and returns a no-op server when addr is empty.
func startMetricsServer(addr string, log *zap.Logger) *http.Server {
	if addr == "" {
		log.Warn("METRICS_ADDR is empty — metrics endpoint disabled")
		return &http.Server{}
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Sugar().Infof("Metrics server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Sugar().Errorf("Metrics server error: %v", err)
		}
	}()

	return srv
}
