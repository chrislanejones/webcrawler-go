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
	"sort"
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
	sitemapURLs   sync.Map // stores discovered URLs with their metadata
	sitemapConfig Config
	sitemapWG     sync.WaitGroup
	sitemapSema   chan struct{}
	sitemapBase   *url.URL
	sitemapStats  struct {
		PagesFound    int64
		PagesChecked  int64
		ErrorCount    int64
		BlockedCount  int64
		SkippedCount  int64
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
	sitemapConfig = cfg
	sitemapStart = time.Now()

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

	// Remove fragment
	parsedURL.Fragment = ""
	normalizedURL := parsedURL.String()

	// Check if already visited
	if _, loaded := sitemapURLs.LoadOrStore(normalizedURL, &SitemapEntry{URL: normalizedURL}); loaded {
		return
	}

	// Apply path filter if set
	if sitemapConfig.PathFilter != "" {
		if !strings.HasPrefix(parsedURL.Path, sitemapConfig.PathFilter) &&
			parsedURL.Path+"/" != sitemapConfig.PathFilter {
			sitemapURLs.Delete(normalizedURL)
			atomic.AddInt64(&sitemapStats.SkippedCount, 1)
			return
		}
	}

	atomic.AddInt64(&sitemapStats.PagesFound, 1)

	sitemapWG.Add(1)
	go func() {
		defer sitemapWG.Done()
		sitemapSema <- struct{}{}
		defer func() { <-sitemapSema }()

		fetchForSitemap(normalizedURL)
	}()
}

func fetchForSitemap(link string) {
	atomic.AddInt64(&sitemapStats.PagesChecked, 1)

	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		atomic.AddInt64(&sitemapStats.ErrorCount, 1)
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
		return
	}
	defer resp.Body.Close()

	// Handle blocked/error responses
	if resp.StatusCode == 403 || resp.StatusCode == 503 || resp.StatusCode == 429 {
		atomic.AddInt64(&sitemapStats.BlockedCount, 1)
		sitemapURLs.Delete(link)
		return
	}

	if resp.StatusCode >= 400 {
		atomic.AddInt64(&sitemapStats.ErrorCount, 1)
		sitemapURLs.Delete(link)
		return
	}

	// Only include HTML pages in sitemap
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		sitemapURLs.Delete(link)
		return
	}

	// Get Last-Modified header if requested
	var lastMod string
	if sitemapConfig.SitemapOpts.IncludeLastMod {
		if lm := resp.Header.Get("Last-Modified"); lm != "" {
			if t, err := time.Parse(time.RFC1123, lm); err == nil {
				lastMod = t.Format("2006-01-02")
			}
		}
	}

	// Update the entry with lastmod
	if entry, ok := sitemapURLs.Load(link); ok {
		e := entry.(*SitemapEntry)
		e.LastMod = lastMod
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

	// Check for bot protection
	if detectBotProtection(string(bodyBytes)) {
		atomic.AddInt64(&sitemapStats.BlockedCount, 1)
		sitemapURLs.Delete(link)
		return
	}

	// Extract and follow internal links
	extractLinksForSitemap(bodyBytes, link)
}

func extractLinksForSitemap(body []byte, sourceURL string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return
	}

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

					// Add small delay to be polite
					time.Sleep(30 * time.Millisecond)

					// Crawl the discovered URL
					crawlForSitemap(resolved.String())
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
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