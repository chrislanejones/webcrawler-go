package crawler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// needChrome skips a test when no Chrome is installed. GitHub's Ubuntu
// runners ship one, so CI runs these.
func needChrome(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("drives a real browser; skipped with -short")
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "google-chrome-stable"} {
		if _, err := exec.LookPath(name); err == nil {
			return
		}
	}
	t.Skip("no Chrome or Chromium installed")
}

// articleServer serves a few pages with enough body text for the settle wait
// to see a stable page quickly, and a JSON feed that lists them.
func articleServer(t *testing.T, n int) *httptest.Server {
	t.Helper()
	filler := strings.Repeat("The Commonwealth announced the program today. ", 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/feed.json":
			var items []string
			for i := range n {
				items = append(items, fmt.Sprintf(`{"headline": "Release %d", "link": "/news/%d", "date": "2026-10-%02d"}`, i, i, i+1))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
		case r.URL.Path == "/":
			var links strings.Builder
			for i := range n {
				fmt.Fprintf(&links, `<a href="/news/%d">Release %d</a> `, i, i)
			}
			fmt.Fprintf(w, "<html><body><h1>News</h1><p>%s</p>%s</body></html>", filler, links.String())
		case strings.HasPrefix(r.URL.Path, "/news/"):
			fmt.Fprintf(w, "<html><body><h1>Release %s</h1><p>%s</p></body></html>", strings.TrimPrefix(r.URL.Path, "/news/"), filler)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func countFiles(t *testing.T, pattern string) int {
	t.Helper()
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func TestPageCaptureWritesPDFAndPNG(t *testing.T) {
	needChrome(t)
	srv := articleServer(t, 2)
	t.Chdir(t.TempDir())

	start := time.Now()
	Start(Config{
		StartURL:          srv.URL + "/",
		Mode:              ModePDFCapture,
		CaptureFormat:     CaptureBoth,
		MaxConcurrency:    3,
		RequestsPerSecond: -1,
		// Page Capture guesses ?page=2..10 for listing pages; this test
		// server answers every one, so fold them into the page itself.
		IgnoreQueryParams: true,
	})

	// Home plus two articles, each as a PDF and a PNG.
	if n := countFiles(t, "page_captures_*/*.pdf"); n != 3 {
		t.Errorf("want 3 PDFs, got %d", n)
	}
	if n := countFiles(t, "page_captures_*/*.png"); n != 3 {
		t.Errorf("want 3 PNGs, got %d", n)
	}
	// Screenshots in a shared browser once stalled on hidden tabs,
	// turning seconds into minutes. Three pages in parallel should take
	// about one page's settle time.
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Errorf("capture took %v; screenshots may be stalling on hidden tabs", elapsed)
	}

	pngs, _ := filepath.Glob("page_captures_*/*.png")
	for _, p := range pngs {
		data, err := os.ReadFile(p)
		if err != nil || len(data) < 8 || string(data[1:4]) != "PNG" {
			t.Errorf("%s is not a PNG", p)
		}
	}
}

func TestFeedCaptureHonorsPageLimit(t *testing.T) {
	needChrome(t)
	srv := articleServer(t, 5)
	t.Chdir(t.TempDir())

	Start(Config{
		StartURL:          srv.URL + "/",
		Mode:              ModeJSONFeed,
		CaptureFormat:     CapturePDFOnly,
		MaxConcurrency:    3,
		RequestsPerSecond: -1,
		MaxPages:          2,
		JSONFeedOpts:      JSONFeedOptions{FeedURL: srv.URL + "/feed.json"},
	})

	// The limit trims the feed to two items, and those two are captured.
	// An earlier version stopped before the first one.
	if n := countFiles(t, "json_feed_captures_*/*.pdf"); n != 2 {
		t.Errorf("want 2 PDFs with a page limit of 2, got %d", n)
	}
	if !strings.Contains(StopReason(), "page limit") {
		t.Errorf("StopReason = %q", StopReason())
	}
}
