package main

import (
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/stretchr/testify/suite"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	RetryDelay = 1 * time.Millisecond
	os.Exit(m.Run())
}

type FakeTransport struct {
	base  *url.URL
	inner http.RoundTripper
}

func (t *FakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return t.inner.RoundTrip(req)
}

func WithFakeHttpClient(h http.HandlerFunc) func() {
	ts := httptest.NewServer(h)
	u, err := url.Parse(ts.URL)
	if err != nil {
		panic(err)
	}
	old := HTTPClient.Transport
	HTTPClient.Transport = &FakeTransport{base: u, inner: http.DefaultTransport}
	return func() {
		ts.Close()
		HTTPClient.Transport = old
	}
}

func WriteResult(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": v})
}

func NewIPServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
}

func NewFakeCloudflareClient() *CloudflareClient {
	return &CloudflareClient{Token: "test-token"}
}

type Suite struct {
	suite.Suite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (s *Suite) TestLoadConfig() {
	is := s.Assert()
	must := s.Require()

	s.Run("parses full config", func() {
		dir := s.T().TempDir()
		p := filepath.Join(dir, "config.yaml")
		err := os.WriteFile(p, []byte(`
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
`), 0600)
		must.NoError(err)

		cfg := LoadConfig(p)

		is.Equal("MY_TOKEN", cfg.TokenEnv)
		is.Equal([]string{"https://example.com/ip"}, cfg.IPSources)
		r := cfg.Zones[0].Records[0]
		is.Equal("www", r.Name)
		is.Equal("A", r.Type)
		is.Equal(300, r.TTL)
		is.True(r.Proxied)
	})

	s.Run("missing file returns defaults", func() {
		cfg := LoadConfig("/nonexistent/path.yaml")
		is.Equal("CLOUDFLARE_TOKEN", cfg.TokenEnv)
	})

	s.Run("empty file preserves defaults", func() {
		dir := s.T().TempDir()
		p := filepath.Join(dir, "config.yaml")
		err := os.WriteFile(p, []byte(`zones: []`), 0600)
		must.NoError(err)

		cfg := LoadConfig(p)

		is.NotEmpty(cfg.IPSources)
		is.Equal("CLOUDFLARE_TOKEN", cfg.TokenEnv)
	})
}

func (s *Suite) TestGetPublicIP() {
	is := s.Assert()
	must := s.Require()

	s.Run("returns first success", func() {
		s1 := NewIPServer("1.2.3.4")
		defer s1.Close()
		s2 := NewIPServer("5.6.7.8")
		defer s2.Close()

		ip, err := GetPublicIP(context.Background(), []string{s1.URL, s2.URL})

		must.NoError(err)
		is.Contains([]string{"1.2.3.4", "5.6.7.8"}, ip)
	})

	s.Run("all sources fail", func() {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer s.Close()

		ip, err := GetPublicIP(context.Background(), []string{s.URL})

		must.Error(err)
		is.Equal(ip, "")
	})

	s.Run("fast wins over slow", func() {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = fmt.Fprint(w, "slow")
		}))
		defer slow.Close()
		fast := NewIPServer("1.2.3.4")
		defer fast.Close()

		ip, err := GetPublicIP(context.Background(), []string{slow.URL, fast.URL})

		must.NoError(err)
		is.Equal("1.2.3.4", ip)
	})
}

