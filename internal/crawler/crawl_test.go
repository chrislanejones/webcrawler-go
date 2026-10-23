package crawler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// siteServer is a small site with the link shapes that trip a crawler: skip
// links, fragments, a JavaScript template left in an href, an external link,
// and a section below /news/.
type siteServer struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newSiteServer(t *testing.T) *siteServer {
	t.Helper()
	s := &siteServer{hits: map[string]int{}}
	pages := map[string]string{
		"/": `<a href="#main">Skip</a>
			<a href="/a">A</a>
			<a href="/a#section">A again</a>
			<a href="'+ item.link +'">template</a>
			<a href="https://elsewhere.invalid/">external</a>
			<a href="/news/">News</a>`,
		"/a":        `<p>Leaked: <?php echo $secret; ?></p>`,
		"/news/":    `<a href="/news/one">One</a><a href="/">Home</a>`,
		"/news/one": `<p>Nothing to see</p><a href="/a">A</a>`,
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body>%s</body></html>", body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *siteServer) hitCount(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// runCrawl runs a real crawl in a temp directory and returns the rows of the
// results file matching pattern, header excluded.
func runCrawl(t *testing.T, cfg Config, pattern string) [][]string {
	t.Helper()
	t.Chdir(t.TempDir())

	if cfg.MaxConcurrency == 0 {
		cfg.MaxConcurrency = 2
	}
	cfg.RetryDelay = 10 * time.Millisecond
	if cfg.RequestsPerSecond == 0 {
		cfg.RequestsPerSecond = -1 // no pacing against a local server
	}
	Start(cfg)

	files, _ := filepath.Glob(pattern)
	if len(files) != 1 {
		t.Fatalf("want one results file, found %v", files)
	}
	f, err := os.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows[1:]
}

func runSearch(t *testing.T, cfg Config) [][]string {
	t.Helper()
	cfg.Mode = ModeSearchWord
	return runCrawl(t, cfg, "results-search-*.csv")
}

func TestCrawlFetchesEachPageOnce(t *testing.T) {
	s := newSiteServer(t)
	rows := runSearch(t, Config{StartURL: s.URL + "/", SearchTarget: "<?php"})

	for _, path := range []string{"/", "/a", "/news/", "/news/one"} {
		if n := s.hitCount(path); n != 1 {
			t.Errorf("%s fetched %d times, want 1", path, n)
		}
	}
	if n := s.hitCount("/'+ item.link +'"); n != 0 {
		t.Errorf("followed a JavaScript template href %d times", n)
	}
	if len(rows) != 1 || rows[0][0] != s.URL+"/a" {
		t.Errorf("want one match on /a, got %v", rows)
	}
}

func TestSameSite(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"www.labor.virginia.gov", "labor.virginia.gov", true},
		{"labor.virginia.gov", "WWW.Labor.Virginia.gov", true},
		{"www.labor.virginia.gov", "www.labor.virginia.gov", true},
		{"vita.virginia.gov", "www.virginia.gov", false},
		{"data.virginia.gov", "virginia.gov", false},
	}
	for _, c := range cases {
		if got := sameSite(c.a, c.b); got != c.want {
			t.Errorf("sameSite(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVisitedKeyDropsFragments(t *testing.T) {
	prev := config
	t.Cleanup(func() { config = prev })

	config = Config{}
	if getVisitedKey("https://x.gov/a#main") != getVisitedKey("https://x.gov/a") {
		t.Error("a fragment made the same page a new visit")
	}
	if getVisitedKey("https://x.gov/a?page=2") == getVisitedKey("https://x.gov/a") {
		t.Error("query was dropped although IgnoreQueryParams is off")
	}

	config = Config{IgnoreQueryParams: true}
	if getVisitedKey("https://x.gov/a?page=2#top") != getVisitedKey("https://x.gov/a") {
		t.Error("query was kept although IgnoreQueryParams is on")
	}
}

func TestPathFilterKeepsCrawlInSection(t *testing.T) {
	s := newSiteServer(t)
	runSearch(t, Config{StartURL: s.URL + "/news/", PathFilter: "/news/", SearchTarget: "<?php"})

	if n := s.hitCount("/news/one"); n != 1 {
		t.Errorf("/news/one fetched %d times, want 1", n)
	}
	for _, path := range []string{"/", "/a"} {
		if n := s.hitCount(path); n != 0 {
			t.Errorf("%s is outside /news/ but was fetched %d times", path, n)
		}
	}
}

func TestInPathFilter(t *testing.T) {
	prev := config
	t.Cleanup(func() { config = prev })

	config = Config{PathFilter: "/newsroom/"}
	for p, want := range map[string]bool{
		"/newsroom/":               true,
		"/newsroom":                true,
		"/newsroom/news-releases/": true,
		"/newsroom-archive/":       false,
		"/cabinet/":                false,
	} {
		if got := inPathFilter(p); got != want {
			t.Errorf("inPathFilter(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestUnreadPagesAreReportedNotSearched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><body><a href="/waf">waf</a><a href="/broken">500</a><a href="/gone">404</a></body></html>`)
		case "/waf":
			w.WriteHeader(http.StatusForbidden)
		case "/broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	rows := runSearch(t, Config{StartURL: srv.URL + "/", SearchTarget: "<?php", MaxRetries: 1})

	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[2]
	}
	for _, path := range []string{"/waf", "/broken"} {
		if !strings.HasPrefix(got[srv.URL+path], "NOT SEARCHED") {
			t.Errorf("%s: want a NOT SEARCHED row, got %q", path, got[srv.URL+path])
		}
	}
	if _, ok := got[srv.URL+"/gone"]; ok {
		t.Error("a 404 has nothing to search and should not be reported")
	}
}

func TestOversizedImagesMeasuredOnce(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	sizes := map[string]int{"/big.jpg": 600 << 10, "/small.png": 10 << 10, "/huge.jpg": 900 << 10}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.Method+" "+r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><body><img src="/big.jpg"><img src="/small.png"><a href="/two">two</a></body></html>`)
		case "/two":
			fmt.Fprint(w, `<html><body><img src="/big.jpg" srcset="/small.png 1x, /huge.jpg 2x"></body></html>`)
		default:
			n, ok := sizes[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", fmt.Sprint(n))
			if r.Method == http.MethodGet {
				w.Write(make([]byte, n))
			}
		}
	}))
	t.Cleanup(srv.Close)

	rows := runCrawl(t, Config{
		StartURL:           srv.URL + "/",
		Mode:               ModeOversizedImages,
		ImageSizeThreshold: 500 << 10,
	}, "results-oversized-images-*.csv")

	found := map[string]bool{}
	for _, r := range rows {
		found[r[0]+" on "+r[1]] = true
	}
	for _, want := range []string{
		srv.URL + "/big.jpg on " + srv.URL + "/",
		srv.URL + "/big.jpg on " + srv.URL + "/two",
		srv.URL + "/huge.jpg on " + srv.URL + "/two", // only in srcset
	} {
		if !found[want] {
			t.Errorf("missing row %q; got %v", want, rows)
		}
	}
	if len(rows) != 3 {
		t.Errorf("want 3 rows, got %d: %v", len(rows), rows)
	}

	mu.Lock()
	defer mu.Unlock()
	if n := hits["HEAD /big.jpg"]; n != 1 {
		t.Errorf("/big.jpg measured %d times, want once", n)
	}
	if n := hits["GET /big.jpg"]; n != 0 {
		t.Errorf("/big.jpg downloaded %d times although HEAD gave its size", n)
	}
}

func TestSearchSeveralTermsIgnoringCase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><body><a href="/short">s</a><a href="/error">e</a><p>clean</p></body></html>`)
		case "/short":
			fmt.Fprint(w, `<html><body><?= $title ?></body></html>`)
		case "/error":
			fmt.Fprint(w, `<html><body><b>FATAL ERROR</b>: Uncaught Error in /var/www/index.php on line 12</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	rows := runSearch(t, Config{
		StartURL:    srv.URL + "/",
		SearchTerms: SplitTerms("<?php | <?= | fatal error"),
		IgnoreCase:  true,
	})

	got := map[string]string{}
	for _, r := range rows {
		got[r[0]] = r[3]
	}
	want := map[string]string{
		srv.URL + "/short": "<?=",
		srv.URL + "/error": "fatal error",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d matches, got %v", len(want), rows)
	}
	for url, term := range want {
		if got[url] != term {
			t.Errorf("%s: matched %q, want %q", url, got[url], term)
		}
	}
}

func TestSplitTerms(t *testing.T) {
	got := SplitTerms(" <?php | <?= || Richmond, VA ")
	want := []string{"<?php", "<?=", "Richmond, VA"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("SplitTerms = %q, want %q", got, want)
	}
}

func TestPageLimitStopsTheCrawl(t *testing.T) {
	s := newSiteServer(t)
	runSearch(t, Config{StartURL: s.URL + "/", SearchTarget: "<?php", MaxPages: 2})

	total := 0
	for _, p := range []string{"/", "/a", "/news/", "/news/one"} {
		total += s.hitCount(p)
	}
	if total != 2 {
		t.Errorf("fetched %d pages with a limit of 2", total)
	}
	if !strings.Contains(StopReason(), "page limit") {
		t.Errorf("StopReason = %q, want it to mention the page limit", StopReason())
	}
}

func TestCancelledCrawlStillWritesResults(t *testing.T) {
	s := newSiteServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Chdir(t.TempDir())
	StartContext(ctx, Config{StartURL: s.URL + "/", Mode: ModeSearchWord, SearchTarget: "<?php", MaxConcurrency: 1, RequestsPerSecond: -1})

	if files, _ := filepath.Glob("results-search-*.csv"); len(files) != 1 {
		t.Errorf("want the results file even after a stop, found %v", files)
	}
	if StopReason() != "stopped by the user" {
		t.Errorf("StopReason = %q", StopReason())
	}
}

func TestOutputDir(t *testing.T) {
	s := newSiteServer(t)
	t.Chdir(t.TempDir())
	Start(Config{StartURL: s.URL + "/", Mode: ModeSearchWord, SearchTarget: "<?php", MaxConcurrency: 1, RequestsPerSecond: -1, OutputDir: "out/oaa"})

	if files, _ := filepath.Glob("out/oaa/results-search-*.csv"); len(files) != 1 {
		t.Errorf("want the results file in out/oaa, found %v", files)
	}
}

func TestStartRedirectToAnotherHostIsFollowed(t *testing.T) {
	// The new site, reached only by redirect, links relatively.
	newSite := newSiteServer(t)
	oldSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, newSite.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	t.Cleanup(oldSite.Close)

	rows := runSearch(t, Config{StartURL: oldSite.URL + "/", SearchTarget: "<?php"})

	if n := newSite.hitCount("/news/one"); n != 1 {
		t.Errorf("/news/one on the redirect target fetched %d times, want 1", n)
	}
	if len(rows) != 1 {
		t.Errorf("want the match on the redirect target, got %v", rows)
	}
}
