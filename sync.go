package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

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
