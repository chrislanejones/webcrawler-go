package crawler

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"webcrawler/internal/parser"

	"golang.org/x/net/html"
)

type SearchMode int

const (
	ModeSearchLink SearchMode = iota + 1
	ModeSearchWord
	ModeBrokenLinks
	ModeOversizedImages
	ModePDFCapture
	ModeSitemap
	ModeJSONFeed
)

func (m SearchMode) String() string {
	switch m {
	case ModeSearchLink:
		return "Find Link"
	case ModeSearchWord:
		return "Find Word/Phrase"
	case ModeBrokenLinks:
		return "Broken Link Check"
	case ModeOversizedImages:
		return "Oversized Image Check"
	case ModePDFCapture:
		return "Page Capture"
	case ModeSitemap:
		return "XML Sitemap Generator"
	case ModeJSONFeed:
		return "JSON Feed Capture"
	default:
		return "Unknown"
	}
}

type CaptureFormat int

const (
	CapturePDFOnly CaptureFormat = iota + 1
	CaptureImagesOnly
	CaptureBoth
	CaptureCMYKPDF
	CaptureCMYKTIFF
)

func (c CaptureFormat) String() string {
	switch c {
	case CapturePDFOnly:
		return "PDF only"
	case CaptureImagesOnly:
		return "Images only (PNG)"
	case CaptureBoth:
		return "PDF + Images"
	case CaptureCMYKPDF:
		return "CMYK PDF (for print)"
	case CaptureCMYKTIFF:
		return "CMYK TIFF (for InDesign)"
	default:
		return "Unknown"
	}
}

type SitemapOptions struct {
	Filename       string
	ChangeFreq     string
	Priority       float64
	IncludeLastMod bool
}

type JSONFeedOptions struct {
	FeedURL       string // URL of the JSON feed
	TagFilter     string // Optional tag to filter items by
	LinkField     string // JSON field containing the article link (default: "link")
	HeadlineField string // JSON field containing the headline (default: "headline")
	DateField     string // JSON field containing the date (default: "date")
	BriefField    string // JSON field containing the brief/summary (default: "brief")
	TagsField     string // JSON field containing tags (default: "tags")
}

type Config struct {
	StartURL           string
	AltEntryPoints     []string
	Mode               SearchMode
	SearchTarget       string
	MaxConcurrency     int
	ImageSizeThreshold int64
	MaxRetries         int
	RetryDelay         time.Duration
	RetryBlockedPages  bool
	BlockedRetryPasses int
	CaptureFormat      CaptureFormat
	PathFilter         string // Only crawl URLs starting with this path (e.g., "/newsroom/")
	IgnoreQueryParams  bool   // Treat URLs with different query params as the same page
	SitemapOpts        SitemapOptions
	JSONFeedOpts       JSONFeedOptions
}

type Stats struct {
	PagesChecked      int64
	PagesQueued       int64
	MatchesFound      int64
	ErrorCount        int64
	BlockedCount      int64
	RetryCount        int64
	BytesDownloaded   int64
	PDFsScanned       int64
	DOCXScanned       int64
	HTMLScanned       int64
	ImagesChecked     int64
	LinksChecked      int64
	LinksBlocked      int64
	SkippedExternal   int64
	Status2xx         int64
	Status3xx         int64
	Status4xx         int64
	Status5xx         int64
	Timeouts          int64
	DNSErrors         int64
	SSLErrors         int64
	ConnectionRefused int64
	BlockedRetried    int64
	BlockedRecovered  int64
}

type BlockedPage struct {
	URL       string
	Attempts  int
	LastError string
}

var (
	visited       sync.Map
	blockedQueue  sync.Map
	wg            sync.WaitGroup
	sema          chan struct{}
	csvMu         sync.Mutex
	stats         Stats
	startTime     time.Time
	httpClient    *http.Client
	resultFile    string
	config        Config
	baseURL       *url.URL
	successfulHit bool
	successMu     sync.Mutex
)

// Keep these close to current. A user agent claiming a browser several years
// old is itself a bot signal, which is what the rotation is trying to avoid.
// Last refreshed September 2026.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:154.0) Gecko/20100101 Firefox/154.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36 Edg/152.0.0.0",
}

