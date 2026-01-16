package crawler

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// FeedItem represents a single item from a JSON feed
type FeedItem struct {
	Headline string `json:"headline"`
	Link     string `json:"link"`
	Date     string `json:"date"`
	DateCode string `json:"datecode"`
	Brief    string `json:"brief"`
	Tags     string `json:"tags"`
}

type JSONFeedStats struct {
	ItemsFetched   int64
	ItemsFiltered  int64
	PagesCapture   int64
	PDFsGenerated  int64
	ScreenshotsGen int64
	Errors         int64
}

var (
	jsonFeedStats      JSONFeedStats
	jsonFeedStartTime  time.Time
	jsonFeedOutputDir  string
	jsonFeedFormat     CaptureFormat
	jsonFeedBaseURL    *url.URL
	jsonFeedWg         sync.WaitGroup
	jsonFeedSema       chan struct{}
	jsonFeedCSVFile    string
	jsonFeedCSVMu      sync.Mutex
	jsonCancelRequested int32
)

// StartJSONFeedCapture fetches a JSON feed and captures all article pages
func StartJSONFeedCapture(cfg Config) {
	jsonFeedStats = JSONFeedStats{}
	jsonFeedStartTime = time.Now()
	jsonFeedFormat = cfg.CaptureFormat
	atomic.StoreInt32(&jsonCancelRequested, 0)

	if jsonFeedFormat == 0 {
		jsonFeedFormat = CaptureBoth
	}

	var err error
	jsonFeedBaseURL, err = url.Parse(cfg.StartURL)
	if err != nil {
		fmt.Printf("❌ Invalid base URL: %v\n", err)
		return
	}

	// Create output directory with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	jsonFeedOutputDir = fmt.Sprintf("json_feed_captures_%s", timestamp)
	os.MkdirAll(jsonFeedOutputDir, 0755)

	// Create CSV file for feed data
	jsonFeedCSVFile = filepath.Join(jsonFeedOutputDir, "feed_items.csv")
	createJSONFeedCSV()

	jsonFeedSema = make(chan struct{}, cfg.MaxConcurrency)

	// Start live stats
	stopStats := make(chan bool)
	go printJSONFeedLiveStats(stopStats)

	// Start keyboard listener for cancellation
	stopKeyListener := make(chan bool)
	go listenForJSONFeedCancel(stopKeyListener)

	fmt.Println("┌─────────────────── JSON FEED CAPTURE STARTING ──────────────────┐")
	fmt.Printf("│  🌐 Base URL:  %-45s │\n", truncateString(cfg.StartURL, 45))
	fmt.Printf("│  📡 Feed URL:  %-45s │\n", truncateString(cfg.JSONFeedOpts.FeedURL, 45))
	if cfg.JSONFeedOpts.TagFilter != "" {
		fmt.Printf("│  🏷️  Tag Filter: %-43s │\n", cfg.JSONFeedOpts.TagFilter)
	}
	fmt.Printf("│  📁 Output:    %-45s │\n", jsonFeedOutputDir)
	fmt.Printf("│  📋 Format:    %-45s │\n", jsonFeedFormat.String())
	fmt.Println("├──────────────────────────────────────────────────────────────────┤")
	fmt.Println("│  💡 Press 'c' + Enter to cancel and save current progress       │")
	fmt.Println("└──────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Fetch and parse the JSON feed
	items, err := fetchJSONFeed(cfg.JSONFeedOpts.FeedURL, cfg.JSONFeedOpts)
	if err != nil {
		fmt.Printf("❌ Error fetching JSON feed: %v\n", err)
		stopStats <- true
		stopKeyListener <- true
		return
	}

	atomic.StoreInt64(&jsonFeedStats.ItemsFetched, int64(len(items)))
	fmt.Printf("📊 Fetched %d items from feed\n\n", len(items))

	// Filter items by tag if specified
	if cfg.JSONFeedOpts.TagFilter != "" {
		filtered := make([]FeedItem, 0)
		for _, item := range items {
			if strings.Contains(item.Tags, cfg.JSONFeedOpts.TagFilter) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
		atomic.StoreInt64(&jsonFeedStats.ItemsFiltered, int64(len(items)))
		fmt.Printf("🏷️  Filtered to %d items with tag '%s'\n\n", len(items), cfg.JSONFeedOpts.TagFilter)
	} else {
		atomic.StoreInt64(&jsonFeedStats.ItemsFiltered, int64(len(items)))
	}

	// Process each item
	for _, item := range items {
		if atomic.LoadInt32(&jsonCancelRequested) == 1 {
			break
		}

		// Resolve relative URLs
		itemURL := resolveURL(cfg.StartURL, item.Link)

		// Write to CSV
		writeJSONFeedCSV(item, itemURL)

		// Capture the page
		jsonFeedWg.Add(1)
		go func(feedItem FeedItem, pageURL string) {
			defer jsonFeedWg.Done()
			jsonFeedSema <- struct{}{}
			defer func() { <-jsonFeedSema }()

			if atomic.LoadInt32(&jsonCancelRequested) == 1 {
				return
			}

			captureJSONFeedPage(pageURL, feedItem)
		}(item, itemURL)
	}

	jsonFeedWg.Wait()
	stopStats <- true
	stopKeyListener <- true
	printJSONFeedFinalStats()
}

func createJSONFeedCSV() {
	f, _ := os.Create(jsonFeedCSVFile)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"Headline", "Link", "Date", "Brief", "Tags", "CapturedFile"})
}

