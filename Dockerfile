# Multi-stage build for ByteFreezer Proxy
# Stage 1: Build the Go binary
FROM golang:1.24.4 AS builder

# Set working directory
WORKDIR /src

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
ARG GIT_COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" \
    -o /app .

# Debug: Check what we built
RUN ls -l /app

# Verify the binary is statically linked
RUN ldd /app 2>&1 | grep -q "not a dynamic executable" || (echo "Binary is not static!" && ldd /app && exit 1)

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
             /var/spool/bytefreezer-proxy && \
    chown -R bytefreezer:bytefreezer /etc/bytefreezer-proxy \
                                    /var/log/bytefreezer-proxy \
                                    /var/spool/bytefreezer-proxy

# Copy the binary from builder stage
COPY --from=builder /app /app

# Copy default configuration
COPY --chown=bytefreezer:bytefreezer config.yaml /etc/bytefreezer-proxy/config.yaml

# Copy CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Set up proper permissions
RUN chmod +x /app

# Debug: Check the binary is there
RUN ls -l /app

# Switch to non-root user
USER bytefreezer

# Set environment variables
ENV CONFIG_FILE=/etc/bytefreezer-proxy/config.yaml

# Expose ports
# 8088: API/Health endpoint
# 9090: Metrics endpoint (OTEL/Prometheus)  
# 2056-2065: UDP plugin ports (configurable)
# 8081-8090: HTTP plugin ports (configurable)
# Only configured plugin ports will be active, but common ones are exposed for flexibility
EXPOSE 8088/tcp 9090/tcp 2056/udp 2057/udp 2058/udp 2059/udp 2060/udp 2061/udp 2062/udp 2063/udp 2064/udp 2065/udp 8081/tcp 8082/tcp 8083/tcp 8084/tcp 8085/tcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8088/api/v1/health || exit 1

# Set working directory
WORKDIR /

# Default command
CMD ["/app", "--config", "/etc/bytefreezer-proxy/config.yaml"]

# Metadata labels following OCI standards
ARG VERSION=unknown
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown
LABEL maintainer="ByteFreezer Team" \
      org.opencontainers.image.title="ByteFreezer Proxy" \
      org.opencontainers.image.description="High-performance multi-protocol data streaming proxy with plugin architecture supporting UDP, HTTP, Kafka, NATS and custom plugins" \
      org.opencontainers.image.vendor="ByteFreezer" \
      org.opencontainers.image.source="https://github.com/n0needt0/bytefreezer-proxy" \
      org.opencontainers.image.documentation="https://github.com/n0needt0/bytefreezer-proxy/blob/main/README.md" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_TIME}" \
      org.opencontainers.image.revision="${GIT_COMMIT}"