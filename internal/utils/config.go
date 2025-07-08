package utils

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	StartURL       string `yaml:"startURL"`
	TargetLink     string `yaml:"targetLink"`
	MaxConcurrency int    `yaml:"maxConcurrency"`
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
