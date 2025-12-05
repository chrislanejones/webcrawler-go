# 🕷️ Web Crawler - Cloudflare Buster Edition

![Golang Web Crawler Banner with Spider](Golang-Web-Crawler-Banner.jpg)
A powerful Go-based web crawler with an interactive terminal wizard interface. Features intelligent Cloudflare bypass strategies, comprehensive statistics, and support for HTML, PDF, and DOCX content scanning.

![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)
![License](https://img.shields.io/badge/License-MIT-green.svg)

---

## ✨ Features

### 🎯 Four Powerful Search Modes

| Mode                     | Description                                                                |
| ------------------------ | -------------------------------------------------------------------------- |
| **🔗 Find Link**         | Search for specific URLs/links across HTML pages, PDFs, and Word documents |
| **📝 Find Word/Phrase**  | Search for any text string across all supported content types              |
| **💔 Broken Link Check** | Scan entire site for 404s, timeouts, and connection errors                 |
| **🖼️ Oversized Images**  | Find images exceeding a specified file size threshold                      |

### 🛡️ Cloudflare Bypass Strategies

The crawler employs multiple techniques to handle bot protection:

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

- Go 1.21 or higher
- `pdfcpu` CLI tool (for PDF text extraction)

### Installation

```bash
# Clone or download the project
git clone <repository-url>
cd webcrawler

# Install dependencies
go mod tidy

# Install pdfcpu for PDF support
go install github.com/pdfcpu/pdfcpu/cmd/pdfcpu@latest
export PATH=$PATH:$(go env GOPATH)/bin
```

### Running

```bash
go run main.go
```

The interactive wizard will guide you through the configuration.

---

## 📖 Usage Guide

### Step-by-Step Wizard

```
╔═══════════════════════════════════════════════════════════════════╗
║                   🕷️  Web Crawler Wizard  🕷️                       ║
║                        v2.1 - Cloudflare Buster                   ║
╚═══════════════════════════════════════════════════════════════════╝

🌐 What site do you want to check?
   → example.com

🔍 Testing connection to https://example.com...
   🔄 Attempt 1/3 ✅ OK (200) - 245ms latency

📋 What should I check the site for?

   ┌─────────────────────────────────────────────────────────┐
   │  1. 🔗 Find a link on site (HTML, Word, PDF)            │
   │  2. 📝 Find a word/phrase on site (HTML, Word, PDF)     │
   │  3. 💔 Search for broken links                          │
   │  4. 🖼️  Search for oversized images                      │
   └─────────────────────────────────────────────────────────┘

   Enter choice (1-4): 2

📝 Enter the word or phrase to search for:
   → privacy policy

⚡ Max concurrent requests (default 5, max 20): 10

🔄 Max retries per page (default 3): 3
```

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

During crawling, you'll see real-time updates:

```
📊 [2m 15s] Pages: 142 | Matches: 8 | Errors: 3 | Blocked: 2 (Queue: 1, Recovered: 1) | 1.1 p/s | 45.2 KB/s
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
BrokenURL,FoundOnPage,StatusCode,Error,Timestamp
https://example.com/old-page,https://example.com/links,404,Not Found,2024-01-15T14:32:45Z
```

**Oversized Images Mode:**

```csv
ImageURL,FoundOnPage,SizeKB,ContentType,Timestamp
https://example.com/hero.jpg,https://example.com/,2048,image/jpeg,2024-01-15T14:32:45Z
```

---

## ⚙️ Configuration Options

| Option               | Default | Description                                          |
| -------------------- | ------- | ---------------------------------------------------- |
| Concurrency          | 5       | Number of concurrent requests (max 20)               |
| Max Retries          | 3       | Retry attempts per page on failure                   |
| Retry Delay          | 2s      | Base delay between retries (increases exponentially) |
| Blocked Retry Passes | 3       | Number of passes to retry blocked pages              |
| Image Size Threshold | 500KB   | Threshold for oversized image detection              |

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
    │   └── crawler.go           # Core crawling logic & statistics
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

---

## 🛠️ Building

```bash
# Build for current platform
go build -o webcrawler main.go

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o webcrawler-linux main.go

# Cross-compile for Windows
GOOS=windows GOARCH=amd64 go build -o webcrawler.exe main.go

# Cross-compile for macOS
GOOS=darwin GOARCH=amd64 go build -o webcrawler-mac main.go
```

---

## 📝 Dependencies

- [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) - HTML parsing
- [baliance.com/gooxml](https://github.com/baliance/gooxml) - DOCX parsing
- [pdfcpu](https://github.com/pdfcpu/pdfcpu) - PDF text extraction (external CLI)

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
