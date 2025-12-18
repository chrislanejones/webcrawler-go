package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"webcrawler/internal/crawler"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   🕷️  Web Crawler Wizard  🕷️                       ║")
	fmt.Println("║                        v2.1 - Cloudflare Buster                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Get the site URL
	var siteURL string
	var altEntryPoints []string

	for {
		fmt.Print("🌐 What site do you want to check?\n   → ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("❌ Error reading input:", err)
			continue
		}

		siteURL = strings.TrimSpace(input)

		if !strings.HasPrefix(siteURL, "http://") && !strings.HasPrefix(siteURL, "https://") {
			siteURL = "https://" + siteURL
		}

		parsedURL, err := url.Parse(siteURL)
		if err != nil || parsedURL.Host == "" {
			fmt.Println("❌ Invalid URL. Please enter a valid website address.")
			continue
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

			altEntryPoints = suggestAndTestAlternatives(siteURL, reader)

			if len(altEntryPoints) > 0 {
				fmt.Printf("\n   ✅ Found %d working entry point(s)!\n", len(altEntryPoints))
				fmt.Println("   🔄 Will start from these and retry blocked pages later")
				break
			} else {
				fmt.Println("\n   😔 No alternative entry points worked")
			}
		}

		fmt.Print("\n⚠️  Connection issues detected. Try anyway? (y/n): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
			break
		}
	}

	fmt.Println()

	// Step 2: Get the search mode
	fmt.Println("📋 What should I check the site for?")
	fmt.Println()
	fmt.Println("   ┌─────────────────────────────────────────────────────────┐")
	fmt.Println("   │  1. 🔗 Find a link on site (HTML, Word, PDF)            │")
	fmt.Println("   │  2. 📝 Find a word/phrase on site (HTML, Word, PDF)     │")
	fmt.Println("   │  3. 💔 Search for broken links                          │")
	fmt.Println("   │  4. 🖼️  Search for oversized images                     │")
	fmt.Println("   │  5. 📄 Generate PDF/Image for every page                │")
	fmt.Println("   └─────────────────────────────────────────────────────────┘")
	fmt.Println()

	var mode crawler.SearchMode
	for {
		fmt.Print("   Enter choice (1-5): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("❌ Error reading input:", err)
			continue
		}

		choice, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil || choice < 1 || choice > 5 {
			fmt.Println("   ❌ Please enter a number between 1 and 5")
			continue
		}

		mode = crawler.SearchMode(choice)
		break
	}

	fmt.Println()

	// Step 3: Get additional input based on mode
	var searchTarget string
	var imageSizeThreshold int64 = 500
	var captureFormat crawler.CaptureFormat = crawler.CaptureBoth

	switch mode {
	case crawler.ModeSearchLink:
		fmt.Print("🔗 Enter the link to search for:\n   → ")
		input, _ := reader.ReadString('\n')
		searchTarget = strings.TrimSpace(input)
		if searchTarget == "" {
			fmt.Println("❌ Link cannot be empty")
			os.Exit(1)
		}

	case crawler.ModeSearchWord:
		fmt.Print("📝 Enter the word or phrase to search for:\n   → ")
		input, _ := reader.ReadString('\n')
		searchTarget = strings.TrimSpace(input)
		if searchTarget == "" {
			fmt.Println("❌ Search term cannot be empty")
			os.Exit(1)
		}

	case crawler.ModeBrokenLinks:
		fmt.Println("💔 Will search for broken links (404s, timeouts, connection errors)")

	case crawler.ModeOversizedImages:
		fmt.Print("🖼️  Enter max image size in KB (default 500): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			if size, err := strconv.ParseInt(input, 10, 64); err == nil && size > 0 {
				imageSizeThreshold = size
			}
		}
		fmt.Printf("   Looking for images larger than %dKB\n", imageSizeThreshold)

	case crawler.ModePDFCapture:
		fmt.Println("📄 What format do you want to capture?")
		fmt.Println()
		fmt.Println("   ┌─────────────────────────────────────────────────────────┐")
		fmt.Println("   │  a. 📑 PDF only                                         │")
		fmt.Println("   │  b. 🖼️  Images only (PNG)                                │")
		fmt.Println("   │  c. 📑🖼️  Both PDF + Images                              │")
		fmt.Println("   │  d. 🎨 CMYK PDF (for print) *                            │")
		fmt.Println("   │  e. 🎨 CMYK TIFF (for InDesign) *                        │")
		fmt.Println("   └─────────────────────────────────────────────────────────┘")
		fmt.Println("   * Requires Ghostscript (d) or ImageMagick (e) installed")
		fmt.Println()
		for {
			fmt.Print("   Enter choice (a/b/c/d/e): ")
			formatInput, _ := reader.ReadString('\n')
			formatChoice := strings.ToLower(strings.TrimSpace(formatInput))
			switch formatChoice {
			case "a":
				captureFormat = crawler.CapturePDFOnly
				fmt.Println("   📑 Will generate PDFs only")
			case "b":
				captureFormat = crawler.CaptureImagesOnly
				fmt.Println("   🖼️  Will generate PNG screenshots only")
			case "c":
				captureFormat = crawler.CaptureBoth
				fmt.Println("   📑🖼️  Will generate both PDFs and PNG screenshots")
			case "d":
				captureFormat = crawler.CaptureCMYKPDF
				fmt.Println("   🎨 Will generate CMYK PDFs (requires Ghostscript)")
			case "e":
				captureFormat = crawler.CaptureCMYKTIFF
				fmt.Println("   🎨 Will generate CMYK TIFFs (requires ImageMagick)")
			default:
				fmt.Println("   ❌ Please enter a, b, c, d, or e")
				continue
			}
			break
		}
		fmt.Println("   📁 Output folder: ./page_captures/")
	}

	fmt.Println()

	// Step 4: Get concurrency setting
	fmt.Print("⚡ Max concurrent requests (default 5, max 20): ")
	concurrencyInput, _ := reader.ReadString('\n')
	concurrency := 5
	if c, err := strconv.Atoi(strings.TrimSpace(concurrencyInput)); err == nil && c > 0 {
		if c > 20 {
			c = 20
			fmt.Println("   ⚠️  Capped at 20 to avoid getting banned")
		}
		concurrency = c
	}

	// Step 5: Get retry settings
	fmt.Println()
	fmt.Print("🔄 Max retries per page (default 3): ")
	retryInput, _ := reader.ReadString('\n')
	maxRetries := 3
	if r, err := strconv.Atoi(strings.TrimSpace(retryInput)); err == nil && r >= 0 {
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
	}

	fmt.Println("┌─────────────────── LAUNCH CONFIG ───────────────────┐")
	fmt.Printf("│  🌐 Target:       %-35s │\n", truncateString(siteURL, 35))
	fmt.Printf("│  📊 Mode:         %-35s │\n", mode.String())
	if searchTarget != "" {
		fmt.Printf("│  🎯 Search for:   %-35s │\n", truncateString(searchTarget, 35))
	}
	fmt.Printf("│  ⚡ Concurrency:  %-35d │\n", concurrency)
	fmt.Printf("│  🔄 Max retries:  %-35d │\n", maxRetries)
	if len(altEntryPoints) > 0 {
		fmt.Printf("│  🚪 Alt entries:  %-35d │\n", len(altEntryPoints))
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

func suggestAndTestAlternatives(siteURL string, reader *bufio.Reader) []string {
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
	fmt.Print("   🔧 Enter a custom path to try (or press Enter to skip): ")
	customPath, _ := reader.ReadString('\n')
	customPath = strings.TrimSpace(customPath)

	if customPath != "" {
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
