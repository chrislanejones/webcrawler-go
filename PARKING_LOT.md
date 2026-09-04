# Parking Lot — webcrawler-go

Findings from the September 4, 2026 update pass. Nothing here is committed
behavior yet. Ranked by how much it costs you today.

---

## 1. The client is HTTP/1.1 only while claiming to be Chrome — HIGH

`internal/crawler/crawler.go:181`

Setting `TLSClientConfig` on a custom `http.Transport` without
`ForceAttemptHTTP2: true` turns off Go's automatic HTTP/2. Verified against
cloudflare.com:

| Transport                     | Negotiated |
| ----------------------------- | ---------- |
| Current crawler transport     | HTTP/1.1   |
| Same, plus ForceAttemptHTTP2  | HTTP/2.0   |
| Go's DefaultClient            | HTTP/2.0   |

Every browser in the user-agent list speaks HTTP/2. A request that says
"Chrome 120" and then negotiates HTTP/1.1 is a mismatch a CDN can see for
free, and Cloudflare, Akamai and Fastly all look at it. This is the single
most likely reason a site blocks the crawler but not the browser.

Fix is one line: add `ForceAttemptHTTP2: true` to the Transport.

## 2. Broken Link mode reports firewalls as broken links — HIGH

`internal/crawler/crawler.go:836` (`checkLink`)

Three separate problems in one function, all of which manufacture false
"broken link" rows:

- **403 is treated as broken.** `resp.StatusCode >= 400` catches it. A 403
  from a WAF means "I don't like your client", not "the page is gone".
- **HEAD with no GET fallback.** Plenty of CDNs answer HEAD with 405, or
  with a 404 they would not give a GET.
- **No session, no browser headers, 10s timeout.** It builds its own bare
  `http.Client`, so it does not share the cookie jar the crawler just spent
  three phases establishing, and it sends a User-Agent with no `Accept` or
  `Accept-Language`. Real Chrome always sends those.

Measured against the governor.virginia.gov newsroom, 247 pages and 953
distinct links:

| False positive source            | Count |
| -------------------------------- | ----- |
| Alive but answered 403 to script  | 18    |
| HEAD failed, GET returned 200     | 3     |
| **Total bogus "broken links"**    | **21** |

Suggested behavior: fall back to GET when HEAD is not 2xx, reuse
`httpClient` so cookies carry, send the same headers as the crawler, raise
the timeout to match, and split the report into "broken" and "blocked" so a
403 lands in its own column instead of being called a 404.

## 3. User agents are three years stale — MEDIUM

`internal/crawler/crawler.go:167`

Chrome 120 and Firefox 121 shipped in December 2023. Claiming a browser
that old in late 2026 is itself a signal. Refresh the list, and consider
pulling the version from a constant so it is a one-line bump next time.

## 4. TLS verification is off everywhere, permanently — MEDIUM

`internal/crawler/crawler.go:182` — `InsecureSkipVerify: true`

This does solve a real problem. vaemergency.gov currently serves an
incomplete chain, missing the DigiCert G2 intermediate, so a verifying
client fails where Chrome succeeds. But blanket-disabling verification
hides that finding instead of reporting it, and accepts any MITM.

Better: verify by default, and on `x509.UnknownAuthorityError` retry once
with verification off and mark the row "TLS chain incomplete". That turns a
silently swallowed problem into a reportable one, which is what a site
auditor actually wants.

## 5. 429 ignores Retry-After — LOW

`internal/crawler/crawler.go:670`

The backoff is exponential but blind. When a server tells you exactly how
long to wait, use it.

## 6. Formatting and hygiene — DONE

`internal/crawler` had never been through gofmt. Fixed in the update commit.
Worth a CI check so it does not drift again.
