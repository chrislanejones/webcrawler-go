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
	var firstErr error
	read := 0
	for i := 1; i <= pages; i++ {
		pageText, err := readPage(r, i)
		if err != nil {
			// One damaged page, a corrupt stream or an unsupported filter,
			// should not hide the rest of the document from the search.
			if firstErr == nil {
				firstErr = fmt.Errorf("page %d: %w", i, err)
			}
			continue
		}
		read++
		out.WriteString(pageText)
	}
	if read == 0 && firstErr != nil {
		return "", firstErr
	}
	return out.String(), nil
}

// readPage returns one page's text and link URIs. It recovers from the
// library's panics itself, so a panic on one page costs only that page.
func readPage(r *pdf.Reader, i int) (text string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("malformed page: %v", rec)
		}
	}()

	page := r.Page(i)
	if page.V.IsNull() {
		return "", nil
	}
	// No shared font cache: font names like /F1 are scoped to each
	// page's resources and can mean a different font on the next page.
	pageText, err := page.GetPlainText(nil)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(pageText)
	out.WriteByte('\n')
	writeLinkURIs(&out, page.V.Key("Annots"))
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
