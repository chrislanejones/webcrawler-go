package parser

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// buildDocx assembles a minimal Word document the way Word writes one: the
// sentence is split across runs, and the hyperlink URL lives only in the
// relationships part.
func buildDocx(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<w:body>
<w:p><w:r><w:t>Apply for the </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>Opioid Abatement</w:t></w:r><w:r><w:t xml:space="preserve"> grant</w:t></w:r></w:p>
<w:p><w:hyperlink r:id="rId5"><w:r><w:t>portal</w:t></w:r></w:hyperlink></w:p>
</w:body></w:document>`,
		"word/footer1.xml": `<?xml version="1.0"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Footer note</w:t></w:r></w:p></w:ftr>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://www.oaa.virginia.gov/portal-grants/" TargetMode="External"/>
</Relationships>`,
	}
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDocxText(t *testing.T) {
	text, err := DocxText(buildDocx(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Apply for the Opioid Abatement grant", // crosses three runs
		"https://www.oaa.virginia.gov/portal-grants/",
		"Footer note",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("docx text is missing %q\ngot: %q", want, text)
		}
	}
}

func TestDocxTextRejectsNonZip(t *testing.T) {
	if _, err := DocxText([]byte("<html>not a docx</html>")); err == nil {
		t.Error("expected an error for a body that is not a docx archive")
	}
}

// buildPDF writes a one-page PDF with a line of text and a link annotation,
// computing the cross-reference offsets so the file is valid.
func buildPDF() []byte {
	content := "BT /F1 12 Tf 72 720 Td (Notice of public meeting) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		// Real PDFs carry glyph widths. Without them every glyph lands on
		// the same X and word gaps cannot be inferred.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier /FirstChar 32 /LastChar 126 /Widths [" + strings.Repeat("600 ", 95) + "] >>",
		"<< /Type /Annot /Subtype /Link /Rect [72 700 300 730] /A << /S /URI /URI (https://www.governor.virginia.gov/newsroom/) >> >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

func TestPDFText(t *testing.T) {
	text, err := PDFText(buildPDF())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Notice of public meeting",
		"https://www.governor.virginia.gov/newsroom/", // only in the annotation
	} {
		if !strings.Contains(text, want) {
			t.Errorf("pdf text is missing %q\ngot: %q", want, text)
		}
	}
}

func TestPDFTextSurvivesGarbage(t *testing.T) {
	inputs := [][]byte{
		[]byte("not a pdf at all"),
		[]byte("%PDF-1.4\n1 0 obj << /Type /Catalog"), // truncated
		buildPDF()[:200],
	}
	for i, in := range inputs {
		if _, err := PDFText(in); err == nil {
			t.Errorf("input %d: expected an error, got none", i)
		}
	}
}
