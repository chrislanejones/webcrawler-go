package main

import (
	"fmt"
	"os"
	"webcrawler/internal/crawler"
	"webcrawler/internal/utils"
)

func main() {
	config, err := utils.LoadConfig("config.yaml")
	if err != nil {
		fmt.Println("❌ Failed to load config.yaml:", err)
		os.Exit(1)
	}
	crawler.Start(config.StartURL, config.TargetLink, config.MaxConcurrency)
}
