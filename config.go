package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	RequiresAuth  bool   `yaml:"requires_auth"`
	RequiresOAuth bool   `yaml:"requires_oauth"`
}

type Config struct {
	Apps []AppConfig `yaml:"apps"`
}

func (a AppConfig) Addr() string {
	return fmt.Sprintf("%s:%d", a.Host, a.Port)
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}
