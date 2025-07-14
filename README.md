# Webcrawler

A Go web crawler that recursively scans all HTML, PDF, and DOCX pages on a website to find occurrences of a specific link or string.

---

## 🛠 Features

- ✅ Recursive crawling of internal links
- ✅ HTML body text scanning
- ✅ PDF text extraction (via external `pdfcpu` CLI)
- ✅ DOCX text scanning using `gooxml`
- ✅ CSV reporting (`results.csv`)
- ✅ TLS certificate validation skipped (for sites with self-signed or untrusted certs)
- ✅ Ignores `mailto:`, `tel:`, and non-HTTP links
- ✅ Progress printed every 20 pages checked
- ✅ Supports `--verbose` and `--quiet` flags

---

## 🔧 Configuration

Edit `config.yaml`:

```yaml
startURL: "https://www.icann.org"
targetLink: "https://gnso.icann.org/en/council/policy/new-gtlds"
maxConcurrency: 5
```

- `startURL`: Root of the site to begin crawling
- `targetLink`: The link or text you want to search for
- `maxConcurrency`: Number of concurrent fetches

---

## 🚀 Usage

Run the crawler:

```bash
go run main.go [--verbose] [--quiet]
```

- `--verbose`: Show every match found (PDF, DOCX, HTML)
- `--quiet`: Suppress all output except errors and final summary

Results will be saved to `results.csv`:

```csv
URL,ContentType,FoundIn
https://example.com/page1, text/html, HTML
https://example.com/file.pdf, application/pdf, PDF
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

## ⚠️ Notes

- PDF extraction requires `pdfcpu` to be installed and available in your shell.
- DOCX extraction reads paragraph text only (not headers/footers/tables).
- Crawling skips external domains and non-HTTP(S) links (`mailto:`, `tel:`, etc).

---

## 📁 Output

- `results.csv`: A summary of matches with file type and source
- `assets/tmp/`: Temporary working directory for storing PDFs and extracted text

---

## ✅ Tested With

- Go 1.21+
- Sites with public HTML and document content
- Self-signed or misconfigured HTTPS certificates