func writeJSONFeedCSV(item FeedItem, fullURL string) {
	jsonFeedCSVMu.Lock()
	defer jsonFeedCSVMu.Unlock()

	f, _ := os.OpenFile(jsonFeedCSVFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	filename := sanitizeFilename(fullURL)
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{item.Headline, fullURL, item.Date, item.Brief, item.Tags, filename})
}

func fetchJSONFeed(feedURL string, opts JSONFeedOptions) ([]FeedItem, error) {
	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgents[0])
	req.Header.Set("Accept", "application/json, */*")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// Try to parse as array of objects with flexible field names
	var rawItems []map[string]any
	if err := json.Unmarshal(body, &rawItems); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}

	// Map raw items to FeedItem using configured or default field names
	items := make([]FeedItem, 0, len(rawItems))
	for _, raw := range rawItems {
		item := FeedItem{
			Headline: getStringField(raw, opts.HeadlineField, "headline", "title", "name"),
			Link:     getStringField(raw, opts.LinkField, "link", "url", "href", "permalink"),
			Date:     getStringField(raw, opts.DateField, "date", "published", "pubDate", "created"),
			DateCode: getStringField(raw, "", "datecode", "timestamp"),
			Brief:    getStringField(raw, opts.BriefField, "brief", "summary", "description", "excerpt"),
			Tags:     getStringField(raw, opts.TagsField, "tags", "categories", "keywords"),
		}

		// Skip items without a link
		if item.Link == "" {
			continue
		}

		items = append(items, item)
	}

	return items, nil
}

// getStringField extracts a string value from a map, trying multiple field names
func getStringField(m map[string]any, preferred string, fallbacks ...string) string {
	// Try preferred field first if specified
	if preferred != "" {
		if v, ok := m[preferred]; ok {
			return toString(v)
		}
	}

	// Try fallback fields
	for _, field := range fallbacks {
		if v, ok := m[field]; ok {
			return toString(v)
		}
	}

	return ""
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%.0f", val)
	case int:
		return fmt.Sprintf("%d", val)
	case bool:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func resolveURL(baseURL, link string) string {
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return link
	}

	ref, err := url.Parse(link)
	if err != nil {
		return link
	}

	return base.ResolveReference(ref).String()
}

