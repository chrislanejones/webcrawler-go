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

## 6. No CI — LOW

`gofmt` had never been run on `internal/crawler`. A small workflow running
build, vet, gofmt and govulncheck would stop that drifting again.