// UserAgentFor returns a user agent from the rotation. Callers outside this
// package use it so there is one list to keep current, not several.
func UserAgentFor(i int) string {
	if i < 0 {
		i = -i
	}
	return userAgents[i%len(userAgents)]
}

// NewTransport returns the transport every client in this program should use.
// The ForceAttemptHTTP2 flag is the point of it: setting TLSClientConfig on a
// custom Transport otherwise leaves the client on HTTP/1.1, which contradicts
// the browser it claims to be.
func NewTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2:   true,
		DisableKeepAlives:   false,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

// BrowserHeaders sets the header block a real browser sends alongside its user
// agent. Sending a Chrome user agent with no Accept or Accept-Language is an
// easy bot tell, so every outbound request should carry these.
// Accept-Encoding is deliberately absent: setting it by hand switches off Go's
// transparent decompression.
func BrowserHeaders(req *http.Request, ua string) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
}

func init() {
	jar, _ := cookiejar.New(nil)

	httpClient = &http.Client{
		Timeout:   30 * time.Second,
		Jar:       jar,
		Transport: NewTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if len(via) > 0 {
				for key, val := range via[0].Header {
					req.Header[key] = val
				}
			}
			return nil
		},
	}
}

func Start(cfg Config) {
	visited = sync.Map{}
	blockedQueue = sync.Map{}
	stats = Stats{}
	startTime = time.Now()
	config = cfg
	successfulHit = false

	sema = make(chan struct{}, cfg.MaxConcurrency)

	var err error
	baseURL, err = url.Parse(cfg.StartURL)
	if err != nil {
		fmt.Printf("❌ Invalid start URL: %v\n", err)
		return
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	switch cfg.Mode {
	case ModeSearchLink, ModeSearchWord:
		resultFile = fmt.Sprintf("results-search-%s.csv", timestamp)
	case ModeBrokenLinks:
		resultFile = fmt.Sprintf("results-broken-links-%s.csv", timestamp)
	case ModeOversizedImages:
		resultFile = fmt.Sprintf("results-oversized-images-%s.csv", timestamp)
	case ModePDFCapture:
		// PDF capture uses its own output handling
		StartPDFCapture(cfg)
		return
	case ModeSitemap:
		// Sitemap uses its own output handling
		StartSitemapGeneration(cfg)
		return
	case ModeJSONFeed:
		// JSON feed uses its own output handling
		StartJSONFeedCapture(cfg)
		return
	}

	createCSV()

	stopStats := make(chan bool)
	go printLiveStats(stopStats)

	fmt.Println("┌─────────────────── CRAWL STARTING ───────────────────┐")
	fmt.Printf("│  🎯 Target: %-40s │\n", truncateString(cfg.StartURL, 40))
	fmt.Println("└──────────────────────────────────────────────────────┘")
	fmt.Println()

	if len(cfg.AltEntryPoints) > 0 {
		fmt.Println("🚪 PHASE 1: Starting from alternative entry points...")
		fmt.Println()

		for i, entryPoint := range cfg.AltEntryPoints {
			fmt.Printf("   📍 Entry point %d/%d: %s\n", i+1, len(cfg.AltEntryPoints), entryPoint)
			crawl(entryPoint)
		}

		blockedQueue.Store(cfg.StartURL, &BlockedPage{URL: cfg.StartURL, Attempts: 0})
	} else {
		crawl(cfg.StartURL)
	}

	wg.Wait()

	if cfg.RetryBlockedPages {
		for pass := 1; pass <= cfg.BlockedRetryPasses; pass++ {
			blockedCount := countBlockedQueue()
			if blockedCount == 0 {
				break
			}

			fmt.Printf("\n\n🔄 PHASE %d: RETRYING BLOCKED PAGES (%d pages)\n", pass+1, blockedCount)
			fmt.Println("   💡 Using cookies/session from successful requests...")
			fmt.Println()

			if pass > 1 {
				delay := time.Duration(pass*5) * time.Second
				fmt.Printf("   ⏳ Waiting %v before retry pass...\n", delay)
				time.Sleep(delay)
			}

			retryBlockedPages()
			wg.Wait()
		}
	}

	stopStats <- true
	printFinalStats()
}

func countBlockedQueue() int {
	count := 0
	blockedQueue.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

func retryBlockedPages() {
	blockedQueue.Range(func(key, value interface{}) bool {
		pageURL := key.(string)
		page := value.(*BlockedPage)

		if page.Attempts >= config.BlockedRetryPasses {
			return true
		}

		page.Attempts++
		atomic.AddInt64(&stats.BlockedRetried, 1)

		blockedQueue.Delete(pageURL)
		visited.Delete(getVisitedKey(pageURL))

		wg.Add(1)
		go func(link string, attemptNum int) {
			defer wg.Done()
			sema <- struct{}{}
			defer func() { <-sema }()

			fmt.Printf("   🔄 Retrying: %s\n", link)
			time.Sleep(time.Duration(attemptNum) * time.Second)

			success := fetchPageForRetry(link, attemptNum)
			if success {
				atomic.AddInt64(&stats.BlockedRecovered, 1)
				fmt.Printf("   ✅ RECOVERED: %s\n", link)
			}
		}(pageURL, page.Attempts)

		return true
	})
}

func printLiveStats(stop chan bool) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			elapsed := time.Since(startTime)
			checked := atomic.LoadInt64(&stats.PagesChecked)
			matches := atomic.LoadInt64(&stats.MatchesFound)
			errors := atomic.LoadInt64(&stats.ErrorCount)
			blocked := atomic.LoadInt64(&stats.BlockedCount)
			bytesDown := atomic.LoadInt64(&stats.BytesDownloaded)
			recovered := atomic.LoadInt64(&stats.BlockedRecovered)

			pagesPerSec := float64(checked) / elapsed.Seconds()
			bytesPerSec := float64(bytesDown) / elapsed.Seconds()

			blockedQueueSize := countBlockedQueue()

			fmt.Printf("\r📊 [%s] Pages: %d | Matches: %d | Errors: %d | Blocked: %d (Queue: %d, Recovered: %d) | %.1f p/s | %s/s     ",
				formatDuration(elapsed),
				checked,
				matches,
				errors,
				blocked,
				blockedQueueSize,
				recovered,
				pagesPerSec,
				formatBytes(int64(bytesPerSec)),
			)
		}
	}
}

