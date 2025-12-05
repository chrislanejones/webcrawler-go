package parser

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ContainsLinkInPDF(r io.Reader, target string) bool {
	buf, err := io.ReadAll(r)
	if err != nil {
		return false
	}

	os.MkdirAll("assets/tmp", 0755)
	tmpPDF := "assets/tmp/tmp.pdf"
	if err := os.WriteFile(tmpPDF, buf, 0644); err != nil {
		return false
	}

	outDir := "assets/tmp/text"
	os.MkdirAll(outDir, 0755)

	cmd := exec.Command("pdfcpu", "extract", "-mode", "text", tmpPDF, outDir)
	if err := cmd.Run(); err != nil {
		// Cleanup and return false
		os.RemoveAll(outDir)
		os.Remove(tmpPDF)
		return false
	}

	found := false
	filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasSuffix(path, ".txt") {
			data, readErr := os.ReadFile(path)
			if readErr == nil && strings.Contains(string(data), target) {
				found = true
			}
		}
		return nil
	})

	// Cleanup
	os.RemoveAll(outDir)
	os.Remove(tmpPDF)

	return found
}
