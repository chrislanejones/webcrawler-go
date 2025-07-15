package crawler

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"webcrawler/internal/parser"

	"golang.org/x/net/html"
)

var (
	visited            = make(map[string]bool)
	mu                 sync.Mutex
	wg                 sync.WaitGroup
	sema               chan struct{}
	csvMu              sync.Mutex
	checkCount         int
	matchCount         int
	errorCount         int
	blockedCount       int
	startTime          = time.Now()
	httpClient         *http.Client
	resultFile         string
	currentSearchIndex int
	Verbose            = flag.Bool("verbose", false, "Enable verbose output")
	Quiet              = flag.Bool("quiet", false, "Suppress non-error output")
)

func init() {
	// Create cookie jar
	jar, _ := cookiejar.New(nil)
	
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: false,
			MaxIdleConns: 100,
			MaxIdleConnsPerHost: 10,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			// Copy headers to redirected request
			if len(via) > 0 {
				for key, val := range via[0].Header {
					req.Header[key] = val
				}
			}
			return nil
		},
	}
}

func Start(startURL, target string, concurrency int, operationIndex, websiteIndex, totalWebsites, targetIndex, totalTargets int) {
	// Reset counters for each search
	mu.Lock()
	visited = make(map[string]bool)
	checkCount = 0
	matchCount = 0
	errorCount = 0
	blockedCount = 0
	startTime = time.Now()
	currentSearchIndex = operationIndex
	mu.Unlock()

	flag.Parse()
	sema = make(chan struct{}, concurrency)

	base, err := url.Parse(startURL)
	if err != nil {
		panic(err)
	}

	// Create unique result file for each operation
	resultFile = fmt.Sprintf("results-operation-%d-website-%d-target-%d.csv", operationIndex, websiteIndex, targetIndex)

	createCSV()
	crawl(startURL, base, target)
	wg.Wait()

	if !*Quiet {
		fmt.Printf("✅ Operation %d completed (Website %d/%d, Target %d/%d)\n", 
			operationIndex, websiteIndex, totalWebsites, targetIndex, totalTargets)
		fmt.Printf("📊 Total checked: %d, Matches: %d, Errors: %d, Blocked: %d, Time: %s\n", 
			checkCount, matchCount, errorCount, blockedCount, time.Since(startTime).Truncate(time.Second))
		fmt.Printf("📄 Results saved to: %s\n", resultFile)
		
		if blockedCount > 0 {
			fmt.Printf("⚠️  Warning: %d pages were blocked by anti-bot protection\n", blockedCount)
		}
		if errorCount > 0 {
			fmt.Printf("⚠️  Warning: %d pages had errors (timeouts, 404s, etc.)\n", errorCount)
		}
	}
}

func createCSV() {
	f, _ := os.Create(resultFile)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"URL", "ContentType", "FoundIn", "TargetLink", "StartURL", "OperationIndex"})
}

func writeCSV(link, contentType, foundIn, target, startURL string) {
	csvMu.Lock()
	defer csvMu.Unlock()

	matchCount++

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{link, contentType, foundIn, target, startURL, fmt.Sprintf("%d", currentSearchIndex)})
}

