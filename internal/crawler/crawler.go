package crawler

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	SearchTarget       string   // One term; used when SearchTerms is empty
	SearchTerms        []string // Several terms, each reported on its own row
	IgnoreCase         bool     // Match terms regardless of letter case
	MaxConcurrency     int
	ImageSizeThreshold int64
	MaxRetries         int
	RetryDelay         time.Duration
	RetryBlockedPages  bool
	BlockedRetryPasses int
	CaptureFormat      CaptureFormat
	PathFilter         string        // Only crawl URLs starting with this path (e.g., "/newsroom/")
	IgnoreQueryParams  bool          // Treat URLs with different query params as the same page
	RequestsPerSecond  float64       // Shared request ceiling; 0 means DefaultRequestsPerSecond, below 0 means none
	MaxPages           int           // Stop queuing pages after this many; 0 means no limit
	MaxDuration        time.Duration // Stop queuing pages after this long; 0 means no limit
	OutputDir          string        // Where the results CSV goes; empty means the working directory
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
	ParseErrors       int64
}

type BlockedPage struct {
	URL       string
	Attempts  int
	LastError string
}

var (
	runCtx        = context.Background()
	stopReason    atomic.Value // string
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
// It verifies certificates, and falls back to an unverified connection for a
// host whose certificate fails, recording why (see TLSProblems). A site
// auditor wants to hear that a chain is broken, not to have it hidden, but
// the crawl should still reach the site the way a browser that has the
// missing intermediate cached would.
func NewTransport() http.RoundTripper {
	return &tlsFallback{
		verified:   newHTTPTransport(false),
		unverified: newHTTPTransport(true),
	}
}

// newHTTPTransport builds the underlying transport. The ForceAttemptHTTP2
// flag matters: setting TLSClientConfig on a custom Transport otherwise
// leaves the client on HTTP/1.1, which contradicts the browser it claims to be.
func newHTTPTransport(skipVerify bool) *http.Transport {
	return &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: skipVerify},
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

type tlsFallback struct {
	verified, unverified *http.Transport
}

// tlsProblems maps a host to why its certificate failed verification.
var tlsProblems sync.Map

func (t *tlsFallback) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, bad := tlsProblems.Load(req.URL.Host); bad {
		return t.unverified.RoundTrip(req)
	}
	resp, err := t.verified.RoundTrip(req)
	var certErr *tls.CertificateVerificationError
	if err == nil || !errors.As(err, &certErr) {
		return resp, err
	}
	// Only requests that can be sent twice are retried. The crawler only
	// sends GET and HEAD, which have no body.
	if req.Body != nil && req.GetBody == nil {
		return resp, err
	}
	reason := describeCertError(certErr.Err)
	if _, loaded := tlsProblems.LoadOrStore(req.URL.Host, reason); !loaded {
		atomic.AddInt64(&stats.SSLErrors, 1)
		fmt.Printf("\n🔒 TLS PROBLEM on %s: %s. Continuing without verification.\n", req.URL.Host, reason)
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		if retry.Body, err = req.GetBody(); err != nil {
			return nil, err
		}
	}
	return t.unverified.RoundTrip(retry)
}

func describeCertError(err error) string {
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &unknown):
		return "certificate chain is incomplete or not trusted"
	case errors.As(err, &hostname):
		return "certificate does not match the host name"
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "certificate has expired or is not yet valid"
	default:
		return err.Error()
	}
}

