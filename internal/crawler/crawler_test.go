package crawler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestServer stands in for the mix of behavior real sites show a crawler:
// pages that work, pages that are gone, a CDN that rejects HEAD, and a
// firewall that refuses the client outright.
func newTestServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	// Rejects HEAD with 405 but serves GET. A HEAD-only checker calls this
	// broken; it is not.
	mux.HandleFunc("/head-hostile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	// Answers HEAD with 404 but GET with 200, which some CDNs really do.
	mux.HandleFunc("/head-lies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/waf", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	mux.HandleFunc("/ratelimited", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/unavailable", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/servererror", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	return httptest.NewServer(mux)
}

// setupBrokenLinkRun points the package globals at a throwaway CSV so
// checkLink can be exercised directly.
func setupBrokenLinkRun(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "results.csv")

	prevFile, prevConfig, prevStats := resultFile, config, stats
	t.Cleanup(func() { resultFile, config, stats = prevFile, prevConfig, prevStats })

	resultFile = path
	config = Config{Mode: ModeBrokenLinks}
	stats = Stats{}
	createCSV()
	return path
}

// readResults returns the CSV rows keyed by the URL column.
func readResults(t *testing.T, path string) map[string][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening results: %v", err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parsing results: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("results file has no header")
	}
	want := []string{"Result", "URL", "FoundOnPage", "StatusCode", "Error", "Timestamp"}
	if strings.Join(rows[0], ",") != strings.Join(want, ",") {
		t.Fatalf("header = %v, want %v", rows[0], want)
	}

	out := map[string][]string{}
	for _, r := range rows[1:] {
		out[r[1]] = r
	}
	return out
}

func TestCheckLinkClassification(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()
	path := setupBrokenLinkRun(t)

	paths := []string{"/ok", "/gone", "/head-hostile", "/head-lies", "/waf",
		"/ratelimited", "/unavailable", "/servererror"}
	for _, p := range paths {
		checkLink(srv.URL+p, srv.URL+"/index.html")
	}

	got := readResults(t, path)

	cases := []struct {
		path   string
		result string // "" means the link must not be reported at all
		status string
		why    string
	}{
		{"/ok", "", "", "a healthy page must not be reported"},
		{"/head-hostile", "", "", "405 on HEAD must fall back to GET, not be called broken"},
		{"/head-lies", "", "", "404 on HEAD must fall back to GET, not be called broken"},
		{"/gone", "BROKEN", "404", "a real 404 is a broken link"},
		{"/servererror", "BROKEN", "500", "a 500 is a broken link"},
		{"/waf", "BLOCKED", "403", "a firewall 403 is not a dead page"},
		{"/ratelimited", "BLOCKED", "429", "rate limiting is not a dead page"},
		{"/unavailable", "BLOCKED", "503", "503 is not a dead page"},
	}

	for _, c := range cases {
		row, reported := got[srv.URL+c.path]
		if c.result == "" {
			if reported {
				t.Errorf("%s was reported as %q: %s", c.path, row[0], c.why)
			}
			continue
		}
		if !reported {
			t.Errorf("%s was not reported at all, expected %s: %s", c.path, c.result, c.why)
			continue
		}
		if row[0] != c.result {
			t.Errorf("%s result = %q, want %q: %s", c.path, row[0], c.result, c.why)
		}
		if row[3] != c.status {
			t.Errorf("%s status = %q, want %q", c.path, row[3], c.status)
		}
	}
}

// A blocked link must not inflate the "matches found" count, which the final
// report presents as the number of problems discovered.
func TestBlockedLinksCountedSeparately(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()
	setupBrokenLinkRun(t)

	checkLink(srv.URL+"/waf", srv.URL+"/index.html")
	checkLink(srv.URL+"/gone", srv.URL+"/index.html")

	if stats.LinksBlocked != 1 {
		t.Errorf("LinksBlocked = %d, want 1", stats.LinksBlocked)
	}
	if stats.MatchesFound != 1 {
		t.Errorf("MatchesFound = %d, want 1 (the 404 only)", stats.MatchesFound)
	}
	if stats.LinksChecked != 2 {
		t.Errorf("LinksChecked = %d, want 2", stats.LinksChecked)
	}
}

