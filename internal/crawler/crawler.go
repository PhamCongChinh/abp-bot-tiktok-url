package crawler

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"abp-bot-tiktok-url/internal/repository"
	"abp-bot-tiktok-url/internal/utils"
	"abp-bot-tiktok-url/pkg/api"
	"abp-bot-tiktok-url/pkg/config"
	"abp-bot-tiktok-url/pkg/gpm"
	"abp-bot-tiktok-url/pkg/logger"

	"github.com/playwright-community/playwright-go"
	"go.uber.org/zap"
)

// preBrowserPause is how long Run() waits after resolving the URL list —
// and logging it — before it opens any browser/GPM connection.
const preBrowserPause = 5 * time.Second

// Crawler is the top-level orchestrator that coordinates direct-URL crawling
// across multiple GPM profiles. Each sub-concern (GPM connections, page
// scraping, video parsing/publishing, fetch loops) is delegated to a
// dedicated service.
type Crawler struct {
	cfg       *config.Config
	log       *zap.Logger
	videoRepo repository.VideoStore
	orgRepo   repository.OrgStore
	urlRepo   repository.URLStore
	apiClient api.APIClient
	gpmSvc    *GPMService
	scraper   *Scraper
	publisher *Publisher
	fetcher   *Fetcher
	metrics   *CrawlerMetrics
}

// New creates a fully wired Crawler with all sub-services. orgRepo/urlRepo
// may be nil (e.g. in tests or when MongoDB is not wired) — Run() then falls
// back to the static cfg.URLs/cfg.URLOrgMap loaded from the URLS env var.
func New(cfg *config.Config, log *zap.Logger, videoRepo repository.VideoStore, orgRepo repository.OrgStore, urlRepo repository.URLStore, metrics *CrawlerMetrics) *Crawler {
	var apiClient api.APIClient
	if cfg.APIURL != "" {
		apiClient = api.NewClient(cfg.APIURL, time.Duration(cfg.HTTPTimeoutSeconds)*time.Second, log)
	}
	gpmSvc := NewGPMService(metrics)
	scraper := NewScraper()
	publisher := NewPublisher(apiClient, log, metrics)
	fetcher := NewFetcher(cfg, publisher, gpmSvc, scraper)

	return &Crawler{
		cfg:       cfg,
		log:       log,
		videoRepo: videoRepo,
		orgRepo:   orgRepo,
		urlRepo:   urlRepo,
		apiClient: apiClient,
		gpmSvc:    gpmSvc,
		scraper:   scraper,
		publisher: publisher,
		fetcher:   fetcher,
		metrics:   metrics,
	}
}

// Run starts direct-URL crawling across all configured GPM profiles.
func (c *Crawler) Run(ctx context.Context) {
	if c.cfg == nil || c.log == nil {
		if c.log != nil {
			c.log.Error("Crawler.Run: nil config")
		}
		return
	}
	if !c.cfg.UseGPM {
		c.log.Error("GPM config required. Set GPM_API and PROFILE_IDS in .env")
		return
	}

	urls, urlOrgMap, urlSourceOwnershipMap, err := c.loadURLs()
	if err != nil {
		c.log.Error("Crawler.Run: failed to load URLs", zap.Error(err))
		return
	}
	if len(urls) == 0 {
		c.log.Warn("Crawler.Run: no active URLs to crawl this cycle, skipping")
		return
	}
	c.cfg.URLOrgMap = urlOrgMap
	c.cfg.URLSourceOwnershipMap = urlSourceOwnershipMap
	c.logURLsByOrg(urls, urlOrgMap)

	c.log.Info("Crawler.Run: pausing before opening browser", zap.Duration("pause", preBrowserPause))
	select {
	case <-time.After(preBrowserPause):
	case <-ctx.Done():
		c.log.Warn("Crawler.Run: context cancelled during pre-browser pause")
		return
	}

	start := time.Now()
	if c.metrics != nil {
		c.metrics.IncCrawlCycles()
	}
	defer func() {
		if c.metrics != nil {
			c.metrics.CrawlDuration.Observe(time.Since(start).Seconds())
		}
	}()

	rand.Shuffle(len(urls), func(i, j int) {
		urls[i], urls[j] = urls[j], urls[i]
	})

	numProfiles := len(c.cfg.ProfileIDs)
	chunks := splitURLs(urls, numProfiles)

	var wg sync.WaitGroup
launch:
	for i, profileID := range c.cfg.ProfileIDs {
		wg.Add(1)
		go func(profileID string, urls []string, idx int) {
			defer wg.Done()
			c.runProfile(ctx, profileID, urls, idx)
		}(profileID, chunks[i], i)

		if i < numProfiles-1 {
			staggerSec := utils.RandInt(15, 45)
			select {
			case <-time.After(time.Duration(staggerSec) * time.Second):
			case <-ctx.Done():
				break launch
			}
		}
	}
	wg.Wait()
}

