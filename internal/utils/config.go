package utils

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	StartURLs      string `yaml:"startURLs"`
	TargetLinks    string `yaml:"targetLinks"`
	MaxConcurrency int    `yaml:"maxConcurrency"`
}

func (c *Config) GetStartURLs() []string {
	urls := strings.Split(c.StartURLs, ",")
	var result []string
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (c *Config) GetTargetLinks() []string {
	links := strings.Split(c.TargetLinks, ",")
	var result []string
	for _, link := range links {
		trimmed := strings.TrimSpace(link)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	return &cfg, err
}