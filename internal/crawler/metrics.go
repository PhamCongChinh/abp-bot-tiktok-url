package crawler

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metric labels used across the crawler package.
const (
	labelProfile = "profile_id"
	labelOutcome = "outcome" // "success" or "failure"
)

// CrawlerMetrics holds Prometheus counters and histograms for the TikTok
// crawler. It is wired into Crawler, Publisher, and GPMService so every
// subsystem can record its own metrics independently.
type CrawlerMetrics struct {
	// VideosCrawled counts videos successfully queued for API push.
	VideosCrawled prometheus.Counter

	// CrawlDuration records the wall-clock duration of each Crawler.Run cycle.
	CrawlDuration prometheus.Histogram

	// APIPushErrors counts API push failures (network errors, non-2xx, etc.).
	APIPushErrors prometheus.Counter

	// GPMConnAttempts counts every GPM connection attempt.
	GPMConnAttempts prometheus.Counter

	// GPMConnFailures counts failed GPM connections.
	GPMConnFailures prometheus.Counter

	// CrawlCycles counts each call to Crawler.Run (a full crawl cycle).
	CrawlCycles prometheus.Counter
}

// NewCrawlerMetrics creates and registers all crawler metrics on the given
// Prometheus registry. Pass prometheus.DefaultRegisterer for the default
// global registry, or a custom one for isolation (e.g. in tests).
func NewCrawlerMetrics(reg prometheus.Registerer) *CrawlerMetrics {
	m := &CrawlerMetrics{
		VideosCrawled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "abp_crawler_videos_crawled_total",
			Help: "Total number of videos queued for API push.",
		}),
		CrawlDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "abp_crawler_run_duration_seconds",
			Help:    "Wall-clock duration of each Crawler.Run cycle.",
			Buckets: prometheus.DefBuckets,
		}),
		APIPushErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "abp_crawler_api_push_errors_total",
			Help: "Total number of failed API push operations.",
		}),
		GPMConnAttempts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "abp_crawler_gpm_connection_attempts_total",
			Help: "Total number of GPM connection attempts.",
		}),
		GPMConnFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "abp_crawler_gpm_connection_failures_total",
			Help: "Total number of failed GPM connections.",
		}),
		CrawlCycles: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "abp_crawler_cycles_total",
			Help: "Total number of crawl cycles started.",
		}),
	}

	reg.MustRegister(
		m.VideosCrawled,
		m.CrawlDuration,
		m.APIPushErrors,
		m.GPMConnAttempts,
		m.GPMConnFailures,
		m.CrawlCycles,
	)

	return m
}

// IncVideosCrawled increments the videos-crawled counter by n.
func (m *CrawlerMetrics) IncVideosCrawled(n int) {
	m.VideosCrawled.Add(float64(n))
}

// IncAPIPushErrors increments the API push errors counter.
func (m *CrawlerMetrics) IncAPIPushErrors() {
	m.APIPushErrors.Inc()
}

// IncGPMConnAttempt increments the GPM connection attempts counter.
func (m *CrawlerMetrics) IncGPMConnAttempt() {
	m.GPMConnAttempts.Inc()
}

// IncGPMConnFailure increments the GPM connection failures counter.
func (m *CrawlerMetrics) IncGPMConnFailure() {
	m.GPMConnFailures.Inc()
}

// IncCrawlCycles increments the crawl cycles counter.
func (m *CrawlerMetrics) IncCrawlCycles() {
	m.CrawlCycles.Inc()
}
