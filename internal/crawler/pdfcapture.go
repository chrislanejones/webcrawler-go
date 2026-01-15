package crawler

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
	PagesQueued     int64
	PDFsGenerated   int64
	ScreenshotsGen  int64
	Errors          int64
	SkippedExternal int64
}

var (
	cancelRequested int32 // atomic flag for cancellation
)

var (
	pdfVisited           sync.Map
	pdfWg                sync.WaitGroup
	pdfSema              chan struct{}
	pdfStats             PDFCaptureStats
	pdfStartTime         time.Time
	pdfBaseURL           *url.URL
	pdfOutputDir         string
	pdfConcurrency       int
	pdfCaptureFormat     CaptureFormat
	pdfPathFilter        string // Only crawl URLs matching this path prefix
	pdfIgnoreQueryParams bool   // Treat URLs with different query params as the same page
	pdfCurrentPage       string // Currently processing page (for status display)
	pdfCurrentMu         sync.Mutex
)

// StartPDFCapture begins crawling and capturing PDFs/screenshots
func StartPDFCapture(cfg Config) {
	pdfVisited = sync.Map{}
	pdfStats = PDFCaptureStats{}
	pdfStartTime = time.Now()
	pdfConcurrency = cfg.MaxConcurrency
	pdfCaptureFormat = cfg.CaptureFormat
	pdfPathFilter = cfg.PathFilter
	pdfIgnoreQueryParams = cfg.IgnoreQueryParams
	atomic.StoreInt32(&cancelRequested, 0)

	// Default to both if not set
	if pdfCaptureFormat == 0 {
		pdfCaptureFormat = CaptureBoth
	}

	var err error
	pdfBaseURL, err = url.Parse(cfg.StartURL)
	if err != nil {
		fmt.Printf("❌ Invalid start URL: %v\n", err)
		return
	}

	// Create output directory with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	pdfOutputDir = fmt.Sprintf("page_captures_%s", timestamp)
	os.MkdirAll(pdfOutputDir, 0755)

	pdfSema = make(chan struct{}, cfg.MaxConcurrency)

	// Start live stats
	stopStats := make(chan bool)
	go printPDFLiveStats(stopStats)

	// Start keyboard listener for cancellation
	stopKeyListener := make(chan bool)
	go listenForCancel(stopKeyListener)

	// Determine format label
	formatLabel := pdfCaptureFormat.String()

	fmt.Println("┌─────────────────── PAGE CAPTURE STARTING ──────────────────┐")
	fmt.Printf("│  🎯 Target: %-43s │\n", truncateString(cfg.StartURL, 43))
	if pdfPathFilter != "" {
		fmt.Printf("│  🌲 Path:   %-43s │\n", truncateString(pdfPathFilter, 43))
	}
	fmt.Printf("│  📁 Output: %-43s │\n", pdfOutputDir)
	fmt.Printf("│  📋 Format: %-43s │\n", formatLabel)
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

	atomic.AddInt64(&pdfStats.PagesQueued, 1)

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

		// Capture PDF/screenshot and extract links from the rendered DOM
		links := capturePage(pageURL)

		// Queue discovered links for crawling (only if not cancelled)
		if atomic.LoadInt32(&cancelRequested) == 0 {
			for _, nextLink := range links {
				crawlForPDF(nextLink)
			}
		}
	}(link)
}

