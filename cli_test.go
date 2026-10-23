package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"webcrawler/internal/crawler"
)

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags(append(strings.Fields(
		"-url oaa.virginia.gov -mode word -ignore-case -path newsroom -entry /newsroom/news-releases/ -max-time 30m -rate 2 -concurrency 50",
	), "-terms", "php"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// -terms holds spaces and bars, so it cannot go through strings.Fields.
	cfg2, err := parseFlags([]string{"-url", "https://www.oaa.virginia.gov/", "-terms", "<?php | <?= | Fatal error"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name      string
		got, want any
	}{
		{"start URL gains a scheme", cfg.StartURL, "https://oaa.virginia.gov"},
		{"mode", cfg.Mode, crawler.ModeSearchWord},
		{"path filter is normalized", cfg.PathFilter, "/newsroom/"},
		{"entry resolves against the site", strings.Join(cfg.AltEntryPoints, ","), "https://oaa.virginia.gov/newsroom/news-releases/"},
		{"time limit", cfg.MaxDuration, 30 * time.Minute},
		{"rate", cfg.RequestsPerSecond, 2.0},
		{"concurrency is capped", cfg.MaxConcurrency, 20},
		{"ignore case", cfg.IgnoreCase, true},
		{"terms split on bars", strings.Join(cfg2.SearchTerms, "\n"), "<?php\n<?=\nFatal error"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestParseFlagsNeedsTermsForSearch(t *testing.T) {
	_, err := parseFlags([]string{"-url", "example.gov", "-mode", "word"}, io.Discard)
	if err == nil {
		t.Fatal("word mode with no -terms should be an error")
	}
}

func TestParseFlagsAllowsNoTermsOutsideSearch(t *testing.T) {
	if _, err := parseFlags([]string{"-url", "example.gov", "-mode", "broken"}, io.Discard); err != nil {
		t.Fatalf("broken mode needs no -terms: %v", err)
	}
}

func TestParseFlagsRejectsNonsense(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-url", "example.gov", "-mode", "everything"},
		{"-url", "example.gov", "-mode", "capture", "-format", "gif"},
		{"-url", "example.gov", "-mode", "feed"},
		{"-url", "example.gov", "stray"},
	} {
		if _, err := parseFlags(args, io.Discard); err == nil {
			t.Errorf("parseFlags(%q) should fail", args)
		}
	}
}

func TestRateZeroMeansNoLimit(t *testing.T) {
	cfg, err := parseFlags([]string{"-url", "example.gov", "-mode", "broken", "-rate", "0"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequestsPerSecond >= 0 {
		t.Errorf("-rate 0 gave RequestsPerSecond %v, want negative (no limit)", cfg.RequestsPerSecond)
	}
}
