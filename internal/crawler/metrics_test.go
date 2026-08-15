package crawler

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewCrawlerMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewCrawlerMetrics(reg)

	if m == nil {
		t.Fatal("expected non-nil CrawlerMetrics")
	}

	// Verify all counters and histograms are registered.
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range metricFamilies {
		names[mf.GetName()] = true
	}

	expected := []string{
		"abp_crawler_videos_crawled_total",
		"abp_crawler_run_duration_seconds",
		"abp_crawler_api_push_errors_total",
		"abp_crawler_gpm_connection_attempts_total",
		"abp_crawler_gpm_connection_failures_total",
		"abp_crawler_cycles_total",
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected metric %q to be registered", name)
		}
	}
}

func TestCrawlerMetrics_IncrementMethods(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewCrawlerMetrics(reg)

	m.IncCrawlCycles()
	m.IncCrawlCycles()

	m.IncVideosCrawled(5)

	m.IncAPIPushErrors()

	m.IncGPMConnAttempt()
	m.IncGPMConnAttempt()
	m.IncGPMConnAttempt()

	m.IncGPMConnFailure()
	m.IncGPMConnFailure()

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	values := make(map[string]float64)
	for _, mf := range metricFamilies {
		for _, m := range mf.GetMetric() {
			if mf.GetType().String() == "COUNTER" {
				values[mf.GetName()] = m.GetCounter().GetValue()
			}
		}
	}

	if v := values["abp_crawler_cycles_total"]; v != 2 {
		t.Errorf("crawl cycles = %v, want 2", v)
	}
	if v := values["abp_crawler_videos_crawled_total"]; v != 5 {
		t.Errorf("videos crawled = %v, want 5", v)
	}
	if v := values["abp_crawler_api_push_errors_total"]; v != 1 {
		t.Errorf("api push errors = %v, want 1", v)
	}
	if v := values["abp_crawler_gpm_connection_attempts_total"]; v != 3 {
		t.Errorf("gpm conn attempts = %v, want 3", v)
	}
	if v := values["abp_crawler_gpm_connection_failures_total"]; v != 2 {
		t.Errorf("gpm conn failures = %v, want 2", v)
	}
}

func TestNewCrawlerMetrics_DefaultRegisterer(t *testing.T) {
	// Should work with the default registerer.
	m := NewCrawlerMetrics(prometheus.DefaultRegisterer)
	if m == nil {
		t.Fatal("expected non-nil CrawlerMetrics with default registerer")
	}
}
