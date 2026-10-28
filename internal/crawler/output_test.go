package crawler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCSVOpenAndFlushFailures(t *testing.T) {
	if err := writeCSVRow(t.TempDir(), true, []string{"header"}); err == nil {
		t.Fatal("creating a CSV over a directory must fail")
	}
	if err := writeCSVRow(filepath.Join(t.TempDir(), "missing.csv"), false, []string{"row"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("append to missing CSV: %v", err)
	}
	// /dev/full accepts open but fails on write, exercising csv.Writer.Flush.
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full unavailable")
	}
	if err := writeCSVRow("/dev/full", false, []string{"buffered row"}); err == nil {
		t.Fatal("buffered flush failure was ignored")
	}
}

func TestCrawlOutputFailureStopsAndReturnsError(t *testing.T) {
	t.Cleanup(resetOutputError)
	dir := t.TempDir()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The header is saved before the first request. Make the next append
		// fail deterministically, even when tests run as root.
		files, err := filepath.Glob(filepath.Join(dir, "*.csv"))
		if err != nil || len(files) != 1 {
			t.Errorf("CSV not created: %v %v", files, err)
		} else {
			if err := os.Remove(files[0]); err != nil {
				t.Error(err)
			}
			if err := os.Mkdir(files[0], 0o755); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<p>needle</p>")
	}))
	defer s.Close()
	err := Start(Config{StartURL: s.URL, Mode: ModeSearchWord, SearchTarget: "needle", MaxConcurrency: 1, RequestsPerSecond: -1, OutputDir: dir})
	if err == nil || !strings.Contains(err.Error(), "CSV") {
		t.Fatalf("expected CSV failure, got %v", err)
	}
	if StopReason() != "output failed" {
		t.Fatalf("stop reason=%q", StopReason())
	}
	if stats.MatchesFound != 0 {
		t.Fatal("failed write counted as saved match")
	}
	if runCtx.Err() != context.Canceled {
		t.Fatal("crawl was not cancelled")
	}

	// A failed run must not poison the next run.
	s2 := newSiteServer(t)
	rows := runSearch(t, Config{StartURL: s2.URL + "/", SearchTarget: "<?php"})
	if len(rows) == 0 {
		t.Fatal("subsequent run did not save results")
	}
}

func TestStartReportsOutputDirectoryFailureInEveryMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for mode := ModeSearchLink; mode <= ModeJSONFeed; mode++ {
		err := Start(Config{StartURL: "https://example.invalid", Mode: mode, MaxConcurrency: 1, OutputDir: filepath.Join(path, "out")})
		if err == nil {
			t.Fatalf("mode %v ignored output directory failure", mode)
		}
	}
}

func TestSitemapWriteFailureReturnsError(t *testing.T) {
	s := newSiteServer(t)
	err := Start(Config{StartURL: s.URL + "/", Mode: ModeSitemap, MaxConcurrency: 1, MaxPages: 1, RequestsPerSecond: -1, SitemapOpts: SitemapOptions{Filename: t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "write sitemap") {
		t.Fatalf("expected sitemap write error, got %v", err)
	}
}

func TestFeedCSVReturnsWriteErrors(t *testing.T) {
	previous := jsonFeedCSVFile
	t.Cleanup(func() { jsonFeedCSVFile = previous })
	jsonFeedCSVFile = t.TempDir()
	if err := createJSONFeedCSV(); err == nil {
		t.Fatal("feed CSV create error ignored")
	}
	if err := writeJSONFeedCSV(FeedItem{Headline: "test"}, "https://example.invalid"); err == nil {
		t.Fatal("feed CSV append error ignored")
	}
}
