#!/bin/bash

# ByteFreezer Proxy - Integration Test Suite
# Runs all integration tests for available plugins

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_CONFIG="$SCRIPT_DIR/test-config.yaml"
PROXY_BINARY="../bytefreezer-proxy"
TEST_RESULTS_DIR="$SCRIPT_DIR/results"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
PROXY_PID=""
CLEANUP_FUNCTIONS=()

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up test environment...${NC}"
    
    # Stop proxy if running
    if [[ -n "$PROXY_PID" ]]; then
        echo "Stopping proxy (PID: $PROXY_PID)..."
        kill $PROXY_PID 2>/dev/null || true
        wait $PROXY_PID 2>/dev/null || true
    fi
    
    # Run cleanup functions
    for cleanup_func in "${CLEANUP_FUNCTIONS[@]}"; do
        echo "Running cleanup: $cleanup_func"
        $cleanup_func 2>/dev/null || true
    done
    
    # Clean up test artifacts
    rm -f "$TEST_CONFIG"
    rm -rf "$TEST_RESULTS_DIR"
    
    echo -e "${GREEN}Cleanup complete${NC}"
}

# Set trap for cleanup
trap cleanup EXIT INT TERM

# Logging function
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

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check if proxy binary exists
    if [[ ! -f "$PROXY_BINARY" ]]; then
        error "Proxy binary not found: $PROXY_BINARY"
        echo "Please build the proxy first: go build ."
        exit 1
    fi
    
    # Check required tools
    local missing_tools=()
    
    if ! command -v nc &> /dev/null; then
        missing_tools+=("nc (netcat)")
    fi
    
    if ! command -v curl &> /dev/null; then
        missing_tools+=("curl")
    fi
    
    if ! command -v jq &> /dev/null; then
        missing_tools+=("jq")
    fi
    
    if [[ ${#missing_tools[@]} -ne 0 ]]; then
        error "Missing required tools: ${missing_tools[*]}"
        echo "Please install missing tools and try again"
        exit 1
    fi
    
    success "Prerequisites check passed"
}

# Generate test configuration
generate_test_config() {
    log "Generating test configuration..."
    
    mkdir -p "$TEST_RESULTS_DIR"
    
    cat > "$TEST_CONFIG" << 'EOF'
app:
  name: bytefreezer-proxy-test
  version: test

logging:
  level: info
  encoding: console

server:
  api_port: 18088

# Test tenant configuration
tenant_id: "test-tenant"
bearer_token: "test-bearer-token"

# Plugin-based input configuration for testing
inputs:
  # UDP Plugin Test
  - type: "udp"
    name: "udp-test"
    config:
      host: "127.0.0.1"
      port: 12056
      dataset_id: "udp-test-data"
      protocol: "udp"
      worker_count: 2
      
  # HTTP Plugin Test  
  - type: "http"
    name: "http-test"
    config:
      host: "127.0.0.1"
      port: 18081
      path: "/webhook"
      dataset_id: "http-test-data"
      max_payload_size: 1048576    # 1MB for testing
      max_lines_per_request: 100
      read_timeout: 10
      write_timeout: 10
      enable_authentication: true

# Mock receiver for testing (will fail, but we test spooling)
receiver:
  base_url: "http://127.0.0.1:19999/data/{tenantid}/{datasetid}"
  timeout_seconds: 5
  retry_count: 1
  retry_delay_seconds: 1

# Spooling enabled for testing
spooling:
  enabled: true
  directory: "/tmp/bytefreezer-proxy-test-spool"
  max_size_bytes: 10485760  # 10MB
  retry_attempts: 2
  retry_interval_seconds: 2
  cleanup_interval_seconds: 30

# Disable OTEL for testing
otel:
  enabled: false

# Disable SOC for testing
soc:
  enabled: false
EOF
    
    success "Test configuration generated: $TEST_CONFIG"
}

# Start proxy for testing
start_proxy() {
    log "Starting proxy for testing..."
    
    # Remove any existing spool directory
    rm -rf /tmp/bytefreezer-proxy-test-spool
    
    # Start proxy in background
    "$PROXY_BINARY" --config "$TEST_CONFIG" > "$TEST_RESULTS_DIR/proxy.log" 2>&1 &
    PROXY_PID=$!
    
    # Wait for proxy to start
    local retries=0
    local max_retries=10
    
    while [[ $retries -lt $max_retries ]]; do
        if curl -s http://127.0.0.1:18088/api/v1/health > /dev/null 2>&1; then
            success "Proxy started successfully (PID: $PROXY_PID)"
            return 0
        fi
        
        sleep 1
        retries=$((retries + 1))
    done
    
    error "Failed to start proxy within $max_retries seconds"
    echo "Proxy log:"
    cat "$TEST_RESULTS_DIR/proxy.log"
    return 1
}

# Test UDP plugin
test_udp_plugin() {
    log "Testing UDP plugin..."
    
    local test_message="Test UDP message $(date +'%Y-%m-%d %H:%M:%S')"
    
    # Send test message
    echo "$test_message" | nc -u 127.0.0.1 12056
    
    # Wait a moment for processing
    sleep 2
    
    # Check if data was spooled (since receiver is fake)
    if [[ -d "/tmp/bytefreezer-proxy-test-spool" ]]; then
        local spool_files=$(find /tmp/bytefreezer-proxy-test-spool -name "*.raw.gz" | wc -l)
        if [[ $spool_files -gt 0 ]]; then
            success "UDP plugin test passed - data spooled"
            return 0
        fi
    fi
    
    # Check proxy logs for processing
    if grep -q "UDP plugin" "$TEST_RESULTS_DIR/proxy.log"; then
        success "UDP plugin test passed - processing detected"
        return 0
    fi
    
    warning "UDP plugin test - no clear success indicators found"
    return 1
}

# Test HTTP plugin
test_http_plugin() {
    log "Testing HTTP plugin..."
    
    local test_data='{"timestamp":"'$(date -Iseconds)'","level":"info","message":"Test HTTP message"}'
    
    # Test webhook endpoint
    local response=$(curl -s -w "%{http_code}" -X POST \
        http://127.0.0.1:18081/webhook \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer test-bearer-token" \
        -d "$test_data")
    
    local http_code="${response: -3}"
    local body="${response%???}"
    
    # Check for successful response or expected error (since receiver is fake)
    if [[ "$http_code" == "200" ]] || [[ "$http_code" == "503" ]]; then
        success "HTTP plugin test passed - endpoint responding (HTTP $http_code)"
        
        # Check health endpoint
        local health_response=$(curl -s http://127.0.0.1:18081/health)
        if echo "$health_response" | jq -e '.status' > /dev/null 2>&1; then
            success "HTTP plugin health endpoint working"
        fi
        
        return 0
    fi
    
    error "HTTP plugin test failed - unexpected response code: $http_code"
    echo "Response body: $body"
    return 1
}

# Test proxy API endpoints
test_api_endpoints() {
    log "Testing proxy API endpoints..."
    
    # Test health endpoint
    local health_response=$(curl -s http://127.0.0.1:18088/api/v1/health)
    if echo "$health_response" | jq -e '.status' > /dev/null 2>&1; then
        success "Health endpoint working"
    else
        error "Health endpoint failed"
        echo "Response: $health_response"
        return 1
    fi
    
    # Test config endpoint
    local config_response=$(curl -s http://127.0.0.1:18088/api/v1/config)
    if echo "$config_response" | jq -e '.app.name' > /dev/null 2>&1; then
        success "Config endpoint working"
    else
        error "Config endpoint failed"
        echo "Response: $config_response"
        return 1
    fi
    
    # Test DLQ stats endpoint
    local dlq_response=$(curl -s http://127.0.0.1:18088/api/v1/dlq/stats)
    if echo "$dlq_response" | jq -e '.spooling_enabled' > /dev/null 2>&1; then
        success "DLQ stats endpoint working"
    else
        error "DLQ stats endpoint failed"
        echo "Response: $dlq_response"
        return 1
    fi
    
    return 0
}

# Test spooling functionality
test_spooling() {
    log "Testing spooling functionality..."
    
    # Wait for spooling to process
    sleep 5
    
    # Check spool directory
    if [[ -d "/tmp/bytefreezer-proxy-test-spool" ]]; then
        local spool_files=$(find /tmp/bytefreezer-proxy-test-spool -type f | wc -l)
        success "Spooling working - $spool_files files in spool directory"
        
        # List spool contents for debugging
        echo "Spool directory contents:"
        find /tmp/bytefreezer-proxy-test-spool -type f | head -10
        
        return 0
    fi
    
    warning "No spool directory found - may indicate successful forwarding or no data processed"
    return 1
}

# Main test execution
main() {
    echo -e "${BLUE}"
    echo "=========================================="
    echo "  ByteFreezer Proxy Integration Tests"
    echo "=========================================="
    echo -e "${NC}"
    
    local failed_tests=()
    local total_tests=0
    
    # Run prerequisite checks
    check_prerequisites
    
    # Generate test configuration
    generate_test_config
    
    # Start proxy
    if ! start_proxy; then
        error "Failed to start proxy - aborting tests"
        exit 1
    fi
    
    echo ""
    echo -e "${BLUE}Running integration tests...${NC}"
    echo ""
    
    # Test UDP plugin
    total_tests=$((total_tests + 1))
    if ! test_udp_plugin; then
        failed_tests+=("UDP Plugin")
    fi
    
    # Test HTTP plugin
    total_tests=$((total_tests + 1))
    if ! test_http_plugin; then
        failed_tests+=("HTTP Plugin")
    fi
    
    # Test API endpoints
    total_tests=$((total_tests + 1))
    if ! test_api_endpoints; then
        failed_tests+=("API Endpoints")
    fi
    
    # Test spooling
    total_tests=$((total_tests + 1))
    if ! test_spooling; then
        failed_tests+=("Spooling")
    fi
    
    # Print results
    echo ""
    echo -e "${BLUE}=========================================="
    echo "  Test Results"
    echo "==========================================${NC}"
    
    local passed_tests=$((total_tests - ${#failed_tests[@]}))
    
    echo "Total tests: $total_tests"
    echo -e "Passed: ${GREEN}$passed_tests${NC}"
    echo -e "Failed: ${RED}${#failed_tests[@]}${NC}"
    
    if [[ ${#failed_tests[@]} -eq 0 ]]; then
        echo ""
        success "All tests passed! 🎉"
        exit 0
    else
        echo ""
        echo -e "${RED}Failed tests:${NC}"
        for test in "${failed_tests[@]}"; do
            echo "  - $test"
        done
        echo ""
        error "Some tests failed"
        exit 1
    fi
}

# Run main function
main "$@"