func crawl(link string, base *url.URL, target string) {
	mu.Lock()
	if visited[link] {
		mu.Unlock()
		return
	}
	visited[link] = true
	checkCount++
	count := checkCount
	elapsed := time.Since(startTime)
	mu.Unlock()

	wg.Add(1)
	go func() {
		defer wg.Done()
		sema <- struct{}{}
		defer func() { <-sema }()

		if !*Quiet {
			fmt.Printf("🔍 [Op %d] Checking: %s\n", currentSearchIndex, link)
			if count%20 == 0 {
				fmt.Printf("📊 [Op %d] Checked %d pages (Elapsed: %s) Matches: %d\n", 
					currentSearchIndex, count, elapsed.Truncate(time.Second), matchCount)
			}
		}

		req, err := http.NewRequest("GET", link, nil)
		if err != nil {
			if !*Quiet {
				fmt.Printf("❌ [Op %d] Error creating request: %s - %v\n", currentSearchIndex, link, err)
			}
			return
		}
		
		// Add headers to mimic a browser
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("DNT", "1")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		
		resp, err := httpClient.Do(req)
		if err != nil {
			mu.Lock()
			errorCount++
			mu.Unlock()
			
			if !*Quiet {
				// Enhanced error detection
				errStr := err.Error()
				switch {
				case strings.Contains(errStr, "timeout"):
					fmt.Printf("⏱️  [Op %d] TIMEOUT: %s - The server is not responding (may be overloaded or blocking requests)\n", currentSearchIndex, link)
				case strings.Contains(errStr, "connection refused"):
					fmt.Printf("🚫 [Op %d] CONNECTION REFUSED: %s - The server is actively refusing connections\n", currentSearchIndex, link)
				case strings.Contains(errStr, "no such host"):
					fmt.Printf("🌐 [Op %d] DNS ERROR: %s - Domain name not found or DNS resolution failed\n", currentSearchIndex, link)
				case strings.Contains(errStr, "certificate"):
					fmt.Printf("🔒 [Op %d] SSL/TLS ERROR: %s - Certificate validation failed\n", currentSearchIndex, link)
				default:
					fmt.Printf("❌ [Op %d] NETWORK ERROR: %s - %v\n", currentSearchIndex, link, err)
				}
			}
			return
		}
		defer resp.Body.Close()

		// Enhanced status code handling
		statusCode := resp.StatusCode
		if statusCode >= 400 {
			mu.Lock()
			if statusCode == 403 || statusCode == 429 {
				blockedCount++
			} else {
				errorCount++
			}
			mu.Unlock()
		}
		
		if !*Quiet {
			switch {
			case statusCode == 200:
				if *Verbose {
					fmt.Printf("✅ [Op %d] SUCCESS: %s (Status: %d)\n", currentSearchIndex, link, statusCode)
				}
			case statusCode == 403:
				fmt.Printf("🚫 [Op %d] ACCESS FORBIDDEN (403): %s - The server is blocking this request (likely bot detection)\n", currentSearchIndex, link)
				fmt.Printf("   💡 Suggestion: The website may have anti-bot protection (Cloudflare, etc.)\n")
			case statusCode == 404:
				fmt.Printf("📄 [Op %d] PAGE NOT FOUND (404): %s - This page doesn't exist\n", currentSearchIndex, link)
			case statusCode == 429:
				fmt.Printf("🐌 [Op %d] RATE LIMITED (429): %s - Too many requests, server is throttling\n", currentSearchIndex, link)
				fmt.Printf("   💡 Suggestion: Reduce maxConcurrency in config.yaml\n")
			case statusCode == 503:
				fmt.Printf("⚙️  [Op %d] SERVICE UNAVAILABLE (503): %s - Server is temporarily down or overloaded\n", currentSearchIndex, link)
			case statusCode >= 400 && statusCode < 500:
				fmt.Printf("❌ [Op %d] CLIENT ERROR (%d): %s - Request was rejected by server\n", currentSearchIndex, statusCode, link)
			case statusCode >= 500:
				fmt.Printf("🔥 [Op %d] SERVER ERROR (%d): %s - Internal server problem\n", currentSearchIndex, statusCode, link)
			case statusCode >= 300 && statusCode < 400:
				if *Verbose {
					fmt.Printf("🔄 [Op %d] REDIRECT (%d): %s -> %s\n", currentSearchIndex, statusCode, link, resp.Request.URL.String())
				}
			default:
				if *Verbose {
					fmt.Printf("ℹ️  [Op %d] Response status for %s: %d\n", currentSearchIndex, link, statusCode)
				}
			}
		}

		// Skip processing if we got an error status
		if statusCode >= 400 {
			return
		}

		if *Verbose {
			fmt.Printf("[Op %d] Final URL after redirects: %s\n", currentSearchIndex, resp.Request.URL.String())
		}

		contentType := resp.Header.Get("Content-Type")
		
		// Handle gzip compression
		var reader io.Reader = resp.Body
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gzReader, err := gzip.NewReader(resp.Body)
			if err != nil {
				if !*Quiet {
					fmt.Printf("❌ [Op %d] Error creating gzip reader: %v\n", currentSearchIndex, err)
				}
				return
			}
			defer gzReader.Close()
			reader = gzReader
		}
		
		bodyBytes, _ := io.ReadAll(reader)
		
		if *Verbose && len(bodyBytes) < 500 {
			fmt.Printf("[Op %d] Response body preview: %s\n", currentSearchIndex, string(bodyBytes))
		}
		
		// Enhanced bot detection and captcha checking
		bodyStr := string(bodyBytes)
		
		// Check for various bot detection systems
		botDetected := false
		if strings.Contains(bodyStr, "sgcaptcha") || 
		   strings.Contains(bodyStr, "meta http-equiv=\"refresh\"") ||
		   strings.Contains(bodyStr, "cloudflare") && strings.Contains(bodyStr, "checking your browser") ||
		   strings.Contains(bodyStr, "DDoS protection by Cloudflare") ||
		   strings.Contains(bodyStr, "Please enable JavaScript and cookies") ||
		   strings.Contains(bodyStr, "Access denied") ||
		   strings.Contains(bodyStr, "security check") ||
		   strings.Contains(bodyStr, "verify you are human") ||
		   strings.Contains(bodyStr, "captcha") ||
		   strings.Contains(bodyStr, "blocked") && strings.Contains(bodyStr, "bot") ||
		   strings.Contains(bodyStr, "Incapsula") ||
		   strings.Contains(bodyStr, "PerimeterX") ||
		   strings.Contains(bodyStr, "Sucuri") {
			botDetected = true
		}
		
		if botDetected {
			mu.Lock()
			blockedCount++
			mu.Unlock()
			
			if !*Quiet {
				fmt.Printf("🤖 [Op %d] BOT PROTECTION DETECTED: %s\n", currentSearchIndex, link)
				
				// Identify specific protection systems
				if strings.Contains(bodyStr, "cloudflare") {
					fmt.Printf("   🛡️  Protection Type: Cloudflare Bot Management\n")
				} else if strings.Contains(bodyStr, "Incapsula") {
					fmt.Printf("   🛡️  Protection Type: Incapsula/Imperva\n")
				} else if strings.Contains(bodyStr, "PerimeterX") {
					fmt.Printf("   🛡️  Protection Type: PerimeterX\n")
				} else if strings.Contains(bodyStr, "Sucuri") {
					fmt.Printf("   🛡️  Protection Type: Sucuri Security\n")
				} else if strings.Contains(bodyStr, "sgcaptcha") {
					fmt.Printf("   🛡️  Protection Type: CAPTCHA Challenge\n")
				} else {
					fmt.Printf("   🛡️  Protection Type: Generic Anti-Bot System\n")
				}
				
				fmt.Printf("   💡 This website requires manual verification or has strict bot policies\n")
				fmt.Printf("   ⚠️  The crawler cannot bypass this protection automatically\n")
				
				// Try to extract redirect URL for manual verification
				if strings.Contains(bodyStr, "content=\"0;") {
					start := strings.Index(bodyStr, "content=\"0;") + 11
					end := strings.Index(bodyStr[start:], "\"")
					if end > 0 {
						redirectPath := bodyStr[start:start+end]
						redirectURL, err := url.Parse(link)
						if err == nil {
							redirectURL.Path = redirectPath
							fmt.Printf("   🔗 Manual verification URL: %s\n", redirectURL.String())
						}
					}
				}
			}
			return
		}
		
		// Additional check for empty or minimal content (often indicates blocking)
		if len(bodyBytes) < 100 {
			if !*Quiet {
				fmt.Printf("⚠️  [Op %d] MINIMAL CONTENT: %s - Received very little data (%d bytes)\n", currentSearchIndex, link, len(bodyBytes))
				fmt.Printf("   💡 This might indicate the request was blocked or the page is empty\n")
			}
			return
		}

		switch {
		case strings.Contains(contentType, "application/pdf"):
			if parser.ContainsLinkInPDF(bytes.NewReader(bodyBytes), target) {
				if *Verbose {
					fmt.Printf("✅ [Op %d] Found in PDF: %s\n", currentSearchIndex, link)
				}
				writeCSV(link, contentType, "PDF", target, base.String())
			}
		case strings.Contains(contentType, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"):
			if parser.ContainsLinkInDocx(bytes.NewReader(bodyBytes), target) {
				if *Verbose {
					fmt.Printf("✅ [Op %d] Found in DOCX: %s\n", currentSearchIndex, link)
				}
				writeCSV(link, contentType, "DOCX", target, base.String())
			}
		case strings.Contains(contentType, "text/html"):
			if bytes.Contains(bodyBytes, []byte(target)) {
				if *Verbose {
					fmt.Printf("✅ [Op %d] Found in HTML: %s\n", currentSearchIndex, link)
				}
				writeCSV(link, contentType, "HTML", target, base.String())
			}
			extractLinks(bodyBytes, link, base, target)
		}
	}()
}

func extractLinks(body []byte, pageURL string, base *url.URL, target string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		if !*Quiet {
			fmt.Printf("❌ [Op %d] HTML parse error: %s\n", currentSearchIndex, pageURL)
		}
		return
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					link := a.Val
					u, err := url.Parse(link)
					if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
						continue
					}
					next := base.ResolveReference(u).String()
					
					// Parse the resolved URL to check its host
					nextURL, err := url.Parse(next)
					if err != nil {
						continue
					}
					
					// Only crawl if the host matches the base host
					if nextURL.Host != base.Host {
						if *Verbose {
							fmt.Printf("⏭️  [Op %d] Skipping external link: %s\n", currentSearchIndex, next)
						}
						continue
					}
					
					time.Sleep(100 * time.Millisecond)
					crawl(next, base, target)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}