func printFinalStats() {
	elapsed := time.Since(startTime)

	fmt.Println()
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      📊 FINAL STATISTICS 📊                       ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                   ║")
	fmt.Printf("║  ⏱️  Total Time:           %-40s ║\n", formatDuration(elapsed))
	fmt.Printf("║  📄 Pages Checked:         %-40d ║\n", stats.PagesChecked)
	fmt.Printf("║  ✅ Matches Found:         %-40d ║\n", stats.MatchesFound)
	fmt.Printf("║  📁 Results File:          %-40s ║\n", truncateString(resultFile, 40))
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      🔬 CONTENT BREAKDOWN                         ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  📝 HTML Pages:            %-40d ║\n", stats.HTMLScanned)
	fmt.Printf("║  📕 PDF Documents:         %-40d ║\n", stats.PDFsScanned)
	fmt.Printf("║  📘 Word Documents:        %-40d ║\n", stats.DOCXScanned)
	fmt.Printf("║  🖼️  Images Checked:        %-40d ║\n", stats.ImagesChecked)
	fmt.Printf("║  🔗 Links Checked:         %-40d ║\n", stats.LinksChecked)
	fmt.Printf("║  ⏭️  Skipped (External):    %-40d ║\n", stats.SkippedExternal)
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      📡 NETWORK STATS                             ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  📥 Data Downloaded:       %-40s ║\n", formatBytes(stats.BytesDownloaded))
	fmt.Printf("║  🔄 Total Retries:         %-40d ║\n", stats.RetryCount)
	fmt.Printf("║  ❌ Errors:                %-40d ║\n", stats.ErrorCount)
	fmt.Printf("║  🛡️  Blocked (Bot Detect):  %-40d ║\n", stats.BlockedCount)
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      🚪 CLOUDFLARE BYPASS STATS                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  🔄 Blocked Pages Retried: %-40d ║\n", stats.BlockedRetried)
	fmt.Printf("║  ✅ Successfully Recovered:%-40d ║\n", stats.BlockedRecovered)
	blockedRemaining := countBlockedQueue()
	fmt.Printf("║  ❌ Still Blocked:         %-40d ║\n", blockedRemaining)
	if stats.BlockedRetried > 0 {
		recoveryRate := float64(stats.BlockedRecovered) / float64(stats.BlockedRetried) * 100
		fmt.Printf("║  📈 Recovery Rate:         %-40s ║\n", fmt.Sprintf("%.1f%%", recoveryRate))
	}
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      📶 HTTP STATUS CODES                         ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  ✅ 2xx (Success):         %-40d ║\n", stats.Status2xx)
	fmt.Printf("║  ↪️  3xx (Redirect):        %-40d ║\n", stats.Status3xx)
	fmt.Printf("║  ⚠️  4xx (Client Error):    %-40d ║\n", stats.Status4xx)
	fmt.Printf("║  🔥 5xx (Server Error):    %-40d ║\n", stats.Status5xx)
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      🔌 CONNECTION ERRORS                         ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  ⏱️  Timeouts:              %-40d ║\n", stats.Timeouts)
	fmt.Printf("║  🌐 DNS Errors:            %-40d ║\n", stats.DNSErrors)
	fmt.Printf("║  🔒 SSL/TLS Errors:        %-40d ║\n", stats.SSLErrors)
	fmt.Printf("║  🚫 Connection Refused:    %-40d ║\n", stats.ConnectionRefused)
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      ⚡ PERFORMANCE                               ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")

	pagesPerSec := float64(stats.PagesChecked) / elapsed.Seconds()
	bytesPerSec := float64(stats.BytesDownloaded) / elapsed.Seconds()
	avgPageSize := int64(0)
	if stats.PagesChecked > 0 {
		avgPageSize = stats.BytesDownloaded / stats.PagesChecked
	}

	fmt.Printf("║  📈 Pages/Second:          %-40.2f ║\n", pagesPerSec)
	fmt.Printf("║  📊 Avg Download Speed:    %-40s ║\n", formatBytes(int64(bytesPerSec))+"/s")
	fmt.Printf("║  📐 Avg Page Size:         %-40s ║\n", formatBytes(avgPageSize))
	fmt.Println("║                                                                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")

	if blockedRemaining > 0 {
		fmt.Printf("\n⚠️  WARNING: %d pages still blocked after all retry attempts\n", blockedRemaining)
		fmt.Println("   💡 Tips:")
		fmt.Println("      - Try running again later (Cloudflare might be more lenient)")
		fmt.Println("      - Reduce concurrency to look less like a bot")
		fmt.Println("      - Some pages may genuinely require browser JavaScript")
	}
	if stats.BlockedRecovered > 0 {
		fmt.Printf("\n✅ SUCCESS: Recovered %d pages that were initially blocked!\n", stats.BlockedRecovered)
		fmt.Println("   💡 The alternative entry point strategy worked!")
	}
	if stats.ErrorCount > 10 {
		fmt.Printf("\n⚠️  WARNING: High error count (%d errors)\n", stats.ErrorCount)
		fmt.Println("   💡 The site may be having issues or blocking requests")
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func createCSV() {
	f, _ := os.Create(resultFile)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	switch config.Mode {
	case ModeSearchLink, ModeSearchWord:
		w.Write([]string{"URL", "ContentType", "FoundIn", "Target", "Timestamp"})
	case ModeBrokenLinks:
		w.Write([]string{"Result", "URL", "FoundOnPage", "StatusCode", "Error", "Timestamp"})
	case ModeOversizedImages:
		w.Write([]string{"ImageURL", "FoundOnPage", "SizeKB", "ContentType", "Timestamp"})
	}
}

func writeSearchResult(pageURL, contentType, foundIn string) {
	csvMu.Lock()
	defer csvMu.Unlock()
	atomic.AddInt64(&stats.MatchesFound, 1)

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{pageURL, contentType, foundIn, config.SearchTarget, time.Now().Format(time.RFC3339)})
}

func writeBrokenLink(brokenURL, foundOnPage string, statusCode int, errMsg string) {
	csvMu.Lock()
	defer csvMu.Unlock()
	atomic.AddInt64(&stats.MatchesFound, 1)

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"BROKEN", brokenURL, foundOnPage, strconv.Itoa(statusCode), errMsg, time.Now().Format(time.RFC3339)})
}

