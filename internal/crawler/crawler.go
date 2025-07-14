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
	visited     = make(map[string]bool)
	mu          sync.Mutex
	wg          sync.WaitGroup
	sema        chan struct{}
	csvMu       sync.Mutex
	checkCount  int
	matchCount  int
	startTime   = time.Now()
	httpClient  *http.Client
	resultFile  string
	Verbose = flag.Bool("verbose", false, "Enable verbose output")
	Quiet   = flag.Bool("quiet", false, "Suppress non-error output")
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

func Start(startURL, target string, concurrency int) {
	flag.Parse()
	sema = make(chan struct{}, concurrency)

	base, err := url.Parse(startURL)
	if err != nil {
		panic(err)
	}

	// Find the next available result file number
	fileNum := 1
	for {
		resultFile = fmt.Sprintf("results-%d.csv", fileNum)
		if _, err := os.Stat(resultFile); os.IsNotExist(err) {
			break
		}
		fileNum++
	}

	createCSV()
	crawl(startURL, base, target)
	wg.Wait()

	if !*Quiet {
		fmt.Printf("✅ Done. Total checked: %d, Matches: %d, Time: %s\n", checkCount, matchCount, time.Since(startTime).Truncate(time.Second))
		fmt.Printf("Results saved to: %s\n", resultFile)
	}
}

func createCSV() {
	f, _ := os.Create(resultFile)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"URL", "ContentType", "FoundIn"})
}

func writeCSV(link, contentType, foundIn string) {
	csvMu.Lock()
	defer csvMu.Unlock()

	matchCount++

	f, _ := os.OpenFile(resultFile, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{link, contentType, foundIn})
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
			fmt.Printf("🔍 Checking: %s\n", link)
			if count%20 == 0 {
				fmt.Printf("📊 Checked %d pages (Elapsed: %s) Matches: %d\n", count, elapsed.Truncate(time.Second), matchCount)
			}
		}

		req, err := http.NewRequest("GET", link, nil)
		if err != nil {
			if !*Quiet {
				fmt.Println("Error creating request:", link, err)
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
			if !*Quiet {
				fmt.Println("Error fetching:", link, err)
			}
			return
		}
		defer resp.Body.Close()

		if *Verbose {
			fmt.Printf("Response status for %s: %s\n", link, resp.Status)
			fmt.Printf("Final URL after redirects: %s\n", resp.Request.URL.String())
		}

		contentType := resp.Header.Get("Content-Type")
		
		// Handle gzip compression
		var reader io.Reader = resp.Body
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gzReader, err := gzip.NewReader(resp.Body)
			if err != nil {
				if !*Quiet {
					fmt.Println("Error creating gzip reader:", err)
				}
				return
			}
			defer gzReader.Close()
			reader = gzReader
		}
		
		bodyBytes, _ := io.ReadAll(reader)
		
		if *Verbose && len(bodyBytes) < 500 {
			fmt.Printf("Response body preview: %s\n", string(bodyBytes))
		}
		
		// Check if this is a captcha page
		bodyStr := string(bodyBytes)
		if strings.Contains(bodyStr, "sgcaptcha") || strings.Contains(bodyStr, "meta http-equiv=\"refresh\"") {
			if !*Quiet {
				fmt.Printf("⚠️  Captcha/redirect page detected for %s. The site requires manual verification.\n", link)
			}
			// Try to extract the redirect URL
			if strings.Contains(bodyStr, "content=\"0;") {
				start := strings.Index(bodyStr, "content=\"0;") + 11
				end := strings.Index(bodyStr[start:], "\"")
				if end > 0 {
					redirectPath := bodyStr[start:start+end]
					redirectURL, err := url.Parse(link)
					if err == nil {
						redirectURL.Path = redirectPath
						if !*Quiet {
							fmt.Printf("  Redirect URL found: %s\n", redirectURL.String())
							fmt.Println("  This appears to be a captcha protection. The crawler cannot bypass this automatically.")
						}
					}
				}
			}
			return
		}

		switch {
		case strings.Contains(contentType, "application/pdf"):
			if parser.ContainsLinkInPDF(bytes.NewReader(bodyBytes), target) {
				if *Verbose {
					fmt.Println("✅ Found in PDF:", link)
				}
				writeCSV(link, contentType, "PDF")
			}
		case strings.Contains(contentType, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"):
			if parser.ContainsLinkInDocx(bytes.NewReader(bodyBytes), target) {
				if *Verbose {
					fmt.Println("✅ Found in DOCX:", link)
				}
				writeCSV(link, contentType, "DOCX")
			}
		case strings.Contains(contentType, "text/html"):
			if bytes.Contains(bodyBytes, []byte(target)) {
				if *Verbose {
					fmt.Println("✅ Found in HTML:", link)
				}
				writeCSV(link, contentType, "HTML")
			}
			extractLinks(bodyBytes, link, base, target)
		}
	}()
}

func extractLinks(body []byte, pageURL string, base *url.URL, target string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		if !*Quiet {
			fmt.Println("HTML parse error:", pageURL)
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
							fmt.Printf("⏭️  Skipping external link: %s\n", next)
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