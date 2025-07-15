# Webcrawler

A Go web crawler that recursively scans multiple websites for multiple target links or strings across HTML, PDF, and DOCX pages.

---

## 🚨 Error Detection & Troubleshooting

The crawler provides detailed error analysis and suggestions for common issues:

### **Initial Connection Testing**

Before starting operations, the crawler tests connectivity to all start URLs:

- ✅ **OK** - Website is accessible
- 🚫 **BLOCKED (403)** - Website is blocking automated requests
- 📄 **NOT FOUND (404)** - Main page doesn't exist
- 🐌 **RATE LIMITED (429)** - Too many requests
- 🔥 **SERVER ERROR (5xx)** - Website internal problems

### **Bot Protection Detection**

The crawler automatically identifies major anti-bot systems:

- 🛡️ **Cloudflare Bot Management** - "Checking your browser..." pages
- 🛡️ **Incapsula/Imperva** - Enterprise bot protection
- 🛡️ **PerimeterX** - Advanced bot detection
- 🛡️ **Sucuri Security** - WordPress security plugin
- 🛡️ **CAPTCHA Challenge** - Manual verification required
- 🛡️ **Generic Anti-Bot System** - Other protection mechanisms

### **Network Error Categories**

- ⏱️ **TIMEOUT** - Server not responding (may be overloaded or blocking)
- 🚫 **CONNECTION REFUSED** - Server actively refusing connections
- 🌐 **DNS ERROR** - Domain name not found or DNS resolution failed
- 🔒 **SSL/TLS ERROR** - Certificate validation failed

### **Status Code Explanations**

- **403 Forbidden** - Bot detection or access restrictions
- **404 Not Found** - Page doesn't exist
- **429 Rate Limited** - Too many requests (reduce `maxConcurrency`)
- **503 Service Unavailable** - Server temporarily down or overloaded

### **Summary Statistics**

Each operation shows:

- **Total checked** - Number of pages crawled
- **Matches** - Target links/strings found
- **Errors** - Network errors, 404s, timeouts, etc.
- **Blocked** - Pages blocked by anti-bot protection

---

## 🛠 Features

- ✅ **Multiple start URLs** - Crawl several websites in sequence
- ✅ **Multiple target links** - Search for multiple links/strings per website
- ✅ **Enhanced error detection** - Detailed analysis of connection issues and bot blocking
- ✅ **Initial connectivity testing** - Pre-flight checks for all start URLs
- ✅ **Bot protection detection** - Identifies Cloudflare, Incapsula, and other anti-bot systems
- ✅ Recursive crawling of internal links
- ✅ HTML body text scanning
- ✅ PDF text extraction (via external `pdfcpu` CLI)
- ✅ DOCX text scanning using `gooxml`
- ✅ Individual CSV reporting for each operation
- ✅ TLS certificate validation skipped (for sites with self-signed or untrusted certs)
- ✅ Ignores `mailto:`, `tel:`, and non-HTTP links
- ✅ Progress tracking with operation numbers
- ✅ Supports `--verbose` and `--quiet` flags

---

## 🔧 Configuration

Edit `config.yaml`:

```yaml
# Multiple start URLs (comma-separated)
startURLs: "https://www.icann.org,https://www.iana.org,https://root-servers.org"

# Multiple target links to search for (comma-separated)
targetLinks: "https://gnso.icann.org/en/council/policy/new-gtlds,https://www.icann.org/resources/pages/gtlds,https://newgtlds.icann.org"

maxConcurrency: 5
```

**Configuration Options:**

- `startURLs`: Comma-separated list of websites to crawl
- `targetLinks`: Comma-separated list of links or text strings to search for
- `maxConcurrency`: Number of concurrent fetches per operation

---

## 🚀 Usage

Run the crawler:

```bash
go run main.go [--verbose] [--quiet]
```

**Flags:**

