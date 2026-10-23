package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

// DocxText returns the searchable content of a Word document: the text of
// every paragraph in the body, headers, footers, footnotes and comments, plus
// every relationship target, which is where hyperlink URLs live.
//
// Paragraphs are joined run by run. Word splits a sentence into runs wherever
// formatting or spell-check state changes, so matching one run at a time
// misses phrases that cross those boundaries.
func DocxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("not a docx archive: %w", err)
	}

	var out strings.Builder
	for _, f := range zr.File {
		name := f.Name
		switch {
		case strings.HasPrefix(name, "word/_rels/") && strings.HasSuffix(name, ".rels"):
			if err := readRelTargets(f, &out); err != nil {
				return "", err
			}
		case path.Dir(name) == "word" && strings.HasSuffix(name, ".xml"):
			if err := readParagraphText(f, &out); err != nil {
				return "", err
			}
		}
	}
	return out.String(), nil
}

// readParagraphText writes the text of each w:p in a WordprocessingML part on
// its own line. w:instrText is included because older documents store links
// as HYPERLINK field codes rather than relationships.
func readParagraphText(f *zip.File, out *strings.Builder) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t", "instrText":
				inText = true
			case "tab":
				out.WriteByte('\t')
			case "br", "cr":
				out.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t", "instrText":
				inText = false
			case "p":
				out.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				out.Write(t)
			}
		}
	}
}

// readRelTargets writes the Target of every relationship in a .rels part.
func readRelTargets(f *zip.File, out *strings.Builder) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	var rels struct {
		Relationship []struct {
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.NewDecoder(rc).Decode(&rels); err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	for _, r := range rels.Relationship {
		out.WriteString(r.Target)
		out.WriteByte('\n')
	}
	return nil
}
