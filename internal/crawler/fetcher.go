package crawler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"abp-bot-tiktok-url/internal/parser"
	"abp-bot-tiktok-url/internal/utils"
	"abp-bot-tiktok-url/pkg/config"
	"abp-bot-tiktok-url/pkg/gpm"

	"github.com/playwright-community/playwright-go"
	"go.uber.org/zap"
)

const (
	itemDetailAPI   = "/api/item/detail/"
	postItemListAPI = "/api/post/item_list/"
	cutoffSpan      = 7 * 24 * 60 * 60
)

// urlKind classifies a TikTok URL so Fetcher knows which internal API to
// intercept and how to load the page (a single video vs. a scrolling feed).
type urlKind int

const (
	urlKindUnknown urlKind = iota
	urlKindVideo
	urlKindProfile
)

// classifyURL inspects a TikTok URL and determines whether it points at a
// single video or a profile (user) page.
func classifyURL(rawURL string) urlKind {
	if strings.Contains(rawURL, "/video/") {
		return urlKindVideo
	}
	if strings.Contains(rawURL, "/@") {
		return urlKindProfile
	}
	return urlKindUnknown
}

// Fetcher orchestrates TikTok crawling by navigating directly to given
// video/profile URLs — no search involved — collecting API responses,
// parsing videos, and pushing to the backend API. This separates the fetch
// concern from the Crawler orchestrator.
type Fetcher struct {
	cfg       *config.Config
	publisher *Publisher
	gpmSvc    *GPMService
	scraper   *Scraper
}

// NewFetcher creates a Fetcher wired with all its dependencies.
func NewFetcher(cfg *config.Config, publisher *Publisher, gpmSvc *GPMService, scraper *Scraper) *Fetcher {
	return &Fetcher{
		cfg:       cfg,
		publisher: publisher,
		gpmSvc:    gpmSvc,
		scraper:   scraper,
	}
}

