package main

import (
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DryRun    bool         `yaml:"dry_run"`
	TokenEnv  string       `yaml:"token_env"`
	IPSources []string     `yaml:"ip_sources"`
	Zones     []ZoneConfig `yaml:"zones"`
}

type ZoneConfig struct {
	Zone    string         `yaml:"zone"`
	Records []RecordConfig `yaml:"records"`
}

type RecordConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	TTL     int    `yaml:"ttl"`
	Proxied bool   `yaml:"proxied"`
}

func LoadConfig(path string) Config {
	cfg := Config{
		TokenEnv:  "CLOUDFLARE_TOKEN",
		IPSources: DefaultIPSources,
	}
	data, err := os.ReadFile(path) //nolint:gosec // path comes from user flag, intentional
	if err != nil {
		slog.Warn("config not found, using defaults", "path", path)
		return cfg
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Warn("invalid config, using defaults", "error", err)
		return cfg
	}
	if cfg.TokenEnv == "" {
		cfg.TokenEnv = "CLOUDFLARE_TOKEN"
	}
	if len(cfg.IPSources) == 0 {
		cfg.IPSources = DefaultIPSources
	}
	return cfg
}