// writeBlockedLink records a link the crawler could not verify because the far
// end refused the request. These are reported separately from broken links:
// the page is probably fine, it just will not answer a script.
func writeBlockedLink(blockedURL, foundOnPage string, statusCode int) {
	csvMu.Lock()
	defer csvMu.Unlock()

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"BLOCKED", blockedURL, foundOnPage, strconv.Itoa(statusCode),
		"Refused by bot protection, verify in a browser", time.Now().Format(time.RFC3339)})
}

func writeOversizedImage(imageURL, foundOnPage string, sizeKB int64, contentType string) {
	csvMu.Lock()
	defer csvMu.Unlock()
	atomic.AddInt64(&stats.MatchesFound, 1)

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{imageURL, foundOnPage, strconv.FormatInt(sizeKB, 10), contentType, time.Now().Format(time.RFC3339)})
}

func crawl(link string) {
	visitedKey := getVisitedKey(link)
	if _, loaded := visited.LoadOrStore(visitedKey, true); loaded {
		return
	}

	atomic.AddInt64(&stats.PagesQueued, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		sema <- struct{}{}
		defer func() { <-sema }()

		fetchWithRetry(link)
	}()
}

func fetchWithRetry(link string) {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			atomic.AddInt64(&stats.RetryCount, 1)
			delay := config.RetryDelay * time.Duration(attempt)
			time.Sleep(delay)
		}

		success, blocked, err := fetchPage(link, attempt)
		if success {
			successMu.Lock()
			successfulHit = true
			successMu.Unlock()
			return
		}

		if blocked {
			blockedQueue.Store(link, &BlockedPage{URL: link, Attempts: 0, LastError: err.Error()})
			return
		}

		lastErr = err

		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "no such host") {
				break
			}
		}
	}

	if lastErr != nil {
		atomic.AddInt64(&stats.ErrorCount, 1)
	}
}