// sanitizeHeadlineFilename creates a filename from a headline and optional date
func sanitizeHeadlineFilename(headline, dateCode string) string {
	// Start with date prefix if available
	name := ""
	if dateCode != "" && len(dateCode) >= 8 {
		// Extract YYYY-MM-DD from datecode (format: YYYYMMDDHHMM)
		name = dateCode[:4] + "-" + dateCode[4:6] + "-" + dateCode[6:8] + "_"
	}

	// Clean headline
	headline = strings.ToLower(headline)
	headline = strings.ReplaceAll(headline, " ", "-")

	// Remove invalid filename characters
	invalidChars := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*", "'", ",", ".", ";", "!", "(", ")", "[", "]", "{", "}"}
	for _, char := range invalidChars {
		headline = strings.ReplaceAll(headline, char, "")
	}

	// Replace multiple dashes with single dash
	for strings.Contains(headline, "--") {
		headline = strings.ReplaceAll(headline, "--", "-")
	}

	// Trim dashes from ends
	headline = strings.Trim(headline, "-")

	name += headline

	// Limit length
	if len(name) > 200 {
		name = name[:200]
	}

	if name == "" {
		name = "article"
	}

	return name
}

func captureJSONFeedPage(pageURL string, item FeedItem) {
	atomic.AddInt64(&jsonFeedStats.PagesCapture, 1)

	// Use headline for filename if available, otherwise use URL
	var filename string
	if item.Headline != "" {
		filename = sanitizeHeadlineFilename(item.Headline, item.DateCode)
	} else {
		filename = sanitizeFilename(pageURL)
	}
	pdfPath := filepath.Join(jsonFeedOutputDir, filename+".pdf")
	pngPath := filepath.Join(jsonFeedOutputDir, filename+".png")

	// Check if already captured
	switch jsonFeedFormat {
	case CapturePDFOnly, CaptureCMYKPDF:
		if _, err := os.Stat(pdfPath); err == nil {
			return
		}
	case CaptureImagesOnly, CaptureCMYKTIFF:
		if _, err := os.Stat(pngPath); err == nil {
			return
		}
	case CaptureBoth:
		if _, err := os.Stat(pdfPath); err == nil {
			return
		}
	}

	// Create Chrome context
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

	ctx, cancel = context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	var pdfBuf []byte
	var pngBuf []byte

	actions := []chromedp.Action{
		chromedp.Navigate(pageURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(2 * time.Second),
		chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		chromedp.Sleep(1 * time.Second),
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(500 * time.Millisecond),
		// Wait for content to stabilize
		chromedp.ActionFunc(func(ctx context.Context) error {
			var lastLength int
			stableCount := 0
			for attempt := 0; attempt < 30; attempt++ {
				var currentLength int
				err := chromedp.Evaluate(`document.body.innerText.length`, &currentLength).Do(ctx)
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				if currentLength > 500 && currentLength == lastLength {
					stableCount++
					if stableCount >= 3 {
						return nil
					}
				} else {
					stableCount = 0
				}
				lastLength = currentLength
				time.Sleep(500 * time.Millisecond)
			}
			time.Sleep(2 * time.Second)
			return nil
		}),
		chromedp.Sleep(1 * time.Second),
	}

	// Add screenshot capture if needed
	needsScreenshot := jsonFeedFormat == CaptureImagesOnly ||
		jsonFeedFormat == CaptureBoth ||
		jsonFeedFormat == CaptureCMYKTIFF

	if needsScreenshot {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}

			width, height := int64(contentSize.Width), int64(contentSize.Height)
			if height > 16384 {
				height = 16384
			}

			err = emulation.SetDeviceMetricsOverride(width, height, 1, false).
				WithScreenOrientation(&emulation.ScreenOrientation{
					Type:  emulation.OrientationTypePortraitPrimary,
					Angle: 0,
				}).Do(ctx)
			if err != nil {
				return err
			}

			pngBuf, err = page.CaptureScreenshot().
				WithQuality(100).
				WithFromSurface(true).
				Do(ctx)
			return err
		}))
	}

	// Add PDF generation if needed
	needsPDF := jsonFeedFormat == CapturePDFOnly ||
		jsonFeedFormat == CaptureBoth ||
		jsonFeedFormat == CaptureCMYKPDF

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
				Do(ctx)
			return err
		}))
	}

	err := chromedp.Run(ctx, actions...)
	if err != nil {
		atomic.AddInt64(&jsonFeedStats.Errors, 1)
		fmt.Print("\033[2K\r")
		fmt.Printf("❌ Error: %s - %v\n\n", truncateString(pageURL, 40), err)
		return
	}

	// Save files
	if jsonFeedFormat == CapturePDFOnly || jsonFeedFormat == CaptureBoth {
		if err := os.WriteFile(pdfPath, pdfBuf, 0644); err != nil {
			atomic.AddInt64(&jsonFeedStats.Errors, 1)
			return
		}
		atomic.AddInt64(&jsonFeedStats.PDFsGenerated, 1)
	}

	if jsonFeedFormat == CaptureCMYKPDF {
		tempPdfPath := filepath.Join(jsonFeedOutputDir, filename+"_temp.pdf")
		if err := os.WriteFile(tempPdfPath, pdfBuf, 0644); err != nil {
			atomic.AddInt64(&jsonFeedStats.Errors, 1)
			return
		}
		cmykPdfPath := filepath.Join(jsonFeedOutputDir, filename+"_cmyk.pdf")
		if err := convertToCMYKPDF(tempPdfPath, cmykPdfPath); err != nil {
			atomic.AddInt64(&jsonFeedStats.Errors, 1)
			os.Remove(tempPdfPath)
			return
		}
		os.Remove(tempPdfPath)
		atomic.AddInt64(&jsonFeedStats.PDFsGenerated, 1)
	}

	if jsonFeedFormat == CaptureImagesOnly || jsonFeedFormat == CaptureBoth {
		if err := os.WriteFile(pngPath, pngBuf, 0644); err != nil {
			atomic.AddInt64(&jsonFeedStats.Errors, 1)
			return
		}
		atomic.AddInt64(&jsonFeedStats.ScreenshotsGen, 1)
	}

	if jsonFeedFormat == CaptureCMYKTIFF {
		tempPngPath := filepath.Join(jsonFeedOutputDir, filename+"_temp.png")
		if err := os.WriteFile(tempPngPath, pngBuf, 0644); err != nil {
			atomic.AddInt64(&jsonFeedStats.Errors, 1)
			return
		}
		tiffPath := filepath.Join(jsonFeedOutputDir, filename+"_cmyk.tiff")
		if err := convertToCMYKTIFF(tempPngPath, tiffPath); err != nil {
			atomic.AddInt64(&jsonFeedStats.Errors, 1)
			os.Remove(tempPngPath)
			return
		}
		os.Remove(tempPngPath)
		atomic.AddInt64(&jsonFeedStats.ScreenshotsGen, 1)
	}
}

