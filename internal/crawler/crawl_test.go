package crawler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// runSearch runs a real crawl in a temp directory and returns the rows of the
// results file, header excluded.
func runSearch(t *testing.T, cfg Config) [][]string {
	t.Helper()
	t.Chdir(t.TempDir())

	cfg.Mode = ModeSearchWord
	if cfg.MaxConcurrency == 0 {
		cfg.MaxConcurrency = 2
	}
	cfg.RetryDelay = 10 * time.Millisecond
	Start(cfg)

	files, _ := filepath.Glob("results-search-*.csv")
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
