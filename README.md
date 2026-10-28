# 🕷️ Web Crawler - Cloudflare Buster Edition

![Golang Web Crawler Banner with Spider](Golang-Web-Crawler-Banner.jpg)
A Go web crawler with an interactive terminal wizard and a command line for scripted runs. It works on sites behind bot protection, and it reads HTML, PDF, and DOCX content rather than just HTML.

![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)
![License](https://img.shields.io/badge/License-MIT-green.svg)

---

## ✨ Features

### 🎯 Seven Modes

| Mode                     | Description                                                                |
| ------------------------ | -------------------------------------------------------------------------- |
| **🔗 Find Link**         | Search for specific URLs/links across HTML pages, PDFs, and Word documents |
| **📝 Find Word/Phrase**  | Search for any text string across all supported content types              |
| **💔 Broken Link Check** | Scan entire site for 404s, timeouts, and connection errors                 |
| **🖼️ Oversized Images**  | Find images exceeding a specified file size threshold                      |
| **📄 Page Capture**      | Generate PDFs, screenshots, or CMYK files for every page on the site       |
| **🗺️ XML Sitemap**       | Generate a standards-compliant XML sitemap by crawling the entire site     |
| **📡 JSON Feed Capture** | Capture pages listed in a JSON feed, for sites that build listings in JS   |

### 🔎 What Gets Searched

Find Link and Find Word read every page, PDF and Word file the crawl reaches:

| Content  | What is searched                                                               |
| -------- | ------------------------------------------------------------------------------ |
| **HTML** | The raw page source, so markup like `<?php` or an inline script counts          |
| **PDF**  | The text on every page, plus the URL behind every link                          |
| **DOCX** | Body, headers, footers, footnotes and comments, plus the URL behind every link |

Search for several terms in one crawl by separating them with `|`:

```
<?php | <?= | .php</b> on line
```

Each term found on a page gets its own row in the results. Turn off **Match letter case** to catch `<?PHP` and `<?php` with the same term.

A page the crawler could not read is listed as `NOT SEARCHED` with the reason, so a page with no row in the results was read and did not match.

### 🌲 Path Filtering (Crawl Subsections)

Crawl only a specific section of a website by including the path in your URL:

```
🌐 What site do you want to check?
   (Tip: Include a path like /newsroom/ to only crawl that section)
   → https://www.example.gov/newsroom/news-releases

   🌲 Detected path: /newsroom/news-releases/
   📍 Only crawl pages under this path? (Y/n): y
   ✓ Will only crawl pages under /newsroom/news-releases/
```

This is useful for:

- Crawling only a blog, newsroom, or documentation section
- Avoiding irrelevant pages on large sites
- Faster, more focused crawls

**Smart Archive Detection:** For news/press release sections, the crawler automatically generates year/month archive URLs (e.g., `/newsroom/news-releases/2025/january/`) to discover all articles even when the listing page uses JavaScript pagination.

### 📄 Page Capture Options

| Format                | Output          | Requirements         |
| --------------------- | --------------- | -------------------- |
| **PDF only**          | `.pdf`          | Chrome/Chromium      |
| **Images only**       | `.png`          | Chrome/Chromium      |
| **Both PDF + Images** | `.pdf` + `.png` | Chrome/Chromium      |
| **CMYK PDF**          | `_cmyk.pdf`     | Chrome + Ghostscript |
| **CMYK TIFF**         | `_cmyk.tiff`    | Chrome + ImageMagick |

### 🗺️ XML Sitemap Generation

Generate a standards-compliant XML sitemap for any website:

| Option               | Description                                                                   |
| -------------------- | ----------------------------------------------------------------------------- |
| **Filename**         | Custom output filename (default: `sitemap.xml`)                               |
| **Change Frequency** | How often pages change: always, hourly, daily, weekly, monthly, yearly, never |
| **Priority**         | Page priority from 0.0 to 1.0 (default: 0.5)                                  |
| **Last Modified**    | Optionally include `<lastmod>` dates from server headers                      |

The generated sitemap follows the [sitemaps.org protocol](https://www.sitemaps.org/protocol.html) and is compatible with all major search engines.

### 📡 JSON Feed Capture

Some sites ship an empty listing page and build the list of articles in the browser from a JSON endpoint. A normal crawl of those pages finds nothing, because there are no links in the HTML to follow.

This mode skips the listing page and reads the feed directly:

```
📡 Enter the JSON feed URL
   Direct URL to the JSON endpoint (e.g., /newsroom/feed.json)
   → https://www.example.gov/newsroom/news-releases/newsfeed.php

🏷️  Filter by tag (optional)
   Only capture items containing this tag
   → news

📄 What format do you want to capture?
   → 📑🖼️  Both PDF + Images
```

The crawler reads every item in the feed, applies the tag filter if you set one, and captures each linked page in the format you picked.

Feed items are read with these field names by default. They cover the common shape of a CMS news feed:

| Field           | Default key  | Holds                       |
| --------------- | ------------ | --------------------------- |
| `LinkField`     | `link`       | URL of the article          |
| `HeadlineField` | `headline`   | Article title               |
| `DateField`     | `date`       | Display date                |
| `BriefField`    | `brief`      | Summary or teaser text      |
| `TagsField`     | `tags`       | Tags used by the tag filter |

To find the feed for a site, open the listing page, look at the page source or the network tab, and find the URL the page fetches its articles from.

### 🛡️ Cloudflare Bypass Strategies

The crawler employs multiple techniques to handle bot protection:

- **HTTP/2**: Negotiates HTTP/2 like a real browser. A client that sends a Chrome user agent and then speaks HTTP/1.1 is a mismatch CDNs use to spot bots
- **Full browser headers**: Sends the `Sec-Fetch-*` set alongside `Accept` and `Accept-Language`, so requests match the browser they claim to be
- **Alternative Entry Points**: Automatically tests 17+ common pages (`/about`, `/contact`, `/sitemap.xml`, etc.) when the main page is blocked
- **Custom Entry Point**: Specify your own "back door" URL, in the wizard or with `-entry`
- **Multi-Phase Crawling**: Start from working pages, then retry blocked pages with established session cookies
- **User Agent Rotation**: Cycles through 5 different browser signatures
- **Session Persistence**: Maintains cookies across requests
- **One Shared Rate Limit**: 5 requests a second by default across every worker, so raising concurrency overlaps slow pages without hitting the site harder
- **Retry-After**: When a server answers 429 or 503 and says how long to wait, the whole crawl waits that long, up to 2 minutes

### 📊 Comprehensive Statistics

Real-time and final statistics include:

- Pages checked, matches found, errors, blocked pages
- Content breakdown (HTML, PDF, DOCX, images, links)
- Network stats (bytes downloaded, retries, blocked count)
- HTTP status code distribution (2xx, 3xx, 4xx, 5xx)
- Connection error categorization (timeouts, DNS, SSL, refused)
- Performance metrics (pages/second, avg download speed, avg page size)
- Cloudflare bypass stats (retried, recovered, still blocked, recovery rate)

---

## 🚀 Quick Start

### Prerequisites

- Go 1.26 or higher
- Chrome or Chromium (for page capture mode)
- Ghostscript (optional, for CMYK PDF output)
- ImageMagick (optional, for CMYK TIFF output)

### Installation

```bash
# Clone the project
git clone https://github.com/chrislanejones/webcrawler-go.git
# or: git clone https://codeberg.org/chrislanejones/webcrawler-go.git
cd webcrawler-go

# Download dependencies
go mod download

# Install Chrome/Chromium (required for page capture mode)
# Ubuntu/Debian:
sudo apt install chromium-browser
# Or Google Chrome:
wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
sudo dpkg -i google-chrome-stable_current_amd64.deb

# Optional: Install Ghostscript (for CMYK PDF)
sudo apt install ghostscript

# Optional: Install ImageMagick (for CMYK TIFF)
sudo apt install imagemagick
```

### Running

```bash
go run .
```

With no arguments, the interactive wizard walks you through the settings.

Give it flags and it runs straight through, which is what you want for a list of sites or a scheduled job:

```bash
go run . -url https://www.example.gov/ -mode word \
  -terms '<?php | <?= | .php</b> on line' -ignore-case -max-time 30m
```

Run `go run . -h` for every flag, or see **Command Line** below.

To build and run a reusable executable:

```bash
go build -o webcrawler .
./webcrawler
```

For a bounded broken-link scan, saving the CSV in `./results`:

```bash
./webcrawler -url https://example.com -mode broken \
  -max-pages 100 -max-time 5m -out ./results
```

---

## 📖 Usage Guide

### Step-by-Step Wizard

```
╔═══════════════════════════════════════════════════════════════════╗
║                   🕷️  Web Crawler Wizard  🕷️                       ║
║                              v3.0                                 ║
╚═══════════════════════════════════════════════════════════════════╝

🌐 What site do you want to check?
   (Tip: Include a path like /newsroom/ to only crawl that section)
   → example.com

🔍 Testing connection to https://example.com...
   🔄 Attempt 1/3 ✅ OK (200) - 245ms latency

📋 What should I check the site for?

   ┌─────────────────────────────────────────────────────────┐
   │  1. 🔗 Find a link on site (HTML, Word, PDF)            │
   │  2. 📝 Find a word/phrase on site (HTML, Word, PDF)     │
   │  3. 💔 Search for broken links                          │
   │  4. 🖼️  Search for oversized images                     │
   │  5. 📄 Generate PDF/Image for every page                │
   │  6. 🗺️  Generate XML sitemap                            │
   │  7. 📡 Capture pages from JSON feed                     │
   └─────────────────────────────────────────────────────────┘

   Enter choice (1-7): 2

📝 Enter the word or phrase to search for:
   → privacy policy

⚡ Max concurrent requests (default 5, max 20): 10

🔄 Max retries per page (default 3): 3

🐢 Requests per second (default 5): 5
```

### ⌨️ Command Line

Every wizard setting has a flag with the same default.

| Flag             | Default       | What it does                                                    |
| ---------------- | ------------- | --------------------------------------------------------------- |
| `-url`           | (required)    | Site to crawl. `example.gov` works; `https://` is added         |
| `-mode`          | `word`        | `link`, `word`, `broken`, `images`, `capture`, `sitemap`, `feed` |
| `-terms`         |               | What to search for in `link` and `word` mode. Separate with `\|` |
| `-ignore-case`   | off           | Match terms regardless of letter case                           |
| `-path`          |               | Only crawl pages under this path, e.g. `/newsroom/`             |
| `-entry`         |               | Start from this page instead of the homepage. May repeat        |
| `-concurrency`   | `5`           | Requests in flight at once, at most 20                          |
| `-rate`          | `5`           | Requests per second, shared by all workers. `0` for no limit    |
| `-retries`       | `3`           | Retries per page                                                |
| `-ignore-query`  | off           | Treat `page.html?a=1` and `page.html?b=2` as the same page      |
| `-max-body-mb`   | `32`          | Maximum decoded HTTP response size in MiB; must be positive    |
| `-max-pages`     | `0`           | Stop after this many pages. `0` for no limit                    |
| `-max-time`      | `0`           | Stop after this long, e.g. `30m`. `0` for no limit              |
| `-out`           | current folder | Folder for the results CSV                                     |
| `-threshold-kb`  | `500`         | `images` mode: flag images larger than this                     |
| `-format`        | `both`        | `capture` and `feed` mode: `pdf`, `images`, `both`, `cmyk-pdf`, `cmyk-tiff` |
| `-feed`, `-tag`  |               | `feed` mode: the JSON feed URL, and an optional tag filter      |
| `-sitemap-file`, `-changefreq`, `-priority`, `-lastmod` | | `sitemap` mode options                 |

Check a list of sites, eight at a time, each in its own folder:

```bash
xargs -P 8 -I{} webcrawler -url {} -mode word -terms '<?php' -max-time 25m -out results/{} < sites.txt
```

### Response size limits and failures

HTTP page, PDF/DOCX document, sitemap-page, and JSON feed downloads are limited to **32 MiB per response**, including after gzip decompression. Larger search documents are recorded as `NOT SEARCHED` with the reason and are not retried. A partial document is never searched. Sitemap pages that exceed the limit are omitted with an error; an oversized JSON feed fails the run.

Raise the limit for sites with large documents:

```bash
go run . -url https://example.com -mode word -terms "privacy" -max-body-mb 64
```

The wizard uses the same 32 MiB default. This limits HTTP bodies buffered in memory; it does not cap Chrome's page rendering, generated captures, document-parser memory, or image measurements streamed without buffering.

CSV creation, writing, flushing, and closing errors are checked. Output failures stop new work, let active workers finish, and report an error while retaining files already saved. Capture output and sitemap write failures are also reported.

Exit codes for the built executable:

- **0:** Completed, or stopped gracefully by a page/time limit or the user.
- **1:** Startup, feed-fetch, or output failure.
- **2:** Invalid command-line arguments.

Individual crawl findings such as broken links, blocked pages, or oversized documents remain in the results/statistics and do not by themselves produce a nonzero exit code. `go run` may wrap the executable's exit code; use `go build -o webcrawler .` and `./webcrawler` when a script needs the exact code.

### ⏹️ Stopping Early

Press **Ctrl-C**, or set `-max-time` or `-max-pages`, and the crawl stops taking on new pages, lets the requests already out finish, and still writes the results file and the final statistics, with a line saying why it stopped. Press Ctrl-C a second time to quit at once.

This works in every mode. In Page Capture and JSON Feed Capture, `c` + Enter does the same thing as Ctrl-C, and the pages Chrome is already rendering are finished and saved.

### Path Filtering Example

To crawl only a specific section of a site, include the path in the URL:

```
🌐 What site do you want to check?
   (Tip: Include a path like /newsroom/ to only crawl that section)
   → https://www.governor.virginia.gov/newsroom/news-releases

   🌲 Detected path: /newsroom/news-releases/
   📍 Only crawl pages under this path? (Y/n): y
   ✓ Will only crawl pages under /newsroom/news-releases/

┌─────────────────── LAUNCH CONFIG ───────────────────┐
│  🌐 Target:       https://www.governor.virginia.go... │
│  🌲 Path filter:  /newsroom/news-releases/            │
│  📊 Mode:         Page Capture                        │
│  ⚡ Concurrency:  20                                  │
│  🔄 Max retries:  3                                   │
└─────────────────────────────────────────────────────┘
```

The crawler will only visit pages whose URL path starts with `/newsroom/news-releases/`, skipping all other sections of the site.

### Page Capture Mode (Option 5)

When you select option 5, you'll see a sub-menu for output format:

```
📄 What format do you want to capture?

   ┌─────────────────────────────────────────────────────────┐
   │  a. 📑 PDF only                                         │
   │  b. 🖼️  Images only (PNG)                                │
   │  c. 📑🖼️  Both PDF + Images                              │
   │  d. 🎨 CMYK PDF (for print) *                            │
   │  e. 🎨 CMYK TIFF (for InDesign) *                        │
   └─────────────────────────────────────────────────────────┘
   * Requires Ghostscript (d) or ImageMagick (e) installed

   Enter choice (a/b/c/d/e): c
   📑🖼️  Will generate both PDFs and PNG screenshots
   📁 Output folder: ./page_captures/

┌─────────────────── PAGE CAPTURE STARTING ──────────────────┐
│  🎯 Target: https://example.com/newsroom/                  │
│  🌲 Path:   /newsroom/                                     │
│  📁 Output: page_captures_2024-01-15_14-30-00              │
│  📋 Format: PDF + Images                                   │
├────────────────────────────────────────────────────────────┤
│  💡 Press 'c' + Enter to cancel and save current progress  │
└────────────────────────────────────────────────────────────┘
```

**Tip:** Press `c` + Enter at any time to stop crawling and keep the files captured so far.

### Sitemap Generation Mode (Option 6)

When you select option 6, you can configure the sitemap output:

```
🗺️  Sitemap Generation Options

   📄 Output filename (default: sitemap.xml): my-sitemap.xml

   📅 Default change frequency:
      1. always
      2. hourly
      3. daily
      4. weekly (default)
      5. monthly
      6. yearly
      7. never
   Enter choice (1-7): 4
   ✓ Change frequency: weekly

   ⭐ Default priority (0.0-1.0, default 0.5): 0.8
   ✓ Priority: 0.8

   🕐 Include last modified date from server? (Y/n): y
   ✓ Will include Last-Modified dates when available

   📁 Output file: ./my-sitemap.xml

┌─────────────────── SITEMAP GENERATION ───────────────────┐
│  🌐 Target: https://example.com                          │
│  📄 Output: my-sitemap.xml                               │
│  📅 Freq:   weekly                                       │
│  ⭐ Priority: 0.8                                        │
└───────────────────────────────────────────────────────────┘

🗺️  [2m 15s] Found: 142 | Checked: 140 | Errors: 2 | Blocked: 0 | 1.0 p/s

📝 Generating sitemap XML...
✅ Sitemap written to: my-sitemap.xml
   📊 Total URLs: 140
   📦 File size: 18.5 KB
```

The generated sitemap follows the standard XML format:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
    <lastmod>2024-01-15</lastmod>
    <changefreq>weekly</changefreq>
    <priority>0.8</priority>
  </url>
  <url>
    <loc>https://example.com/about</loc>
    <lastmod>2024-01-10</lastmod>
    <changefreq>weekly</changefreq>
    <priority>0.8</priority>
  </url>
  ...
</urlset>
```

### Batch Mode (Process URL List)

Instead of crawling a site, you can capture PDFs from a specific list of URLs by creating a `targets.txt` file:

1. Create a file named `targets.txt` in the project directory
2. Paste your URLs (one per line, or the crawler will extract them automatically)
3. Run with batch mode enabled

```
╔═══════════════════════════════════════════════════════════════════╗
║                   🕷️  WEB CRAWLER: BATCH MODE                     ║
╚═══════════════════════════════════════════════════════════════════╝

📂 Looking for 'targets.txt'...
✅ Found 47 unique URLs in targets.txt

🚀 Start generating PDFs? (y/n): y

🚀 BATCH CAPTURE STARTING
📦 Links to process: 47
📁 Saving to: batch_captures_2024-01-15_143022
──────────────────────────────────────────────────
✅ Progress: 47/47 pages handled...

🎉 ALL DONE! Check the 'batch_captures_2024-01-15_143022' folder.
```

This is useful when you:

- Already have a list of specific URLs to capture
- Want to re-capture pages that failed in a previous crawl
- Need to process URLs from an external source (spreadsheet, sitemap, etc.)

### Handling Cloudflare Protection

When Cloudflare blocks the main page:

```
🔍 Testing connection to https://protected-site.com...
   🔄 Attempt 1/3 🛡️  CLOUDFLARE DETECTED (403)
   ⏳ Waiting 3s before retry with different headers...
   🔄 Attempt 2/3 🛡️  CLOUDFLARE DETECTED (403)
   ⏳ Waiting 6s before retry with different headers...
   🔄 Attempt 3/3 🛡️  CLOUDFLARE DETECTED (403)

   🛡️  Cloudflare/Bot protection detected on main page!
   💡 Let's try some alternative entry points...

   Testing common entry points...

   [ 1/17] Testing /about                ✅ WORKS!
   [ 2/17] Testing /about-us             ❌ Failed
   [ 3/17] Testing /contact              ✅ WORKS!
   [ 4/17] Testing /contact-us           🛡️  Blocked
   ...

   ✅ Found 2 working entry point(s)!
   🔄 Will start from these and retry blocked pages later
```

---

## 📊 Output

### Live Statistics

During crawling, you'll see real-time updates with a progress bar:

```
📊 [2m 15s] Pages: 142 | Matches: 8 | Errors: 3 | Blocked: 2 (Queue: 1, Recovered: 1) | 1.1 p/s | 45.2 KB/s
```

For page capture mode, you'll see a live progress bar with the current page being processed:

```
⠹ [████████████░░░░░░░░]  60% │ ⏱ 4m 30s │ 📑 45 captured │ ⏳ 30 pending │ ❌ 2 │ 0.8/s
   → .../newsroom/news-releases/2025/december/name-1072620-en.html
```

### Final Report

```
╔═══════════════════════════════════════════════════════════════════╗
║                      📊 FINAL STATISTICS 📊                       ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║  ⏱️  Total Time:           5m 32s                                  ║
║  📄 Pages Checked:         347                                    ║
║  ✅ Matches Found:         23                                     ║
║  📁 Results File:          results-search-2024-01-15_14-30-00.csv ║
║                                                                   ║
╠═══════════════════════════════════════════════════════════════════╣
║                      🔬 CONTENT BREAKDOWN                         ║
╠═══════════════════════════════════════════════════════════════════╣
║  📝 HTML Pages:            312                                    ║
║  📕 PDF Documents:         28                                     ║
║  📘 Word Documents:        7                                      ║
║  🖼️  Images Checked:        0                                      ║
║  🔗 Links Checked:         0                                      ║
║  ⏭️  Skipped (External):    156                                    ║
...
```

For page capture mode:

```
╔═══════════════════════════════════════════════════════════════════╗
║                  📊 PAGE CAPTURE COMPLETE 📊                      ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║  ⏱️  Total Time:           4m 30s                                  ║
║  📄 Pages Visited:         180                                    ║
║  📑 PDFs Generated:        152                                    ║
║  🖼️  Images Generated:      152                                    ║
║  ❌ Errors:                9                                      ║
║  📁 Output Directory:      page_captures_2024-01-15_14-30-00      ║
║                                                                   ║
╚═══════════════════════════════════════════════════════════════════╝
```

For sitemap generation mode:

```
╔═══════════════════════════════════════════════════════════════════╗
║                  🗺️  SITEMAP GENERATION COMPLETE  🗺️               ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║  ⏱️  Total Time:           2m 15s                                  ║
║  📄 URLs in Sitemap:       140                                    ║
║  🔍 Pages Checked:         145                                    ║
║  ❌ Errors:                3                                      ║
║  🛡️  Blocked:               2                                      ║
║  ⏭️  Skipped (filtered):    0                                      ║
║                                                                   ║
╠═══════════════════════════════════════════════════════════════════╣
║                      📁 OUTPUT FILE                               ║
╠═══════════════════════════════════════════════════════════════════╣
║  📄 Filename:              sitemap.xml                            ║
║  📅 Change Frequency:      weekly                                 ║
║  ⭐ Priority:              0.5                                    ║
║  🕐 Include Last Modified: Yes                                    ║
║                                                                   ║
╚═══════════════════════════════════════════════════════════════════╝

⚡ Performance: 1.07 pages/second
```

### CSV Results

Results are saved to timestamped CSV files:

**Search Mode:**

```csv
URL,ContentType,FoundIn,Target,Timestamp
https://example.com/page1,text/html,HTML,privacy policy,2024-01-15T14:32:45Z
https://example.com/docs/terms.pdf,application/pdf,PDF,privacy policy,2024-01-15T14:33:12Z
https://example.com/members,,NOT SEARCHED: blocked by the server (blocked: 403),privacy policy,2024-01-15T14:33:40Z
```

`Target` is the term that matched. A `FoundIn` that starts with `NOT SEARCHED` is a page the crawler could not read: still blocked after the retry passes, failed every retry, exceeded the response size limit, or a PDF or Word file that would not parse. A 404 is not listed, since there is nothing there to search.

**Broken Links Mode:**

```csv
Result,URL,FoundOnPage,StatusCode,Error,Timestamp
BROKEN,https://example.com/old-page,https://example.com/links,404,Not Found,2024-01-15T14:32:45Z
BLOCKED,https://news.example.com/story,https://example.com/links,403,Refused by bot protection,2024-01-15T14:33:02Z
```

The `Result` column separates two things that are not the same:

| Result      | Meaning                                                             |
| ----------- | ------------------------------------------------------------------- |
| **BROKEN**  | The page is gone. A 404, a 5xx, a DNS failure, a refused connection |
| **BLOCKED** | A firewall refused the request. The page itself is probably fine    |

A 403 from a web application firewall means "I don't like your client", not
"this page is missing". Filter on `Result` to get a clean list of genuinely
dead links, then spot-check the BLOCKED rows in a browser.

**Oversized Images Mode:**

```csv
ImageURL,FoundOnPage,SizeKB,ContentType,Timestamp
https://example.com/hero.jpg,https://example.com/,2048,image/jpeg,2024-01-15T14:32:45Z
```

**Page Capture Mode:**

Files are saved directly to a timestamped folder (e.g., `pdf_captures_2024-01-15_14-30-00/`):

- `index.pdf` / `index.png` - Homepage
- `about.pdf` / `about.png` - About page
- `contact_us.pdf` / `contact_us.png` - Contact page
- etc.

**Sitemap Mode:**

An XML file is generated (e.g., `sitemap.xml`) containing all discovered URLs with optional metadata.

---

## ⚙️ Configuration Options

| Option               | Default | Description                                              |
| -------------------- | ------- | -------------------------------------------------------- |
| Concurrency          | 5       | Number of concurrent requests (max 20)                   |
| Requests per Second  | 5       | One limit shared by every worker                         |
| Max Retries          | 3       | Retry attempts per page on failure                       |
| Retry Delay          | 2s      | Base delay between retries (increases exponentially)     |
| Blocked Retry Passes | 3       | Number of passes to retry blocked pages                  |
| Image Size Threshold | 500KB   | Threshold for oversized image detection                  |
| Path Filter          | (none)  | Only crawl URLs starting with this path (e.g., `/blog/`) |
| Ignore Query Params  | No      | Treat URLs with different query strings as the same page |
| Match Letter Case    | Yes     | Find Link and Find Word compare terms exactly            |
| Max Pages / Max Time | none    | Stop early, keeping what was found (`-max-pages`, `-max-time`) |
| Max Response Size   | 32 MiB  | Decoded HTTP body limit (`-max-body-mb`); excludes Chrome rendering |
| Page Timeout         | 180s    | Max time to wait for a page to render (Page Capture)     |

### Ignore Query Parameters

Some websites use cache-busting or tracking query parameters that create duplicate URLs pointing to the same content:

```
https://example.com/page.html?cache=abc123
https://example.com/page.html?cache=def456
https://example.com/page.html?tracking=xyz
```

When **Ignore Query Params** is enabled, the crawler treats all of these as the same page (`https://example.com/page.html`) and only captures it once. This prevents duplicate files and speeds up crawling.

**When to use:**
- Sites with cache-busting query parameters
- Sites with tracking/analytics parameters in URLs
- News sites that add random query strings to links

### Sitemap-Specific Options

| Option          | Default       | Description                                     |
| --------------- | ------------- | ----------------------------------------------- |
| Filename        | `sitemap.xml` | Output filename for the generated sitemap       |
| Change Freq     | `weekly`      | How often pages typically change                |
| Priority        | `0.5`         | Default priority for all URLs (0.0 - 1.0)       |
| Include LastMod | `true`        | Include Last-Modified dates from server headers |

---

## 🚨 Error Detection

### Network Errors

| Icon | Error Type         | Description                          |
| ---- | ------------------ | ------------------------------------ |
| ⏱️   | Timeout            | Server not responding                |
| 🚫   | Connection Refused | Server actively refusing connections |
| 🌐   | DNS Error          | Domain not found                     |
| 🔒   | SSL/TLS Error      | Certificate failed verification. The host is listed in the final report with the reason, and the crawl carries on |

### HTTP Status Codes

| Code    | Handling                                   |
| ------- | ------------------------------------------ |
| 200-299 | Success - content processed                |
| 300-399 | Redirects followed (up to 10)              |
| 403/503 | Bot protection detected - queued for retry |
| 429     | Rate limited - honors Retry-After, retried |
| 404     | Not found - logged as error                |
| 5xx     | Server error - retried                     |

### Bot Protection Detection

A page counts as a challenge when it carries the wording of one, in combination: "Just a moment" with "enable JavaScript", "Attention Required" with "Cloudflare", "verify you are human" with a captcha, and the like.

It does not go by vendor names alone. Plenty of ordinary pages load fonts from `cdnjs.cloudflare.com` or carry a reCAPTCHA contact form, and treating those as challenges ended crawls at the homepage.

---

## 📁 Project Structure

```
webcrawler/
├── cli.go                       # Entry point and command-line flags
├── main.go                      # Interactive wizard
├── go.mod                       # Go module definition
├── go.sum                       # Dependency checksums
└── internal/
    ├── crawler/
    │   ├── body.go              # Bounded HTTP response reading
    │   ├── chrome.go            # One shared browser, page rendering
    │   ├── crawler.go           # Core crawling logic & statistics
    │   ├── output.go            # Checked CSV writes and output failure handling
    │   ├── pace.go              # Shared rate limit and Retry-After
    │   ├── jsonfeed.go          # JSON feed reader & capture
    │   ├── pdfcapture.go        # Page capture with Chrome/PDF/CMYK
    │   └── sitemap.go           # XML sitemap generation
    └── parser/
        ├── docx.go              # Word text and hyperlinks
        └── pdf.go               # PDF text and link annotations
```

---

## 🔧 Troubleshooting

### "response body exceeds size limit"

The decoded response exceeded the default 32 MiB limit. Search results mark the page `NOT SEARCHED`; truncated content is never parsed. If you need to read larger documents, rerun with `-max-body-mb 64` or another positive limit. Higher limits increase memory use per concurrent request.

### "open CSV", "write CSV", or "write sitemap"

Check that the output path is writable and the disk has free space. These failures return exit code 1. Files saved before the failure remain available, but the run is incomplete; fix the output problem and rerun.

### "google-chrome: executable file not found" (Page Capture Mode)

```bash
# Install Chromium
sudo apt install chromium-browser

# Or install Google Chrome
wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
sudo dpkg -i google-chrome-stable_current_amd64.deb
sudo apt --fix-broken install -y
```

### "ghostscript (gs) not found" (CMYK PDF)

```bash
sudo apt install ghostscript
```

### "imagemagick not found" (CMYK TIFF)

```bash
sudo apt install imagemagick
```

### "context deadline exceeded" (Page Capture Mode)

This means a page took longer than 180 seconds to render. Options:

- Ignore it (a few timeouts are normal for slow pages)
- Reduce concurrency to give Chrome more resources
- Some pages with heavy JavaScript may always timeout

### Path filter not finding all pages

If the crawler isn't finding all pages in a section:

- The site may use JavaScript-loaded content that doesn't expose links in the DOM
- For news/press release sections, the crawler auto-generates year/month archive URLs
- Try entering a more specific path or a known archive URL directly
- Some sites use infinite scroll or AJAX pagination that can't be fully crawled

### PDFs show only header/footer, no body content

This happens when the page body is loaded via JavaScript/AJAX after the initial page load:

- The crawler now waits for body content to stabilize before capturing
- If still seeing empty bodies, the site may use a complex loading pattern
- Try reducing concurrency to give Chrome more time to render
- Some heavily JavaScript-dependent pages may not capture well

### High blocked page count

- Reduce concurrency: `⚡ Max concurrent requests: 3`
- Increase retry count
- Try running at a different time
- Some sites genuinely require JavaScript execution

### Rate limiting (429 errors)

The crawler waits out any `Retry-After` the server sends. If the 429s keep coming:

- Lower the rate: `-rate 2`, or the **Requests per second** prompt in the wizard
- Reduce concurrency

### SSL certificate errors

Certificates are verified. When a host's certificate fails (an incomplete chain is the common one on government sites, a wrong host name or an expired certificate are the others), the crawler prints one warning, keeps going without verification for that host, and lists it in the final report:

```
🔒 TLS: 1 host(s) failed certificate verification and were read without it:
      vdem.virginia.gov: certificate chain is incomplete or not trusted
```

That list is a finding worth passing on to whoever runs the site.

### Empty sitemap generated

If the sitemap has no URLs:

- Check if the site is accessible and not blocking the crawler
- Verify the path filter isn't too restrictive
- The site might be heavily JavaScript-dependent (sitemap mode only crawls static HTML links)
- Try with lower concurrency to avoid rate limiting

---

## 🧪 Development

```bash
# Run the tests
go test ./...

# Same as CI runs them
go test -race ./...

# The full gate set before committing
gofmt -l . && go vet ./... && go build ./... && go test ./...
```

Tests run real crawls against local servers that imitate the awkward cases:
a CDN that rejects `HEAD`, one that answers `HEAD` with 404 but `GET` with
200, firewall 403s, rate limits, skip links and fragments, a start page that
redirects to another host, the path filter, page and time limits, and
self-signed certificates. Page Capture and JSON Feed Capture are tested against
a real Chrome when one is installed (`go test -short ./...` skips them). The parsers are tested on Word files with sentences
split across runs, and on PDFs with link annotations and malformed input.
CI runs gofmt, vet, build, tests and `govulncheck` on every push.

---

## 🛠️ Building

```bash
# Build for current platform
go build -o webcrawler .

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o webcrawler-linux .

# Cross-compile for Windows
GOOS=windows GOARCH=amd64 go build -o webcrawler.exe .

# Cross-compile for macOS
GOOS=darwin GOARCH=amd64 go build -o webcrawler-mac .
```

---

## 📝 Dependencies

| Package                                                              | Version  | Used for                            |
| -------------------------------------------------------------------- | -------- | ----------------------------------- |
| [golang.org/x/net](https://pkg.go.dev/golang.org/x/net)              | v0.61.0  | HTML parsing                        |
| [chromedp](https://github.com/chromedp/chromedp)                     | v0.20.1  | Chrome DevTools Protocol, capture   |
| [huh](https://github.com/charmbracelet/huh)                          | v1.0.0   | Terminal wizard forms               |
| [ledongthuc/pdf](https://github.com/ledongthuc/pdf)                  | 6c8c28e0 | PDF text and link extraction        |

Word files are read with the standard library, so there is nothing to install for PDF or DOCX search.

---

## ⚠️ Legal & Ethical Considerations

- Always respect `robots.txt` (manual check recommended)
- Be mindful of rate limits and server load
- Only crawl sites you have permission to access
- This tool is for legitimate purposes like SEO auditing, content verification, and site maintenance

---

## 📄 License

MIT License - feel free to use, modify, and distribute.

---

## 🤝 Contributing

Contributions welcome! Please feel free to submit issues and pull requests.

---

**Made with ❤️ and Go**
