# Cloudflare DNS Updater

Dynamic DNS updater that syncs your public IP to Cloudflare DNS records.

## Usage

```bash
uv run main.py
```

### Dry run

Enable `dry_run: true` in `config.yaml` to log what would happen without making any changes.

## Configuration

```yaml
dry_run: false
token_env: CLOUDFLARE_TOKEN
ip_sources:
  - https://ifconfig.me/ip
  - https://api.ipify.org
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
task test       # run tests
task coverage   # test with coverage report
task lint       # ruff check
task typecheck  # pyright
task format     # ruff format
```
