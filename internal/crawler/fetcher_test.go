package crawler

import (
	"context"
	"testing"
	"time"

	"abp-bot-tiktok-url/pkg/api"
	"abp-bot-tiktok-url/pkg/config"

	"go.uber.org/zap"
)

func TestNewFetcher(t *testing.T) {
	cfg := &config.Config{}
	log := zap.NewNop()
	apiClient := api.NewClient("http://localhost:9999", 10*time.Second, log)
	publisher := NewPublisher(apiClient, log, nil)
	defer publisher.Shutdown()
	gpmSvc := NewDefaultGPMService()
	scrpr := NewScraper()

	f := NewFetcher(cfg, publisher, gpmSvc, scrpr)

	if f == nil {
		t.Fatal("NewFetcher returned nil")
	}
	if f.cfg != cfg {
		t.Error("config not set correctly")
	}
	if f.publisher != publisher {
		t.Error("publisher not set correctly")
	}
	if f.gpmSvc != gpmSvc {
		t.Error("gpmSvc not set correctly")
	}
	if f.scraper != scrpr {
		t.Error("scraper not set correctly")
	}
}

func TestCrawlURLs_ContextCancelled(t *testing.T) {
	cfg := &config.Config{
		MaxPagesPerSession: 10,
	}
	log := zap.NewNop()
	apiClient := api.NewClient("http://localhost:9999", 10*time.Second, log)
	publisher := NewPublisher(apiClient, log, nil)
	defer publisher.Shutdown()
	gpmSvc := NewDefaultGPMService()
	scrpr := NewScraper()

	f := NewFetcher(cfg, publisher, gpmSvc, scrpr)

	// When context is already cancelled, CrawlURLs should return immediately
	// without making any GPM connections or page interactions.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	// nil playwright and urls — these should not be accessed since
	// context is already done.
	f.CrawlURLs(ctx, nil, nil, "profile-12345", []string{"https://www.tiktok.com/@u1", "https://www.tiktok.com/@u2"}, log, "test-tag")
	elapsed := time.Since(start)

	// Should return quickly since context was cancelled before the loop starts.
	if elapsed > 500*time.Millisecond {
		t.Errorf("CrawlURLs with cancelled context took %v, expected < 500ms", elapsed)
	}

	t.Logf("CrawlURLs returned after %v with cancelled context", elapsed)
}

func TestCrawlURLs_EmptyURLs(t *testing.T) {
	cfg := &config.Config{}
	log := zap.NewNop()
	apiClient := api.NewClient("http://localhost:9999", 10*time.Second, log)
	publisher := NewPublisher(apiClient, log, nil)
	defer publisher.Shutdown()
	gpmSvc := NewDefaultGPMService()
	scrpr := NewScraper()

	f := NewFetcher(cfg, publisher, gpmSvc, scrpr)

	ctx := context.Background()
	start := time.Now()

	// Empty URL list — loop should never enter.
	f.CrawlURLs(ctx, nil, nil, "profile-12345", nil, log, "test-tag")
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("CrawlURLs with nil urls took %v, expected < 500ms", elapsed)
	}

	// Also test with explicit empty slice.
	f.CrawlURLs(ctx, nil, nil, "profile-12345", []string{}, log, "test-tag")
}

