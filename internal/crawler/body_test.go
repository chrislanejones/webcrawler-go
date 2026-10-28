package crawler

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResponseBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		length   int64
		tooLarge bool
	}{
		{"below limit", "abc", 3, false},
		{"exact limit", "abcd", 4, false},
		{"declared oversized", "abcde", 5, true},
		{"unknown length oversized", "abcde", -1, true},
		{"unknown length exact", "abcd", -1, false},
		{"understated length", "abcde", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.body)
			resp := &http.Response{Body: io.NopCloser(reader), ContentLength: tc.length}
			body, err := readResponseBody(resp, 4)
			if errors.Is(err, errBodyTooLarge) != tc.tooLarge {
				t.Fatalf("body=%q err=%v; want too large=%v", body, err, tc.tooLarge)
			}
			if tc.tooLarge {
				if body != nil {
					t.Fatal("truncated body must not be passed to a parser")
				}
				if tc.length > 4 && reader.Len() != len(tc.body) {
					t.Fatal("read a body already declared too large")
				}
			} else if err != nil || string(body) != tc.body {
				t.Fatalf("body=%q err=%v", body, err)
			}
		})
	}
}

func TestResponseLimitAppliesAfterGzipAndToChunkedBodies(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "chunked", true: "gzip"}[compressed], func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if compressed {
					w.Header().Set("Content-Encoding", "gzip")
					z := gzip.NewWriter(w)
					io.WriteString(z, strings.Repeat("x", 4096))
					z.Close()
				} else {
					w.(http.Flusher).Flush()
					io.WriteString(w, strings.Repeat("x", 4096))
				}
			}))
			defer s.Close()
			resp, err := s.Client().Get(s.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if body, err := readResponseBody(resp, 128); body != nil || !errors.Is(err, errBodyTooLarge) {
				t.Fatalf("body length=%d, err=%v", len(body), err)
			}
		})
	}
}

func TestOversizedPageIsReportedWithoutRetry(t *testing.T) {
	var hits atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			io.WriteString(w, "<a href='/large'>large</a><a href='/small'>small</a>")
		} else if r.URL.Path == "/large" {
			hits.Add(1)
			io.WriteString(w, strings.Repeat("needle ", 200))
		} else {
			io.WriteString(w, "<p>needle</p>")
		}
	}))
	defer s.Close()
	rows := runSearch(t, Config{StartURL: s.URL + "/", SearchTarget: "needle", MaxBodyBytes: 128, MaxRetries: 3})
	if hits.Load() != 1 {
		t.Fatalf("oversized page fetched %d times", hits.Load())
	}
	var unread, matched bool
	for _, row := range rows {
		if row[0] == s.URL+"/large" && strings.Contains(row[2], "NOT SEARCHED: response body exceeds") {
			unread = true
		}
		if row[0] == s.URL+"/small" && row[3] == "needle" {
			matched = true
		}
	}
	if !unread || !matched {
		t.Fatalf("unread=%v matched=%v rows=%v", unread, matched, rows)
	}
}

func TestFeedResponseSizeLimit(t *testing.T) {
	previous := config
	t.Cleanup(func() { config = previous })
	config.MaxBodyBytes = 32
	setRate(-1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[{\"headline\":\""+strings.Repeat("x", 100)+"\"}]")
	}))
	defer s.Close()
	if _, err := fetchJSONFeed(s.URL, JSONFeedOptions{}); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("expected feed size error, got %v", err)
	}
}

func TestSitemapOmitsOversizedPage(t *testing.T) {
	t.Chdir(t.TempDir())
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, strings.Repeat("x", 100))
	}))
	defer s.Close()
	err := Start(Config{StartURL: s.URL, Mode: ModeSitemap, MaxConcurrency: 1, RequestsPerSecond: -1, MaxBodyBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := sitemapURLs.Load(s.URL); exists {
		t.Fatal("oversized page included in sitemap")
	}
	if sitemapStats.ErrorCount != 1 {
		t.Fatalf("errors=%d", sitemapStats.ErrorCount)
	}
}
