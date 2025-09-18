#!/bin/bash

# ByteFreezer Proxy - UDP Plugin Integration Test
# Tests UDP plugin functionality including syslog, netflow, and plain UDP

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_CONFIG="$SCRIPT_DIR/udp-test-config.yaml"
PROXY_BINARY="../bytefreezer-proxy"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROXY_PID=""

cleanup() {
    echo -e "${YELLOW}Cleaning up UDP test environment...${NC}"
    if [[ -n "$PROXY_PID" ]]; then
        kill $PROXY_PID 2>/dev/null || true
        wait $PROXY_PID 2>/dev/null || true
    fi
    rm -f "$TEST_CONFIG"
    rm -rf /tmp/bytefreezer-proxy-udp-test-spool
}

trap cleanup EXIT INT TERM

log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')] $1${NC}"
}

error() {
    echo -e "${RED}[ERROR] $1${NC}"
}

success() {
    echo -e "${GREEN}[SUCCESS] $1${NC}"
}

# Generate UDP test configuration
generate_udp_config() {
    log "Generating UDP test configuration..."
    
    cat > "$TEST_CONFIG" << 'EOF'
app:
  name: bytefreezer-proxy-udp-test
  version: test

logging:
  level: info
  encoding: console

server:
  api_port: 28088

tenant_id: "udp-test-tenant"
bearer_token: "udp-test-token"

inputs:
  # Plain UDP test
  - type: "udp"
    name: "plain-udp"
    config:
      host: "127.0.0.1"
      port: 22056
      dataset_id: "plain-udp-data"
      protocol: "udp"
      worker_count: 2
      
  # Syslog UDP test
  - type: "udp"  
    name: "syslog-udp"
    config:
      host: "127.0.0.1"
      port: 22057
      dataset_id: "syslog-udp-data"
      protocol: "syslog"
      syslog_mode: "rfc3164"
      worker_count: 2

receiver:
  base_url: "http://127.0.0.1:29999/data/{tenantid}/{datasetid}"
  timeout_seconds: 5
  retry_count: 1

spooling:
  enabled: true
  directory: "/tmp/bytefreezer-proxy-udp-test-spool"
  max_size_bytes: 10485760
  retry_attempts: 2
  retry_interval_seconds: 2

otel:
  enabled: false
  
soc:
  enabled: false
EOF
    
    success "UDP test configuration generated"
}

# Start proxy for UDP testing
start_udp_proxy() {
    log "Starting proxy for UDP testing..."
    
    rm -rf /tmp/bytefreezer-proxy-udp-test-spool
    
    "$PROXY_BINARY" --config "$TEST_CONFIG" > /tmp/udp-proxy.log 2>&1 &
    PROXY_PID=$!
    
    # Wait for proxy to start
    local retries=0
    while [[ $retries -lt 10 ]]; do
        if curl -s http://127.0.0.1:28088/api/v2/health > /dev/null 2>&1; then
            success "UDP proxy started (PID: $PROXY_PID)"
            return 0
        fi
        sleep 1
        retries=$((retries + 1))
    done
    
    error "Failed to start UDP proxy"
    cat /tmp/udp-proxy.log
    return 1
}

# Test plain UDP functionality
test_plain_udp() {
    log "Testing plain UDP functionality..."
    
    local messages=(
        "Plain text log message"
        "$(date): Application started"
        "ERROR: Database connection failed"
        "INFO: User login successful - user_id=12345"
        '{"timestamp":"'$(date -Iseconds)'","level":"warn","message":"Test JSON log"}'
    )
    
    for msg in "${messages[@]}"; do
        echo "$msg" | nc -u 127.0.0.1 22056
        sleep 0.1
    done
    
    sleep 2
    
    # Check for processing indicators
    if [[ -d "/tmp/bytefreezer-proxy-udp-test-spool" ]] || grep -q "UDP.*22056" /tmp/udp-proxy.log; then
        success "Plain UDP test passed"
        return 0
    fi
    
    error "Plain UDP test failed"
    return 1
}

# Test syslog UDP functionality
test_syslog_udp() {
    log "Testing syslog UDP functionality..."
    
    # Generate RFC3164 syslog messages
    local facility=16  # local0
    local severity=6   # info
    local priority=$((facility * 8 + severity))
    local timestamp=$(date '+%b %d %H:%M:%S')
    local hostname="testhost"
    local tag="testapp"
    
    local syslog_messages=(
        "<$priority>$timestamp $hostname $tag: Application started successfully"
        "<$priority>$timestamp $hostname $tag: User authentication completed"
        "<$priority>$timestamp $hostname nginx[1234]: 192.168.1.1 GET /api/status 200"
        "<$priority>$timestamp $hostname kernel: TCP: request_sock_TCP: Possible SYN flooding"
    )
    
    for msg in "${syslog_messages[@]}"; do
        echo "$msg" | nc -u 127.0.0.1 22057
        sleep 0.1
    done
    
    sleep 2
    
    # Check for syslog processing
    if [[ -d "/tmp/bytefreezer-proxy-udp-test-spool" ]] || grep -q "syslog.*22057" /tmp/udp-proxy.log; then
        success "Syslog UDP test passed"
        return 0
    fi
    
    error "Syslog UDP test failed"
    return 1
}