// CrawlURLs runs the full crawl loop for a set of TikTok URLs within a
// single GPM profile session.
func (f *Fetcher) CrawlURLs(ctx context.Context, pw *playwright.Playwright, gpmClient gpm.GPMClient, profileID string, urls []string, sessionLog *zap.Logger, tag string) {
	sessionLog.Info("Crawl session started",
		zap.Int("total_urls", len(urls)),
		zap.String("profile_id", profileID[:8]),
	)

	total := len(urls)
	i := 0
	pageCount := 0

	for i < total {
		select {
		case <-ctx.Done():
			sessionLog.Warn("crawlURLs: context cancelled, stopping crawl session")
			return
		default:
		}

		// Pagination guard: enforce per-session page limit.
		if f.cfg.MaxPagesPerSession > 0 && pageCount >= f.cfg.MaxPagesPerSession {
			sessionLog.Sugar().Warnf("reached maxPagesPerSession=%d", f.cfg.MaxPagesPerSession)
			return
		}
		pageCount++

		batchSize := utils.RandInt(f.cfg.BatchMin, f.cfg.BatchMax)
		batch := urls[i:min(i+batchSize, total)]

		if !utils.WaitForResources(ctx, sessionLog, tag) {
			return
		}

		browser, bctx, err := f.gpmSvc.Connect(ctx, pw, gpmClient, profileID, sessionLog)
		if err != nil {
			sessionLog.Sugar().Warnf("%s GPM connect failed after retries: %v", tag, err)
			for _, u := range batch {
				sessionLog.Sugar().Infof("%s %q -> 0 videos pushed to API (GPM connect failed)", tag, u)
			}
			i += batchSize
			continue
		}

		// Dùng anonymous func + defer để đảm bảo browser luôn được đóng
		func() {
			defer func() { _ = browser.Close() }()
			defer func() { _ = gpmClient.StopProfile(ctx, profileID) }()

			for urlIdx, targetURL := range batch {
				select {
				case <-ctx.Done():
					sessionLog.Warn("crawlURLs: context cancelled during url batch")
					return
				default:
				}

				sessionLog.Sugar().Infof("%s -> crawling url %d/%d: %q", tag, i+urlIdx+1, total, targetURL)

				page, err := f.scraper.CreatePageWithRetry(bctx, 3, sessionLog)
				if err != nil {
					sessionLog.Sugar().Infof("%s %q -> 0 videos pushed to API", tag, targetURL)
				} else {
					f.CrawlURL(ctx, page, targetURL, sessionLog, tag)
					_ = page.Close()
				}

				if urlIdx < len(batch)-1 {
					sleepSec := utils.RandInt(f.cfg.SleepMinURL, f.cfg.SleepMaxURL)
					sessionLog.Sugar().Infof("%s -> waiting %ds before next url", tag, sleepSec)
					select {
					case <-time.After(time.Duration(sleepSec) * time.Second):
					case <-ctx.Done():
						return
					}
				}
			}
		}()

		i += batchSize
		if i < total {
			restSec := utils.RandInt(f.cfg.RestMinSession, f.cfg.RestMaxSession)
			sessionLog.Sugar().Infof("%s -> batch done (%d/%d urls), waiting %ds before next batch", tag, i, total, restSec)
			select {
			case <-time.After(time.Duration(restSec) * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

// CrawlURL crawls a single TikTok URL directly: navigates to the video or
// profile page, scrolls to load more (for profiles), collects the
// intercepted API response items, parses them into VideoItem models, and
// pushes them to the backend API.
func (f *Fetcher) CrawlURL(ctx context.Context, page playwright.Page, targetURL string, log *zap.Logger, tag string) {
	kind := classifyURL(targetURL)
	if kind == urlKindUnknown {
		log.Sugar().Warnf("%s %q -> unsupported URL (expected a TikTok video or profile URL), skipping", tag, targetURL)
		return
	}

	_ = page.Route("**/*", func(route playwright.Route) {
		switch route.Request().ResourceType() {
		case "stylesheet", "font", "image", "media", "other":
			_ = route.Abort()
		default:
			_ = route.Continue()
		}
	})

	matchAPI := itemDetailAPI
	if kind == urlKindProfile {
		matchAPI = postItemListAPI
	}

	var mu sync.Mutex
	var collectedItems []map[string]any

	page.On("response", func(res playwright.Response) {
		if !containsAny(res.URL(), []string{matchAPI}) {
			return
		}
		go func(res playwright.Response) {
			var body map[string]any
			if err := res.JSON(&body); err != nil || body == nil {
				log.Sugar().Warnf("%s %q -> failed to parse API response: %v", tag, targetURL, err)
				return
			}
			statusCode := extractStatusCode(body)

			// Detect rate limit / captcha
			if statusCode == 2061 || statusCode == 10000 || statusCode == -1 {
				log.Sugar().Warnf("%s %q -> rate limit detected (status=%v), pausing 5 minutes", tag, targetURL, statusCode)
				select {
				case <-time.After(5 * time.Minute):
				case <-ctx.Done():
				}
				return
			}

			if statusCode != 0 {
				return
			}

			items := extractItems(body, kind)

			mu.Lock()
			defer mu.Unlock()
			seen := make(map[string]bool)
			for _, existing := range collectedItems {
				if id, ok := existing["id"].(string); ok {
					seen[id] = true
				}
			}
			for _, item := range items {
				id, _ := item["id"].(string)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				collectedItems = append(collectedItems, item)
			}
		}(res)
	})

	if _, err := page.Goto(targetURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		log.Sugar().Infof("%s %q -> 0 videos pushed to API", tag, targetURL)
		return
	}
	_, _ = page.Evaluate("window.moveTo(0, 0); window.resizeTo(screen.availWidth, screen.availHeight);")
	utils.Sleep(4000, 7000)
	if err := utils.RandomMouseMove(page); err != nil {
		log.Sugar().Warnf("%s %q -> RandomMouseMove error (non-critical): %v", tag, targetURL, err)
	}
	utils.Sleep(1500, 3000)

	if kind == urlKindProfile {
		// Scroll to trigger pagination through /api/post/item_list/.
		scrollTimes := utils.RandInt(10, 15)
		if err := utils.HumanScroll(page, scrollTimes); err != nil {
			log.Sugar().Warnf("%s %q -> HumanScroll error (non-critical): %v", tag, targetURL, err)
		}
		if err := utils.RandomViewVideo(page); err != nil {
			log.Sugar().Warnf("%s %q -> RandomViewVideo error (non-critical): %v", tag, targetURL, err)
		}
	}
	utils.Sleep(1500, 2500)

	mu.Lock()
	items := collectedItems
	mu.Unlock()

	orgID := f.cfg.URLOrgMap[targetURL]
	sourceOwnership := f.cfg.URLSourceOwnershipMap[targetURL]
	if sourceOwnership == "" {
		sourceOwnership = "nature"
	}
	results := f.publisher.ParseVideos(targetURL, orgID, sourceOwnership, items)

	// Pagination guard: enforce per-URL video limit (only meaningful for
	// profile pages, which can yield many videos from one URL).
	if f.cfg.MaxVideosPerURL > 0 && len(results) > f.cfg.MaxVideosPerURL {
		log.Sugar().Warnf("reached maxVideosPerURL=%d for url=%s", f.cfg.MaxVideosPerURL, targetURL)
		results = results[:f.cfg.MaxVideosPerURL]
	}

	// Log the parsed results before pushing so they're visible in logs too.
	for _, v := range results {
		post := parser.FromVideoItem(v)
		payload, _ := json.Marshal(post)
		log.Sugar().Infof("%s %q -> crawled video: %s", tag, targetURL, string(payload))
	}

	if len(results) > 0 {
		f.publisher.PushBatch(ctx, results)
	}
	log.Sugar().Infof("%s %q -> %d videos queued for API push", tag, targetURL, len(results))
}

// extractStatusCode reads TikTok's response status code, tolerating both the
// snake_case and camelCase key spellings TikTok uses across its internal
// endpoints.
func extractStatusCode(body map[string]any) float64 {
	if v, ok := body["status_code"].(float64); ok {
		return v
	}
	if v, ok := body["statusCode"].(float64); ok {
		return v
	}
	return 0
}

// extractItems pulls the video item(s) out of a TikTok internal API
// response, depending on the URL kind:
//   - video: a single item under itemInfo.itemStruct
//   - profile: a list of items under itemList (or item_list)
func extractItems(body map[string]any, kind urlKind) []map[string]any {
	if kind == urlKindVideo {
		if info, ok := body["itemInfo"].(map[string]any); ok {
			if item, ok := info["itemStruct"].(map[string]any); ok {
				return []map[string]any{item}
			}
		}
		return nil
	}

	var raw []any
	if v, ok := body["itemList"].([]any); ok {
		raw = v
	} else if v, ok := body["item_list"].([]any); ok {
		raw = v
	}

	items := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if item, ok := r.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}
