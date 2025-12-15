package crawler

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type PDFCaptureStats struct {
	PagesVisited    int64
	PDFsGenerated   int64
	ScreenshotsGen  int64
	Errors          int64
	SkippedExternal int64
}

var (
	cancelRequested int32 // atomic flag for cancellation
)

var (
	pdfVisited     sync.Map
	pdfWg          sync.WaitGroup
	pdfSema        chan struct{}
	pdfStats       PDFCaptureStats
	pdfStartTime   time.Time
	pdfBaseURL     *url.URL
	pdfOutputDir   string
	pdfConcurrency int
)

// StartPDFCapture begins crawling and capturing PDFs/screenshots
func StartPDFCapture(cfg Config) {
	pdfVisited = sync.Map{}
	pdfStats = PDFCaptureStats{}
	pdfStartTime = time.Now()
	pdfConcurrency = cfg.MaxConcurrency
	atomic.StoreInt32(&cancelRequested, 0)

	var err error
	pdfBaseURL, err = url.Parse(cfg.StartURL)
	if err != nil {
		fmt.Printf("❌ Invalid start URL: %v\n", err)
		return
	}

	// Create output directory with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	pdfOutputDir = fmt.Sprintf("pdf_captures_%s", timestamp)
	os.MkdirAll(pdfOutputDir, 0755)

	pdfSema = make(chan struct{}, cfg.MaxConcurrency)

	// Start live stats
	stopStats := make(chan bool)
	go printPDFLiveStats(stopStats)

	// Start keyboard listener for cancellation
	stopKeyListener := make(chan bool)
	go listenForCancel(stopKeyListener)

	fmt.Println("┌─────────────────── PDF CAPTURE STARTING ───────────────────┐")
	fmt.Printf("│  🎯 Target: %-43s │\n", truncateString(cfg.StartURL, 43))
	fmt.Printf("│  📁 Output: %-43s │\n", pdfOutputDir)
	fmt.Println("├────────────────────────────────────────────────────────────┤")
	fmt.Println("│  💡 Press 'c' + Enter to cancel and save current progress  │")
	fmt.Println("└────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Start crawling
	crawlForPDF(cfg.StartURL)
	pdfWg.Wait()

	stopStats <- true
	stopKeyListener <- true
	printPDFFinalStats()
}

// listenForCancel listens for 'c' key press to cancel crawling
func listenForCancel(stop chan bool) {
	reader := bufio.NewReader(os.Stdin)
	inputChan := make(chan string)

	go func() {
		for {
			input, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			inputChan <- strings.TrimSpace(strings.ToLower(input))
		}
	}()

	for {
		select {
		case <-stop:
			return
		case input := <-inputChan:
			if input == "c" {
				atomic.StoreInt32(&cancelRequested, 1)
				fmt.Println("\n\n⏹️  CANCEL REQUESTED - Finishing current captures...")
				fmt.Println("   (Waiting for in-progress pages to complete)")
				return
			}
		}
	}
}

func crawlForPDF(link string) {
	// Check if cancel requested
	if atomic.LoadInt32(&cancelRequested) == 1 {
		return
	}

	// Normalize URL
	link = normalizeURL(link)

	// Check if already visited
	if _, exists := pdfVisited.LoadOrStore(link, true); exists {
		return
	}

	pdfWg.Add(1)
	go func(pageURL string) {
		defer pdfWg.Done()
		pdfSema <- struct{}{}
		defer func() { <-pdfSema }()

		// Check again in case cancel happened while waiting
		if atomic.LoadInt32(&cancelRequested) == 1 {
			return
		}

		atomic.AddInt64(&pdfStats.PagesVisited, 1)

		// First, fetch the page to extract links (lightweight)
		links := fetchAndExtractLinks(pageURL)

		// Then capture PDF and screenshot using Chrome
		capturePage(pageURL)

		// Queue discovered links for crawling (only if not cancelled)
		if atomic.LoadInt32(&cancelRequested) == 0 {
			for _, nextLink := range links {
				crawlForPDF(nextLink)
			}
		}
	}(link)
}

func fetchAndExtractLinks(pageURL string) []string {
	var links []string

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return links
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return links
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return links
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return links
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return links
	}

	// Extract href links using regex (faster than parsing)
	hrefRe := regexp.MustCompile(`href=["']([^"'#]+)["']`)
	matches := hrefRe.FindAllStringSubmatch(string(body), -1)

	for _, match := range matches {
		if len(match) > 1 {
			href := match[1]

			// Skip non-http links
			if strings.HasPrefix(href, "mailto:") ||
				strings.HasPrefix(href, "tel:") ||
				strings.HasPrefix(href, "javascript:") {
				continue
			}

			// Resolve relative URLs
			u, err := url.Parse(href)
			if err != nil {
				continue
			}

			resolved := pdfBaseURL.ResolveReference(u)

			// Only follow same-domain links
			if resolved.Host != pdfBaseURL.Host {
				atomic.AddInt64(&pdfStats.SkippedExternal, 1)
				continue
			}

			links = append(links, resolved.String())
		}
	}

	return links
}

