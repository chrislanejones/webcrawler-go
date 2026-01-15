package crawler

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
)

// SitemapURL represents a single URL entry in the sitemap
type SitemapURL struct {
	Loc        string  `xml:"loc"`
	LastMod    string  `xml:"lastmod,omitempty"`
	ChangeFreq string  `xml:"changefreq,omitempty"`
	Priority   float64 `xml:"priority,omitempty"`
}

// URLSet is the root element of a sitemap
type URLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []SitemapURL `xml:"url"`
}

// Sitemap-specific variables
var (
	sitemapURLs    sync.Map // stores URLs to include in sitemap
	sitemapVisited sync.Map // tracks all visited URLs to avoid duplicates
	sitemapConfig  Config
	sitemapWG      sync.WaitGroup
	sitemapSema    chan struct{}
	sitemapBase    *url.URL
	sitemapStats   struct {
		PagesFound   int64
		PagesChecked int64
		ErrorCount   int64
		BlockedCount int64
		SkippedCount int64
	}
	sitemapStart time.Time
)

// SitemapEntry holds URL info for sitemap generation
type SitemapEntry struct {
	URL     string
	LastMod string
}

// StartSitemapGeneration initiates the sitemap crawl and generation
func StartSitemapGeneration(cfg Config) {
	sitemapURLs = sync.Map{}
	sitemapVisited = sync.Map{}
	sitemapConfig = cfg
	sitemapStart = time.Now()
	sitemapStats = struct {
		PagesFound   int64
		PagesChecked int64
		ErrorCount   int64
		BlockedCount int64
		SkippedCount int64
	}{}

	sitemapSema = make(chan struct{}, cfg.MaxConcurrency)

	var err error
	sitemapBase, err = url.Parse(cfg.StartURL)
	if err != nil {
		fmt.Printf("❌ Invalid start URL: %v\n", err)
		return
	}

	fmt.Println("┌─────────────────── SITEMAP GENERATION ───────────────────┐")
	fmt.Printf("│  🌐 Target: %-44s │\n", truncateString(cfg.StartURL, 44))
	fmt.Printf("│  📄 Output: %-44s │\n", cfg.SitemapOpts.Filename)
	fmt.Printf("│  📅 Freq:   %-44s │\n", cfg.SitemapOpts.ChangeFreq)
	fmt.Printf("│  ⭐ Priority: %-42.1f │\n", cfg.SitemapOpts.Priority)
	fmt.Println("└───────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Start live stats
	stopStats := make(chan bool)
	go printSitemapLiveStats(stopStats)

	// Begin crawling
	crawlForSitemap(cfg.StartURL)
	sitemapWG.Wait()

	// Stop live stats
	stopStats <- true

	// Generate the sitemap file
	generateSitemapFile(cfg)

	// Print final stats
	printSitemapFinalStats(cfg)
}

func printSitemapLiveStats(stop chan bool) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			elapsed := time.Since(sitemapStart)
			found := atomic.LoadInt64(&sitemapStats.PagesFound)
			checked := atomic.LoadInt64(&sitemapStats.PagesChecked)
			errors := atomic.LoadInt64(&sitemapStats.ErrorCount)
			blocked := atomic.LoadInt64(&sitemapStats.BlockedCount)

			pagesPerSec := float64(checked) / elapsed.Seconds()

			fmt.Printf("\r🗺️  [%s] Found: %d | Checked: %d | Errors: %d | Blocked: %d | %.1f p/s     ",
				formatDuration(elapsed),
				found,
				checked,
				errors,
				blocked,
				pagesPerSec,
			)
		}
	}
}

func crawlForSitemap(link string) {
	// Normalize URL
	parsedURL, err := url.Parse(link)
	if err != nil {
		return
	}

	// Remove fragment and query for normalization
	parsedURL.Fragment = ""
	normalizedURL := parsedURL.String()

	// Check if already visited using separate visited map
	if _, loaded := sitemapVisited.LoadOrStore(normalizedURL, true); loaded {
		return
	}

	// Check path filter to determine if URL should be in sitemap
	includeInSitemap := true
	if sitemapConfig.PathFilter != "" {
		pathWithSlash := parsedURL.Path
		if !strings.HasSuffix(pathWithSlash, "/") {
			pathWithSlash = pathWithSlash + "/"
		}
		filterPath := sitemapConfig.PathFilter
		if !strings.HasSuffix(filterPath, "/") {
			filterPath = filterPath + "/"
		}

		// Check if the URL path starts with the filter path
		if !strings.HasPrefix(pathWithSlash, filterPath) && pathWithSlash != filterPath {
			includeInSitemap = false
			atomic.AddInt64(&sitemapStats.SkippedCount, 1)
		}
	}

	// Only add to sitemap URLs if it matches the filter
	if includeInSitemap {
		sitemapURLs.Store(normalizedURL, &SitemapEntry{URL: normalizedURL})
	}

	atomic.AddInt64(&sitemapStats.PagesFound, 1)

	sitemapWG.Add(1)
	go func(shouldInclude bool) {
		defer sitemapWG.Done()
		sitemapSema <- struct{}{}
		defer func() { <-sitemapSema }()

		fetchForSitemap(normalizedURL, shouldInclude)
	}(includeInSitemap)
}