func listenForJSONFeedCancel(stop chan bool) {
	// Reuse the same cancel listener pattern
	for {
		select {
		case <-stop:
			return
		default:
			// Non-blocking check - actual input handled by main cancel listener
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func printJSONFeedLiveStats(stop chan bool) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinIdx := 0

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			elapsed := time.Since(jsonFeedStartTime)
			total := atomic.LoadInt64(&jsonFeedStats.ItemsFiltered)
			captured := atomic.LoadInt64(&jsonFeedStats.PagesCapture)
			pdfs := atomic.LoadInt64(&jsonFeedStats.PDFsGenerated)
			screenshots := atomic.LoadInt64(&jsonFeedStats.ScreenshotsGen)
			errors := atomic.LoadInt64(&jsonFeedStats.Errors)

			pagesPerSec := float64(captured) / elapsed.Seconds()
			if elapsed.Seconds() < 1 {
				pagesPerSec = 0
			}

			// Progress bar
			barWidth := 20
			var pct int64 = 0
			if total > 0 {
				pct = captured * 100 / total
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

			spinner := spinChars[spinIdx%len(spinChars)]
			spinIdx++

			pending := total - captured
			if pending < 0 {
				pending = 0
			}

			fmt.Print("\033[2K\r")
			switch jsonFeedFormat {
			case CapturePDFOnly:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 📑 %d captured │ ⏳ %d pending │ ❌ %d │ %.1f/s",
					spinner, bar, pct, formatDuration(elapsed), pdfs, pending, errors, pagesPerSec)
			case CaptureImagesOnly:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 🖼️ %d captured │ ⏳ %d pending │ ❌ %d │ %.1f/s",
					spinner, bar, pct, formatDuration(elapsed), screenshots, pending, errors, pagesPerSec)
			case CaptureBoth:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 📑 %d │ 🖼️ %d │ ⏳ %d pending │ ❌ %d │ %.1f/s",
					spinner, bar, pct, formatDuration(elapsed), pdfs, screenshots, pending, errors, pagesPerSec)
			default:
				fmt.Printf("%s \033[32m[%s]\033[0m %3d%% │ ⏱ %s │ 📄 %d/%d │ ❌ %d │ %.1f/s",
					spinner, bar, pct, formatDuration(elapsed), captured, total, errors, pagesPerSec)
			}
		}
	}
}

