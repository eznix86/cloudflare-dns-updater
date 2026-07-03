package main

import (
	"context"
	"net/http"
)

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

		err := SyncRecord(
			context.Background(),
			NewFakeCloudflareClient(),
			"z123",
			"example.com",
			RecordConfig{Name: "www", TTL: 300, Proxied: true},
			"1.2.3.4",
		)

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
