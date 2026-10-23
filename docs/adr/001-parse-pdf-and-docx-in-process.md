# ADR-001: Parse PDF and DOCX in-process with ledongthuc/pdf and the standard library
Date: 2026-10-23   Status: draft

## Context
PDF search shelled out to the `pdfcpu` CLI through one fixed temp file
(`assets/tmp/tmp.pdf`). Without pdfcpu installed every PDF counted as "no
match", and concurrent workers overwrote each other's file. Link URIs live in
annotations, which were never read. DOCX search via `baliance.com/gooxml`
(unmaintained since 2019) matched one run at a time and never read the
relationships part, so split phrases and hyperlinks were missed.

## Decision
PDF: `github.com/ledongthuc/pdf`, in-process, page text plus link annotation
URIs, pinned to `6c8c28e0e8a0` (09-07-2026). DOCX: standard library
`archive/zip` + `encoding/xml` (body, headers, footers, footnotes, field
codes, relationship targets). `gooxml` is dropped. `PDFText` wraps the
library in `recover` and stops at 1,000 pages. A document that cannot be
parsed is reported as NOT SEARCHED, not as no match.

Why the pin: the repo has no tagged releases (0 tags), and its newest master
commit (`5e46805`, 10-20-2026) was 3 days old. The rule applied: pin the
newest master commit at least 2 weeks old, which is `6c8c28e`.

## Consequences
+ No external binary. Real PDFs from osig and oaa.virginia.gov: 0 of 4
  readable before (pdfcpu not installed), 4 of 4 after, 177 link URIs from
  the OAA annual report.
+ One less dependency for DOCX; phrases across runs and hyperlinks now match.
- Untagged dependency that panics on malformed input. `recover` contains it,
  but every bump is a judgment call with no release notes to read.
- The pin predates upstream fixes merged on 10-20-2026 (hex-string EOF,
  bfrange carry in CMap decode, RC4 object key length). Those bugs ship here
  until the pin moves. Measured on the 28 PDFs the sweep could not read:
  the pinned commit reads 0 of them whole, `5e46805` reads 17. PARKING_LOT
  item 18 moves the pin on or after 11-03-2026.
- PDFs past page 1,000 are only partly searched.

## Alternatives rejected
- `digitorus/pdf` v0.3.0: returned raw glyph IDs for Identity-H (CID) fonts,
  which Word's Save as PDF produces (in commit `37a8965`). Session note, not
  in the commit: in a sample of 4 real PDFs it garbled both from
  osig.virginia.gov; ledongthuc decoded all 4.
- `pdfcpu` as a library: weak at text extraction (session note).
- `go-pdfium`: needs cgo or WebAssembly (wazero) to call PDFium.

## Pre-mortem
It is six months later and this was a mistake. Most likely reason: the
CMap and font-decoding bugs fixed upstream after the pin, or new ones, garble
text on a class of state PDFs. `GetPlainText` returns no error, so those
PDFs pass as "searched, no match", which is the silent failure this change
set out to remove.
Early warning sign to watch for: a PDF that a human can search in a viewer
shows no match, or the NOT SEARCHED count climbs on a site that never had any.
