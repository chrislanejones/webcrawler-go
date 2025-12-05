package parser

import (
	"bytes"
	"io"
	"strings"

	"baliance.com/gooxml/document"
)

func ContainsLinkInDocx(r io.Reader, target string) bool {
	buf, err := io.ReadAll(r)
	if err != nil {
		return false
	}

	reader := bytes.NewReader(buf)
	doc, err := document.Read(reader, int64(len(buf)))
	if err != nil {
		return false
	}

	for _, para := range doc.Paragraphs() {
		for _, run := range para.Runs() {
			if strings.Contains(run.Text(), target) {
				return true
			}
		}
	}
	return false
}