func printJSONFeedFinalStats() {
	elapsed := time.Since(jsonFeedStartTime)
	wasCancelled := atomic.LoadInt32(&jsonCancelRequested) == 1

	fmt.Print("\033[2K\r")
	fmt.Println()
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	if wasCancelled {
		fmt.Println("║               📊 JSON FEED CAPTURE CANCELLED 📊                  ║")
	} else {
		fmt.Println("║                📊 JSON FEED CAPTURE COMPLETE 📊                  ║")
	}
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                   ║")
	fmt.Printf("║  ⏱️  Total Time:           %-40s ║\n", formatDuration(elapsed))
	fmt.Printf("║  📡 Feed Items Fetched:    %-40d ║\n", jsonFeedStats.ItemsFetched)
	fmt.Printf("║  🏷️  Items After Filter:    %-40d ║\n", jsonFeedStats.ItemsFiltered)
	fmt.Printf("║  📄 Pages Captured:        %-40d ║\n", jsonFeedStats.PagesCapture)

	switch jsonFeedFormat {
	case CapturePDFOnly:
		fmt.Printf("║  📑 PDFs Generated:        %-40d ║\n", jsonFeedStats.PDFsGenerated)
	case CaptureImagesOnly:
		fmt.Printf("║  🖼️  Images Generated:      %-40d ║\n", jsonFeedStats.ScreenshotsGen)
	case CaptureBoth:
		fmt.Printf("║  📑 PDFs Generated:        %-40d ║\n", jsonFeedStats.PDFsGenerated)
		fmt.Printf("║  🖼️  Images Generated:      %-40d ║\n", jsonFeedStats.ScreenshotsGen)
	case CaptureCMYKPDF:
		fmt.Printf("║  🎨 CMYK PDFs Generated:   %-40d ║\n", jsonFeedStats.PDFsGenerated)
	case CaptureCMYKTIFF:
		fmt.Printf("║  🎨 CMYK TIFFs Generated:  %-40d ║\n", jsonFeedStats.ScreenshotsGen)
	}

	fmt.Printf("║  ❌ Errors:                %-40d ║\n", jsonFeedStats.Errors)
	fmt.Printf("║  📁 Output Directory:      %-40s ║\n", jsonFeedOutputDir)
	fmt.Printf("║  📋 CSV Index:             %-40s ║\n", "feed_items.csv")
	fmt.Println("║                                                                   ║")
	if wasCancelled {
		fmt.Println("║  ℹ️  Capture was cancelled early - partial results saved         ║")
		fmt.Println("║                                                                   ║")
	}
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
}
