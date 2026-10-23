# ADR-004: Add a command-line flag interface alongside the wizard, with graceful stop
Date: 2026-10-23   Status: draft

## Context
The interactive wizard was the only way in, so a crawl could not be
scripted, scheduled or run over a list of sites. The 69-domain sweep needed
a pty harness typing keystrokes. Ctrl-C killed the process and the final
statistics were lost.

## Decision
With no arguments the program still starts the wizard. With arguments it
parses flags (`cli.go`, standard library `flag`) and runs straight through.
Every wizard setting has a flag with the same default; `-entry` (repeatable)
and `-out` are added. Ctrl-C or SIGTERM, `-max-time` or `-max-pages` stop
new pages being queued, let requests in flight finish, and still write the
results and statistics with a STOPPED EARLY line. A second Ctrl-C exits at
once.

## Consequences
+ Crawls can be scripted and scheduled. Live on oaa.virginia.gov, Ctrl-C
  after 8 seconds stopped cleanly at 16s with 45 pages, 26 PDFs parsed and
  11 matches kept.
+ Interrupted runs keep their results.
- Two front ends to keep in step: every new setting needs a wizard prompt
  and a flag with the same default.
- Graceful stop covers link, word, broken-link and image modes only.

## Alternatives rejected
- Flags only, dropping the wizard: the wizard stays the default for people
  who run it by hand. (Recorded as the choice made; no further rationale is
  in the commits.)

## Pre-mortem
It is six months later and this was a mistake. Most likely reason: the
wizard and the flags drift, with a default changed in one place and not the
other, so the same site crawled two ways gives two different results and
nobody can tell why.
Early warning sign to watch for: a setting reachable from only one of the
two, or a flag default that differs from the wizard's.
