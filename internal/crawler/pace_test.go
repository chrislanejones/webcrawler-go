package crawler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"30", 30 * time.Second, true},
		{"0", 0, true},
		{"Fri, 23 Oct 2026 12:00:45 GMT", 45 * time.Second, true},
		{"Fri, 23 Oct 2026 11:59:00 GMT", 0, true}, // already past
		{"", 0, false},
		{"-5", 0, false},
		{"soon", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRetryAfter(c.in, now)
		if got != c.want || ok != c.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestPacerSpacesRequests(t *testing.T) {
	t.Cleanup(func() { setRate(-1) })
	setRate(20) // one request every 50ms

	start := time.Now()
	for range 5 {
		waitTurn()
	}
	// Five turns are four intervals apart.
	if elapsed := time.Since(start); elapsed < 190*time.Millisecond {
		t.Errorf("5 requests at 20/s took %v, want at least 200ms", elapsed)
	}
}

func TestRetryAfterPausesEveryone(t *testing.T) {
	t.Cleanup(func() { setRate(-1) })
	setRate(-1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := doPaced(http.DefaultClient, req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// The next request, from any worker, waits out the second.
	start := time.Now()
	waitTurn()
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("request after Retry-After: 1 went out after %v, want about 1s", elapsed)
	}
}