func fetchPage(link string, attempt int) (success bool, blocked bool, err error) {
	atomic.AddInt64(&stats.PagesChecked, 1)

	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return false, false, err
	}

	ua := userAgents[attempt%len(userAgents)]
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	// Note: Don't set Accept-Encoding manually - let Go's http client handle it
	// It will automatically add gzip and decompress responses
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")

	successMu.Lock()
	hadSuccess := successfulHit
	successMu.Unlock()
	if hadSuccess {
		req.Header.Set("Referer", config.StartURL)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		handleNetworkError(err)
		return false, false, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		atomic.AddInt64(&stats.Status2xx, 1)
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		atomic.AddInt64(&stats.Status3xx, 1)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		atomic.AddInt64(&stats.Status4xx, 1)
	case resp.StatusCode >= 500:
		atomic.AddInt64(&stats.Status5xx, 1)
	}

	if resp.StatusCode == 403 || resp.StatusCode == 503 {
		atomic.AddInt64(&stats.BlockedCount, 1)
		return false, true, fmt.Errorf("blocked: %d", resp.StatusCode)
	}

	if resp.StatusCode == 429 {
		atomic.AddInt64(&stats.BlockedCount, 1)
		return false, true, fmt.Errorf("rate limited")
	}

	if resp.StatusCode >= 400 {
		return false, false, fmt.Errorf("status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")

	// Go's http client automatically decompresses gzip when we don't set Accept-Encoding manually
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, false, err
	}

	atomic.AddInt64(&stats.BytesDownloaded, int64(len(bodyBytes)))

	if detectBotProtection(string(bodyBytes)) {
		atomic.AddInt64(&stats.BlockedCount, 1)
		return false, true, fmt.Errorf("bot protection detected")
	}

	switch config.Mode {
	case ModeSearchLink, ModeSearchWord:
		processSearchMode(link, contentType, bodyBytes)
	case ModeBrokenLinks:
		if strings.Contains(contentType, "text/html") {
			extractAndCheckLinks(bodyBytes, link)
		}
	case ModeOversizedImages:
		if strings.Contains(contentType, "text/html") {
			extractAndCheckImages(bodyBytes, link)
		}
	}

	if strings.Contains(contentType, "text/html") {
		atomic.AddInt64(&stats.HTMLScanned, 1)
		extractInternalLinks(bodyBytes, link)
	}

	return true, false, nil
}

