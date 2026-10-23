# ADR-002: Verify TLS by default, fall back per host, and report it
Date: 2026-10-23   Status: draft

## Context
Every HTTP client set `InsecureSkipVerify: true`. It was added because some
state sites serve an incomplete chain (vaemergency.gov was missing the
DigiCert G2 intermediate, PARKING_LOT item 4), but it hid that finding from a
site auditor and accepted any certificate at all.

## Decision
`NewTransport` returns a `tlsFallback` round-tripper that verifies first. On
a `tls.CertificateVerificationError` it records the host and a reason
(incomplete or untrusted chain, wrong host name, expired), prints one
warning, counts it in `SSLErrors`, retries that request unverified, and uses
the unverified transport for that host for the rest of the run. Final
statistics list every failed host and why. There is no setting to turn the
fallback off.

## Consequences
+ Broken chains are now a reported finding. Live: vaemergency.gov ->
  vdem.virginia.gov reported "certificate chain is incomplete or not
  trusted" and the crawl continued; governor.virginia.gov verified with 0
  TLS errors.
+ Every other host gets real certificate checking.
- A host that fails once is trusted blindly for the rest of the run, so a
  real interception on that host is reported but not stopped.
- Two transports and a package-level `sync.Map` of failed hosts: state that
  lives for the whole process.

## Alternatives rejected
- Keep the global `InsecureSkipVerify`: hides the finding and accepts any
  certificate.
- Verify strictly with no fallback: the crawl would stop at sites a browser
  with the intermediate cached still reaches.

## Pre-mortem
It is six months later and this was a mistake. Most likely reason: the
fallback is automatic and the warning is one line in a long run, so people
read "crawl finished" and never the TLS list. The auditing win shrinks to a
log line, and the tool still talks unverified to any host with a bad cert.
Early warning sign to watch for: TLS problems listed in the final statistics
that nobody follows up on, or a request for a strict mode.
