# Multi-stage build for ByteFreezer Proxy
# Stage 1: Build the Go binary
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimization flags
# Disable CGO for a fully static binary
ARG VERSION=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o bytefreezer-proxy .

# Verify the binary is statically linked
RUN ldd bytefreezer-proxy 2>&1 | grep -q "not a dynamic executable" || (echo "Binary is not static!" && exit 1)

# Stage 2: Create minimal runtime image
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    netcat-openbsd \
    && rm -rf /var/cache/apk/*

# Create non-root user for security
RUN addgroup -g 1000 -S bytefreezer && \
    adduser -u 1000 -S bytefreezer -G bytefreezer -s /bin/sh -D

# Create required directories
RUN mkdir -p /etc/bytefreezer-proxy \
             /var/log/bytefreezer-proxy \
             /var/spool/bytefreezer-proxy \
             /opt/bytefreezer-proxy && \
    chown -R bytefreezer:bytefreezer /etc/bytefreezer-proxy \
                                    /var/log/bytefreezer-proxy \
                                    /var/spool/bytefreezer-proxy \
                                    /opt/bytefreezer-proxy

# Copy the binary from builder stage
COPY --from=builder /app/bytefreezer-proxy /opt/bytefreezer-proxy/bytefreezer-proxy

# Copy default configuration
COPY --chown=bytefreezer:bytefreezer config.yaml /etc/bytefreezer-proxy/config.yaml

# Copy CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Set up proper permissions
RUN chmod +x /opt/bytefreezer-proxy/bytefreezer-proxy

# Switch to non-root user
USER bytefreezer

# Set environment variables
ENV CONFIG_FILE=/etc/bytefreezer-proxy/config.yaml
ENV PATH="/opt/bytefreezer-proxy:${PATH}"

# Expose ports
# 8088: API/Health endpoint
# 2056-2058: UDP listeners (default configuration)
EXPOSE 8088/tcp 2056/udp 2057/udp 2058/udp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8088/health || exit 1

# Set working directory
WORKDIR /opt/bytefreezer-proxy

# Default command
CMD ["./bytefreezer-proxy", "--config", "/etc/bytefreezer-proxy/config.yaml"]

# Metadata labels following OCI standards
ARG VERSION=unknown
ARG BUILD_TIME=unknown
LABEL maintainer="ByteFreezer Team" \
      org.opencontainers.image.title="ByteFreezer Proxy" \
      org.opencontainers.image.description="High-performance UDP log proxy for ByteFreezer platform" \
      org.opencontainers.image.vendor="ByteFreezer" \
      org.opencontainers.image.source="https://github.com/n0needt0/bytefreezer-proxy" \
      org.opencontainers.image.documentation="https://github.com/n0needt0/bytefreezer-proxy/blob/main/README.md" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_TIME}"