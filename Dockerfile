# ============================================================
# GreenForge - Multi-stage Docker build
# ============================================================

# Stage 1: Build the Go binary
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git gcc musl-dev sqlite-dev

WORKDIR /build

# Cache dependencies
COPY go.mod ./
RUN go mod download || true

# Copy source, resolve deps, and build
COPY . .
RUN go mod tidy

RUN CGO_ENABLED=1 GOOS=linux go build \
    -tags "fts5" \
    -ldflags "-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo 0.1.0-dev) \
              -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) \
              -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /build/greenforge \
    ./cmd/greenforge

# Stage 2: Runtime image
FROM alpine:3.19

RUN apk add --no-cache \
    ca-certificates \
    openssh-client \
    git \
    docker-cli \
    sqlite \
    sqlite-libs \
    curl \
    bash \
    tzdata

# Create non-root user
RUN addgroup -S greenforge && adduser -S greenforge -G greenforge

# Create directories
RUN mkdir -p /home/greenforge/.greenforge/{ca,certs,index,tools,sessions} && \
    chown -R greenforge:greenforge /home/greenforge/.greenforge

# Copy binary
COPY --from=builder /build/greenforge /usr/local/bin/greenforge

# Copy default configs and tool manifests
COPY configs/ /etc/greenforge/configs/
COPY tools/ /etc/greenforge/tools/

# Expose ports
# 18788: Gateway (gRPC/WS)
# 18789: Web UI
EXPOSE 18788 18789

# Volume for persistent data
VOLUME ["/home/greenforge/.greenforge"]

# Health check
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD curl -f http://localhost:18788/api/v1/health || exit 1

USER greenforge
WORKDIR /home/greenforge

ENV GREENFORGE_HOME=/home/greenforge/.greenforge

ENTRYPOINT ["greenforge"]
CMD ["run"]