func capturePage(pageURL string) []string {
	var extractedLinks []string
	
	// Track current page for status display
	pdfCurrentMu.Lock()
	pdfCurrentPage = pageURL
	pdfCurrentMu.Unlock()
	
	// Create a safe filename from URL
	filename := sanitizeFilename(pageURL)

	pdfPath := filepath.Join(pdfOutputDir, filename+".pdf")
	pngPath := filepath.Join(pdfOutputDir, filename+".png")

	// Check if already captured based on format
	switch pdfCaptureFormat {
	case CapturePDFOnly:
		if _, err := os.Stat(pdfPath); err == nil {
			return nil
		}
	case CaptureImagesOnly:
		if _, err := os.Stat(pngPath); err == nil {
			return nil
		}
	case CaptureBoth:
		if _, err := os.Stat(pdfPath); err == nil {
			return nil
		}
	case CaptureCMYKPDF:
		cmykPdfPath := filepath.Join(pdfOutputDir, filename+"_cmyk.pdf")
		if _, err := os.Stat(cmykPdfPath); err == nil {
			return nil
		}
	case CaptureCMYKTIFF:
		tiffPath := filepath.Join(pdfOutputDir, filename+"_cmyk.tiff")
		if _, err := os.Stat(tiffPath); err == nil {
			return nil
		}
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

	// Set timeout (180s for slow/heavy pages)
	ctx, cancel = context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	var pdfBuf []byte
	var pngBuf []byte
	var linksHTML string

	// Build actions based on capture format
	actions := []chromedp.Action{
		// Navigate to page and wait for network to be mostly idle
		chromedp.Navigate(pageURL),
		// Wait for DOM to be ready
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Initial wait for JS frameworks to initialize
		chromedp.Sleep(2 * time.Second),
		// Scroll to trigger lazy loading
		chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		chromedp.Sleep(1 * time.Second),
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(500 * time.Millisecond),
		// Wait for content to actually load - check body text length
		chromedp.ActionFunc(func(ctx context.Context) error {
			var lastLength int
			stableCount := 0
			
			// Wait until body content stabilizes (stops growing)
			for attempt := 0; attempt < 30; attempt++ {
				var currentLength int
				err := chromedp.Evaluate(`document.body.innerText.length`, &currentLength).Do(ctx)
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				
				// Check if content has stabilized
				if currentLength > 500 && currentLength == lastLength {
					stableCount++
					if stableCount >= 3 {
						// Content stable for 1.5 seconds, good to go
						return nil
					}
				} else {
					stableCount = 0
				}
				
				lastLength = currentLength
				time.Sleep(500 * time.Millisecond)
			}
			
			// Final fallback wait
			time.Sleep(2 * time.Second)
			return nil
		}),
		// Extra wait for any final rendering
		chromedp.Sleep(1 * time.Second),
		// Extract all links from the rendered DOM
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('a[href]'))
				.map(a => a.href)
				.filter(href => href && !href.startsWith('javascript:') && !href.startsWith('mailto:') && !href.startsWith('tel:'))
				.join('\n')
		`, &linksHTML),
	}

	// Add screenshot capture if needed
	needsScreenshot := pdfCaptureFormat == CaptureImagesOnly || 
		pdfCaptureFormat == CaptureBoth || 
		pdfCaptureFormat == CaptureCMYKTIFF
	
	if needsScreenshot {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
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
		}))
	}

	// Add PDF generation if needed
	needsPDF := pdfCaptureFormat == CapturePDFOnly || 
		pdfCaptureFormat == CaptureBoth || 
		pdfCaptureFormat == CaptureCMYKPDF
	
	if needsPDF {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
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
				WithDisplayHeaderFooter(false).
				WithGenerateDocumentOutline(false).
				Do(ctx)
			return err
		}))
	}

	err := chromedp.Run(ctx, actions...)

	if err != nil {
		atomic.AddInt64(&pdfStats.Errors, 1)
		// Clear progress bar line and print error
		fmt.Print("\033[2K\r")
		fmt.Printf("❌ Error: %s - %v\n\n", truncateString(pageURL, 40), err)
		return nil
	}

	// Save PDF if generated
	if pdfCaptureFormat == CapturePDFOnly || pdfCaptureFormat == CaptureBoth {
		if err := os.WriteFile(pdfPath, pdfBuf, 0644); err != nil {
			atomic.AddInt64(&pdfStats.Errors, 1)
			return extractedLinks
		}
		atomic.AddInt64(&pdfStats.PDFsGenerated, 1)
	}

	// Save and convert to CMYK PDF if needed
	if pdfCaptureFormat == CaptureCMYKPDF {
		// First save the RGB PDF temporarily
		tempPdfPath := filepath.Join(pdfOutputDir, filename+"_temp.pdf")
		if err := os.WriteFile(tempPdfPath, pdfBuf, 0644); err != nil {
			atomic.AddInt64(&pdfStats.Errors, 1)
			return extractedLinks
		}
		
		// Convert to CMYK using Ghostscript
		cmykPdfPath := filepath.Join(pdfOutputDir, filename+"_cmyk.pdf")
		if err := convertToCMYKPDF(tempPdfPath, cmykPdfPath); err != nil {
			atomic.AddInt64(&pdfStats.Errors, 1)
			os.Remove(tempPdfPath)
			return extractedLinks
		}
		os.Remove(tempPdfPath) // Clean up temp file
		atomic.AddInt64(&pdfStats.PDFsGenerated, 1)
	}

	// Save screenshot if generated
	if pdfCaptureFormat == CaptureImagesOnly || pdfCaptureFormat == CaptureBoth {
		if err := os.WriteFile(pngPath, pngBuf, 0644); err != nil {
			atomic.AddInt64(&pdfStats.Errors, 1)
			return extractedLinks
		}
		atomic.AddInt64(&pdfStats.ScreenshotsGen, 1)
	}

	// Save and convert to CMYK TIFF if needed
	if pdfCaptureFormat == CaptureCMYKTIFF {
		// First save the PNG temporarily
		tempPngPath := filepath.Join(pdfOutputDir, filename+"_temp.png")
		if err := os.WriteFile(tempPngPath, pngBuf, 0644); err != nil {
			atomic.AddInt64(&pdfStats.Errors, 1)
			return extractedLinks
		}
		
		// Convert to CMYK TIFF using ImageMagick
		tiffPath := filepath.Join(pdfOutputDir, filename+"_cmyk.tiff")
		if err := convertToCMYKTIFF(tempPngPath, tiffPath); err != nil {
			atomic.AddInt64(&pdfStats.Errors, 1)
			os.Remove(tempPngPath)
			return extractedLinks
		}
		os.Remove(tempPngPath) // Clean up temp file
		atomic.AddInt64(&pdfStats.ScreenshotsGen, 1)
	}

	// Progress bar shows current status, so no need for individual messages

	// Process extracted links from the rendered DOM
	if linksHTML != "" {
		for _, href := range strings.Split(linksHTML, "\n") {
			href = strings.TrimSpace(href)
			if href == "" {
				continue
			}
			
			// Parse and validate the link
			u, err := url.Parse(href)
			if err != nil {
				continue
			}
			
			// Only follow same-domain links
			if u.Host != pdfBaseURL.Host {
				atomic.AddInt64(&pdfStats.SkippedExternal, 1)
				continue
			}
			
			// Apply path filter if set (only crawl URLs within the specified path)
			if pdfPathFilter != "" && !strings.HasPrefix(u.Path, pdfPathFilter) {
				continue
			}
			
			extractedLinks = append(extractedLinks, href)
			
			// Detect pagination pattern and generate additional page URLs
			// Check for ?page=N pattern
			if q := u.Query(); q.Get("page") != "" {
				if pageNum, err := strconv.Atoi(q.Get("page")); err == nil {
					// Generate next few page URLs
					for i := 1; i <= 5; i++ {
						newQ := u.Query()
						newQ.Set("page", strconv.Itoa(pageNum+i))
						newURL := *u
						newURL.RawQuery = newQ.Encode()
						extractedLinks = append(extractedLinks, newURL.String())
					}
				}
			}
		}
	}
	
	// If this is a listing page (index or no specific article), try common pagination patterns
	parsedPage, _ := url.Parse(pageURL)
	if parsedPage != nil {
		// Normalize paths for comparison (remove trailing slash)
		normalizedPath := strings.TrimSuffix(parsedPage.Path, "/")
		normalizedFilter := strings.TrimSuffix(pdfPathFilter, "/")
		
		// Check if this is a listing/index page (ends with / or matches filter path exactly)
		isListingPage := strings.HasSuffix(parsedPage.Path, "/") || 
			normalizedPath == normalizedFilter ||
			parsedPage.Path == pdfPathFilter
		
		if isListingPage {
			// Try adding ?page=N if not already present
			if parsedPage.Query().Get("page") == "" {
				for i := 2; i <= 10; i++ {
					newQ := parsedPage.Query()
					newQ.Set("page", strconv.Itoa(i))
					newURL := *parsedPage
					newURL.RawQuery = newQ.Encode()
					extractedLinks = append(extractedLinks, newURL.String())
				}
			}
		}
	}
	
	return extractedLinks
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

	// Add query string hash if present (unless ignoring query params)
	if u.RawQuery != "" && !pdfIgnoreQueryParams {
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

	// Strip query parameters when IgnoreQueryParams is enabled
	if pdfIgnoreQueryParams {
		u.RawQuery = ""
	}

	// Normalize path
	if u.Path == "" {
		u.Path = "/"
	}

	return u.String()
}

func printPDFLiveStats(stop chan bool) {
	ticker := time.NewTicker(1 * time.Second) // Update more frequently
	defer ticker.Stop()
	
	spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinIdx := 0

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			elapsed := time.Since(pdfStartTime)
			queued := atomic.LoadInt64(&pdfStats.PagesQueued)
			visited := atomic.LoadInt64(&pdfStats.PagesVisited)
			pdfs := atomic.LoadInt64(&pdfStats.PDFsGenerated)
			screenshots := atomic.LoadInt64(&pdfStats.ScreenshotsGen)
			errors := atomic.LoadInt64(&pdfStats.Errors)
			
			// Get current page
			pdfCurrentMu.Lock()
			currentPage := pdfCurrentPage
			pdfCurrentMu.Unlock()

			pagesPerSec := float64(visited) / elapsed.Seconds()
			if elapsed.Seconds() < 1 {
				pagesPerSec = 0
			}
			
			// Build progress bar based on visited vs queued
			barWidth := 20
			var pct int64 = 0
			if queued > 0 {
				pct = visited * 100 / queued
				if pct > 100 {
					pct = 100
				}
			}
			filled := int(pct) * barWidth / 100
			if filled > barWidth {
				filled = barWidth
			}
			
			bar := ""
			for i := 0; i < filled; i++ {
				bar += "█"
			}
			for i := filled; i < barWidth; i++ {
				bar += "░"
			}
			
			// Spinner for activity
			spinner := spinChars[spinIdx%len(spinChars)]
			spinIdx++
			
			// Truncate current page URL for display
			displayURL := currentPage
			if len(displayURL) > 60 {
				displayURL = "..." + displayURL[len(displayURL)-57:]
			}
			
			// Clear lines and print status
			fmt.Print("\033[2K\r") // Clear line 1
			
			// Format stats based on capture mode - show queue progress
			pending := queued - visited
			if pending < 0 {
				pending = 0
			}
			
			switch pdfCaptureFormat {
			case CapturePDFOnly:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 📑 %d captured │ ⏳ %d pending │ ❌ %d │ %.1f/s\n",
					spinner, bar, pct, formatDuration(elapsed), pdfs, pending, errors, pagesPerSec)
			case CaptureImagesOnly:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 🖼️ %d captured │ ⏳ %d pending │ ❌ %d │ %.1f/s\n",
					spinner, bar, pct, formatDuration(elapsed), screenshots, pending, errors, pagesPerSec)
			case CaptureBoth:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 📑 %d │ 🖼️ %d │ ⏳ %d pending │ ❌ %d │ %.1f/s\n",
					spinner, bar, pct, formatDuration(elapsed), pdfs, screenshots, pending, errors, pagesPerSec)
			case CaptureCMYKPDF:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 🎨 %d CMYK │ ⏳ %d pending │ ❌ %d │ %.1f/s\n",
					spinner, bar, pct, formatDuration(elapsed), pdfs, pending, errors, pagesPerSec)
			case CaptureCMYKTIFF:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 🎨 %d TIFF │ ⏳ %d pending │ ❌ %d │ %.1f/s\n",
					spinner, bar, pct, formatDuration(elapsed), screenshots, pending, errors, pagesPerSec)
			}
			
			// Show current page on second line
			fmt.Print("\033[2K") // Clear line 2
			if displayURL != "" {
				fmt.Printf("   \033[2m→ %s\033[0m", displayURL)
			}
			
			// Move cursor up for next update
			fmt.Print("\033[1A\r")
		}
	}
}

func printPDFFinalStats() {
	elapsed := time.Since(pdfStartTime)
	wasCancelled := atomic.LoadInt32(&cancelRequested) == 1

	// Clear any remaining progress bar output
	fmt.Print("\033[2K\r") // Clear current line
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	if wasCancelled {
		fmt.Println("║                 📊 PAGE CAPTURE CANCELLED 📊                      ║")
	} else {
		fmt.Println("║                  📊 PAGE CAPTURE COMPLETE 📊                      ║")
	}
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                   ║")
	fmt.Printf("║  ⏱️  Total Time:           %-40s ║\n", formatDuration(elapsed))
	fmt.Printf("║  📄 Pages Visited:         %-40d ║\n", pdfStats.PagesVisited)

	// Show stats based on capture format
	switch pdfCaptureFormat {
	case CapturePDFOnly:
		fmt.Printf("║  📑 PDFs Generated:        %-40d ║\n", pdfStats.PDFsGenerated)
	case CaptureImagesOnly:
		fmt.Printf("║  🖼️  Images Generated:      %-40d ║\n", pdfStats.ScreenshotsGen)
	case CaptureBoth:
		fmt.Printf("║  📑 PDFs Generated:        %-40d ║\n", pdfStats.PDFsGenerated)
		fmt.Printf("║  🖼️  Images Generated:      %-40d ║\n", pdfStats.ScreenshotsGen)
	case CaptureCMYKPDF:
		fmt.Printf("║  🎨 CMYK PDFs Generated:   %-40d ║\n", pdfStats.PDFsGenerated)
	case CaptureCMYKTIFF:
		fmt.Printf("║  🎨 CMYK TIFFs Generated:  %-40d ║\n", pdfStats.ScreenshotsGen)
	}

	fmt.Printf("║  ❌ Errors:                %-40d ║\n", pdfStats.Errors)
	fmt.Printf("║  📁 Output Directory:      %-40s ║\n", pdfOutputDir)
	fmt.Println("║                                                                   ║")
	if wasCancelled {
		fmt.Println("║  ℹ️  Crawl was cancelled early - partial results saved            ║")
		fmt.Println("║                                                                   ║")
	}
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
}

// convertToCMYKPDF converts an RGB PDF to CMYK using Ghostscript
func convertToCMYKPDF(inputPath, outputPath string) error {
	// Check if Ghostscript is available
	if _, err := exec.LookPath("gs"); err != nil {
		return fmt.Errorf("ghostscript (gs) not found in PATH - install with: sudo apt install ghostscript")
	}

	cmd := exec.Command("gs",
		"-dSAFER",
		"-dBATCH",
		"-dNOPAUSE",
		"-dNOCACHE",
		"-sDEVICE=pdfwrite",
		"-sColorConversionStrategy=CMYK",
		"-dProcessColorModel=/DeviceCMYK",
		"-dAutoRotatePages=/None",
		"-sOutputFile="+outputPath,
		inputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ghostscript error: %v - %s", err, string(output))
	}

	return nil
}

// convertToCMYKTIFF converts a PNG to CMYK TIFF using ImageMagick
func convertToCMYKTIFF(inputPath, outputPath string) error {
	// Check if ImageMagick is available (try both 'convert' and 'magick')
	var cmdName string
	if _, err := exec.LookPath("magick"); err == nil {
		cmdName = "magick"
	} else if _, err := exec.LookPath("convert"); err == nil {
		cmdName = "convert"
	} else {
		return fmt.Errorf("imagemagick not found in PATH - install with: sudo apt install imagemagick")
	}

	args := []string{
		inputPath,
		"-colorspace", "CMYK",
		"-compress", "LZW",
		outputPath,
	}

	// ImageMagick 7 uses 'magick convert', older versions just 'convert'
	if cmdName == "magick" {
		args = append([]string{"convert"}, args...)
	}

	cmd := exec.Command(cmdName, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick error: %v - %s", err, string(output))
	}

	return nil
}