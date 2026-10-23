# Parking Lot — webcrawler-go

Items 1 through 7 came from the September 4, 2026 update pass. Items 8 on
came from the October 23, 2026 PHP sweep of 69 virginia.gov domains and the
v3.0 modernization that followed. Open items are at the bottom.

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

## 4. TLS verification is off everywhere, permanently — FIXED (357d164)

`internal/crawler/crawler.go` — `InsecureSkipVerify: true`

This solves a real problem. vaemergency.gov currently serves an incomplete
chain, missing the DigiCert G2 intermediate, so a verifying client fails
where Chrome succeeds. But disabling verification everywhere hides that
finding instead of reporting it, and accepts any MITM.

Now verifies by default. A host whose certificate fails is recorded with the
reason, warned about once, read without verification for the rest of the
run, and listed in the final report. vaemergency.gov (lands on
vdem.virginia.gov) reports "certificate chain is incomplete or not trusted".

## 5. 429 ignores Retry-After — FIXED (169330e)

A 429 or 503 with Retry-After pauses the whole crawl, capped at 2 minutes.

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

## 8. Bot detection trips on the word "cloudflare" in normal pages — FIXED (95b5676)

`internal/crawler/crawler.go` (`detectBotProtection`)

Found October 23, 2026 while searching the Virginia secretary sites for
`<?php`. labor.virginia.gov loads Font Awesome from
`cdnjs.cloudflare.com`. The bot check is a plain substring match over the
whole body, so the homepage counted as a challenge page, its links were
never followed, and the retry passes failed the same way. The site was
never searched: 4 pages checked, 0 HTML pages scanned.

The same list has "captcha", "access denied" and "security check", which
show up on ordinary pages (a contact form with reCAPTCHA, an FOIA page).
The same day's sweep of 69 virginia.gov domains hit it on 14 sites, including
vita.virginia.gov (highlight.js from cdnjs) and www.virginia.gov. Each one
stopped at the homepage, and the run reported "0 matches".

The crawler now uses the challenge-pattern
detector Sitemap mode already had, so there is one detector instead of two.
A regression test uses the labor and vita markup, and it fails on the old
code. After the fix, all 14 sites crawled normally with 0 blocked, and vita
went from 4 pages to 1,300.

## 9. Path filter is ignored outside Page Capture and Sitemap — FIXED (b16e5e0)

`internal/crawler/crawler.go` (`extractInternalLinks`)

The wizard says "Will only crawl pages under /newsroom/", but only
`pdfcapture.go` and `sitemap.go` read `PathFilter`. In Find Link, Find
Word, Broken Links and Oversized Images the whole host is crawled.
governor.virginia.gov from `/newsroom` with the filter on: 636 pages, the
same as the whole-site run (635).

The LAUNCH CONFIG box also leaves the path filter out, though the README
shows a "🌲 Path filter" row there.

## 10. Search mode does not record which pages were blocked — FIXED (a28f7a5)

Find Word/Link writes only matches to the CSV. A blocked page shows up as
a count in the stats and nowhere else, so "0 matches" cannot be told
apart from "never read". Now written as `NOT SEARCHED` rows with the reason,
for blocked pages, pages that failed every retry, and unparseable documents.

## 11. One case-sensitive search term per run — FIXED (ee06417)

`processSearchMode` is `bytes.Contains` on the raw body. Hunting for leaked
PHP means one full crawl per pattern (`<?php`, `<?PHP`, `<?=`, "Fatal
error"), each one hitting the server again. Terms now separate with `|`
(commas appear inside ordinary phrases), with a match-case option.

## 12. PDF search never ran, and DOCX missed phrases and links — FIXED (37a8965, 15334ec)

PDF search shelled out to the `pdfcpu` CLI through one shared temp file.
Without pdfcpu installed every PDF passed as "no match", and concurrent
workers overwrote each other's file. DOCX matched one run at a time and never
read hyperlink targets. Both now parse in-process (`ledongthuc/pdf`, standard
library zip/xml) and include link URLs. A PDF with one damaged page still has
its other pages searched. `gooxml` is gone.

## 13. Pages fetched twice, www treated as another site — FIXED (7af542f, b2e04b1)

`#main` skip links made every page a second fetch and a duplicate match. The
bare domain and `www.` are now one site, links resolve against the URL a
response came from after redirects, and a start page that redirects to a
different site (cdo → odga) moves the crawl there.

## 14. Images re-downloaded on every page — FIXED (aa89eb3)

Each image is measured once per crawl, HEAD first, with the shared client.
`srcset`, `data-src` and `<picture>` sources are checked.

## 15. Politeness held worker slots — FIXED (169330e)

A 50ms sleep per link, inside the worker that held a concurrency slot, is
replaced by one shared pacer (5 requests a second by default, `-rate`).

## 16. No way to script a run — FIXED (2acfe8f)

Command-line flags for every wizard setting, `-entry` for a back-door start
page, `-max-pages`, `-max-time`, `-out`, and a graceful Ctrl-C that still
writes the results and statistics.

## 17. Reachable vulnerabilities in the toolchain — FIXED (bfd169d)

govulncheck found 10 at go1.26.8 and x/net v0.58.0. Toolchain is now
go1.26.10 and x/net v0.61.0; govulncheck reports none.

---

## Still open

## 18. Move the PDF library pin forward — MEDIUM, due 11-03-2026

`ledongthuc/pdf` is pinned to `6c8c28e0e8a0` (09-07-2026), the newest
master commit at least two weeks old. Upstream `5e46805` (10-20-2026)
merges three fixes, including the RC4 key length for encrypted PDFs.

Of 28 PDFs the sweep could not read, the pinned commit reads 0 whole and
the newer one reads 17. Most are "no copy/print" PDFs encrypted with an
empty password. Move the pin on or after 11-03-2026 and rerun
`TestPDFText*`.

## 19. chromedp is stuck at v0.16.0 — MEDIUM

Every release from v0.17.0 rewrites the API around generics. Page Capture
(`pdfcapture.go`) and JSON Feed Capture (`jsonfeed.go`) need porting, about
1,400 lines. No reachable vulnerability today. See docs/adr/005.

## 20. Limits and pacing do not cover the browser modes — LOW

`-max-pages`, `-max-time`, Ctrl-C and the shared pacer apply to Find Link,
Find Word, Broken Links and Oversized Images. Page Capture, Sitemap and JSON
Feed run their own loops; Page Capture still has its own `c` + Enter.

## 21. Go 1.27 — LOW, a decision

Go 1.27.3 is out. The September pass chose to stay on 1.26 and this pass
kept that, patching to 1.26.10. Moving is a `go.mod` change plus a test run.

## 22. Scanned PDFs have no text — NOTE

PDFs that are page images (`/CCITTFaxDecode`, `/DCTDecode` only) read as
empty. Searching them would need OCR, which is out of scope.

## Tests

`internal/crawler/crawler_test.go` covers link classification against a local
server that imitates the awkward cases. `crawl_test.go` runs real crawls:
fetch-once, path filter, NOT SEARCHED rows, image measuring, several terms,
page and time limits, redirects to another host. `pace_test.go` covers the
pacer and Retry-After, `internal/parser/parser_test.go` the PDF and DOCX
parsers, and `cli_test.go` the flags.

Run them with `go test -race ./...`. The suite takes about 3 seconds.