func TestCrawlURLs_MaxPagesGuard(t *testing.T) {
	// When MaxPagesPerSession is 1, CrawlURLs should stop after 1 page load
	// attempt, before actually loading a page (since GPM won't connect without
	// a real server, it will fail the connect and move to the next batch).
	cfg := &config.Config{
		MaxPagesPerSession: 1,
		BatchMin:           2,
		BatchMax:           2,
	}
	log := zap.NewNop()
	apiClient := api.NewClient("http://localhost:9999", 10*time.Second, log)
	publisher := NewPublisher(apiClient, log, nil)
	defer publisher.Shutdown()
	gpmSvc := NewDefaultGPMService()
	scrpr := NewScraper()

	f := NewFetcher(cfg, publisher, gpmSvc, scrpr)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	// Long URL list, but MaxPagesPerSession=1 should stop after pageCount reaches 1.
	// A mock GPM client (rather than a literal nil) is required here because
	// this path actually reaches gpmSvc.Connect, which calls gpmClient.StartProfile —
	// a nil interface value would panic on that call.
	urls := []string{
		"https://www.tiktok.com/@u1",
		"https://www.tiktok.com/@u2",
		"https://www.tiktok.com/@u3",
		"https://www.tiktok.com/@u4",
	}
	f.CrawlURLs(ctx, nil, &mockGPMClient{}, "profile-12345", urls, log, "test-tag")
	elapsed := time.Since(start)

	t.Logf("CrawlURLs with MaxPagesPerSession=1 completed in %v", elapsed)
	// Should complete quickly since WaitForResources may take a moment,
	// but then pageCount hits the limit. Even if WaitForResources blocks
	// briefly, the test timeout of 30s handles it.
}

func TestCrawlURLs_Constants(t *testing.T) {
	// Verify critical constants used by Fetcher.
	if itemDetailAPI != "/api/item/detail/" {
		t.Errorf("itemDetailAPI = %q, want %q", itemDetailAPI, "/api/item/detail/")
	}
	if postItemListAPI != "/api/post/item_list/" {
		t.Errorf("postItemListAPI = %q, want %q", postItemListAPI, "/api/post/item_list/")
	}
	if cutoffSpan != 7*24*60*60 {
		t.Errorf("cutoffSpan = %d, want %d (7 days in seconds)", cutoffSpan, 7*24*60*60)
	}
}

func TestClassifyURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want urlKind
	}{
		{"video URL", "https://www.tiktok.com/@someuser/video/7123456789012345678", urlKindVideo},
		{"profile URL", "https://www.tiktok.com/@someuser", urlKindProfile},
		{"profile URL trailing slash", "https://www.tiktok.com/@someuser/", urlKindProfile},
		{"unrelated URL", "https://www.tiktok.com/foryou", urlKindUnknown},
		{"empty string", "", urlKindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyURL(tt.url)
			if got != tt.want {
				t.Errorf("classifyURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractStatusCode(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want float64
	}{
		{"snake_case key", map[string]any{"status_code": float64(2061)}, 2061},
		{"camelCase key", map[string]any{"statusCode": float64(10000)}, 10000},
		{"missing key", map[string]any{}, 0},
		{"snake_case takes precedence", map[string]any{"status_code": float64(1), "statusCode": float64(2)}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStatusCode(tt.body)
			if got != tt.want {
				t.Errorf("extractStatusCode(%v) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestExtractItems_Video(t *testing.T) {
	body := map[string]any{
		"itemInfo": map[string]any{
			"itemStruct": map[string]any{
				"id":   "12345",
				"desc": "a video",
			},
		},
	}

	items := extractItems(body, urlKindVideo)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["id"] != "12345" {
		t.Errorf("id = %v, want %q", items[0]["id"], "12345")
	}
}

func TestExtractItems_VideoMissing(t *testing.T) {
	items := extractItems(map[string]any{}, urlKindVideo)
	if len(items) != 0 {
		t.Errorf("expected 0 items for missing itemInfo, got %d", len(items))
	}
}

func TestExtractItems_ProfileCamelCase(t *testing.T) {
	body := map[string]any{
		"itemList": []any{
			map[string]any{"id": "1"},
			map[string]any{"id": "2"},
		},
	}

	items := extractItems(body, urlKindProfile)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestExtractItems_ProfileSnakeCase(t *testing.T) {
	body := map[string]any{
		"item_list": []any{
			map[string]any{"id": "1"},
		},
	}

	items := extractItems(body, urlKindProfile)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestExtractItems_ProfileMissing(t *testing.T) {
	items := extractItems(map[string]any{}, urlKindProfile)
	if len(items) != 0 {
		t.Errorf("expected 0 items for missing list, got %d", len(items))
	}
}
