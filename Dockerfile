# BubbleFish Nexus — Multi-stage Dockerfile
# Stage 1: Build the binary with CGO enabled (required for SQLite).
# Stage 2: Minimal runtime image.

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-s -w -X github.com/BubbleFish-Nexus/internal/version.Version=0.1.0" \
    -o /bubblefish \
    ./cmd/bubblefish/

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

# Install minimal runtime deps (libc for CGO/SQLite, ca-certificates for TLS).
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user for the daemon.
RUN groupadd -r nexus && useradd -r -g nexus -m nexus

# Copy binary.
COPY --from=builder /bubblefish /usr/local/bin/bubblefish

# Create config and data directories with correct permissions (0700).
RUN mkdir -p /home/nexus/.bubblefish/Nexus/wal \
             /home/nexus/.bubblefish/Nexus/sources \
             /home/nexus/.bubblefish/Nexus/destinations \
             /home/nexus/.bubblefish/Nexus/compiled && \
    chown -R nexus:nexus /home/nexus/.bubblefish

USER nexus
WORKDIR /home/nexus

# Expose daemon HTTP port and MCP port.
EXPOSE 8080 7474 8081

# Health check against the liveness endpoint (no auth required).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["bubblefish", "doctor"] || exit 1

ENTRYPOINT ["bubblefish"]
CMD ["start"]