func capturePage(pageURL string) {
	// Create a safe filename from URL
	filename := sanitizeFilename(pageURL)

	pdfPath := filepath.Join(pdfOutputDir, filename+".pdf")
	pngPath := filepath.Join(pdfOutputDir, filename+".png")

	// Check if already captured
	if _, err := os.Stat(pdfPath); err == nil {
		return
	}

	// Create Chrome context with options for better rendering
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.WindowSize(1920, 1080),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Set timeout
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var pdfBuf []byte
	var pngBuf []byte

	err := chromedp.Run(ctx,
		// Navigate to page
		chromedp.Navigate(pageURL),

		// Wait for page to load
		chromedp.WaitReady("body", chromedp.ByQuery),

		// Wait a bit for any JS to finish
		chromedp.Sleep(2*time.Second),

		// Take full-page screenshot at high quality
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Get page dimensions
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}

			width, height := int64(contentSize.Width), int64(contentSize.Height)

			// Cap height to avoid memory issues
			if height > 16384 {
				height = 16384
			}

			// Set viewport to full page size
			err = emulation.SetDeviceMetricsOverride(width, height, 1, false).
				WithScreenOrientation(&emulation.ScreenOrientation{
					Type:  emulation.OrientationTypePortraitPrimary,
					Angle: 0,
				}).Do(ctx)
			if err != nil {
				return err
			}

			// Capture screenshot
			pngBuf, err = page.CaptureScreenshot().
				WithQuality(100).
				WithFromSurface(true).
				Do(ctx)
			return err
		}),

		// Generate PDF
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfBuf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithScale(1.0).
				WithPaperWidth(8.5).
				WithPaperHeight(11).
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				Do(ctx)
			return err
		}),
	)

	if err != nil {
		atomic.AddInt64(&pdfStats.Errors, 1)
		fmt.Printf("\n❌ Error capturing %s: %v\n", truncateString(pageURL, 50), err)
		return
	}

	// Save PDF
	if err := os.WriteFile(pdfPath, pdfBuf, 0644); err != nil {
		atomic.AddInt64(&pdfStats.Errors, 1)
		return
	}
	atomic.AddInt64(&pdfStats.PDFsGenerated, 1)

	// Save screenshot
	if err := os.WriteFile(pngPath, pngBuf, 0644); err != nil {
		atomic.AddInt64(&pdfStats.Errors, 1)
		return
	}
	atomic.AddInt64(&pdfStats.ScreenshotsGen, 1)

	fmt.Printf("\n📄 Captured: %s\n", truncateString(pageURL, 60))
}

func sanitizeFilename(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "page"
	}

	// Use path as filename base
	name := u.Path
	if name == "" || name == "/" {
		name = "index"
	}

	// Remove leading slash
	name = strings.TrimPrefix(name, "/")

	// Replace path separators with underscores
	name = strings.ReplaceAll(name, "/", "_")

	// Remove or replace invalid filename characters
	invalidChars := regexp.MustCompile(`[<>:"/\\|?*]`)
	name = invalidChars.ReplaceAllString(name, "_")

	// Add query string hash if present
	if u.RawQuery != "" {
		name += "_q" + hashString(u.RawQuery)[:8]
	}

	// Limit length
	if len(name) > 200 {
		name = name[:200]
	}

	// Remove trailing dots/spaces
	name = strings.TrimRight(name, ". ")

	if name == "" {
		name = "page"
	}

	return name
}

func hashString(s string) string {
	h := uint32(0)
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", h)
}

func normalizeURL(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}

	// Remove fragment
	u.Fragment = ""

	// Normalize path
	if u.Path == "" {
		u.Path = "/"
	}

	return u.String()
}

func printPDFLiveStats(stop chan bool) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			elapsed := time.Since(pdfStartTime)
			visited := atomic.LoadInt64(&pdfStats.PagesVisited)
			pdfs := atomic.LoadInt64(&pdfStats.PDFsGenerated)
			screenshots := atomic.LoadInt64(&pdfStats.ScreenshotsGen)
			errors := atomic.LoadInt64(&pdfStats.Errors)

			pagesPerSec := float64(visited) / elapsed.Seconds()

			fmt.Printf("\r📊 [%s] Pages: %d | PDFs: %d | Screenshots: %d | Errors: %d | %.1f p/s     ",
				formatDuration(elapsed),
				visited,
				pdfs,
				screenshots,
				errors,
				pagesPerSec,
			)
		}
	}
}

func printPDFFinalStats() {
	elapsed := time.Since(pdfStartTime)
	wasCancelled := atomic.LoadInt32(&cancelRequested) == 1

	fmt.Println()
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	if wasCancelled {
		fmt.Println("║                 📊 PDF CAPTURE CANCELLED 📊                       ║")
	} else {
		fmt.Println("║                   📊 PDF CAPTURE COMPLETE 📊                      ║")
	}
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                   ║")
	fmt.Printf("║  ⏱️  Total Time:           %-40s ║\n", formatDuration(elapsed))
	fmt.Printf("║  📄 Pages Visited:         %-40d ║\n", pdfStats.PagesVisited)
	fmt.Printf("║  📑 PDFs Generated:        %-40d ║\n", pdfStats.PDFsGenerated)
	fmt.Printf("║  🖼️  Screenshots Generated: %-40d ║\n", pdfStats.ScreenshotsGen)
	fmt.Printf("║  ❌ Errors:                %-40d ║\n", pdfStats.Errors)
	fmt.Printf("║  📁 Output Directory:      %-40s ║\n", pdfOutputDir)
	fmt.Println("║                                                                   ║")
	if wasCancelled {
		fmt.Println("║  ℹ️  Crawl was cancelled early - partial results saved            ║")
		fmt.Println("║                                                                   ║")
	}
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
}