// loadURLs resolves the URL list to crawl this cycle. When orgRepo/urlRepo
// are wired (PostgreSQL + MongoDB configured), it queries active orgs from
// `tbl_org` (status="ACTIVE"), then their URLs from Mongo's `source_crawl`
// collection (projectId in activeOrgIDs, source="tiktok"), so enabling/
// disabling an org takes effect on the next crawl cycle without a bot
// restart. Otherwise it falls back to the static cfg.URLs/cfg.URLOrgMap
// loaded from the URLS env var (local runs, tests).
func (c *Crawler) loadURLs() ([]string, map[string]int, map[string]string, error) {
	if c.orgRepo == nil || c.urlRepo == nil {
		urls := make([]string, len(c.cfg.URLs))
		copy(urls, c.cfg.URLs)
		return urls, c.cfg.URLOrgMap, c.cfg.URLSourceOwnershipMap, nil
	}

	orgs, err := c.orgRepo.FindActiveOrgs()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loadURLs: FindActiveOrgs: %w", err)
	}
	if len(orgs) == 0 {
		c.log.Warn("loadURLs: no active orgs found in `tbl_org` (status=ACTIVE)")
		return nil, nil, nil, nil
	}

	orgIDs := make([]int, len(orgs))
	for i, o := range orgs {
		orgIDs[i] = o.OrgID
		c.log.Info("loadURLs: active org", zap.Int("org_id", o.OrgID), zap.String("name", o.Name))
	}

	urlEntries, err := c.urlRepo.FindByOrgIDs(orgIDs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loadURLs: FindByOrgIDs: %w", err)
	}

	activeOrgSet := make(map[int]bool, len(orgIDs))
	for _, id := range orgIDs {
		activeOrgSet[id] = true
	}

	urls := make([]string, 0, len(urlEntries))
	urlOrgMap := make(map[string]int, len(urlEntries))
	urlSourceOwnershipMap := make(map[string]string, len(urlEntries))
	for _, u := range urlEntries {
		// projectId is an array — attribute the URL to whichever active
		// org_id it belongs to (a URL doc may list multiple projects).
		matchedOrgID := 0
		for _, pid := range u.ProjectID {
			if activeOrgSet[pid] {
				matchedOrgID = pid
				break
			}
		}
		if matchedOrgID == 0 {
			c.log.Warn("loadURLs: source_crawl doc matched query but has no active org_id in projectId, skipping",
				zap.String("url", u.URL),
			)
			continue
		}
		// TEMP: skip org_id 18576 for testing other URLs — remove after testing.
		if matchedOrgID == 18576 {
			c.log.Info("loadURLs: skipping org_id=18576 (test filter)", zap.String("url", u.URL))
			continue
		}
		urls = append(urls, u.URL)
		urlOrgMap[u.URL] = matchedOrgID
		urlSourceOwnershipMap[u.URL] = u.SourceOwnership
	}

	c.log.Info("loadURLs: loaded active URLs from MongoDB",
		zap.Int("active_org_count", len(orgIDs)),
		zap.Int("active_url_count", len(urls)),
	)

	return urls, urlOrgMap, urlSourceOwnershipMap, nil
}

// logURLsByOrg groups urls by their org_id (via urlOrgMap) and logs each
// group, so the resolved crawl list is visible before the browser opens.
func (c *Crawler) logURLsByOrg(urls []string, urlOrgMap map[string]int) {
	byOrg := make(map[int][]string)
	for _, u := range urls {
		orgID := urlOrgMap[u]
		byOrg[orgID] = append(byOrg[orgID], u)
	}

	orgIDs := make([]int, 0, len(byOrg))
	for orgID := range byOrg {
		orgIDs = append(orgIDs, orgID)
	}
	sort.Ints(orgIDs)

	for _, orgID := range orgIDs {
		c.log.Info("loadURLs: URLs for org",
			zap.Int("org_id", orgID),
			zap.Int("url_count", len(byOrg[orgID])),
			zap.Strings("urls", byOrg[orgID]),
		)
	}
}

// splitURLs distributes URLs across n profiles in a round-robin fashion.
func splitURLs(urls []string, n int) [][]string {
	chunks := make([][]string, n)
	for i, u := range urls {
		chunks[i%n] = append(chunks[i%n], u)
	}
	return chunks
}

// runProfile runs the crawl for a single GPM profile.
func (c *Crawler) runProfile(ctx context.Context, profileID string, urls []string, idx int) {
	tag := fmt.Sprintf("[P%d|%s...]", idx+1, profileID[:8])

	// Generate session ID for this profile run and use session-aware logger.
	sessionID := logger.NewSessionID()
	sessionLog := logger.WithSession(c.log, sessionID)
	sessionLog.Info("Profile run started",
		zap.String("tag", tag),
		zap.Int("total_urls", len(urls)),
	)

	pw, err := playwright.Run()
	if err != nil {
		sessionLog.Sugar().Errorf("%s playwright error: %v", tag, err)
		return
	}
	defer func() { _ = pw.Stop() }()

	gpmClient := gpm.NewClient(c.cfg.GPMAPI, sessionLog)
	c.fetcher.CrawlURLs(ctx, pw, gpmClient, profileID, urls, sessionLog, tag)
}

// Utility functions shared across the crawler package.

// containsAny returns true if s contains any of the substrings in subs.
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// toString converts an arbitrary value to its string representation.
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// toFloat converts an arbitrary value to float64, returning 0 for non-float values.
func toFloat(v any) float64 {
	if v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// mapGet safely retrieves a value from a map, returning nil if the map is nil.
func mapGet(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// backoffDuration returns the exponential backoff duration for the given
// retry attempt: 1s → 2s → 4s → 8s → ...
func backoffDuration(attempt int) time.Duration {
	return time.Duration(1<<(attempt-1)) * time.Second
}

// Shutdown gracefully shuts down the Crawler's publisher, draining any
// buffered videos and flushing the final batch to the API.
func (c *Crawler) Shutdown() {
	if c.publisher != nil {
		c.log.Info("Crawler: shutting down publisher...")
		c.publisher.Shutdown()
		c.log.Info("Crawler: publisher shutdown complete")
	}
}
