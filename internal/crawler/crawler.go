package crawler

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/http"
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
	httpClient  = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	Verbose = flag.Bool("verbose", false, "Enable verbose output")
	Quiet   = flag.Bool("quiet", false, "Suppress non-error output")
)

func Start(startURL, target string, concurrency int) {
	flag.Parse()
	sema = make(chan struct{}, concurrency)

	base, err := url.Parse(startURL)
	if err != nil {
		panic(err)
	}

	createCSV()
	crawl(startURL, base, target)
	wg.Wait()

	if !*Quiet {
		fmt.Printf("✅ Done. Total checked: %d, Matches: %d, Time: %s\n", checkCount, matchCount, time.Since(startTime).Truncate(time.Second))
	}
}

func createCSV() {
	f, _ := os.Create("results.csv")
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"URL", "ContentType", "FoundIn"})
}

func writeCSV(link, contentType, foundIn string) {
	csvMu.Lock()
	defer csvMu.Unlock()

	matchCount++

	f, _ := os.OpenFile("results.csv", os.O_APPEND|os.O_WRONLY, 0644)
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

		resp, err := httpClient.Get(link)
		if err != nil {
			if !*Quiet {
				fmt.Println("Error fetching:", link, err)
			}
			return
		}
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		bodyBytes, _ := io.ReadAll(resp.Body)

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