func fetchPageForRetry(link string, retryAttempt int) bool {
	atomic.AddInt64(&stats.PagesChecked, 1)

	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return false
	}

	ua := userAgents[(retryAttempt+2)%len(userAgents)]
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Referer", config.StartURL)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == 403 || resp.StatusCode == 503 || resp.StatusCode == 429 {
			blockedQueue.Store(link, &BlockedPage{URL: link, Attempts: retryAttempt})
		}
		return false
	}

	contentType := resp.Header.Get("Content-Type")

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	atomic.AddInt64(&stats.BytesDownloaded, int64(len(bodyBytes)))

	if detectBotProtection(string(bodyBytes)) {
		blockedQueue.Store(link, &BlockedPage{URL: link, Attempts: retryAttempt})
		return false
	}

	atomic.AddInt64(&stats.Status2xx, 1)

	switch config.Mode {
	case ModeSearchLink, ModeSearchWord:
		processSearchMode(link, contentType, bodyBytes)
	case ModeBrokenLinks:
		if strings.Contains(contentType, "text/html") {
			extractAndCheckLinks(bodyBytes, link)
		}
	case ModeOversizedImages:
		if strings.Contains(contentType, "text/html") {
			extractAndCheckImages(bodyBytes, link)
		}
	}

	if strings.Contains(contentType, "text/html") {
		atomic.AddInt64(&stats.HTMLScanned, 1)
		extractInternalLinks(bodyBytes, link)
	}

	visited.Store(getVisitedKey(link), true)
	return true
}

