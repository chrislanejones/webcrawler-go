# ADR-005: Hold chromedp at v0.16.0 and defer the v0.17+ migration
Date: 2026-10-23   Status: superseded by 006

## Context
The 10-23-2026 dependency pass (`bfd169d`) moved Go to 1.26.10 and x/net to
v0.61.0 for 10 govulncheck advisories. chromedp drives page capture and JSON
feed capture. Every release from v0.17.0 on rewrites the API around
generics and breaks both.

## Decision
`github.com/chromedp/chromedp` stays at v0.16.0. Migrating to the generics
API is deferred as its own piece of work, not folded into a security patch.

## Consequences
+ Page capture and JSON feed capture keep working with no code change.
+ The security patch stayed small and reviewable.
- The gap grows: upstream is at v0.21.0 (10-11-2026), five minor releases
  ahead, and fixes there do not reach this tool.
- If an advisory lands against v0.16.0, the fix is the full migration, done
  under time pressure.

## Alternatives rejected
- Upgrade now: breaks page capture and JSON feed capture, and would have
  turned a security patch into an API migration.

## Pre-mortem
It is six months later and this was a mistake. Most likely reason: the
deferred migration never got scheduled, Chrome's DevTools protocol moved on,
and v0.16.0 started failing against current Chrome, so capture broke anyway
with the migration now larger.
Early warning sign to watch for: capture failures or protocol errors after a
Chrome update, or govulncheck flagging chromedp or cdproto.