func fetchForSitemap(link string, includeInSitemap bool) {
	atomic.AddInt64(&sitemapStats.PagesChecked, 1)

	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		atomic.AddInt64(&sitemapStats.ErrorCount, 1)
		if includeInSitemap {
			sitemapURLs.Delete(link)
		}
		return
	}

	req.Header.Set("User-Agent", userAgents[0])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")

	resp, err := httpClient.Do(req)
	if err != nil {
		atomic.AddInt64(&sitemapStats.ErrorCount, 1)
		if includeInSitemap {
			sitemapURLs.Delete(link)
		}
		return
	}
	defer resp.Body.Close()

	// Handle blocked/error responses
	if resp.StatusCode == 403 || resp.StatusCode == 503 || resp.StatusCode == 429 {
		atomic.AddInt64(&sitemapStats.BlockedCount, 1)
		if includeInSitemap {
			sitemapURLs.Delete(link)
		}
		return
	}

	if resp.StatusCode >= 400 {
		atomic.AddInt64(&sitemapStats.ErrorCount, 1)
		if includeInSitemap {
			sitemapURLs.Delete(link)
		}
		return
	}

	// Only include HTML pages in sitemap
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		if includeInSitemap {
			sitemapURLs.Delete(link)
		}
		return
	}

	// Get Last-Modified header if requested
	if includeInSitemap && sitemapConfig.SitemapOpts.IncludeLastMod {
		if lm := resp.Header.Get("Last-Modified"); lm != "" {
			if t, err := time.Parse(time.RFC1123, lm); err == nil {
				if entry, ok := sitemapURLs.Load(link); ok {
					e := entry.(*SitemapEntry)
					e.LastMod = t.Format("2006-01-02")
				}
			}
		}
	}

	// Read body
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return
		}
		defer gzReader.Close()
		reader = gzReader
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return
	}

	// Check for bot protection - use sitemap-specific detection that's less aggressive
	if detectSitemapBotProtection(string(bodyBytes)) {
		atomic.AddInt64(&sitemapStats.BlockedCount, 1)
		if includeInSitemap {
			sitemapURLs.Delete(link)
		}
		return
	}

	// Extract and follow internal links
	extractLinksForSitemap(bodyBytes, link)
}

// detectSitemapBotProtection is less aggressive than the main crawler's detection
// It only triggers on actual challenge pages, not just the presence of CDN names
func detectSitemapBotProtection(body string) bool {
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

func extractLinksForSitemap(body []byte, sourceURL string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return
	}

	var extractedLinks []string

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					href := strings.TrimSpace(a.Val)

					// Skip empty, anchors, mailto, tel, javascript
					if href == "" ||
						strings.HasPrefix(href, "#") ||
						strings.HasPrefix(href, "mailto:") ||
						strings.HasPrefix(href, "tel:") ||
						strings.HasPrefix(href, "javascript:") {
						continue
					}

					// Parse and resolve the URL
					u, err := url.Parse(href)
					if err != nil {
						continue
					}

					// Skip non-http(s) schemes
					if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
						continue
					}

					// Resolve relative URLs
					resolved := sitemapBase.ResolveReference(u)

					// Only follow same-host links
					if resolved.Host != sitemapBase.Host {
						continue
					}

					// Skip common non-page extensions
					path := strings.ToLower(resolved.Path)
					skipExtensions := []string{
						".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
						".zip", ".rar", ".tar", ".gz", ".7z",
						".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".ico",
						".mp3", ".mp4", ".avi", ".mov", ".wmv", ".flv",
						".css", ".js", ".json", ".xml", ".rss", ".atom",
					}
					skip := false
					for _, ext := range skipExtensions {
						if strings.HasSuffix(path, ext) {
							skip = true
							break
						}
					}
					if skip {
						continue
					}

					extractedLinks = append(extractedLinks, resolved.String())
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	// Generate archive URLs if this looks like a news/archive section
	parsedSource, _ := url.Parse(sourceURL)
	if parsedSource != nil {
		archiveLinks := generateArchiveURLs(parsedSource)
		extractedLinks = append(extractedLinks, archiveLinks...)

		// Also try pagination patterns for listing pages
		paginationLinks := generatePaginationURLs(parsedSource)
		extractedLinks = append(extractedLinks, paginationLinks...)
	}

	// Crawl all extracted links
	for _, link := range extractedLinks {
		time.Sleep(30 * time.Millisecond)
		crawlForSitemap(link)
	}
}