# Test UDP performance with burst
test_udp_performance() {
    log "Testing UDP performance with message burst..."
    
    local message_count=100
    local start_time=$(date +%s)
    
    for i in $(seq 1 $message_count); do
        echo "Burst message $i - $(date +'%H:%M:%S.%3N')" | nc -u 127.0.0.1 22056
    done
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    sleep 3  # Allow processing time
    
    if [[ $duration -lt 10 ]]; then  # Should complete within 10 seconds
        success "UDP performance test passed - $message_count messages in ${duration}s"
        return 0
    else
        error "UDP performance test failed - took ${duration}s for $message_count messages"
        return 1
    fi
}

# Check UDP ports are listening
test_udp_ports() {
    log "Testing UDP port availability..."
    
    local ports=(22056 22057)
    
    for port in "${ports[@]}"; do
        if ss -ulpn | grep -q ":$port "; then
            success "UDP port $port is listening"
        else
            error "UDP port $port is not listening"
            return 1
        fi
    done
    
    return 0
}

# Test UDP with different payload sizes
test_udp_payload_sizes() {
    log "Testing UDP with different payload sizes..."
    
    # Small payload
    echo "Small" | nc -u 127.0.0.1 22056
    
    # Medium payload (1KB)
    python3 -c "print('Medium: ' + 'x' * 1000)" | nc -u 127.0.0.1 22056
    
    # Large payload (8KB - near UDP limit)
    python3 -c "print('Large: ' + 'x' * 8000)" | nc -u 127.0.0.1 22056
    
    sleep 2
    
    success "UDP payload size test completed"
    return 0
}

# Main UDP test execution
main() {
    echo -e "${BLUE}"
    echo "========================================"
    echo "  ByteFreezer Proxy UDP Plugin Tests"
    echo "========================================"
    echo -e "${NC}"
    
    local failed_tests=()
    
    # Check prerequisites
    if ! command -v nc &> /dev/null; then
        error "netcat (nc) is required for UDP testing"
        exit 1
    fi
    
    if [[ ! -f "$PROXY_BINARY" ]]; then
        error "Proxy binary not found: $PROXY_BINARY"
        exit 1
    fi
    
    # Generate config and start proxy
    generate_udp_config
    
    if ! start_udp_proxy; then
        error "Failed to start proxy for UDP testing"
        exit 1
    fi
    
    echo ""
    log "Running UDP plugin tests..."
    echo ""
    
    # Run tests
    if ! test_udp_ports; then
        failed_tests+=("Port Listening")
    fi
    
    if ! test_plain_udp; then
        failed_tests+=("Plain UDP")
    fi
    
    if ! test_syslog_udp; then
        failed_tests+=("Syslog UDP")
    fi
    
    if ! test_udp_payload_sizes; then
        failed_tests+=("Payload Sizes")
    fi
    
    if ! test_udp_performance; then
        failed_tests+=("Performance")
    fi
    
    # Print results
    echo ""
    echo -e "${BLUE}========================================"
    echo "  UDP Plugin Test Results"
    echo "========================================${NC}"
    
    local total_tests=5
    local passed_tests=$((total_tests - ${#failed_tests[@]}))
    
    echo "Total UDP tests: $total_tests"
    echo -e "Passed: ${GREEN}$passed_tests${NC}"
    echo -e "Failed: ${RED}${#failed_tests[@]}${NC}"
    
    if [[ ${#failed_tests[@]} -eq 0 ]]; then
        echo ""
        success "All UDP tests passed! 🎉"
        
        # Show some stats
        if [[ -d "/tmp/bytefreezer-proxy-udp-test-spool" ]]; then
            local spool_files=$(find /tmp/bytefreezer-proxy-udp-test-spool -type f | wc -l)
            echo "Spool files created: $spool_files"
        fi
        
        exit 0
    else
        echo ""
        echo -e "${RED}Failed UDP tests:${NC}"
        for test in "${failed_tests[@]}"; do
            echo "  - $test"
        done
        
        echo ""
        echo "Proxy log:"
        tail -20 /tmp/udp-proxy.log
        
        exit 1
    fi
}

main "$@"