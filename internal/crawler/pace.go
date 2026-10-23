package crawler

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultRequestsPerSecond is how fast a crawl talks to a site unless told
// otherwise. It is a ceiling shared by every worker, so raising concurrency
// makes slow pages overlap without making the crawl hit harder.
const DefaultRequestsPerSecond = 5

// maxRetryAfter caps how long one Retry-After header can stall the crawl.
const maxRetryAfter = 2 * time.Minute

var (
	pacerMu    sync.Mutex
	nextSlot   time.Time
	interval   time.Duration
	pauseUntil atomic.Int64 // unix nanoseconds; zero means no pause
)

// setRate configures the shared pacer. Zero or less means no limit.
func setRate(perSecond float64) {
	pacerMu.Lock()
	defer pacerMu.Unlock()
	nextSlot = time.Time{}
	pauseUntil.Store(0)
	if perSecond <= 0 {
		interval = 0
		return
	}
	interval = time.Duration(float64(time.Second) / perSecond)
}

// waitTurn blocks until this request may go out: after any pause a server
// asked for, and no sooner than one interval after the previous request.
func waitTurn() {
	if until := pauseUntil.Load(); until != 0 {
		if d := time.Until(time.Unix(0, until)); d > 0 {
			time.Sleep(d)
		}
	}

	pacerMu.Lock()
	if interval == 0 {
		pacerMu.Unlock()
		return
	}
	now := time.Now()
	if nextSlot.Before(now) {
		nextSlot = now
	}
	wait := nextSlot.Sub(now)
	nextSlot = nextSlot.Add(interval)
	pacerMu.Unlock()

	if wait > 0 {
		time.Sleep(wait)
	}
}

// honorRetryAfter pauses the whole crawl when a 429 or 503 says how long to
// wait. A rate limit applies to the client, not to one URL, so every worker
// backs off, not just the one that was told.
func honorRetryAfter(resp *http.Response) {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return
	}
	d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	if !ok {
		return
	}
	d = min(d, maxRetryAfter)
	until := time.Now().Add(d).UnixNano()
	// Only ever extend a pause, never shorten one another worker set.
	for {
		cur := pauseUntil.Load()
		if cur >= until || pauseUntil.CompareAndSwap(cur, until) {
			return
		}
	}
}

// doPaced sends a request through the shared pacer and notes any
// Retry-After in the answer. Every plain HTTP request in the crawler goes
// through here, so the rate limit covers page fetches and link checks alike.
func doPaced(c *http.Client, req *http.Request) (*http.Response, error) {
	waitTurn()
	resp, err := c.Do(req)
	if err == nil {
		honorRetryAfter(resp)
	}
	return resp, err
}

// parseRetryAfter reads either form the header allows: a number of seconds,
// or an HTTP date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}
