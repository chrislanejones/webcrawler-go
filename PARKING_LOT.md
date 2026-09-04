# Parking Lot — webcrawler-go

Findings from the September 4, 2026 update pass. Items 1 through 3 are fixed.
Items 4 and 5 are still open.

---

## 1. HTTP/1.1 while claiming to be Chrome — FIXED

`internal/crawler/crawler.go`

Setting `TLSClientConfig` on a custom `http.Transport` turns off Go's
automatic HTTP/2. Every browser in the user-agent list speaks HTTP/2, so a
request that said "Chrome" and then negotiated HTTP/1.1 was a mismatch a CDN
could see for free.

All clients now come from `crawler.NewTransport()`, which sets
`ForceAttemptHTTP2`. Measured against six hosts that had refused the old
client:

| Host                  | Before   | After    |
| --------------------- | -------- | -------- |
| doe.virginia.gov      | 403      | **200**  |
| deq.virginia.gov      | 403      | **200**  |
| amtrak.com            | timeout  | **200**  |
| wric.com              | 403      | 403      |
| wavy.com              | 403      | 403      |
| wfxrtv.com            | 403      | 403      |

Three of six became reachable, with no regressions. The three Nexstar
stations refuse both clients and need a real browser, which is what the new
BLOCKED category is for.

## 2. Broken Link mode reported firewalls as broken links — FIXED

`internal/crawler/crawler.go` (`checkLink`)

Three problems, all fixed:

- **403 was treated as broken.** 403, 429 and 503 now record as BLOCKED,
  meaning the page could not be verified, not that it is missing.
- **HEAD with no fallback.** Falls back to GET when HEAD is not 2xx, so a CDN
  answering HEAD with 405 no longer invents a dead link.
- **No session, no browser headers.** It built its own bare client, so it
  never saw the cookies the crawler had established. It now uses the shared
  client and the same header block as everything else.

On the governor.virginia.gov newsroom, 247 pages and 953 links, this removes
**21 false broken links**: 18 alive pages that answered 403, and 3 where
HEAD failed but GET returned 200.

The broken-links CSV gained a `Result` column, so the schema is now
`Result, URL, FoundOnPage, StatusCode, Error, Timestamp`. Anything parsing
the old five-column file needs updating.

## 3. Stale user agents and scattered headers — FIXED

The list claimed Chrome 120 and Firefox 121, both from December 2023.
`main.go` kept a second copy of the list plus a hardcoded Chrome 120 in
`quickTest`, so the connection tester that decides whether a site is blocked
was using the worst headers in the program.

There is now one list, refreshed to Chrome 150-152 and Firefox 154, and one
`BrowserHeaders` helper that adds the `Sec-Fetch-*` set real Chrome always
sends. Sitemap and JSON feed fetches use it too. The JSON feed path sends
the CORS variant, since it is a script fetch rather than a navigation.

---

## Still open

## 4. TLS verification is off everywhere, permanently — MEDIUM

`internal/crawler/crawler.go` — `InsecureSkipVerify: true`

This solves a real problem. vaemergency.gov currently serves an incomplete
chain, missing the DigiCert G2 intermediate, so a verifying client fails
where Chrome succeeds. But disabling verification everywhere hides that
finding instead of reporting it, and accepts any MITM.

Better: verify by default, and on `x509.UnknownAuthorityError` retry once
with verification off and mark the row "TLS chain incomplete". That turns a
silently swallowed problem into a reportable one, which is what a site
auditor wants.

## 5. 429 ignores Retry-After — LOW

The backoff is exponential but blind. When a server says exactly how long to
wait, use it.

## 6. No CI — FIXED

`gofmt` had never been run on `internal/crawler`. `.github/workflows/ci.yml`
now runs gofmt, vet, build, `go test -race` and govulncheck on every push and
pull request. It reads the Go version from `go.mod`, so it follows the
project forward rather than pinning a version that goes stale.

## 7. Dead links cost two timeouts each — FIXED

Found while writing the tests: a link on an unreachable host was probed with
HEAD, waited out the full client timeout, then probed again with GET and
waited it out a second time. One dead host cost 60 seconds.

A transport error is now reported after the first attempt. DNS failures,
refused connections and timeouts fail identically for GET, so the retry only
bought a second wait. Link checks also got their own 15 second deadline,
separate from the 30 second page-fetch timeout, since verifying a link is
cheaper than fetching a page to crawl.

## Tests

`internal/crawler/crawler_test.go` covers the classification logic against a
local server that imitates the awkward cases: a CDN that rejects HEAD with
405, one that answers HEAD with 404 but GET with 200, a firewall 403, a 429,
a 503, a real 404 and a 500. It also asserts the transport negotiates HTTP/2
and that the user agents are not the 2023 ones again.

Run them with `go test ./...`. The suite takes well under a second.