func processSearchMode(link, contentType string, bodyBytes []byte) {
	target := config.SearchTarget

	switch {
	case strings.Contains(contentType, "application/pdf"):
		atomic.AddInt64(&stats.PDFsScanned, 1)
		if parser.ContainsLinkInPDF(bytes.NewReader(bodyBytes), target) {
			fmt.Printf("\n✅ MATCH FOUND IN PDF: %s\n", link)
			writeSearchResult(link, contentType, "PDF")
		}
	case strings.Contains(contentType, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"):
		atomic.AddInt64(&stats.DOCXScanned, 1)
		if parser.ContainsLinkInDocx(bytes.NewReader(bodyBytes), target) {
			fmt.Printf("\n✅ MATCH FOUND IN DOCX: %s\n", link)
			writeSearchResult(link, contentType, "DOCX")
		}
	case strings.Contains(contentType, "text/html"):
		if bytes.Contains(bodyBytes, []byte(target)) {
			fmt.Printf("\n✅ MATCH FOUND IN HTML: %s\n", link)
			writeSearchResult(link, contentType, "HTML")
		}
	}
}

func extractAndCheckLinks(body []byte, pageURL string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" && a.Val != "" &&
					!strings.HasPrefix(a.Val, "#") &&
					!strings.HasPrefix(a.Val, "mailto:") &&
					!strings.HasPrefix(a.Val, "tel:") &&
					!strings.HasPrefix(a.Val, "javascript:") {
					checkLink(a.Val, pageURL)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

func checkLink(href, pageURL string) {
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return
	}

	pageBase, err := url.Parse(pageURL)
	if err != nil {
		return
	}
	resolved := pageBase.ResolveReference(u).String()
	atomic.AddInt64(&stats.LinksChecked, 1)

	// Try HEAD first because it is cheap, then fall back to GET. Plenty of
	// CDNs answer HEAD with 405, or with a 404 they would not give a GET,
	// so a HEAD-only checker invents broken links.
	status, err := requestLink("HEAD", resolved)
	if err != nil || status == http.StatusMethodNotAllowed || status >= 400 {
		var getErr error
		status, getErr = requestLink("GET", resolved)
		if getErr != nil {
			err = getErr
		} else {
			err = nil
		}
	}

	if err != nil {
		writeBrokenLink(resolved, pageURL, 0, err.Error())
		fmt.Printf("\n💔 BROKEN LINK (error): %s\n", resolved)
		return
	}

	// A firewall refusing the request is not a dead page. Record it in its
	// own category so the report does not read as a 404.
	if isBlockedStatus(status) {
		atomic.AddInt64(&stats.LinksBlocked, 1)
		writeBlockedLink(resolved, pageURL, status)
		fmt.Printf("\n🛡️  BLOCKED, NOT VERIFIED (%d): %s\n", status, resolved)
		return
	}

	if status >= 400 {
		writeBrokenLink(resolved, pageURL, status, http.StatusText(status))
		fmt.Printf("\n💔 BROKEN LINK (%d): %s\n", status, resolved)
	}
}

// isBlockedStatus reports whether a status means "I will not serve this
// client" rather than "this page does not exist".
func isBlockedStatus(status int) bool {
	switch status {
	case http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	}
	return false
}

// requestLink performs one link check using the shared client, so the session
// cookies the crawler established are reused and the request looks like the
// browser the user agent claims.
func requestLink(method, link string) (int, error) {
	req, err := http.NewRequest(method, link, nil)
	if err != nil {
		return 0, err
	}
	BrowserHeaders(req, userAgents[0])
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func extractAndCheckImages(body []byte, pageURL string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if a.Key == "src" && a.Val != "" && !strings.HasPrefix(a.Val, "data:") {
					checkImage(a.Val, pageURL)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

func checkImage(src, pageURL string) {
	u, err := url.Parse(src)
	if err != nil {
		return
	}

	pageBase, err := url.Parse(pageURL)
	if err != nil {
		return
	}
	resolved := pageBase.ResolveReference(u).String()
	atomic.AddInt64(&stats.ImagesChecked, 1)

	req, err := http.NewRequest("GET", resolved, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", userAgents[0])

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	sizeBytes := int64(len(bodyBytes))
	sizeKB := sizeBytes / 1024

	if sizeBytes > config.ImageSizeThreshold {
		contentType := resp.Header.Get("Content-Type")
		writeOversizedImage(resolved, pageURL, sizeKB, contentType)
		fmt.Printf("\n🖼️  OVERSIZED IMAGE (%dKB): %s\n", sizeKB, resolved)
	}
}

func extractInternalLinks(body []byte, pageURL string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return
	}

	pageBase, err := url.Parse(pageURL)
	if err != nil {
		return
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					u, err := url.Parse(a.Val)
					if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
						continue
					}

					next := pageBase.ResolveReference(u).String()
					nextURL, err := url.Parse(next)
					if err != nil {
						continue
					}

					if nextURL.Host != baseURL.Host {
						atomic.AddInt64(&stats.SkippedExternal, 1)
						continue
					}

					time.Sleep(50 * time.Millisecond)
					crawl(next)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

func detectBotProtection(body string) bool {
	indicators := []string{
		"checking your browser",
		"ddos protection",
		"please enable javascript",
		"access denied",
		"security check",
		"verify you are human",
		"captcha",
		"incapsula",
		"perimeterx",
		"sucuri",
		"cloudflare",
		"please wait while we verify",
		"just a moment",
		"ray id",
		"attention required",
		"sorry, you have been blocked",
	}

	bodyLower := strings.ToLower(body)
	for _, indicator := range indicators {
		if strings.Contains(bodyLower, indicator) {
			return true
		}
	}
	return false
}

func handleNetworkError(err error) {
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "timeout"):
		atomic.AddInt64(&stats.Timeouts, 1)
	case strings.Contains(errStr, "connection refused"):
		atomic.AddInt64(&stats.ConnectionRefused, 1)
	case strings.Contains(errStr, "no such host"):
		atomic.AddInt64(&stats.DNSErrors, 1)
	case strings.Contains(errStr, "certificate"):
		atomic.AddInt64(&stats.SSLErrors, 1)
	}
}

// getVisitedKey returns the key to use for visited URL tracking.
// When IgnoreQueryParams is enabled, it strips query parameters so that
// URLs like page.html?a=1 and page.html?b=2 are treated as the same page.
func getVisitedKey(link string) string {
	if !config.IgnoreQueryParams {
		return link
	}

	u, err := url.Parse(link)
	if err != nil {
		return link
	}

	// Strip query parameters and fragment
	u.RawQuery = ""
	u.Fragment = ""

	return u.String()
}
