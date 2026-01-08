package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"webcrawler/internal/crawler"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\n╔═══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   🕷️  WEB CRAWLER: BATCH MODE                     ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")

	fmt.Println("\n📂 Looking for 'targets.txt'...")
	content, err := os.ReadFile("targets.txt")
	if err != nil {
		fmt.Println("❌ Error: 'targets.txt' not found!")
		fmt.Println("   Please create targets.txt and paste your links there.")
		return
	}

	// Extract unique URLs
	re := regexp.MustCompile(`https?://[^\s"<>]+`)
	matches := re.FindAllString(string(content), -1)
	
	var uniqueLinks []string
	seen := make(map[string]bool)
	for _, link := range matches {
		link = strings.TrimRight(link, ".,\")")
		if !seen[link] {
			seen[link] = true
			uniqueLinks = append(uniqueLinks, link)
		}
	}

	fmt.Printf("✅ Found %d unique URLs in targets.txt\n", len(uniqueLinks))
	fmt.Print("\n🚀 Start generating PDFs? (y/n): ")
	
	answer, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("Aborted.")
		return
	}

	// Settings for the run
	config := crawler.Config{
		MaxConcurrency: 4, // Higher = faster, but uses LOTS of RAM
		CaptureFormat:  crawler.CapturePDFOnly,
	}

	crawler.RunBatchCapture(uniqueLinks, config)
}