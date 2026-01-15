package main

import (
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"webcrawler/internal/crawler"

	"github.com/charmbracelet/huh"
)

func main() {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   🕷️  Web Crawler Wizard  🕷️                      ║")
	fmt.Println("║                              v2.5                                 ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Special mode: Governor VA Newsroom Pull
	var useNewsroomPull bool
	newsroomForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Governor VA Newsroom Pull?").
				Description("Pull all news releases from governor.virginia.gov via sitemap").
				Affirmative("Yes").
				Negative("No, regular crawl").
				Value(&useNewsroomPull),
		),
	)

	if err := newsroomForm.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if useNewsroomPull {
		runNewsroomPull()
		return
	}

	// Step 1: Get the site URL
	var siteURL string
	var altEntryPoints []string
	var pathFilter string
	var usePathFilter bool

	for {
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("What site do you want to check?").
					Description("Tip: Include a path like /newsroom/ to only crawl that section").
					Placeholder("https://example.com").
					Value(&siteURL),
			),
		)

		err := form.Run()
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}

		siteURL = strings.TrimSpace(siteURL)

		if !strings.HasPrefix(siteURL, "http://") && !strings.HasPrefix(siteURL, "https://") {
			siteURL = "https://" + siteURL
		}

		parsedURL, err := url.Parse(siteURL)
		if err != nil || parsedURL.Host == "" {
			fmt.Println("❌ Invalid URL. Please enter a valid website address.")
			siteURL = ""
			continue
		}

		// Check if user provided a path (not just "/" or "")
		if parsedURL.Path != "" && parsedURL.Path != "/" {
			pathFilter = parsedURL.Path
			// Ensure it ends with / for proper prefix matching
			if !strings.HasSuffix(pathFilter, "/") {
				pathFilter = pathFilter + "/"
			}

			confirmForm := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Only crawl pages under %s?", pathFilter)).
						Description("Detected path filter in URL").
						Value(&usePathFilter),
				),
			)

			if err := confirmForm.Run(); err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}

			if !usePathFilter {
				pathFilter = ""
				fmt.Println("◇ Will crawl entire site")
			} else {
				fmt.Printf("◇ Will only crawl pages under %s\n", pathFilter)
			}
		}

		fmt.Printf("\n🔍 Testing connection to %s...\n", siteURL)
		success, attempts, blocked := testConnectionWithRetry(siteURL, 3)

		if success {
			fmt.Printf("   📊 Connected after %d attempt(s)\n", attempts)
			break
		}

		if blocked {
			fmt.Println()
			fmt.Println("   🛡️  Cloudflare/Bot protection detected on main page!")
			fmt.Println("   💡 Let's try some alternative entry points...")
			fmt.Println()

			altEntryPoints = suggestAndTestAlternatives(siteURL)

			if len(altEntryPoints) > 0 {
				fmt.Printf("\n   ✅ Found %d working entry point(s)!\n", len(altEntryPoints))
				fmt.Println("   🔄 Will start from these and retry blocked pages later")
				break
			} else {
				fmt.Println("\n   😔 No alternative entry points worked")
			}
		}

		var tryAnyway bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Connection issues detected. Try anyway?").
					Affirmative("Yes").
					Negative("No").
					Value(&tryAnyway),
			),
		)

		if err := confirmForm.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}

		if tryAnyway {
			break
		}
	}

	fmt.Println()

	// Step 2: Get the search mode
	var modeChoice int
	modeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("What should I check the site for?").
				Options(
					huh.NewOption("🔗 Find a link on site (HTML, Word, PDF)", 1),
					huh.NewOption("📝 Find a word/phrase on site (HTML, Word, PDF)", 2),
					huh.NewOption("💔 Search for broken links", 3),
					huh.NewOption("🖼️  Search for oversized images", 4),
					huh.NewOption("📄 Generate PDF/Image for every page", 5),
					huh.NewOption("🗺️  Generate XML sitemap", 6),
				).
				Value(&modeChoice),
		),
	)

	if err := modeForm.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	mode := crawler.SearchMode(modeChoice)

	fmt.Println()

	// Step 3: Get additional input based on mode
	var searchTarget string
	var imageSizeThreshold int64 = 500
	var captureFormat crawler.CaptureFormat = crawler.CaptureBoth
	var sitemapOptions crawler.SitemapOptions

	switch mode {
	case crawler.ModeSearchLink:
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter the link to search for").
					Placeholder("https://example.com/page").
					Value(&searchTarget).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("link cannot be empty")
						}
						return nil
					}),
			),
		)

		if err := form.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		searchTarget = strings.TrimSpace(searchTarget)

	case crawler.ModeSearchWord:
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter the word or phrase to search for").
					Placeholder("search term").
					Value(&searchTarget).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("search term cannot be empty")
						}
						return nil
					}),
			),
		)

		if err := form.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		searchTarget = strings.TrimSpace(searchTarget)

	case crawler.ModeBrokenLinks:
		fmt.Println("◇ Will search for broken links (404s, timeouts, connection errors)")

	case crawler.ModeOversizedImages:
		var sizeStr string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Max image size in KB").
					Description("Images larger than this will be flagged").
					Placeholder("500").
					Value(&sizeStr),
			),
		)

		if err := form.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}

		if sizeStr != "" {
			if size, err := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64); err == nil && size > 0 {
				imageSizeThreshold = size
			}
		}
		fmt.Printf("◇ Looking for images larger than %dKB\n", imageSizeThreshold)

	case crawler.ModePDFCapture:
		var formatChoice string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("What format do you want to capture?").
					Options(
						huh.NewOption("📑 PDF only", "pdf"),
						huh.NewOption("🖼️  Images only (PNG)", "images"),
						huh.NewOption("📑🖼️  Both PDF + Images", "both"),
						huh.NewOption("🎨 CMYK PDF (for print) - requires Ghostscript", "cmyk-pdf"),
						huh.NewOption("🎨 CMYK TIFF (for InDesign) - requires ImageMagick", "cmyk-tiff"),
					).
					Value(&formatChoice),
			),
		)

		if err := form.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}

		switch formatChoice {
		case "pdf":
			captureFormat = crawler.CapturePDFOnly
			fmt.Println("◇ Will generate PDFs only")
		case "images":
			captureFormat = crawler.CaptureImagesOnly
			fmt.Println("◇ Will generate PNG screenshots only")
		case "both":
			captureFormat = crawler.CaptureBoth
			fmt.Println("◇ Will generate both PDFs and PNG screenshots")
		case "cmyk-pdf":
			captureFormat = crawler.CaptureCMYKPDF
			fmt.Println("◇ Will generate CMYK PDFs (requires Ghostscript)")
		case "cmyk-tiff":
			captureFormat = crawler.CaptureCMYKTIFF
			fmt.Println("◇ Will generate CMYK TIFFs (requires ImageMagick)")
		}
		fmt.Println("◇ Output folder: ./page_captures/")

	case crawler.ModeSitemap:
		var filename string
		var freqChoice string
		var priorityStr string
		var includeLastMod bool

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Output filename").
					Placeholder("sitemap.xml").
					Value(&filename),
			),
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Default change frequency").
					Options(
						huh.NewOption("always", "always"),
						huh.NewOption("hourly", "hourly"),
						huh.NewOption("daily", "daily"),
						huh.NewOption("weekly (default)", "weekly"),
						huh.NewOption("monthly", "monthly"),
						huh.NewOption("yearly", "yearly"),
						huh.NewOption("never", "never"),
					).
					Value(&freqChoice),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Default priority (0.0-1.0)").
					Placeholder("0.5").
					Value(&priorityStr),
			),
			huh.NewGroup(
				huh.NewConfirm().
					Title("Include last modified date from server?").
					Value(&includeLastMod),
			),
		)

		if err := form.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}

		// Process filename
		if filename == "" {
			filename = "sitemap.xml"
		}
		if !strings.HasSuffix(filename, ".xml") {
			filename = filename + ".xml"
		}
		sitemapOptions.Filename = filename

		// Process frequency
		if freqChoice == "" {
			freqChoice = "weekly"
		}
		sitemapOptions.ChangeFreq = freqChoice

		// Process priority
		if priorityStr == "" {
			sitemapOptions.Priority = 0.5
		} else {
			if priority, err := strconv.ParseFloat(strings.TrimSpace(priorityStr), 64); err == nil && priority >= 0.0 && priority <= 1.0 {
				sitemapOptions.Priority = priority
			} else {
				sitemapOptions.Priority = 0.5
				fmt.Println("◇ Invalid priority, using default 0.5")
			}
		}

		sitemapOptions.IncludeLastMod = includeLastMod

		fmt.Printf("◇ Output file: ./%s\n", sitemapOptions.Filename)
		fmt.Printf("◇ Change frequency: %s\n", sitemapOptions.ChangeFreq)
		fmt.Printf("◇ Priority: %.1f\n", sitemapOptions.Priority)
		if sitemapOptions.IncludeLastMod {
			fmt.Println("◇ Will include Last-Modified dates when available")
		}
	}

	fmt.Println()

	// Step 4: Get concurrency and retry settings
	var concurrencyStr string
	var retriesStr string
	var ignoreQueryParams bool

	settingsForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Max concurrent requests").
				Description("Default: 5, max: 20").
				Placeholder("5").
				Value(&concurrencyStr),
			huh.NewInput().
				Title("Max retries per page").
				Description("Default: 3").
				Placeholder("3").
				Value(&retriesStr),
			huh.NewConfirm().
				Title("Ignore query parameters?").
				Description("Treat page.html?a=1 and page.html?b=2 as the same page").
				Affirmative("Yes").
				Negative("No").
				Value(&ignoreQueryParams),
		),
	)

	if err := settingsForm.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	concurrency := 5
	if c, err := strconv.Atoi(strings.TrimSpace(concurrencyStr)); err == nil && c > 0 {
		if c > 20 {
			c = 20
			fmt.Println("◇ Capped at 20 to avoid getting banned")
		}
		concurrency = c
	}

	maxRetries := 3
	if r, err := strconv.Atoi(strings.TrimSpace(retriesStr)); err == nil && r >= 0 {
		maxRetries = r
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println()

	config := crawler.Config{
		StartURL:           siteURL,
		AltEntryPoints:     altEntryPoints,
		Mode:               mode,
		SearchTarget:       searchTarget,
		MaxConcurrency:     concurrency,
		ImageSizeThreshold: imageSizeThreshold * 1024,
		MaxRetries:         maxRetries,
		RetryDelay:         2 * time.Second,
		RetryBlockedPages:  true,
		BlockedRetryPasses: 3,
		CaptureFormat:      captureFormat,
		PathFilter:         pathFilter,
		IgnoreQueryParams:  ignoreQueryParams,
		SitemapOpts:        sitemapOptions,
	}

	fmt.Println("┌─────────────────── LAUNCH CONFIG ───────────────────┐")
	fmt.Printf("│  🌐 Target:       %-35s │\n", truncateString(config.StartURL, 35))
	fmt.Printf("│  📋 Mode:         %-35s │\n", mode.String())
	if searchTarget != "" {
		fmt.Printf("│  🎯 Search for:   %-35s │\n", truncateString(searchTarget, 35))
	}
	fmt.Printf("│  ⚡ Concurrency:  %-35d │\n", concurrency)
	fmt.Printf("│  🔄 Max retries:  %-35d │\n", maxRetries)
	if len(altEntryPoints) > 0 {
		fmt.Printf("│  🚪 Alt entries:  %-35d │\n", len(altEntryPoints))
	}
	if ignoreQueryParams {
		fmt.Printf("│  🔗 Query params: %-35s │\n", "Ignored (dedup)")
	}
	fmt.Println("└─────────────────────────────────────────────────────┘")
	fmt.Println()

	fmt.Println("🚀 LAUNCHING CRAWLER...")
	fmt.Println()

	crawler.Start(config)

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println("🎉 Crawl complete! Check the results CSV file for details.")
	fmt.Println("════════════════════════════════════════════════════════════════════")
}

