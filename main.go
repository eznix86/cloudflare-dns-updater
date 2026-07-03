package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	retry "github.com/avast/retry-go/v5"
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

var Version = "dev"
var Commit = "unknown"
var BuildDate = "unknown"

var DefaultIPSources = []string{
	"https://ipv4.icanhazip.com",
	"https://whatismyip.akamai.com",
	"https://checkip.amazonaws.com",
	"https://api4.ipify.org",
	"https://ifconfig.co/ip",
}

var HTTPClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{IdleConnTimeout: 5 * time.Second},
}

var BaseURL = "https://api.cloudflare.com/client/v4"
var RetryDelay = 500 * time.Millisecond

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

func GetPublicIP(ctx context.Context, sources []string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan string, len(sources))
	var wg sync.WaitGroup
	for _, src := range sources {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			if ip := FetchIP(ctx, url); ip != "" {
				ch <- ip
			}
		}(src)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	if ip, ok := <-ch; ok {
		return ip, nil
	}
	return "", fmt.Errorf("no IP source responded")
}

func FetchIP(ctx context.Context, url string) string {
	var ip string
	if err := retry.New(
		retry.Attempts(3),
		retry.Delay(RetryDelay),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			slog.Warn("retrying IP source", "url", url, "attempt", n+1, "error", err)
		}),
	).Do(func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return retry.Unrecoverable(fmt.Errorf("HTTP %d", resp.StatusCode))
		}
		ip = strings.TrimSpace(string(body))
		return nil
	}); err != nil {
		slog.Warn("IP source failed", "url", url, "error", err)
	}
	return ip
}

type CloudflareClient struct {
	Token  string
	DryRun bool
}

type DNSRecord struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type RecordPayload struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

func (c *CloudflareClient) Do(ctx context.Context, method, path string, body []byte, out any) error {
	return retry.New(
		retry.Attempts(3),
		retry.Delay(RetryDelay),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			slog.Warn("retrying Cloudflare request", "method", method, "path", path, "attempt", n+1, "error", err)
		}),
	).Do(func() error {
		req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := HTTPClient.Do(req)
		if err != nil {
			return err
	}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
		}
		if resp.StatusCode >= 400 {
			return retry.Unrecoverable(fmt.Errorf("HTTP %d: %s", resp.StatusCode, data))
		}
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	})
}

func (c *CloudflareClient) ZoneID(ctx context.Context, name string) (string, error) {
	if c.DryRun {
		return "dry-" + name + "-id", nil
	}
	var resp struct {
		Success bool `json:"success"`
		Result  []struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := c.Do(ctx, http.MethodGet, "/zones?name="+name, nil, &resp); err != nil {
		return "", err
	}
	if !resp.Success || len(resp.Result) == 0 {
		return "", fmt.Errorf("zone %q not found", name)
	}
	return resp.Result[0].ID, nil
}

func (c *CloudflareClient) ListRecords(ctx context.Context, zoneID, fqdn, rtype string) ([]DNSRecord, error) {
	if c.DryRun {
		return nil, nil
	}
	var resp struct {
		Success bool        `json:"success"`
		Result  []DNSRecord `json:"result"`
	}
	path := fmt.Sprintf("/zones/%s/dns_records?name=%s&type=%s", zoneID, fqdn, rtype)
	if err := c.Do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("listRecords: API returned success=false")
	}
	return resp.Result, nil
}

func SyncRecord(ctx context.Context, c *CloudflareClient, zoneID, zone string, rec RecordConfig, ip string) error {
	fqdn := rec.Name + "." + zone
	if rec.Name == "@" {
		fqdn = zone
	}
	ttl := rec.TTL
	if ttl == 0 {
		ttl = 120
	}

	existing, err := c.ListRecords(ctx, zoneID, fqdn, rec.Type)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	payload := RecordPayload{fqdn, rec.Type, ip, ttl, rec.Proxied}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if len(existing) > 0 {
		cur := existing[0]
		if cur.Content == ip {
			slog.Info("IP unchanged, skipping", "record", fqdn, "ip", ip)
			return nil
		}
		slog.Info("updating record", "record", fqdn, "old", cur.Content, "new", ip)
		if c.DryRun {
			slog.Info("dry run: would update", "id", cur.ID, "payload", payload)
			return nil
		}
		return c.Do(ctx, http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, cur.ID), body, nil)
	}

	slog.Info("creating record", "record", fqdn, "ip", ip)
	if c.DryRun {
		slog.Info("dry run: would create", "payload", payload)
		return nil
	}
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), body, nil)
}
