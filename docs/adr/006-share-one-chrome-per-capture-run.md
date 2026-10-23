# ADR-006: Share one Chrome per capture run, on chromedp v0.20.1
Date: 2026-10-23   Status: draft   Supersedes: 005

## Context
Page Capture and JSON Feed Capture built a new exec allocator per page, so
every page launched and tore down a whole Chrome. On
governor.virginia.gov/newsroom (12 pages, PDF, concurrency 5) that peaked at
20 Chrome processes and 5,492 MB summed RSS. Sharing one browser on
chromedp v0.16 stalled screenshots: v0.16 opened every tab in one window,
hidden tabs get no animation frames, and 6 pages PDF + PNG took 267s
against 18s with a browser per page.

## Decision
`newBrowser` (`internal/crawler/chrome.go`) starts one headless Chrome per
run, up front, and each page is a new tab in it. chromedp moves from v0.16.0
to v0.20.1, which opens each tab in its own window. The port touched only
`renderPage` and `newBrowser`, after `ee4e262` merged the two modes' copies
of the page-rendering code. On Unix, Chrome starts in its own process group
(`chrome_unix.go`), so the first Ctrl-C lets captures in flight finish.

## Consequences
+ 12 pages, PDF: peak Chrome processes 20 -> 4, summed RSS 5,492 MB ->
  2,108 MB (summed RSS counts shared memory more than once, so the real
  saving is smaller), wall time 31s -> 28s. A missing Chrome is reported
  once, not on every page.
+ 6 pages PDF + PNG back to 18s on v0.20 (17s PDF only), output matching the
  browser-per-page build. Ctrl-C at 12s: 6 PDFs, 0 errors, no Chrome left.
- A browser crash would fail every capture after it in the run. Since
  `5c472b3`, renderPage restarts Chrome when the browser itself dies and
  retries the page once (test: Chrome killed after the first PDF, 5 of 5
  written; 1 of 5 without the restart). A page that crashes Chrome every
  time costs one restart per attempt, then fails on its own.
- The Chrome tests in `chrome_test.go` skip with `-short` or when no Chrome
  is installed, so a machine without Chrome never exercises this path.

## Alternatives rejected
- Stay on v0.16 and create each tab with `newWindow` through
  `target.CreateTarget` plus `WithTargetID`: not tried, because the v0.20
  port fixed the stall directly and also cleared ADR-005's deferred
  migration.

## Pre-mortem
It is six months later and this was a mistake. Most likely reason: a page
hangs Chrome without crashing it. A crash is caught and restarted (see
above), but a hung browser keeps answering slowly or not at all, so every
page after it waits out the 180-second timeout and fails, and no restart
fires because the process is still alive.
Early warning sign to watch for: a capture run whose errors are all
timeouts and all start at one point, or a "🔁 Chrome stopped responding"
line repeating on every page.
