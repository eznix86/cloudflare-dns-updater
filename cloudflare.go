package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	retry "github.com/avast/retry-go/v5"
)

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
		retry.RetryIf(func(err error) bool {
			if errors.Is(err, context.Canceled) {
				return false
			}
			return retry.IsRecoverable(err)
		}),
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
