package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"webcrawler/internal/crawler"
	"webcrawler/internal/utils"
)

func main() {
	config, err := utils.LoadConfig("config.yaml")
	if err != nil {
		fmt.Println("❌ Failed to load config.yaml:", err)
		os.Exit(1)
	}

	startURLs := config.GetStartURLs()
	targetLinks := config.GetTargetLinks()

	if len(startURLs) == 0 {
		fmt.Println("❌ No start URLs specified in config.yaml")
		os.Exit(1)
	}

	if len(targetLinks) == 0 {
		fmt.Println("❌ No target links specified in config.yaml")
		os.Exit(1)
	}

	totalOperations := len(startURLs) * len(targetLinks)
	currentOperation := 0

	fmt.Printf("🚀 Starting webcrawler with %d website(s) and %d target link(s)\n", len(startURLs), len(targetLinks))
	fmt.Printf("📊 Total operations: %d\n", totalOperations)
	fmt.Println("🌐 Start URLs:")
	for i, url := range startURLs {
		fmt.Printf("   %d. %s\n", i+1, url)
	}
	fmt.Println("🎯 Target links:")
	for i, link := range targetLinks {
		fmt.Printf("   %d. %s\n", i+1, link)
	}
	fmt.Println()

	// Test initial connections to start URLs
	fmt.Println("🔍 Testing initial connections...")
	for i, startURL := range startURLs {
		fmt.Printf("Testing %d/%d: %s", i+1, len(startURLs), startURL)
		
		// Simple HEAD request to test connectivity
		client := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		
		req, err := http.NewRequest("HEAD", startURL, nil)
		if err != nil {
			fmt.Printf(" ❌ INVALID URL\n")
			fmt.Printf("   Error: %v\n", err)
			continue
		}
		
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; WebCrawler/1.0)")
		
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf(" ❌ CONNECTION FAILED\n")
			errStr := err.Error()
			switch {
			case strings.Contains(errStr, "timeout"):
				fmt.Printf("   Issue: Server timeout - the website is not responding\n")
			case strings.Contains(errStr, "connection refused"):
				fmt.Printf("   Issue: Connection refused - the server is blocking connections\n")
			case strings.Contains(errStr, "no such host"):
				fmt.Printf("   Issue: Domain not found - check if the URL is correct\n")
			case strings.Contains(errStr, "certificate"):
				fmt.Printf("   Issue: SSL certificate problem\n")
			default:
				fmt.Printf("   Error: %v\n", err)
			}
			continue
		}
		defer resp.Body.Close()
		
		statusCode := resp.StatusCode
		switch {
		case statusCode == 200:
			fmt.Printf(" ✅ OK\n")
		case statusCode == 403:
			fmt.Printf(" 🚫 BLOCKED (403 Forbidden)\n")
			fmt.Printf("   Issue: The website is blocking automated requests\n")
		case statusCode == 404:
			fmt.Printf(" 📄 NOT FOUND (404)\n")
			fmt.Printf("   Issue: The main page doesn't exist at this URL\n")
		case statusCode == 429:
			fmt.Printf(" 🐌 RATE LIMITED (429)\n")
			fmt.Printf("   Issue: Too many requests - the site is throttling connections\n")
		case statusCode >= 500:
			fmt.Printf(" 🔥 SERVER ERROR (%d)\n", statusCode)
			fmt.Printf("   Issue: The website is experiencing internal problems\n")
		default:
			fmt.Printf(" ⚠️  STATUS %d\n", statusCode)
		}
	}
	
	fmt.Println()
	fmt.Println("🚀 Starting crawl operations...")
	fmt.Println()

	// Process each start URL with each target link
	for urlIndex, startURL := range startURLs {
		fmt.Printf("🌐 Processing website %d of %d: %s\n", urlIndex+1, len(startURLs), startURL)
		fmt.Println("=" + fmt.Sprintf("%*s", 80, "="))
		
		for linkIndex, targetLink := range targetLinks {
			currentOperation++
			
			fmt.Printf("🔍 Operation %d of %d: Searching for target %d of %d\n", 
				currentOperation, totalOperations, linkIndex+1, len(targetLinks))
			fmt.Printf("🎯 Target: %s\n", targetLink)
			fmt.Println("-" + fmt.Sprintf("%*s", 60, "-"))
			
			crawler.Start(startURL, targetLink, config.MaxConcurrency, currentOperation, urlIndex+1, len(startURLs), linkIndex+1, len(targetLinks))
			
			if linkIndex < len(targetLinks)-1 {
				fmt.Println()
				fmt.Println("📋 Moving to next target on same website...")
				fmt.Println()
			}
		}
		
		if urlIndex < len(startURLs)-1 {
			fmt.Println()
			fmt.Println("🌐 Moving to next website...")
			fmt.Println()
		}
	}

	fmt.Println("🎉 All websites and target links processed successfully!")
	fmt.Printf("📁 Check results-operation-*.csv files for individual results\n")
}