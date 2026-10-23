package parser

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// maxPDFPages bounds the work spent on one document. Past this a PDF is a
// scanned archive or a data dump, and the crawl has better places to spend
// its time.
const maxPDFPages = 1000

// PDFText returns the searchable content of a PDF: the text drawn on each
// page, plus the URI of every link annotation. Link URLs live in annotations
// rather than in the page text, so a text-only search never finds them.
//
// It runs in-process. The previous version shelled out to the pdfcpu CLI
// through one fixed temp file, which failed silently when pdfcpu was not
// installed and let concurrent workers overwrite each other's file.
func PDFText(data []byte) (text string, err error) {
	// The PDF library panics on malformed input. One bad document on a
	// site must not take down the crawl.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("malformed pdf: %v", r)
		}
	}()

	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	var out strings.Builder
	pages := min(r.NumPage(), maxPDFPages)
	for i := 1; i <= pages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		// No shared font cache: font names like /F1 are scoped to each
		// page's resources and can mean a different font on the next page.
		pageText, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", i, err)
		}
		out.WriteString(pageText)
		out.WriteByte('\n')
		writeLinkURIs(&out, page.V.Key("Annots"))
	}
	return out.String(), nil
}

func writeLinkURIs(out *strings.Builder, annots pdf.Value) {
	for i := 0; i < annots.Len(); i++ {
		uri := annots.Index(i).Key("A").Key("URI")
		if s := uri.RawString(); s != "" {
			out.WriteString(s)
			out.WriteByte('\n')
		}
	}
}
