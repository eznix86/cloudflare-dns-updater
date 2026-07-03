package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	retry "github.com/avast/retry-go/v5"
)

var DefaultIPSources = []string{
	"https://ipv4.icanhazip.com",
	"https://whatismyip.akamai.com",
	"https://checkip.amazonaws.com",
	"https://api4.ipify.org",
	"https://ifconfig.co/ip",
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
		retry.RetryIf(func(err error) bool {
			if errors.Is(err, context.Canceled) {
				return false
			}
			return retry.IsRecoverable(err)
		}),
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
		raw := strings.TrimSpace(string(body))
		parsed := net.ParseIP(raw)
		if parsed == nil || parsed.To4() == nil {
			return retry.Unrecoverable(fmt.Errorf("invalid IPv4 response %q", raw))
		}
		ip = parsed.String()
		return nil
	}); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("IP source failed", "url", url, "error", err)
	}
	return ip
}