// An unreachable host is a broken link, not a blocked one.
func TestUnreachableHostIsBroken(t *testing.T) {
	path := setupBrokenLinkRun(t)

	// Start a server just to claim a port, then stop it, so the address is
	// guaranteed to refuse instantly rather than hang.
	dead := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	checkLink(deadURL+"/dead", "http://example.com/index.html")

	got := readResults(t, path)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	for _, row := range got {
		if row[0] != "BROKEN" {
			t.Errorf("result = %q, want BROKEN", row[0])
		}
		if row[3] != "0" {
			t.Errorf("status = %q, want 0 for a transport error", row[3])
		}
	}
}

// A transport error must cost one attempt, not two. Retrying an unreachable
// host with GET doubles the wait on the slowest links for no benefit.
func TestTransportErrorIsNotRetried(t *testing.T) {
	setupBrokenLinkRun(t)

	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			methods = append(methods, r.Method)
			mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
		}))
	defer srv.Close()

	// A real 404 should be probed twice: HEAD, then GET.
	checkLink(srv.URL+"/gone", srv.URL+"/index.html")
	mu.Lock()
	got := append([]string(nil), methods...)
	mu.Unlock()

	want := []string{http.MethodHead, http.MethodGet}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("methods = %v, want %v", got, want)
	}
}

func TestBrowserHeadersLookLikeABrowser(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	BrowserHeaders(req, UserAgentFor(0))

	for _, h := range []string{"User-Agent", "Accept", "Accept-Language",
		"Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site"} {
		if req.Header.Get(h) == "" {
			t.Errorf("%s is empty; a Chrome user agent without it is a bot tell", h)
		}
	}
	// Setting this by hand disables Go's transparent decompression, which is
	// the bug that made pages scan as binary garbage.
	if got := req.Header.Get("Accept-Encoding"); got != "" {
		t.Errorf("Accept-Encoding = %q, must be left to the transport", got)
	}
}

func TestUserAgentsAreCurrent(t *testing.T) {
	if len(userAgents) == 0 {
		t.Fatal("no user agents defined")
	}
	for i, ua := range userAgents {
		if strings.Contains(ua, "Chrome/120.") || strings.Contains(ua, "Firefox/121.") {
			t.Errorf("userAgents[%d] is from 2023 and is itself a bot signal: %s", i, ua)
		}
	}
	// The rotation must wrap rather than panic on any attempt number.
	for i := -3; i < len(userAgents)+3; i++ {
		if UserAgentFor(i) == "" {
			t.Errorf("UserAgentFor(%d) returned empty", i)
		}
	}
}

// The transport must negotiate HTTP/2. Without ForceAttemptHTTP2 the client
// speaks HTTP/1.1 while claiming to be Chrome, which CDNs read as a bot.
func TestTransportAttemptsHTTP2(t *testing.T) {
	if !NewTransport().ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 is off; the client will fall back to HTTP/1.1")
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, r.Proto)
		}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	client := &http.Client{Transport: NewTransport()}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Proto != "HTTP/2.0" {
		t.Errorf("negotiated %s, want HTTP/2.0", resp.Proto)
	}
}

func TestIsBlockedStatus(t *testing.T) {
	blocked := []int{403, 429, 503}
	notBlocked := []int{200, 301, 400, 404, 410, 500, 502}

	for _, s := range blocked {
		if !isBlockedStatus(s) {
			t.Errorf("status %d should count as blocked", s)
		}
	}
	for _, s := range notBlocked {
		if isBlockedStatus(s) {
			t.Errorf("status %d should not count as blocked", s)
		}
	}
}
