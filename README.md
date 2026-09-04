# 🕷️ Web Crawler - Cloudflare Buster Edition

![Golang Web Crawler Banner with Spider](Golang-Web-Crawler-Banner.jpg)
A Go web crawler with an interactive terminal wizard. It works on sites behind bot protection, and it reads HTML, PDF, and DOCX content rather than just HTML.

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
- **Custom Entry Point**: Specify your own "back door" URL
- **Multi-Phase Crawling**: Start from working pages, then retry blocked pages with established session cookies
- **User Agent Rotation**: Cycles through 5 different browser signatures
- **Session Persistence**: Maintains cookies across requests
- **Exponential Backoff**: Smart retry delays to avoid rate limiting

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
- `pdfcpu` CLI tool (for PDF text extraction)
- Chrome or Chromium (for page capture mode)
- Ghostscript (optional, for CMYK PDF output)
- ImageMagick (optional, for CMYK TIFF output)

### Installation

```bash
# Clone the project
git clone https://github.com/chrislanejones/webcrawler-go.git
# or: git clone https://codeberg.org/chrislanejones/webcrawler-go.git
cd webcrawler-go

# Install dependencies
go mod tidy

# Install pdfcpu for PDF support
go install github.com/pdfcpu/pdfcpu/cmd/pdfcpu@latest
export PATH=$PATH:$(go env GOPATH)/bin

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

The interactive wizard will guide you through the configuration.

---

## 📖 Usage Guide

### Step-by-Step Wizard

```
╔═══════════════════════════════════════════════════════════════════╗
║                   🕷️  Web Crawler Wizard  🕷️                       ║
║                              v2.5                                 ║
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
```

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
```

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
| Max Retries          | 3       | Retry attempts per page on failure                       |
| Retry Delay          | 2s      | Base delay between retries (increases exponentially)     |
| Blocked Retry Passes | 3       | Number of passes to retry blocked pages                  |
| Image Size Threshold | 500KB   | Threshold for oversized image detection                  |
| Path Filter          | (none)  | Only crawl URLs starting with this path (e.g., `/blog/`) |
| Ignore Query Params  | No      | Treat URLs with different query strings as the same page |
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
| 🔒   | SSL/TLS Error      | Certificate validation failed        |

### HTTP Status Codes

| Code    | Handling                                   |
| ------- | ------------------------------------------ |
| 200-299 | Success - content processed                |
| 300-399 | Redirects followed (up to 10)              |
| 403/503 | Bot protection detected - queued for retry |
| 429     | Rate limited - backed off and retried      |
| 404     | Not found - logged as error                |
| 5xx     | Server error - retried                     |

### Bot Protection Detection

Automatically identifies:

- Cloudflare ("Checking your browser...", "Ray ID")
- Incapsula/Imperva
- PerimeterX
- Sucuri
- Generic CAPTCHA challenges
- DDoS protection pages

---

## 📁 Project Structure

```
webcrawler/
├── main.go                      # Interactive wizard & entry point
├── go.mod                       # Go module definition
├── go.sum                       # Dependency checksums
├── assets/
│   └── tmp/                     # Temporary files for PDF processing
└── internal/
    ├── crawler/
    │   ├── crawler.go           # Core crawling logic & statistics
    │   ├── jsonfeed.go          # JSON feed reader & capture
    │   ├── pdfcapture.go        # Page capture with Chrome/PDF/CMYK
    │   └── sitemap.go           # XML sitemap generation
    └── parser/
        ├── docx.go              # Word document parser
        └── pdf.go               # PDF text extractor
```

---

## 🔧 Troubleshooting

### "pdfcpu: command not found"

```bash
go install github.com/pdfcpu/pdfcpu/cmd/pdfcpu@latest
export PATH=$PATH:$(go env GOPATH)/bin
```

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

The crawler automatically backs off, but you can:

- Reduce concurrency
- Increase the built-in delay (edit `time.Sleep(50 * time.Millisecond)` in `crawler.go`)

### SSL certificate errors

The crawler skips certificate verification by default (`InsecureSkipVerify: true`). This handles self-signed certs but be aware of the security implications.

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

Tests cover the broken-link classification against a local server that
imitates the awkward cases: a CDN that rejects `HEAD`, one that answers
`HEAD` with 404 but `GET` with 200, firewall 403s, rate limits and real
404s. CI runs gofmt, vet, build, tests and `govulncheck` on every push.

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
| [golang.org/x/net](https://pkg.go.dev/golang.org/x/net)              | v0.58.0  | HTML parsing                        |
| [chromedp](https://github.com/chromedp/chromedp)                     | v0.16.0  | Chrome DevTools Protocol, capture   |
| [huh](https://github.com/charmbracelet/huh)                          | v1.0.0   | Terminal wizard forms               |
| [baliance.com/gooxml](https://github.com/baliance/gooxml)            | v1.0.1   | DOCX parsing                        |
| [pdfcpu](https://github.com/pdfcpu/pdfcpu)                           | latest   | PDF text extraction (external CLI)  |

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
