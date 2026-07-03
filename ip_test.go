package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

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
