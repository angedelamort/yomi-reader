# Build stage
FROM golang:1.26-alpine AS builder
ARG VERSION=dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION}" -o yomi-reader .

# Runtime stage
FROM alpine:3.21
# su-exec lets the root entrypoint drop privileges to PUID:PGID (gosu equivalent).
RUN apk add --no-cache ca-certificates tzdata su-exec
WORKDIR /app
COPY --from=builder /app/yomi-reader .
COPY --from=builder /app/static ./static
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# /data: SQLite DB, session key and cover cache. /manga: read-only library root.
# PUID/PGID follow the linuxserver.io / arr convention: the container starts as
# root, the entrypoint chowns /data to PUID:PGID, then drops to that user.
ENV DATA_PATH=/data \
    PORT=8098 \
    AUTH_PROVIDER=local \
    DEFAULT_LIBRARY_PATH=/manga \
    TZ=America/Toronto \
    PUID=1000 \
    PGID=1000
RUN mkdir -p /data /manga
VOLUME ["/data"]
EXPOSE 8098
HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:${PORT}/healthz >/dev/null || exit 1
# Runs as root only long enough to fix /data ownership, then su-exec to PUID:PGID.
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["./yomi-reader"]