- `--verbose`: Show every match found and detailed progress
- `--quiet`: Suppress all output except errors and final summaries

**Example Output:**

```
🚀 Starting webcrawler with 3 website(s) and 3 target link(s)
📊 Total operations: 9

🔍 Testing initial connections...
Testing 1/3: https://www.icann.org ✅ OK
Testing 2/3: https://www.iana.org ✅ OK
Testing 3/3: https://blocked-site.com 🚫 BLOCKED (403 Forbidden)
   Issue: The website is blocking automated requests

🚀 Starting crawl operations...

🌐 Processing website 1 of 3: https://www.icann.org
================================================================================
🔍 Operation 1 of 9: Searching for target 1 of 3
🎯 Target: https://gnso.icann.org/en/council/policy/new-gtlds
------------------------------------------------------------
🔍 [Op 1] Checking: https://www.icann.org
🤖 [Op 1] BOT PROTECTION DETECTED: https://www.icann.org/protected-page
   🛡️  Protection Type: Cloudflare Bot Management
   💡 This website requires manual verification or has strict bot policies
   ⚠️  The crawler cannot bypass this protection automatically
📄 [Op 1] PAGE NOT FOUND (404): https://www.icann.org/nonexistent-page - This page doesn't exist
✅ Operation 1 completed (Website 1/3, Target 1/3)
📊 Total checked: 45, Matches: 3, Errors: 2, Blocked: 1, Time: 2m15s
⚠️  Warning: 1 pages were blocked by anti-bot protection
⚠️  Warning: 2 pages had errors (timeouts, 404s, etc.)
...
```

---

## 📦 Dependencies

Install the required Go modules:

```bash
go mod tidy
```

Install the `pdfcpu` CLI (used to extract PDF text):

```bash
go install github.com/pdfcpu/pdfcpu/cmd/pdfcpu@latest
export PATH=$PATH:$(go env GOPATH)/bin
```

---

## 📁 Output

Results are saved to individual CSV files for each operation:

- `results-operation-1-website-1-target-1.csv`
- `results-operation-2-website-1-target-2.csv`
- `results-operation-3-website-1-target-3.csv`
- ... and so on

**CSV Structure:**

```csv
URL,ContentType,FoundIn,TargetLink,StartURL,OperationIndex
https://example.com/page1,text/html,HTML,https://target.com,https://www.icann.org,1
https://example.com/file.pdf,application/pdf,PDF,https://target.com,https://www.icann.org,1
```

**CSV Columns:**

- `URL`: The page where the target was found
- `ContentType`: MIME type of the content
- `FoundIn`: Type of content (HTML, PDF, DOCX)
- `TargetLink`: The target link/string that was found
- `StartURL`: The website that was being crawled
- `OperationIndex`: Sequential operation number

---

## ⚠️ Notes

- **Processing Order**: The crawler processes each start URL sequentially, searching for all target links on each website before moving to the next
- **Total Operations**: If you have 3 start URLs and 3 target links, you'll have 9 total operations (3×3)
- **File Organization**: Each operation creates its own result file for easy analysis
- PDF extraction requires `pdfcpu` to be installed and available in your shell
- DOCX extraction reads paragraph text only (not headers/footers/tables)
- Crawling skips external domains and non-HTTP(S) links (`mailto:`, `tel:`, etc)
- The `assets/tmp/` directory is used for temporary PDF processing files

---

## ✅ Tested With

- Go 1.21+
- Multiple websites with public HTML and document content
- Self-signed or misconfigured HTTPS certificates
- Large-scale operations (10+ websites × 10+ target links)

---

## 📊 Performance Tips

- **Concurrency**: Adjust `maxConcurrency` based on your system and target websites' rate limits
- **Target Specificity**: More specific target strings will reduce false positives
- **Website Selection**: Ensure start URLs are the root domains you want to crawl
- **Resource Usage**: Monitor system resources during large operations (many URLs × many targets)
