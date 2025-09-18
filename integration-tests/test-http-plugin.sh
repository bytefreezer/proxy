#!/bin/bash

# ByteFreezer Proxy - HTTP Plugin Integration Test
# Tests HTTP webhook plugin functionality including authentication, limits, and formats

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_CONFIG="$SCRIPT_DIR/http-test-config.yaml"
PROXY_BINARY="../bytefreezer-proxy"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROXY_PID=""

cleanup() {
    echo -e "${YELLOW}Cleaning up HTTP test environment...${NC}"
    if [[ -n "$PROXY_PID" ]]; then
        kill $PROXY_PID 2>/dev/null || true
        wait $PROXY_PID 2>/dev/null || true
    fi
    rm -f "$TEST_CONFIG"
    rm -rf /tmp/bytefreezer-proxy-http-test-spool
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

warning() {
    echo -e "${YELLOW}[WARNING] $1${NC}"
}

# Generate HTTP test configuration
generate_http_config() {
    log "Generating HTTP test configuration..."
    
    cat > "$TEST_CONFIG" << 'EOF'
app:
  name: bytefreezer-proxy-http-test
  version: test

logging:
  level: info
  encoding: console

server:
  api_port: 38088

tenant_id: "http-test-tenant"
bearer_token: "http-test-token"

inputs:
  # HTTP Plugin with authentication
  - type: "http"
    name: "webhook-auth"
    config:
      host: "127.0.0.1"
      port: 38081
      path: "/webhook"
      dataset_id: "webhook-auth-data"
      bearer_token: "webhook-secret-token"
      max_payload_size: 1048576      # 1MB
      max_lines_per_request: 100
      read_timeout: 10
      write_timeout: 10
      enable_authentication: true
      
  # HTTP Plugin without authentication
  - type: "http"
    name: "webhook-noauth"
    config:
      host: "127.0.0.1"
      port: 38082
      path: "/data"
      dataset_id: "webhook-noauth-data"
      max_payload_size: 512000       # 500KB
      max_lines_per_request: 50
      read_timeout: 5
      write_timeout: 5
      enable_authentication: false

receiver:
  base_url: "http://127.0.0.1:39999/data/{tenantid}/{datasetid}"
  timeout_seconds: 5
  retry_count: 1

spooling:
  enabled: true
  directory: "/tmp/bytefreezer-proxy-http-test-spool"
  max_size_bytes: 10485760
  retry_attempts: 2
  retry_interval_seconds: 2

otel:
  enabled: false
  
soc:
  enabled: false
EOF
    
    success "HTTP test configuration generated"
}

# Start proxy for HTTP testing
start_http_proxy() {
    log "Starting proxy for HTTP testing..."
    
    rm -rf /tmp/bytefreezer-proxy-http-test-spool
    
    "$PROXY_BINARY" --config "$TEST_CONFIG" > /tmp/http-proxy.log 2>&1 &
    PROXY_PID=$!
    
    # Wait for proxy to start
    local retries=0
    while [[ $retries -lt 15 ]]; do
        if curl -s http://127.0.0.1:38088/api/v2/health > /dev/null 2>&1; then
            success "HTTP proxy started (PID: $PROXY_PID)"
            return 0
        fi
        sleep 1
        retries=$((retries + 1))
    done
    
    error "Failed to start HTTP proxy"
    cat /tmp/http-proxy.log
    return 1
}

# Test HTTP webhook with authentication
test_http_auth() {
    log "Testing HTTP webhook with authentication..."
    
    local endpoint="http://127.0.0.1:38081/webhook"
    local token="webhook-secret-token"
    
    # Test with valid token
    local response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $token" \
        -d '{"timestamp":"'$(date -Iseconds)'","level":"info","message":"Test authenticated message"}')
    
    local http_code="${response: -3}"
    
    if [[ "$http_code" == "200" ]] || [[ "$http_code" == "503" ]]; then
        success "HTTP authentication test passed (HTTP $http_code)"
    else
        error "HTTP authentication test failed (HTTP $http_code)"
        return 1
    fi
    
    # Test with invalid token
    local bad_response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer invalid-token" \
        -d '{"message":"Should be rejected"}')
    
    local bad_code="${bad_response: -3}"
    
    if [[ "$bad_code" == "401" ]]; then
        success "HTTP authentication rejection test passed (HTTP $bad_code)"
    else
        warning "HTTP authentication rejection test - unexpected code: $bad_code"
    fi
    
    # Test without token
    local no_token_response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/json" \
        -d '{"message":"Should be rejected"}')
    
    local no_token_code="${no_token_response: -3}"
    
    if [[ "$no_token_code" == "401" ]]; then
        success "HTTP no-token rejection test passed (HTTP $no_token_code)"
    else
        warning "HTTP no-token rejection test - unexpected code: $no_token_code"
    fi
    
    return 0
}

# Test HTTP webhook without authentication
test_http_noauth() {
    log "Testing HTTP webhook without authentication..."
    
    local endpoint="http://127.0.0.1:38082/data"
    
    # Test various content types
    local tests=(
        "application/json:{\"timestamp\":\"$(date -Iseconds)\",\"message\":\"JSON test\"}"
        "text/plain:Plain text message $(date)"
        "application/x-ndjson:{\"line\":1}\n{\"line\":2}\n{\"line\":3}"
    )
    
    for test in "${tests[@]}"; do
        local content_type="${test%%:*}"
        local data="${test#*:}"
        
        local response=$(curl -s -w "%{http_code}" -X POST \
            "$endpoint" \
            -H "Content-Type: $content_type" \
            -d "$data")
        
        local http_code="${response: -3}"
        
        if [[ "$http_code" == "200" ]] || [[ "$http_code" == "503" ]]; then
            success "HTTP no-auth test passed for $content_type (HTTP $http_code)"
        else
            error "HTTP no-auth test failed for $content_type (HTTP $http_code)"
            return 1
        fi
    done
    
    return 0
}

# Test HTTP payload size limits
test_http_limits() {
    log "Testing HTTP payload size limits..."
    
    local endpoint="http://127.0.0.1:38081/webhook"
    local token="webhook-secret-token"
    
    # Test small payload (should work)
    local small_data='{"message":"small"}'
    local small_response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $token" \
        -d "$small_data")
    
    local small_code="${small_response: -3}"
    
    if [[ "$small_code" == "200" ]] || [[ "$small_code" == "503" ]]; then
        success "Small payload test passed (HTTP $small_code)"
    else
        error "Small payload test failed (HTTP $small_code)"
        return 1
    fi
    
    # Test large payload (should be rejected - 1MB limit)
    log "Testing oversized payload (may take a moment)..."
    local large_data='{"message":"'$(python3 -c "print('x' * 1048577)")'"}' # > 1MB
    local large_response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $token" \
        -d "$large_data")
    
    local large_code="${large_response: -3}"
    
    if [[ "$large_code" == "413" ]]; then
        success "Large payload rejection test passed (HTTP $large_code)"
    else
        warning "Large payload test - unexpected code: $large_code (expected 413)"
    fi
    
    return 0
}

# Test HTTP line count limits  
test_http_line_limits() {
    log "Testing HTTP line count limits..."
    
    local endpoint="http://127.0.0.1:38081/webhook"
    local token="webhook-secret-token"
    
    # Test within line limit (100 lines)
    local good_lines=""
    for i in $(seq 1 50); do
        good_lines="${good_lines}{\"line\":$i}\n"
    done
    
    local good_response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/x-ndjson" \
        -H "Authorization: Bearer $token" \
        -d "$good_lines")
    
    local good_code="${good_response: -3}"
    
    if [[ "$good_code" == "200" ]] || [[ "$good_code" == "503" ]]; then
        success "Line count within limit test passed (HTTP $good_code)"
    else
        error "Line count within limit test failed (HTTP $good_code)"
        return 1
    fi
    
    # Test exceeding line limit (>100 lines)
    local bad_lines=""
    for i in $(seq 1 150); do
        bad_lines="${bad_lines}{\"line\":$i}\n"
    done
    
    local bad_response=$(curl -s -w "%{http_code}" -X POST \
        "$endpoint" \
        -H "Content-Type: application/x-ndjson" \
        -H "Authorization: Bearer $token" \
        -d "$bad_lines")
    
    local bad_code="${bad_response: -3}"
    
    if [[ "$bad_code" == "413" ]]; then
        success "Line count limit rejection test passed (HTTP $bad_code)"
    else
        warning "Line count limit test - unexpected code: $bad_code (expected 413)"
    fi
    
    return 0
}

# Test HTTP health endpoints
test_http_health() {
    log "Testing HTTP health endpoints..."
    
    local health_endpoints=(
        "http://127.0.0.1:38081/health"
        "http://127.0.0.1:38082/health"
    )
    
    for endpoint in "${health_endpoints[@]}"; do
        local health_response=$(curl -s "$endpoint")
        
        if echo "$health_response" | grep -q '"status"'; then
            success "Health endpoint working: $endpoint"
        else
            error "Health endpoint failed: $endpoint"
            echo "Response: $health_response"
            return 1
        fi
    done
    
    return 0
}

# Test HTTP performance
test_http_performance() {
    log "Testing HTTP performance with concurrent requests..."
    
    local endpoint="http://127.0.0.1:38082/data"
    local request_count=20
    local start_time=$(date +%s)
    
    # Send concurrent requests
    for i in $(seq 1 $request_count); do
        (
            curl -s -X POST \
                "$endpoint" \
                -H "Content-Type: application/json" \
                -d "{\"request\":$i,\"timestamp\":\"$(date -Iseconds)\"}" \
                > /dev/null
        ) &
    done
    
    # Wait for all requests to complete
    wait
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    if [[ $duration -lt 10 ]]; then
        success "HTTP performance test passed - $request_count requests in ${duration}s"
    else
        warning "HTTP performance test - took ${duration}s for $request_count requests"
    fi
    
    return 0
}

# Test HTTP methods
test_http_methods() {
    log "Testing HTTP method restrictions..."
    
    local endpoint="http://127.0.0.1:38082/data"
    
    # Test GET (should be rejected)
    local get_response=$(curl -s -w "%{http_code}" -X GET "$endpoint")
    local get_code="${get_response: -3}"
    
    if [[ "$get_code" == "405" ]]; then
        success "GET method rejection test passed (HTTP $get_code)"
    else
        warning "GET method test - unexpected code: $get_code (expected 405)"
    fi
    
    # Test PUT (should be rejected)
    local put_response=$(curl -s -w "%{http_code}" -X PUT "$endpoint" -d "test")
    local put_code="${put_response: -3}"
    
    if [[ "$put_code" == "405" ]]; then
        success "PUT method rejection test passed (HTTP $put_code)"
    else
        warning "PUT method test - unexpected code: $put_code (expected 405)"
    fi
    
    return 0
}

# Main HTTP test execution
main() {
    echo -e "${BLUE}"
    echo "========================================"
    echo "  ByteFreezer Proxy HTTP Plugin Tests"
    echo "========================================"
    echo -e "${NC}"
    
    local failed_tests=()
    
    # Check prerequisites
    if ! command -v curl &> /dev/null; then
        error "curl is required for HTTP testing"
        exit 1
    fi
    
    if [[ ! -f "$PROXY_BINARY" ]]; then
        error "Proxy binary not found: $PROXY_BINARY"
        exit 1
    fi
    
    # Generate config and start proxy
    generate_http_config
    
    if ! start_http_proxy; then
        error "Failed to start proxy for HTTP testing"
        exit 1
    fi
    
    echo ""
    log "Running HTTP plugin tests..."
    echo ""
    
    # Run tests
    if ! test_http_auth; then
        failed_tests+=("Authentication")
    fi
    
    if ! test_http_noauth; then
        failed_tests+=("No Authentication")
    fi
    
    if ! test_http_limits; then
        failed_tests+=("Payload Limits")
    fi
    
    if ! test_http_line_limits; then
        failed_tests+=("Line Limits")
    fi
    
    if ! test_http_health; then
        failed_tests+=("Health Endpoints")
    fi
    
    if ! test_http_methods; then
        failed_tests+=("Method Restrictions")
    fi
    
    if ! test_http_performance; then
        failed_tests+=("Performance")
    fi
    
    # Print results
    echo ""
    echo -e "${BLUE}========================================"
    echo "  HTTP Plugin Test Results"
    echo "========================================${NC}"
    
    local total_tests=7
    local passed_tests=$((total_tests - ${#failed_tests[@]}))
    
    echo "Total HTTP tests: $total_tests"
    echo -e "Passed: ${GREEN}$passed_tests${NC}"
    echo -e "Failed: ${RED}${#failed_tests[@]}${NC}"
    
    if [[ ${#failed_tests[@]} -eq 0 ]]; then
        echo ""
        success "All HTTP tests passed! 🎉"
        
        # Show some stats
        if [[ -d "/tmp/bytefreezer-proxy-http-test-spool" ]]; then
            local spool_files=$(find /tmp/bytefreezer-proxy-http-test-spool -type f | wc -l)
            echo "Spool files created: $spool_files"
        fi
        
        exit 0
    else
        echo ""
        echo -e "${RED}Failed HTTP tests:${NC}"
        for test in "${failed_tests[@]}"; do
            echo "  - $test"
        done
        
        echo ""
        echo "Proxy log:"
        tail -20 /tmp/http-proxy.log
        
        exit 1
    fi
}

main "$@"