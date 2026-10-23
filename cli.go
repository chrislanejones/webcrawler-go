package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"webcrawler/internal/crawler"
)

func main() {
	// Ctrl-C ends the crawl gracefully: no new pages are queued, requests
	// already out finish, and the results and statistics are still written.
	// A second Ctrl-C exits at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	if len(os.Args) == 1 {
		runWizard(ctx)
		return
	}

	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	if err == flag.ErrHelp {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "webcrawler:", err)
		os.Exit(2)
	}
	crawler.StartContext(ctx, cfg)
}

// modeNames maps the -mode flag to a crawl mode.
var modeNames = map[string]crawler.SearchMode{
	"link":    crawler.ModeSearchLink,
	"word":    crawler.ModeSearchWord,
	"broken":  crawler.ModeBrokenLinks,
	"images":  crawler.ModeOversizedImages,
	"capture": crawler.ModePDFCapture,
	"sitemap": crawler.ModeSitemap,
	"feed":    crawler.ModeJSONFeed,
}

var formatNames = map[string]crawler.CaptureFormat{
	"pdf":       crawler.CapturePDFOnly,
	"images":    crawler.CaptureImagesOnly,
	"both":      crawler.CaptureBoth,
	"cmyk-pdf":  crawler.CaptureCMYKPDF,
	"cmyk-tiff": crawler.CaptureCMYKTIFF,
}

// stringList is a flag that can be given more than once.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ", ") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

const usageText = `Usage:
  webcrawler                 start the interactive wizard
  webcrawler -url URL [flags]

Examples:
  # Look for leaked PHP across a site, in one pass, at most 30 minutes
  webcrawler -url https://www.oaa.virginia.gov/ -mode word \
    -terms '<?php | <?= | Fatal error' -ignore-case -max-time 30m

  # Broken links in one section, entering through a page that is not blocked
  webcrawler -url https://www.governor.virginia.gov/ -mode broken \
    -path /newsroom/ -entry /newsroom/news-releases/

  # Sitemap
  webcrawler -url https://www.example.gov/ -mode sitemap -sitemap-file example.xml

Flags:
`

// parseFlags turns command-line arguments into a crawl config. Every setting
// the wizard asks for has a flag, with the same defaults.
func parseFlags(args []string, errOut io.Writer) (crawler.Config, error) {
	fs := flag.NewFlagSet("webcrawler", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprint(errOut, usageText)
		fs.PrintDefaults()
	}

	var (
		site        = fs.String("url", "", "site to crawl (required)")
		mode        = fs.String("mode", "word", "link, word, broken, images, capture, sitemap or feed")
		terms       = fs.String("terms", "", "what to search for in link and word mode; separate several with |")
		ignoreCase  = fs.Bool("ignore-case", false, "match search terms regardless of letter case")
		path        = fs.String("path", "", "only crawl pages under this path, e.g. /newsroom/")
		concurrency = fs.Int("concurrency", 5, "requests in flight at once, at most 20")
		retries     = fs.Int("retries", 3, "retries per page")
		rate        = fs.Float64("rate", crawler.DefaultRequestsPerSecond, "requests per second, shared by all workers; 0 for no limit")
		ignoreQuery = fs.Bool("ignore-query", false, "treat page.html?a=1 and page.html?b=2 as the same page")
		maxPages    = fs.Int("max-pages", 0, "stop after this many pages; 0 for no limit")
		maxTime     = fs.Duration("max-time", 0, "stop after this long, e.g. 30m; 0 for no limit")
		out         = fs.String("out", "", "folder for the results CSV (default: current folder)")
		thresholdKB = fs.Int64("threshold-kb", 500, "images mode: flag images larger than this")
		format      = fs.String("format", "both", "capture and feed mode: pdf, images, both, cmyk-pdf or cmyk-tiff")
		feedURL     = fs.String("feed", "", "feed mode: URL of the JSON feed")
		tag         = fs.String("tag", "", "feed mode: only capture items with this tag")
		sitemapFile = fs.String("sitemap-file", "sitemap.xml", "sitemap mode: output file")
		changeFreq  = fs.String("changefreq", "weekly", "sitemap mode: always, hourly, daily, weekly, monthly, yearly or never")
		priority    = fs.Float64("priority", 0.5, "sitemap mode: priority from 0.0 to 1.0")
		lastMod     = fs.Bool("lastmod", false, "sitemap mode: include Last-Modified dates from the server")
		entries     stringList
	)
	fs.Var(&entries, "entry", "start from this page instead of the homepage; a path or URL, may repeat")

	if err := fs.Parse(args); err != nil {
		return crawler.Config{}, err
	}
	if fs.NArg() > 0 {
		return crawler.Config{}, fmt.Errorf("unexpected argument %q (every setting is a flag, like -url)", fs.Arg(0))
	}

	start, err := normalizeSite(*site)
	if err != nil {
		return crawler.Config{}, err
	}

	m, ok := modeNames[*mode]
	if !ok {
		return crawler.Config{}, fmt.Errorf("unknown -mode %q", *mode)
	}
	f, ok := formatNames[*format]
	if !ok {
		return crawler.Config{}, fmt.Errorf("unknown -format %q", *format)
	}

	cfg := crawler.Config{
		StartURL:           start,
		Mode:               m,
		MaxConcurrency:     min(max(*concurrency, 1), 20),
		ImageSizeThreshold: *thresholdKB * 1024,
		MaxRetries:         max(*retries, 0),
		RetryDelay:         2 * time.Second,
		RetryBlockedPages:  true,
		BlockedRetryPasses: 3,
		CaptureFormat:      f,
		IgnoreQueryParams:  *ignoreQuery,
		RequestsPerSecond:  *rate,
		MaxPages:           *maxPages,
		MaxDuration:        *maxTime,
		OutputDir:          *out,
		IgnoreCase:         *ignoreCase,
		SitemapOpts: crawler.SitemapOptions{
			Filename:       *sitemapFile,
			ChangeFreq:     *changeFreq,
			Priority:       *priority,
			IncludeLastMod: *lastMod,
		},
		JSONFeedOpts: crawler.JSONFeedOptions{
			FeedURL:   *feedURL,
			TagFilter: *tag,
		},
	}
	// -rate 0 means "no limit" on the command line; the crawler spells
	// that as a negative rate, since its zero means "use the default".
	if *rate == 0 {
		cfg.RequestsPerSecond = -1
	}

	if *path != "" {
		p := *path
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		cfg.PathFilter = p
	}

	if m == crawler.ModeSearchLink || m == crawler.ModeSearchWord {
		cfg.SearchTerms = crawler.SplitTerms(*terms)
		if len(cfg.SearchTerms) == 0 {
			return crawler.Config{}, fmt.Errorf("-mode %s needs -terms", *mode)
		}
		cfg.SearchTarget = strings.Join(cfg.SearchTerms, " | ")
	}
	if m == crawler.ModeJSONFeed && cfg.JSONFeedOpts.FeedURL == "" {
		return crawler.Config{}, fmt.Errorf("-mode feed needs -feed")
	}

	base, _ := url.Parse(start)
	for _, e := range entries {
		ref, err := url.Parse(e)
		if err != nil {
			return crawler.Config{}, fmt.Errorf("bad -entry %q: %v", e, err)
		}
		cfg.AltEntryPoints = append(cfg.AltEntryPoints, base.ResolveReference(ref).String())
	}

	return cfg, nil
}

// normalizeSite accepts "example.gov" as well as "https://example.gov/".
func normalizeSite(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("-url is required (run with -h for help)")
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("-url %q is not a web address", s)
	}
	return u.String(), nil
}
