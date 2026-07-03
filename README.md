# Cloudflare DNS Updater

Dynamic DNS updater that syncs your public IP to Cloudflare DNS records.

| vs ddclient | |
|---|---|
| YAML config | No DSL, just YAML |
| Proxied records | Orange cloud toggle works reliably per record |
| Dry run | One config flag, zero risk |
| Kubernetes | CronJob native, no daemon |
| IP detection | HTTP only, ddclient also does interface/firewall |
| IPv6 | Not yet, ddclient does |

## Usage

```bash
# with config.yaml in current directory
./updater

# or specify config path
./updater --config /path/to/config.yaml
```

### Dry run

Set `dry_run: true` in your config to log what would happen without making any changes.

## Configuration

```yaml
dry_run: false
token_env: CLOUDFLARE_TOKEN
ip_sources:
  - https://ipv4.icanhazip.com
  - https://whatismyip.akamai.com
  - https://checkip.amazonaws.com
  - https://api4.ipify.org
  - https://ifconfig.co/ip
zones:
  - zone: example.com
    records:
      - name: "@"
        type: A
        ttl: 120
        proxied: false
```

## Docker

```bash
docker run --rm \
  -v "$(pwd)/config.yaml:/app/config.yaml" \
  -e CLOUDFLARE_TOKEN=xxx \
  ghcr.io/eznix86/cloudflare-dns-updater
```

## Docker Compose

```yaml
services:
  cloudflare-dns-updater:
    image: ghcr.io/eznix86/cloudflare-dns-updater
    restart: unless-stopped
    environment:
      - CLOUDFLARE_TOKEN=xxx
    volumes:
      - ./config.yaml:/app/config.yaml
```

## Kubernetes

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: cloudflare-dns-updater
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: updater
              image: ghcr.io/eznix86/cloudflare-dns-updater
              env:
                - name: CLOUDFLARE_TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: cloudflare-token
                      key: token
              volumeMounts:
                - name: config
                  mountPath: /app/config.yaml
                  subPath: config.yaml
          volumes:
            - name: config
              configMap:
                name: cloudflare-dns-config
```

## Development

```bash
task test       # run tests (testify/suite)
task lint       # go vet + go fix
task golangci   # golangci-lint (full lint suite)
task build      # compile binary
task run        # dry run with example config
task docker     # build Docker image
task all        # lint → test → build → docker
```

Default IP sources (used when `ip_sources` is empty or config is missing):

- `https://ipv4.icanhazip.com`
- `https://whatismyip.akamai.com`
- `https://checkip.amazonaws.com`
- `https://api4.ipify.org`
- `https://ifconfig.co/ip`

IP lookups run concurrently. The first successful response wins. Each source is retried up to 3 times with a configurable delay on 5xx errors. 4xx errors are not retried.
