package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

type testTransport struct {
	base  *url.URL
	inner http.RoundTripper
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return t.inner.RoundTrip(req)
}

func withTestServer(h http.HandlerFunc) func() {
	ts := httptest.NewServer(h)
	u, err := url.Parse(ts.URL)
	if err != nil {
		panic(err)
	}
	old := HTTPClient.Transport
	HTTPClient.Transport = &testTransport{base: u, inner: http.DefaultTransport}
	return func() {
		ts.Close()
		HTTPClient.Transport = old
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
token_env: MY_TOKEN
ip_sources:
  - https://example.com/ip
zones:
  - zone: example.com
    records:
      - name: www
        type: A
        ttl: 300
        proxied: true
`), 0644)

	cfg := LoadConfig(p)
	if cfg.TokenEnv != "MY_TOKEN" {
		t.Fatalf("TokenEnv = %q", cfg.TokenEnv)
	}
	if len(cfg.IPSources) != 1 || cfg.IPSources[0] != "https://example.com/ip" {
		t.Fatalf("IPSources = %v", cfg.IPSources)
	}
	r := cfg.Zones[0].Records[0]
	if r.Name != "www" || r.Type != "A" || r.TTL != 300 || !r.Proxied {
		t.Fatalf("Record = %+v", r)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg := LoadConfig("/nonexistent/path.yaml")
	if cfg.TokenEnv != "CLOUDFLARE_TOKEN" {
		t.Error("expected default TokenEnv")
	}
}

func TestLoadConfig_EmptyFilePreservesDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`zones: []`), 0644)
	cfg := LoadConfig(p)
	if len(cfg.IPSources) == 0 {
		t.Error("expected default IPSources")
	}
	if cfg.TokenEnv != "CLOUDFLARE_TOKEN" {
		t.Error("expected default TokenEnv")
	}
}

func TestGetPublicIP_ReturnsFirstSuccess(t *testing.T) {
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "1.2.3.4")
	}))
	defer s1.Close()
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "5.6.7.8")
	}))
	defer s2.Close()

	ip, err := GetPublicIP(context.Background(), []string{s1.URL, s2.URL})
	if err != nil {
		t.Fatal(err)
	}
	if ip != "1.2.3.4" && ip != "5.6.7.8" {
		t.Errorf("got %q, want 1.2.3.4 or 5.6.7.8", ip)
	}
}

func TestGetPublicIP_AllFail(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s.Close()

	ip, err := GetPublicIP(context.Background(), []string{s.URL})
	if err == nil {
		t.Fatal("expected error")
	}
	if ip != "" {
		t.Errorf("got %q", ip)
	}
}

func TestGetPublicIP_FastWins(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "slow")
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "1.2.3.4")
	}))
	defer fast.Close()

	ip, err := GetPublicIP(context.Background(), []string{slow.URL, fast.URL})
	if err != nil {
		t.Fatal(err)
	}
	if ip != "1.2.3.4" {
		t.Errorf("got %q", ip)
	}
}

func TestFetchIP_Success(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "9.9.9.9")
	}))
	defer s.Close()

	ip := FetchIP(context.Background(), s.URL)
	if ip != "9.9.9.9" {
		t.Errorf("got %q", ip)
	}
}

func TestFetchIP_EmptyBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "  \n  ")
	}))
	defer s.Close()

	ip := FetchIP(context.Background(), s.URL)
	if ip != "" {
		t.Errorf("expected empty")
	}
}

func TestFetchIP_4xxNotRetried(t *testing.T) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer s.Close()

	ip := FetchIP(context.Background(), s.URL)
	if ip != "" {
		t.Errorf("expected empty")
	}
	if n.Load() != 1 {
		t.Errorf("expected 1 call, got %d", n.Load())
	}
}

func TestFetchIP_5xxRetried(t *testing.T) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if n.Load() > 2 {
			fmt.Fprint(w, "1.2.3.4")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s.Close()

	ip := FetchIP(context.Background(), s.URL)
	if ip != "1.2.3.4" {
		t.Errorf("got %q", ip)
	}
	if n.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", n.Load())
	}
}

func TestCloudflareClient_ZoneID(t *testing.T) {
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "example.com" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []map[string]any{{"id": "zone123"}},
		})
	})
	defer cleanup()

	c := &CloudflareClient{Token: "test-token"}
	id, err := c.ZoneID(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if id != "zone123" {
		t.Errorf("got %q", id)
	}
}

func TestCloudflareClient_ZoneID_NotFound(t *testing.T) {
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []any{},
		})
	})
	defer cleanup()

	_, err := (&CloudflareClient{Token: "test-token"}).ZoneID(context.Background(), "missing.com")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestCloudflareClient_ZoneID_RetryOn5xx(t *testing.T) {
	var n atomic.Int32
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if n.Load() > 2 {
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result":  []map[string]any{{"id": "zone456"}},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()

	id, err := (&CloudflareClient{Token: "test-token"}).ZoneID(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if id != "zone456" {
		t.Errorf("got %q", id)
	}
	if n.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", n.Load())
	}
}

func TestCloudflareClient_ZoneID_DryRun(t *testing.T) {
	id, err := (&CloudflareClient{DryRun: true}).ZoneID(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if id != "dry-example.com-id" {
		t.Errorf("got %q", id)
	}
}

func TestCloudflareClient_ListRecords(t *testing.T) {
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []map[string]any{{"id": "rec1", "content": "1.2.3.4"}},
		})
	})
	defer cleanup()

	recs, err := (&CloudflareClient{Token: "test-token"}).ListRecords(context.Background(), "z123", "www.example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != "rec1" {
		t.Fatalf("got %v", recs)
	}
}

func TestCloudflareClient_ListRecords_DryRun(t *testing.T) {
	recs, err := (&CloudflareClient{DryRun: true}).ListRecords(context.Background(), "z123", "www.example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if recs != nil {
		t.Errorf("expected nil, got %v", recs)
	}
}

func TestSyncRecord_Unchanged(t *testing.T) {
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []map[string]any{{"id": "rec123", "content": "1.2.3.4"}},
		})
	})
	defer cleanup()

	err := SyncRecord(context.Background(), &CloudflareClient{Token: "test-token"}, "z123", "example.com", RecordConfig{Name: "www"}, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyncRecord_Update(t *testing.T) {
	var calls []string
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result":  []map[string]any{{"id": "rec123", "content": "9.9.9.9"}},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	})
	defer cleanup()

	err := SyncRecord(context.Background(), &CloudflareClient{Token: "test-token"}, "z123", "example.com", RecordConfig{Name: "www", TTL: 300, Proxied: true}, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
	}
	if calls[0] != "GET /client/v4/zones/z123/dns_records" {
		t.Errorf("first = %q", calls[0])
	}
	if calls[1] != "PUT /client/v4/zones/z123/dns_records/rec123" {
		t.Errorf("second = %q", calls[1])
	}
}

func TestSyncRecord_Create(t *testing.T) {
	var calls []string
	cleanup := withTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []map[string]any{},
		})
	})
	defer cleanup()

	err := SyncRecord(context.Background(), &CloudflareClient{Token: "test-token"}, "z123", "example.com", RecordConfig{Name: "www"}, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
	}
	if calls[0] != "GET /client/v4/zones/z123/dns_records" {
		t.Errorf("first = %q", calls[0])
	}
	if calls[1] != "POST /client/v4/zones/z123/dns_records" {
		t.Errorf("second = %q", calls[1])
	}
}

func TestSyncRecord_DryRunCreate(t *testing.T) {
	err := SyncRecord(context.Background(), &CloudflareClient{DryRun: true}, "z123", "example.com", RecordConfig{Name: "www"}, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyncRecord_DryRunUpdate(t *testing.T) {
	err := SyncRecord(context.Background(), &CloudflareClient{DryRun: true}, "z123", "example.com", RecordConfig{Name: "www", TTL: 300}, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
}
