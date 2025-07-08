package parser

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ContainsLinkInPDF(r io.Reader, target string) bool {
	// Read input to buffer
	buf, err := io.ReadAll(r)
	if err != nil {
		return false
	}

	// Save PDF to disk
	tmpPDF := "assets/tmp/tmp.pdf"
	if err := os.WriteFile(tmpPDF, buf, 0644); err != nil {
		return false
	}

	// Call pdfcpu CLI to extract text
	outDir := "assets/tmp/text"
	os.MkdirAll(outDir, 0755)

	cmd := exec.Command("pdfcpu", "extract", "-mode", "text", tmpPDF, outDir)
	if err := cmd.Run(); err != nil {
		return false
	}

	// Look through .txt files for the target string
	found := false
	filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, ".txt") {
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), target) {
				found = true
			}
		}
		return nil
	})

	return found
}