func (s *Suite) TestFetchIP() {
	is := s.Assert()
	must := s.Require()

	s.Run("success", func() {
		s := NewIPServer("9.9.9.9")
		defer s.Close()

		ip := FetchIP(context.Background(), s.URL)

		is.Equal("9.9.9.9", ip)
	})

	s.Run("empty body returns empty", func() {
		s := NewIPServer("  \n  ")
		defer s.Close()

		ip := FetchIP(context.Background(), s.URL)

		is.Empty(ip)
	})

	s.Run("IPv6 body returns empty", func() {
		s := NewIPServer("2001:db8::1")
		defer s.Close()

		ip := FetchIP(context.Background(), s.URL)

		is.Empty(ip)
	})

	s.Run("IPv4 body is trimmed", func() {
		s := NewIPServer("  \n9.9.9.9 \n")
		defer s.Close()

		ip := FetchIP(context.Background(), s.URL)

		is.Equal("9.9.9.9", ip)
	})

	s.Run("4xx not retried", func() {
		var n atomic.Int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer s.Close()

		ip := FetchIP(context.Background(), s.URL)

		is.Empty(ip)
		is.Equal(int32(1), n.Load())
	})

	s.Run("5xx retried", func() {
		var n atomic.Int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n.Add(1)
			if n.Load() > 2 {
				_, _ = fmt.Fprint(w, "1.2.3.4")
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer s.Close()

		ip := FetchIP(context.Background(), s.URL)

		is.Equal("1.2.3.4", ip)
		must.Equal(int32(3), n.Load())
	})
}

func (s *Suite) TestCloudflareClient() {
	is := s.Assert()
	must := s.Require()

	s.Run("zone ID", func() {
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("name") != "example.com" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			WriteResult(w, []map[string]any{{"id": "zone123"}})
		})
		defer cleanup()

		id, err := NewFakeCloudflareClient().ZoneID(context.Background(), "example.com")

		must.NoError(err)
		is.Equal("zone123", id)
	})

	s.Run("zone ID not found", func() {
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, _ *http.Request) {
			WriteResult(w, []any{})
		})
		defer cleanup()

		_, err := NewFakeCloudflareClient().ZoneID(context.Background(), "missing.com")

		must.Error(err)
		is.True(strings.Contains(err.Error(), "not found"))
	})

	s.Run("zone ID retries on 5xx", func() {
		var n atomic.Int32
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, _ *http.Request) {
			n.Add(1)
			if n.Load() > 2 {
				WriteResult(w, []map[string]any{{"id": "zone456"}})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		})
		defer cleanup()

		id, err := NewFakeCloudflareClient().ZoneID(context.Background(), "example.com")

		must.NoError(err)
		is.Equal("zone456", id)
		is.Equal(int32(3), n.Load())
	})

	s.Run("zone ID dry run", func() {
		id, err := (&CloudflareClient{DryRun: true}).ZoneID(context.Background(), "example.com")

		must.NoError(err)
		is.Equal("dry-example.com-id", id)
	})

	s.Run("list records", func() {
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, _ *http.Request) {
			WriteResult(w, []map[string]any{{"id": "rec1", "content": "1.2.3.4"}})
		})
		defer cleanup()

		recs, err := NewFakeCloudflareClient().ListRecords(context.Background(), "z123", "www.example.com", "A")

		must.NoError(err)
		is.Len(recs, 1)
		is.Equal("rec1", recs[0].ID)
	})

	s.Run("list records dry run", func() {
		recs, err := (&CloudflareClient{DryRun: true}).ListRecords(context.Background(), "z123", "www.example.com", "A")

		must.NoError(err)
		is.Nil(recs)
	})
}

func (s *Suite) TestSyncRecord() {
	is := s.Assert()
	must := s.Require()

	s.Run("unchanged skips update", func() {
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, _ *http.Request) {
			WriteResult(w, []map[string]any{{"id": "rec123", "content": "1.2.3.4"}})
		})
		defer cleanup()

		err := SyncRecord(context.Background(), NewFakeCloudflareClient(), "z123", "example.com", RecordConfig{Name: "www"}, "1.2.3.4")

		must.NoError(err)
	})

	s.Run("updates changed record", func() {
		var calls []string
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			if r.Method == http.MethodGet {
				WriteResult(w, []map[string]any{{"id": "rec123", "content": "9.9.9.9"}})
				return
			}
			WriteResult(w, map[string]any{"success": true})
		})
		defer cleanup()

		err := SyncRecord(context.Background(), NewFakeCloudflareClient(), "z123", "example.com", RecordConfig{Name: "www", TTL: 300, Proxied: true}, "1.2.3.4")

		must.NoError(err)
		is.Len(calls, 2)
		is.Equal("GET /client/v4/zones/z123/dns_records", calls[0])
		is.Equal("PUT /client/v4/zones/z123/dns_records/rec123", calls[1])
	})

	s.Run("creates missing record", func() {
		var calls []string
		cleanup := WithFakeHttpClient(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			WriteResult(w, []map[string]any{})
		})
		defer cleanup()

		err := SyncRecord(context.Background(), NewFakeCloudflareClient(), "z123", "example.com", RecordConfig{Name: "www"}, "1.2.3.4")

		must.NoError(err)
		is.Len(calls, 2)
		is.Equal("GET /client/v4/zones/z123/dns_records", calls[0])
		is.Equal("POST /client/v4/zones/z123/dns_records", calls[1])
	})

	s.Run("dry run create", func() {
		err := SyncRecord(context.Background(), &CloudflareClient{DryRun: true}, "z123", "example.com", RecordConfig{Name: "www"}, "1.2.3.4")
		must.NoError(err)
	})

	s.Run("dry run update", func() {
		err := SyncRecord(context.Background(), &CloudflareClient{DryRun: true}, "z123", "example.com", RecordConfig{Name: "www", TTL: 300}, "1.2.3.4")
		must.NoError(err)
	})
}
