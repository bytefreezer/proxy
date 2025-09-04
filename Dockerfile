# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bytefreezer-proxy .

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

# Create non-root user
RUN addgroup -g 1001 -S proxy && \
    adduser -u 1001 -S proxy -G proxy

# Create directories
RUN mkdir -p /app /etc/bytefreezer-proxy /var/log/bytefreezer-proxy && \
    chown -R proxy:proxy /app /etc/bytefreezer-proxy /var/log/bytefreezer-proxy

# Copy binary and config
COPY --from=builder /app/bytefreezer-proxy /app/
COPY --from=builder /app/config.yaml /etc/bytefreezer-proxy/config.yaml.example

# Set permissions
RUN chmod +x /app/bytefreezer-proxy

# Switch to non-root user
USER proxy

# Set working directory
WORKDIR /app

# Expose ports
EXPOSE 8088/tcp 2056/udp

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://localhost:8088/health || exit 1

# Default command
CMD ["./bytefreezer-proxy"]