func suggestAndTestAlternatives(siteURL string) []string {
	parsedURL, _ := url.Parse(siteURL)
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	commonPaths := []string{
		"/about", "/about-us", "/contact", "/contact-us",
		"/sitemap.xml", "/robots.txt", "/privacy", "/privacy-policy",
		"/terms", "/help", "/faq", "/blog", "/news",
		"/products", "/services", "/team", "/careers",
	}

	fmt.Println("   Testing common entry points...")
	fmt.Println()

	var workingEntries []string

	for i, path := range commonPaths {
		testURL := baseURL + path
		fmt.Printf("   [%2d/%d] Testing %-20s", i+1, len(commonPaths), path)

		success, blocked := quickTest(testURL)

		if success {
			fmt.Println(" ✅ WORKS!")
			workingEntries = append(workingEntries, testURL)
		} else if blocked {
			fmt.Println(" 🛡️  Blocked")
		} else {
			fmt.Println(" ❌ Failed")
		}

		time.Sleep(500 * time.Millisecond)
	}

	fmt.Println()

	var customPath string
	customForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Enter a custom path to try").
				Description("Press Enter to skip").
				Placeholder("/custom-path").
				Value(&customPath),
		),
	)

	if err := customForm.Run(); err == nil && customPath != "" {
		customPath = strings.TrimSpace(customPath)
		if !strings.HasPrefix(customPath, "/") {
			customPath = "/" + customPath
		}
		testURL := baseURL + customPath
		fmt.Printf("   Testing %s...", customPath)

		success, _ := quickTest(testURL)
		if success {
			fmt.Println(" ✅ WORKS!")
			workingEntries = append(workingEntries, testURL)
		} else {
			fmt.Println(" ❌ Failed")
		}
	}

	return workingEntries
}

