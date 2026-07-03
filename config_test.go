package main

import (
	"os"
	"path/filepath"
)

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
