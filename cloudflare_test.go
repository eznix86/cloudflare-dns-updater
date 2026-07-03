package main

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
)

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