func quickTest(testURL string) (success bool, blocked bool) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return false, false
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 || resp.StatusCode == 503 {
		return false, true
	}

	if resp.StatusCode == 404 {
		return false, false
	}

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	bodyStr := strings.ToLower(string(body[:n]))

	if strings.Contains(bodyStr, "checking your browser") ||
		(strings.Contains(bodyStr, "cloudflare") && strings.Contains(bodyStr, "ray id")) ||
		strings.Contains(bodyStr, "ddos protection") ||
		(strings.Contains(bodyStr, "please wait") && strings.Contains(bodyStr, "redirecting")) {
		return false, true
	}

	return resp.StatusCode >= 200 && resp.StatusCode < 400, false
}

func testConnectionWithRetry(siteURL string, maxAttempts int) (success bool, attempts int, blocked bool) {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	}

	wasBlocked := false

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("   🔄 Attempt %d/%d", attempt, maxAttempts)

		client := &http.Client{
			Timeout: time.Duration(10+attempt*5) * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}

		req, err := http.NewRequest("GET", siteURL, nil)
		if err != nil {
			fmt.Printf(" ❌ Invalid URL\n")
			return false, attempt, false
		}

		req.Header.Set("User-Agent", userAgents[attempt%len(userAgents)])
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Upgrade-Insecure-Requests", "1")

		startTime := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(startTime)

		if err != nil {
			errStr := err.Error()
			switch {
			case strings.Contains(errStr, "timeout"):
				fmt.Printf(" ⏱️  TIMEOUT (%.1fs)\n", latency.Seconds())
			case strings.Contains(errStr, "connection refused"):
				fmt.Printf(" 🚫 CONNECTION REFUSED\n")
			case strings.Contains(errStr, "no such host"):
				fmt.Printf(" 🌐 DNS ERROR - Domain not found\n")
				return false, attempt, false
			case strings.Contains(errStr, "certificate"):
				fmt.Printf(" 🔒 SSL ERROR (will skip verification)\n")
			default:
				fmt.Printf(" ❌ %v\n", err)
			}

			if attempt < maxAttempts {
				delay := time.Duration(attempt*2) * time.Second
				fmt.Printf("   ⏳ Waiting %.0fs before retry...\n", delay.Seconds())
				time.Sleep(delay)
			}
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 403 || resp.StatusCode == 503 {
			wasBlocked = true
			body := make([]byte, 4096)
			n, _ := resp.Body.Read(body)
			bodyStr := strings.ToLower(string(body[:n]))

			if strings.Contains(bodyStr, "cloudflare") {
				fmt.Printf(" 🛡️  CLOUDFLARE DETECTED (%d)\n", resp.StatusCode)
			} else if strings.Contains(bodyStr, "ddos protection") {
				fmt.Printf(" 🛡️  DDOS PROTECTION (%d)\n", resp.StatusCode)
			} else {
				fmt.Printf(" 🛡️  BLOCKED (%d)\n", resp.StatusCode)
			}

			if attempt < maxAttempts {
				delay := time.Duration(attempt*3) * time.Second
				fmt.Printf("   ⏳ Waiting %.0fs before retry with different headers...\n", delay.Seconds())
				time.Sleep(delay)
			}
			continue
		}

		if resp.StatusCode == 429 {
			wasBlocked = true
			fmt.Printf(" 🌐 RATE LIMITED (429)\n")
			if attempt < maxAttempts {
				delay := time.Duration(attempt*5) * time.Second
				fmt.Printf("   ⏳ Rate limited! Waiting %.0fs...\n", delay.Seconds())
				time.Sleep(delay)
			}
			continue
		}

		if resp.StatusCode == 200 {
			body := make([]byte, 4096)
			n, _ := resp.Body.Read(body)
			bodyStr := strings.ToLower(string(body[:n]))

			if strings.Contains(bodyStr, "checking your browser") ||
				(strings.Contains(bodyStr, "please wait") && strings.Contains(bodyStr, "redirecting")) {
				wasBlocked = true
				fmt.Printf(" 🛡️  CHALLENGE PAGE DETECTED\n")
				if attempt < maxAttempts {
					delay := time.Duration(attempt*3) * time.Second
					fmt.Printf("   ⏳ Waiting %.0fs...\n", delay.Seconds())
					time.Sleep(delay)
				}
				continue
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			fmt.Printf(" ✅ OK (%d) - %.0fms latency\n", resp.StatusCode, float64(latency.Milliseconds()))
			return true, attempt, false
		}

		fmt.Printf(" ⚠️  Status %d\n", resp.StatusCode)
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	return false, maxAttempts, wasBlocked
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Sitemap XML structures
type SitemapIndex struct {
	XMLName  xml.Name  `xml:"sitemapindex"`
	Sitemaps []Sitemap `xml:"sitemap"`
}

type Sitemap struct {
	Loc string `xml:"loc"`
}

type URLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []SitemapURL `xml:"url"`
}

type SitemapURL struct {
	Loc string `xml:"loc"`
}

func runNewsroomPull() {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              📰 Governor VA Newsroom Pull 📰                      ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Ask for year filter
	var yearFilter string
	yearForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Filter by year (optional)").
				Description("Leave blank for all years, or enter e.g. '2025'").
				Placeholder("2025").
				Value(&yearFilter),
		),
	)

	if err := yearForm.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	yearFilter = strings.TrimSpace(yearFilter)

	// Step 2: Ask for month filter (only if year specified)
	var monthFilter string
	if yearFilter != "" {
		monthForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Filter by month (optional)").
					Description("Leave blank for all months, or enter e.g. 'december'").
					Placeholder("december").
					Value(&monthFilter),
			),
		)

		if err := monthForm.Run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		monthFilter = strings.TrimSpace(strings.ToLower(monthFilter))
	}

	// Step 3: Ask for capture format
	var formatChoice string
	formatForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What format do you want to capture?").
				Options(
					huh.NewOption("📑 PDF only", "pdf"),
					huh.NewOption("🖼️  Images only (PNG)", "images"),
					huh.NewOption("📑🖼️  Both PDF + Images", "both"),
				).
				Value(&formatChoice),
		),
	)

	if err := formatForm.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	var captureFormat crawler.CaptureFormat
	switch formatChoice {
	case "pdf":
		captureFormat = crawler.CapturePDFOnly
	case "images":
		captureFormat = crawler.CaptureImagesOnly
	case "both":
		captureFormat = crawler.CaptureBoth
	}

	// Step 4: Fetch and parse sitemap
	fmt.Println()
	fmt.Println("🗺️  Fetching sitemap from governor.virginia.gov...")

	urls, err := fetchNewsroomURLs(yearFilter, monthFilter)
	if err != nil {
		fmt.Printf("❌ Error fetching sitemap: %v\n", err)
		os.Exit(1)
	}

	if len(urls) == 0 {
		fmt.Println("❌ No news release URLs found matching your filters.")
		os.Exit(1)
	}

	// Build filter description
	filterDesc := "all news releases"
	if yearFilter != "" {
		filterDesc = yearFilter
		if monthFilter != "" {
			filterDesc += "/" + monthFilter
		}
	}

	fmt.Printf("✅ Found %d news release URLs (%s)\n", len(urls), filterDesc)
	fmt.Println()

	// Step 5: Confirm and start
	var confirm bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Start capturing %d pages?", len(urls))).
				Affirmative("Yes, start").
				Negative("Cancel").
				Value(&confirm),
		),
	)

	if err := confirmForm.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if !confirm {
		fmt.Println("Cancelled.")
		return
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println()

	// Step 6: Start capture
	config := crawler.Config{
		StartURL:          "https://www.governor.virginia.gov/newsroom/news-releases/",
		MaxConcurrency:    5,
		MaxRetries:        3,
		RetryDelay:        2 * time.Second,
		CaptureFormat:     captureFormat,
		IgnoreQueryParams: true,
	}

	crawler.StartNewsroomCapture(config, urls)

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println("🎉 Newsroom pull complete!")
	fmt.Println("════════════════════════════════════════════════════════════════════")
}