// generateArchiveURLs creates year/month archive URLs for news sections
func generateArchiveURLs(parsedURL *url.URL) []string {
	var urls []string

	path := parsedURL.Path
	pathLower := strings.ToLower(path)

	// Check if this looks like a news/archive section
	isNewsSection := strings.Contains(pathLower, "news") ||
		strings.Contains(pathLower, "press") ||
		strings.Contains(pathLower, "release") ||
		strings.Contains(pathLower, "archive") ||
		strings.Contains(pathLower, "blog") ||
		strings.Contains(pathLower, "article")

	if !isNewsSection {
		return urls
	}

	// Check if path contains a year (e.g., /news/2025/)
	yearPattern := regexp.MustCompile(`/(\d{4})/?$`)
	if matches := yearPattern.FindStringSubmatch(path); len(matches) > 1 {
		year, _ := strconv.Atoi(matches[1])

		// Generate month URLs
		months := []string{
			"january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december",
		}

		basePath := strings.TrimSuffix(path, "/")
		for _, month := range months {
			monthURL := fmt.Sprintf("%s://%s%s/%s/", parsedURL.Scheme, parsedURL.Host, basePath, month)
			urls = append(urls, monthURL)
		}

		// Also generate previous year if current year
		currentYear := time.Now().Year()
		if year == currentYear {
			prevYearPath := strings.Replace(path, fmt.Sprintf("/%d", year), fmt.Sprintf("/%d", year-1), 1)
			prevYearURL := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, prevYearPath)
			urls = append(urls, prevYearURL)
		}
	}

	// Check if path ends with a month, generate sibling months
	monthPattern := regexp.MustCompile(`/(\d{4})/(january|february|march|april|may|june|july|august|september|october|november|december)/?$`)
	if matches := monthPattern.FindStringSubmatch(pathLower); len(matches) > 2 {
		year := matches[1]
		basePath := path[:strings.LastIndex(strings.ToLower(path), matches[2])]

		months := []string{
			"january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december",
		}

		for _, month := range months {
			monthURL := fmt.Sprintf("%s://%s%s%s/", parsedURL.Scheme, parsedURL.Host, basePath, month)
			urls = append(urls, monthURL)
		}

		// Also try previous year
		if yearInt, err := strconv.Atoi(year); err == nil {
			prevYearBase := strings.Replace(basePath, year, strconv.Itoa(yearInt-1), 1)
			for _, month := range months {
				monthURL := fmt.Sprintf("%s://%s%s%s/", parsedURL.Scheme, parsedURL.Host, prevYearBase, month)
				urls = append(urls, monthURL)
			}
		}
	}

	// If no year in path but it's a news section, try adding current and recent years
	if !yearPattern.MatchString(path) && !monthPattern.MatchString(pathLower) {
		basePath := strings.TrimSuffix(path, "/")
		currentYear := time.Now().Year()

		for y := currentYear; y >= currentYear-2; y-- {
			yearURL := fmt.Sprintf("%s://%s%s/%d/", parsedURL.Scheme, parsedURL.Host, basePath, y)
			urls = append(urls, yearURL)
		}
	}

	return urls
}

// generatePaginationURLs creates paginated URLs for listing pages
func generatePaginationURLs(parsedURL *url.URL) []string {
	var urls []string

	// Check if this is a listing page (ends with / or has no file extension)
	path := parsedURL.Path
	isListingPage := strings.HasSuffix(path, "/") ||
		(!strings.Contains(filepath.Base(path), ".") && path != "")

	if !isListingPage {
		return urls
	}

	// Try ?page=N pattern
	if parsedURL.Query().Get("page") == "" {
		for i := 2; i <= 10; i++ {
			newQ := parsedURL.Query()
			newQ.Set("page", strconv.Itoa(i))
			newURL := *parsedURL
			newURL.RawQuery = newQ.Encode()
			urls = append(urls, newURL.String())
		}
	}

	// Try /page/N pattern
	if !strings.Contains(path, "/page/") {
		basePath := strings.TrimSuffix(path, "/")
		for i := 2; i <= 10; i++ {
			pageURL := fmt.Sprintf("%s://%s%s/page/%d/", parsedURL.Scheme, parsedURL.Host, basePath, i)
			urls = append(urls, pageURL)
		}
	}

	return urls
}

