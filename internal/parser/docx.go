package parser

import (
	"bytes"
	"io"
	"strings"

	"baliance.com/gooxml/document"
)

func ContainsLinkInDocx(r io.Reader, target string) bool {
	// Read the entire DOCX file into memory
	buf, err := io.ReadAll(r)
	if err != nil {
		return false
	}

	// Create a ReaderAt + size for document.Read
	reader := bytes.NewReader(buf)
	doc, err := document.Read(reader, int64(len(buf)))
	if err != nil {
		return false
	}

	// Search all paragraph text runs
	for _, para := range doc.Paragraphs() {
		for _, run := range para.Runs() {
			if strings.Contains(run.Text(), target) {
				return true
			}
		}
	}
	return false
}