func fetchNewsroomURLs(yearFilter, monthFilter string) ([]string, error) {
	baseURL := "https://www.governor.virginia.gov"
	sitemapURL := baseURL + "/sitemap.xml"

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Fetch main sitemap index
	resp, err := client.Get(sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sitemap: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read sitemap: %w", err)
	}

	var allURLs []string

	// Try parsing as sitemap index first
	var sitemapIndex SitemapIndex
	if err := xml.Unmarshal(body, &sitemapIndex); err == nil && len(sitemapIndex.Sitemaps) > 0 {
		fmt.Printf("   📂 Found sitemap index with %d sub-sitemaps\n", len(sitemapIndex.Sitemaps))

		for _, sitemap := range sitemapIndex.Sitemaps {
			// Only fetch sitemaps that might contain newsroom URLs
			if !strings.Contains(sitemap.Loc, "newsroom") && !strings.Contains(sitemap.Loc, "news") {
				continue
			}

			fmt.Printf("   📄 Fetching %s...\n", truncateString(sitemap.Loc, 50))

			subResp, err := client.Get(sitemap.Loc)
			if err != nil {
				continue
			}

			subBody, err := io.ReadAll(subResp.Body)
			subResp.Body.Close()
			if err != nil {
				continue
			}

			var urlSet URLSet
			if err := xml.Unmarshal(subBody, &urlSet); err == nil {
				for _, u := range urlSet.URLs {
					if isNewsReleaseURL(u.Loc, yearFilter, monthFilter) {
						allURLs = append(allURLs, u.Loc)
					}
				}
			}
		}
	} else {
		// Try parsing as direct URL set
		var urlSet URLSet
		if err := xml.Unmarshal(body, &urlSet); err == nil {
			for _, u := range urlSet.URLs {
				if isNewsReleaseURL(u.Loc, yearFilter, monthFilter) {
					allURLs = append(allURLs, u.Loc)
				}
			}
		}
	}

	// If sitemap didn't work, try fetching the known newsroom sitemap directly
	if len(allURLs) == 0 {
		fmt.Println("   🔄 Trying direct newsroom sitemap...")

		// Try common sitemap patterns
		newsroomSitemaps := []string{
			baseURL + "/newsroom-sitemap.xml",
			baseURL + "/sitemap-newsroom.xml",
			baseURL + "/newsroom/sitemap.xml",
		}

		for _, smURL := range newsroomSitemaps {
			resp, err := client.Get(smURL)
			if err != nil || resp.StatusCode != 200 {
				if resp != nil {
					resp.Body.Close()
				}
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}

			var urlSet URLSet
			if err := xml.Unmarshal(body, &urlSet); err == nil {
				for _, u := range urlSet.URLs {
					if isNewsReleaseURL(u.Loc, yearFilter, monthFilter) {
						allURLs = append(allURLs, u.Loc)
					}
				}
				if len(allURLs) > 0 {
					break
				}
			}
		}
	}

	return allURLs, nil
}

func isNewsReleaseURL(urlStr, yearFilter, monthFilter string) bool {
	// Must be a news release page (contains /name-)
	if !strings.Contains(urlStr, "/newsroom/news-releases/") {
		return false
	}
	if !strings.Contains(urlStr, "/name-") {
		return false
	}

	// Apply year filter
	if yearFilter != "" && !strings.Contains(urlStr, "/"+yearFilter+"/") {
		return false
	}

	// Apply month filter
	if monthFilter != "" && !strings.Contains(urlStr, "/"+monthFilter+"/") {
		return false
	}

	return true
}