// TLSProblems returns each host whose certificate failed verification during
// this run, with the reason.
func TLSProblems() map[string]string {
	out := map[string]string{}
	tlsProblems.Range(func(k, v any) bool {
		out[k.(string)] = v.(string)
		return true
	})
	return out
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
	stopReason.Store("")
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

// Start runs a crawl to completion.
func Start(cfg Config) {
	StartContext(context.Background(), cfg)
}

// StartContext runs a crawl that stops early when ctx is done, or when the
// page or time limit in cfg is reached. Stopping is graceful: no new pages
// are queued, requests already out are allowed to finish, and the results
// file and final statistics are still written.
//
// The limits and early stop apply to Find Link, Find Word, Broken Links and
// Oversized Images. Page Capture, Sitemap and JSON Feed run their own loops.
func StartContext(ctx context.Context, cfg Config) {
	if cfg.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.MaxDuration)
		defer cancel()
	}
	runCtx = ctx
	stopReason.Store("")

	visited = sync.Map{}
	blockedQueue = sync.Map{}
	checkedImages = sync.Map{}
	stats = Stats{}
	startTime = time.Now()
	config = cfg
	successfulHit = false

	rate := cfg.RequestsPerSecond
	if rate == 0 {
		rate = DefaultRequestsPerSecond
	}
	setRate(rate)

	sema = make(chan struct{}, cfg.MaxConcurrency)

	start, err := url.Parse(cfg.StartURL)
	if err != nil {
		fmt.Printf("❌ Invalid start URL: %v\n", err)
		return
	}
	siteBase.Store(start)

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	if cfg.OutputDir != "" {
		if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
			fmt.Printf("❌ Cannot create output folder: %v\n", err)
			return
		}
	}
	switch cfg.Mode {
	case ModeSearchLink, ModeSearchWord:
		resultFile = filepath.Join(cfg.OutputDir, fmt.Sprintf("results-search-%s.csv", timestamp))
	case ModeBrokenLinks:
		resultFile = filepath.Join(cfg.OutputDir, fmt.Sprintf("results-broken-links-%s.csv", timestamp))
	case ModeOversizedImages:
		resultFile = filepath.Join(cfg.OutputDir, fmt.Sprintf("results-oversized-images-%s.csv", timestamp))
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

		blockedQueue.Store(config.StartURL, &BlockedPage{URL: config.StartURL, Attempts: 0})
	} else {
		crawl(config.StartURL)
	}

	wg.Wait()

	if cfg.RetryBlockedPages {
		for pass := 1; pass <= cfg.BlockedRetryPasses; pass++ {
			blockedCount := countBlockedQueue()
			if blockedCount == 0 || stopping() {
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

	reportUnsearched()

	stopStats <- true
	printFinalStats()
}

func isSearchMode() bool {
	return config.Mode == ModeSearchLink || config.Mode == ModeSearchWord
}

// reportUnsearched writes a NOT SEARCHED row for every page still blocked
// after the retry passes. Without these rows, a page the server refused looks
// the same in the results as a page that was read and did not match.
func reportUnsearched() {
	if !isSearchMode() {
		return
	}
	blockedQueue.Range(func(key, value any) bool {
		page := value.(*BlockedPage)
		reason := "blocked by the server"
		if page.LastError != "" {
			reason += " (" + page.LastError + ")"
		}
		writeNotSearched(key.(string), "", reason)
		return true
	})
}

// finalURL is the address a response actually came from, after redirects.
// Relative links on the page resolve against it, as they do in a browser.
func finalURL(resp *http.Response, requested string) string {
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return requested
}

// siteBase is the site being crawled. It starts as the URL the user gave and
// moves once if the start page redirects to a different site.
var siteBase atomic.Pointer[url.URL]

// adoptStartRedirect handles a start page that lands on a different site
// (vaemergency.gov to vdem.virginia.gov, cdo to odga). That site is the one
// to crawl: every link on it points there, and the host the user typed would
// make all of them look external. It costs no extra request; it reads where
// the start page's own response came from.
func adoptStartRedirect(requested string, resp *http.Response) {
	if requested != config.StartURL || resp.Request == nil {
		return
	}
	landed := resp.Request.URL
	start := siteBase.Load()
	if sameSite(landed.Host, start.Host) {
		return
	}
	if siteBase.CompareAndSwap(start, landed) {
		fmt.Printf("\n↪️  %s redirects to %s; crawling that site.\n", start.Host, landed.Host)
	}
}

func countBlockedQueue() int {
	count := 0
	blockedQueue.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

func retryBlockedPages() {
	blockedQueue.Range(func(key, value any) bool {
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
	fmt.Printf("║  🚫 Unreadable Documents:  %-40d ║\n", stats.ParseErrors)
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

	if problems := TLSProblems(); len(problems) > 0 {
		hosts := slices.Sorted(maps.Keys(problems))
		fmt.Printf("\n🔒 TLS: %d host(s) failed certificate verification and were read without it:\n", len(hosts))
		for _, h := range hosts {
			fmt.Printf("      %s: %s\n", h, problems[h])
		}
	}
	if reason := StopReason(); reason != "" {
		fmt.Printf("\n⏹️  STOPPED EARLY: %s. Pages already found are in the results file.\n", reason)
	}
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

func writeSearchResult(pageURL, contentType, foundIn, term string) {
	csvMu.Lock()
	defer csvMu.Unlock()
	atomic.AddInt64(&stats.MatchesFound, 1)

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{pageURL, contentType, foundIn, term, time.Now().Format(time.RFC3339)})
}

// writeNotSearched records a page the search could not read, so a results
// file with no row for a URL means "searched, no match" and nothing else.
func writeNotSearched(pageURL, contentType, reason string) {
	csvMu.Lock()
	defer csvMu.Unlock()

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{pageURL, contentType, "NOT SEARCHED: " + reason, strings.Join(searchTerms(), " | "), time.Now().Format(time.RFC3339)})
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
	if stopping() {
		return
	}

	visitedKey := getVisitedKey(link)
	if _, loaded := visited.LoadOrStore(visitedKey, true); loaded {
		return
	}

	queued := atomic.AddInt64(&stats.PagesQueued, 1)
	if config.MaxPages > 0 && queued > int64(config.MaxPages) {
		atomic.AddInt64(&stats.PagesQueued, -1)
		stopReason.CompareAndSwap("", fmt.Sprintf("reached the %d page limit", config.MaxPages))
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		sema <- struct{}{}
		defer func() { <-sema }()

		// Queued before a Ctrl-C or the time limit, but not started:
		// leave it. Pages inside a page limit still run; the limit only
		// stops new pages being queued.
		if runCtx.Err() != nil {
			stopping()
			return
		}
		fetchWithRetry(link)
	}()
}

// stopping reports whether the crawl should stop taking on new pages, and
// records why the first time it notices.
func stopping() bool {
	if stopReason.Load() != "" {
		return true
	}
	switch runCtx.Err() {
	case nil:
		return false
	case context.DeadlineExceeded:
		stopReason.CompareAndSwap("", fmt.Sprintf("reached the %v time limit", config.MaxDuration))
	default:
		stopReason.CompareAndSwap("", "stopped by the user")
	}
	return true
}

// StopReason says why the last crawl ended early, or "" if it ran to the end.
func StopReason() string {
	return stopReason.Load().(string)
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
		// A missing page has nothing to search. Anything else that failed
		// every retry, a timeout or a 500, is a page that went unread.
		if isSearchMode() && !isMissingPage(lastErr) {
			writeNotSearched(link, "", lastErr.Error())
		}
	}
}

func isMissingPage(err error) bool {
	s := err.Error()
	return s == "status 404" || s == "status 410"
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

	resp, err := doPaced(httpClient, req)
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

	adoptStartRedirect(link, resp)
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
			extractAndCheckLinks(bodyBytes, finalURL(resp, link))
		}
	case ModeOversizedImages:
		if strings.Contains(contentType, "text/html") {
			extractAndCheckImages(bodyBytes, finalURL(resp, link))
		}
	}

	if strings.Contains(contentType, "text/html") {
		atomic.AddInt64(&stats.HTMLScanned, 1)
		extractInternalLinks(bodyBytes, finalURL(resp, link))
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

	resp, err := doPaced(httpClient, req)
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

	adoptStartRedirect(link, resp)
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
			extractAndCheckLinks(bodyBytes, finalURL(resp, link))
		}
	case ModeOversizedImages:
		if strings.Contains(contentType, "text/html") {
			extractAndCheckImages(bodyBytes, finalURL(resp, link))
		}
	}

	if strings.Contains(contentType, "text/html") {
		atomic.AddInt64(&stats.HTMLScanned, 1)
		extractInternalLinks(bodyBytes, finalURL(resp, link))
	}

	visited.Store(getVisitedKey(link), true)
	return true
}

