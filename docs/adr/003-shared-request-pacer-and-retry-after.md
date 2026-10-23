# ADR-003: Pace every HTTP request through one shared limiter, with a crawl-wide Retry-After pause
Date: 2026-10-23   Status: draft

## Context
Politeness was a 50ms sleep per link found (30ms in sitemap), taken inside
the worker that parsed the page while it held its concurrency slot. A page
with 150 links tied up a worker for 7.5 seconds whether or not the links
were new, which is most of why crawls ran at about 2 pages a second, and it
never bounded the request rate itself (PARKING_LOT item 5).

## Decision
Every plain HTTP request (page fetches, link checks, image checks, sitemap
and JSON feed fetches) goes through `doPaced`. One pacer, shared by all
workers, spaces requests at `Config.RequestsPerSecond`, default 5 (`-rate`;
0 or less turns it off). A 429 or 503 with `Retry-After`, in seconds or as an
HTTP date, pauses the whole crawl, capped at 2 minutes, and a pause can only
be extended, never shortened. The per-link sleeps are removed.

## Consequences
+ Concurrency and politeness are separate: raising concurrency overlaps slow
  pages without hitting the site harder.
+ A rate limit backs off every worker, not only the one that was told.
- One budget for all hosts: in Broken Links mode, checks against external
  sites spend the same 5/s as the site being crawled.
- Package-level pacer state, so one crawl per process.
- Headless-browser loads (chromedp capture and feed paths) do not go
  through the pacer.

## Alternatives rejected
- Keep the per-link sleep: held concurrency slots and did not bound the rate.

## Pre-mortem
It is six months later and this was a mistake. Most likely reason: 5/s is
too fast for some small state hosts behind a WAF that bans without sending
`Retry-After`, so the crawl gets blocked instead of slowed, and nothing
backs off because the pause only fires on a header.
Early warning sign to watch for: the BLOCKED count rising mid-crawl on sites
that used to finish clean, with 403s and no 429s.