func generateSitemapFile(cfg Config) {
	fmt.Println()
	fmt.Println()
	fmt.Println("📝 Generating sitemap XML...")

	// Collect all URLs
	var urls []SitemapURL
	sitemapURLs.Range(func(key, value interface{}) bool {
		entry := value.(*SitemapEntry)
		sitemapURL := SitemapURL{
			Loc:        entry.URL,
			ChangeFreq: cfg.SitemapOpts.ChangeFreq,
			Priority:   cfg.SitemapOpts.Priority,
		}
		if entry.LastMod != "" {
			sitemapURL.LastMod = entry.LastMod
		}
		urls = append(urls, sitemapURL)
		return true
	})

	// Sort URLs alphabetically for consistency
	sort.Slice(urls, func(i, j int) bool {
		return urls[i].Loc < urls[j].Loc
	})

	// Create the URLSet
	urlSet := URLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	// Marshal to XML
	output, err := xml.MarshalIndent(urlSet, "", "  ")
	if err != nil {
		fmt.Printf("❌ Error generating XML: %v\n", err)
		return
	}

	// Add XML header
	xmlContent := []byte(xml.Header + string(output))

	// Write to file
	filename := cfg.SitemapOpts.Filename
	if filename == "" {
		filename = "sitemap.xml"
	}

	err = os.WriteFile(filename, xmlContent, 0644)
	if err != nil {
		fmt.Printf("❌ Error writing sitemap file: %v\n", err)
		return
	}

	fmt.Printf("✅ Sitemap written to: %s\n", filename)
	fmt.Printf("   📊 Total URLs: %d\n", len(urls))
	fmt.Printf("   📦 File size: %s\n", formatBytes(int64(len(xmlContent))))
}

func printSitemapFinalStats(cfg Config) {
	elapsed := time.Since(sitemapStart)

	urlCount := 0
	sitemapURLs.Range(func(key, value interface{}) bool {
		urlCount++
		return true
	})

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  🗺️  SITEMAP GENERATION COMPLETE  🗺️               ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                   ║")
	fmt.Printf("║  ⏱️  Total Time:           %-40s ║\n", formatDuration(elapsed))
	fmt.Printf("║  📄 URLs in Sitemap:       %-40d ║\n", urlCount)
	fmt.Printf("║  🔍 Pages Checked:         %-40d ║\n", sitemapStats.PagesChecked)
	fmt.Printf("║  ❌ Errors:                %-40d ║\n", sitemapStats.ErrorCount)
	fmt.Printf("║  🛡️  Blocked:               %-40d ║\n", sitemapStats.BlockedCount)
	fmt.Printf("║  ⏭️  Skipped (filtered):    %-40d ║\n", sitemapStats.SkippedCount)
	fmt.Println("║                                                                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                      📁 OUTPUT FILE                               ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  📄 Filename:              %-40s ║\n", cfg.SitemapOpts.Filename)
	fmt.Printf("║  📅 Change Frequency:      %-40s ║\n", cfg.SitemapOpts.ChangeFreq)
	fmt.Printf("║  ⭐ Priority:              %-40.1f ║\n", cfg.SitemapOpts.Priority)
	includeLastMod := "No"
	if cfg.SitemapOpts.IncludeLastMod {
		includeLastMod = "Yes"
	}
	fmt.Printf("║  🕐 Include Last Modified: %-40s ║\n", includeLastMod)
	fmt.Println("║                                                                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")

	if urlCount == 0 {
		fmt.Println()
		fmt.Println("⚠️  WARNING: No URLs were added to the sitemap!")
		fmt.Println("   💡 Tips:")
		fmt.Println("      - Check if the site is accessible")
		fmt.Println("      - The site might be blocking crawlers")
		fmt.Println("      - Try with a different path filter")
	}

	pagesPerSec := float64(sitemapStats.PagesChecked) / elapsed.Seconds()
	fmt.Println()
	fmt.Printf("⚡ Performance: %.2f pages/second\n", pagesPerSec)
}