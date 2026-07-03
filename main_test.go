package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
