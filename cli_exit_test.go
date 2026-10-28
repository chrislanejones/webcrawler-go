package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIExitCodes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "webcrawler")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<p>needle</p>")
	}))
	defer s.Close()
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		code    int
		message string
	}{
		{"help", []string{"-h"}, 0, ""},
		{"invalid flags", []string{"-url", s.URL}, 2, "needs -terms"},
		{"invalid size", []string{"-url", s.URL, "-mode", "broken", "-max-body-mb", "0"}, 2, "-max-body-mb"},
		{"output failure", []string{"-url", s.URL, "-mode", "broken", "-out", filepath.Join(blocked, "results")}, 1, "create output folder"},
		{"sitemap failure", []string{"-url", s.URL, "-mode", "sitemap", "-max-pages", "1", "-sitemap-file", blocked + "/sitemap.xml"}, 1, "write sitemap"},
		{"success", []string{"-url", s.URL, "-terms", "needle", "-max-pages", "1"}, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, tc.args...)
			cmd.Dir = t.TempDir()
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != tc.code || !strings.Contains(stderr.String(), tc.message) {
				t.Fatalf("code=%d want=%d stderr=%s stdout=%s", code, tc.code, &stderr, out)
			}
			if tc.code == 1 && strings.Contains(string(out), "COMPLETE") {
				t.Fatalf("failed run printed completion: %s", out)
			}
		})
	}
}

func TestMaxBodyFlag(t *testing.T) {
	cfg, err := parseFlags([]string{"-url", "example.com", "-mode", "broken", "-max-body-mb", "64"}, io.Discard)
	if err != nil || cfg.MaxBodyBytes != 64<<20 {
		t.Fatalf("size=%d err=%v", cfg.MaxBodyBytes, err)
	}
	for _, value := range []string{"-1", "0", "8796093022208", "NaN"} {
		if _, err := parseFlags([]string{"-url", "example.com", "-mode", "broken", "-max-body-mb", value}, io.Discard); err == nil {
			t.Errorf("accepted invalid response size %q", value)
		}
	}
}