func processSearchMode(link, contentType string, bodyBytes []byte) {
	var kind, text string
	var err error

	switch {
	case strings.Contains(contentType, "application/pdf"):
		atomic.AddInt64(&stats.PDFsScanned, 1)
		kind = "PDF"
		text, err = parser.PDFText(bodyBytes)
	case strings.Contains(contentType, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"):
		atomic.AddInt64(&stats.DOCXScanned, 1)
		kind = "DOCX"
		text, err = parser.DocxText(bodyBytes)
	case strings.Contains(contentType, "text/html"):
		kind = "HTML"
		text = string(bodyBytes)
	default:
		return
	}

	// A document that cannot be read was not searched. Say so, rather than
	// letting it pass as "no match".
	if err != nil {
		atomic.AddInt64(&stats.ParseErrors, 1)
		writeNotSearched(link, contentType, kind+" unreadable: "+err.Error())
		return
	}

	for _, term := range matchedTerms(text) {
		fmt.Printf("\n✅ MATCH FOUND IN %s: %s (%s)\n", kind, link, term)
		writeSearchResult(link, contentType, kind, term)
	}
}

// searchTerms returns the terms this crawl looks for.
func searchTerms() []string {
	if len(config.SearchTerms) > 0 {
		return config.SearchTerms
	}
	if config.SearchTarget != "" {
		return []string{config.SearchTarget}
	}
	return nil
}

// matchedTerms returns the search terms found in text, in the order given.
func matchedTerms(text string) []string {
	if config.IgnoreCase {
		text = strings.ToLower(text)
	}
	var found []string
	for _, term := range searchTerms() {
		needle := term
		if config.IgnoreCase {
			needle = strings.ToLower(term)
		}
		if strings.Contains(text, needle) {
			found = append(found, term)
		}
	}
	return found
}

// SplitTerms turns "a | b | c" into its terms. A bar separates them because
// commas turn up inside ordinary phrases ("Richmond, VA") and bars rarely do.
func SplitTerms(s string) []string {
	var terms []string
	for _, part := range strings.Split(s, "|") {
		if p := strings.TrimSpace(part); p != "" {
			terms = append(terms, p)
		}
	}
	return terms
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
	//
	// A transport error is not retried. DNS failures, refused connections
	// and timeouts fail the same way for GET, and retrying doubles the wait
	// on exactly the links that are slowest to fail.
	status, err := requestLink("HEAD", resolved)
	if err == nil && (status == http.StatusMethodNotAllowed || status >= 400) {
		status, err = requestLink("GET", resolved)
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

// linkCheckTimeout bounds a single link check. Checking a link is cheaper
// than fetching a page to crawl, so it does not need the client's full
// timeout, and a site full of dead external links should not stall a run.
var linkCheckTimeout = 15 * time.Second

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
	ctx, cancel := context.WithTimeout(context.Background(), linkCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return 0, err
	}
	BrowserHeaders(req, userAgents[0])
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	resp, err := doPaced(httpClient, req)
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
		if n.Type == html.ElementNode && (n.Data == "img" || n.Data == "source") {
			for _, src := range imageSources(n) {
				checkImage(src, pageURL)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

// imageSources returns every image URL an <img> or <picture> <source> can
// load: src, the lazy-loading data-src, and each candidate in srcset, which
// is where the large versions usually are.
func imageSources(n *html.Node) []string {
	var out []string
	for _, a := range n.Attr {
		switch a.Key {
		case "src", "data-src":
			out = append(out, a.Val)
		case "srcset", "data-srcset":
			for _, cand := range strings.Split(a.Val, ",") {
				if fields := strings.Fields(cand); len(fields) > 0 {
					out = append(out, fields[0])
				}
			}
		}
	}
	kept := out[:0]
	for _, s := range out {
		if s != "" && !strings.HasPrefix(s, "data:") {
			kept = append(kept, s)
		}
	}
	return kept
}

// imageInfo is what one fetch of an image found. It is cached per crawl:
// a site logo appears on every page and only needs measuring once.
type imageInfo struct {
	size        int64
	contentType string
	ok          bool
}

var checkedImages sync.Map // resolved URL -> imageInfo

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

	var info imageInfo
	if cached, ok := checkedImages.Load(resolved); ok {
		info = cached.(imageInfo)
	} else {
		atomic.AddInt64(&stats.ImagesChecked, 1)
		info = measureImage(resolved)
		checkedImages.Store(resolved, info)
	}

	// Report the image on every page that uses it, so the results say
	// where to fix it, even though it was only downloaded once.
	if info.ok && info.size > config.ImageSizeThreshold {
		sizeKB := info.size / 1024
		writeOversizedImage(resolved, pageURL, sizeKB, info.contentType)
		fmt.Printf("\n🖼️  OVERSIZED IMAGE (%dKB): %s\n", sizeKB, resolved)
	}
}

// measureImage returns an image's size, from Content-Length on a HEAD when
// the server sends one, otherwise by counting a streamed GET. The image is
// never held in memory.
func measureImage(link string) imageInfo {
	if info, ok := imageRequest("HEAD", link); ok && info.size > 0 {
		return info
	}
	info, _ := imageRequest("GET", link)
	return info
}

func imageRequest(method, link string) (imageInfo, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return imageInfo{}, false
	}
	BrowserHeaders(req, userAgents[0])
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Sec-Fetch-Dest", "image")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Del("Sec-Fetch-User")
	req.Header.Del("Upgrade-Insecure-Requests")

	resp, err := doPaced(httpClient, req)
	if err != nil {
		return imageInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return imageInfo{}, false
	}

	info := imageInfo{contentType: resp.Header.Get("Content-Type"), ok: true}
	if method == "HEAD" {
		info.size = resp.ContentLength
		return info, true
	}
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return imageInfo{}, false
	}
	atomic.AddInt64(&stats.BytesDownloaded, n)
	info.size = n
	return info, true
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

					// A JavaScript template left in the markup, such as
					// href="'+ item.link +'", is not a link to anything.
					if strings.Contains(a.Val, "'+") || strings.Contains(a.Val, "+'") {
						continue
					}

					nextURL := pageBase.ResolveReference(u)
					// #main and #top point back into the same page. Keeping
					// them made every page with a skip link a second fetch.
					nextURL.Fragment = ""
					nextURL.RawFragment = ""

					if !sameSite(nextURL.Host, siteBase.Load().Host) {
						atomic.AddInt64(&stats.SkippedExternal, 1)
						continue
					}

					if !inPathFilter(nextURL.Path) {
						continue
					}

					crawl(nextURL.String())
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

// detectBotProtection reports whether body is a bot challenge page rather than
// the page asked for. It matches challenge wording, not vendor names: plenty of
// ordinary pages load assets from cdnjs.cloudflare.com or carry a reCAPTCHA
// form, and a bare substring match on those ended crawls at the homepage.
func detectBotProtection(body string) bool {
	bodyLower := strings.ToLower(body)

	// Only trigger on actual challenge page patterns
	challengePatterns := []struct {
		required []string // ALL must be present
	}{
		{[]string{"checking your browser", "please wait"}},
		{[]string{"just a moment", "enable javascript"}},
		{[]string{"ddos protection", "please wait"}},
		{[]string{"attention required", "cloudflare"}},
		{[]string{"sorry, you have been blocked"}},
		{[]string{"access denied", "you don't have permission"}},
		{[]string{"verify you are human", "captcha"}},
		{[]string{"security check", "please complete"}},
	}

	for _, pattern := range challengePatterns {
		allMatch := true
		for _, required := range pattern.required {
			if !strings.Contains(bodyLower, required) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}

	// Check for very short pages that are likely challenge pages
	if len(body) < 2000 {
		if strings.Contains(bodyLower, "checking your browser") ||
			strings.Contains(bodyLower, "please enable javascript and cookies") {
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

// sameSite reports whether two hosts belong to the site being crawled. The
// bare domain and its www. form are one site: most virginia.gov hosts
// redirect one to the other, and links on the page use whichever the CMS
// prefers, so an exact match treated half the site as external.
func sameSite(a, b string) bool {
	norm := func(h string) string {
		return strings.TrimPrefix(strings.ToLower(h), "www.")
	}
	return norm(a) == norm(b)
}

// inPathFilter reports whether a path is inside the section the user asked
// to crawl. The filter always ends in "/", so the section's own index,
// written without the slash, is let through too.
func inPathFilter(p string) bool {
	f := config.PathFilter
	if f == "" {
		return true
	}
	return strings.HasPrefix(p, f) || p == strings.TrimSuffix(f, "/")
}

// getVisitedKey returns the key to use for visited URL tracking. The
// fragment is always dropped, since #section names a place in a page the
// crawler has already fetched. When IgnoreQueryParams is enabled the query is
// dropped too, so page.html?a=1 and page.html?b=2 are the same page.
func getVisitedKey(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}

	u.Fragment = ""
	u.RawFragment = ""
	if config.IgnoreQueryParams {
		u.RawQuery = ""
	}

	return u.String()
}
