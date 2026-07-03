package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"
)

var RetryDelay = 500 * time.Millisecond

var Version = "dev"
var Commit = "unknown"
var BuildDate = "unknown"

var HTTPClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{IdleConnTimeout: 5 * time.Second},
}

var BaseURL = "https://api.cloudflare.com/client/v4"

func main() {
	configPath := flag.String("config", "config.yaml", "path to config YAML")
	flag.Parse()

	config := LoadConfig(*configPath)
	if config.DryRun {
		slog.Info("dry run mode")
	}

	token := os.Getenv(config.TokenEnv)
	if token == "" && !config.DryRun {
		slog.Error("token not found", "env_var", config.TokenEnv)
		os.Exit(1)
	}

	ip, err := GetPublicIP(context.Background(), config.IPSources)
	if err != nil {
		slog.Error("all IP sources failed", "error", err)
		os.Exit(1)
	}
	slog.Info("got public IP", "ip", ip)

	cloudflare := &CloudflareClient{Token: token, DryRun: config.DryRun}
	for _, zone := range config.Zones {
		zoneID, err := cloudflare.ZoneID(context.Background(), zone.Zone)
		if err != nil {
			slog.Error("zone lookup failed", "zone", zone.Zone, "error", err)
			continue
		}
		for _, record := range zone.Records {
			if err := SyncRecord(context.Background(), cloudflare, zoneID, zone.Zone, record, ip); err != nil {
				slog.Error("sync failed", "record", record.Name, "error", err)
			}
		}
	}
}
