ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

FROM golang:1.26-alpine AS build
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_DATE
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags="-X main.Version=${VERSION} -X main.Commit=${GIT_COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /app/updater main.go

FROM alpine:latest
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_DATE
LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${GIT_COMMIT}
LABEL org.opencontainers.image.created=${BUILD_DATE}
LABEL org.opencontainers.image.title="cloudflare-dns-updater"
LABEL org.opencontainers.image.description="Dynamic DNS updater for Cloudflare"
RUN apk add --no-cache ca-certificates
COPY --from=build /app/updater /updater
COPY config.yaml.example /config.yaml
ENTRYPOINT ["/updater", "--config", "/config.yaml"]
