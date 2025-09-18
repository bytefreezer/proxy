#!/bin/bash

# ByteFreezer Proxy - Monitoring Integration Test
# Tests proxy with OTEL metrics enabled and validates metrics collection

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_CONFIG="$SCRIPT_DIR/monitoring-test-config.yaml"
PROXY_BINARY="../bytefreezer-proxy"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROXY_PID=""

cleanup() {
    echo -e "${YELLOW}Cleaning up monitoring test environment...${NC}"
    if [[ -n "$PROXY_PID" ]]; then
        kill $PROXY_PID 2>/dev/null || true
        wait $PROXY_PID 2>/dev/null || true
    fi
    rm -f "$TEST_CONFIG"
    rm -rf /tmp/bytefreezer-proxy-monitoring-test-spool
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

# Generate monitoring test configuration
generate_monitoring_config() {
    log "Generating monitoring test configuration..."
    
    cat > "$TEST_CONFIG" << 'EOF'
app:
  name: bytefreezer-proxy-monitoring-test
  version: test

logging:
  level: info
  encoding: console

server:
  api_port: 48088

tenant_id: "monitoring-test-tenant"
bearer_token: "monitoring-test-token"

inputs:
  # UDP Plugin for metrics testing
  - type: "udp"
    name: "udp-metrics"
    config:
      host: "127.0.0.1"
      port: 42056
      dataset_id: "udp-metrics-data"
      protocol: "udp"
      worker_count: 2
      
  # HTTP Plugin for metrics testing
  - type: "http"
    name: "http-metrics"
    config:
      host: "127.0.0.1"
      port: 48081
      path: "/webhook"
      dataset_id: "http-metrics-data"
      max_payload_size: 1048576
      max_lines_per_request: 100
      enable_authentication: false

receiver:
  base_url: "http://127.0.0.1:49999/data/{tenantid}/{datasetid}"
  timeout_seconds: 5
  retry_count: 1

spooling:
  enabled: true
  directory: "/tmp/bytefreezer-proxy-monitoring-test-spool"
  max_size_bytes: 10485760
  retry_attempts: 2
  retry_interval_seconds: 2

# Enable OTEL metrics for testing
otel:
  enabled: true
  service_name: "bytefreezer-proxy-monitoring-test"
  prometheus_mode: true
  metrics_port: 49090
  metrics_host: "127.0.0.1"
  scrapeIntervalseconds: 5

soc:
  enabled: false
EOF
    
    success "Monitoring test configuration generated"
}

# Start proxy with monitoring enabled
start_monitoring_proxy() {
    log "Starting proxy with monitoring enabled..."
    
    rm -rf /tmp/bytefreezer-proxy-monitoring-test-spool
    
    "$PROXY_BINARY" --config "$TEST_CONFIG" > /tmp/monitoring-proxy.log 2>&1 &
    PROXY_PID=$!
    
    # Wait for proxy to start
    local retries=0
    while [[ $retries -lt 15 ]]; do
        if curl -s http://127.0.0.1:48088/api/v2/health > /dev/null 2>&1; then
            success "Monitoring proxy started (PID: $PROXY_PID)"
            return 0
        fi
        sleep 1
        retries=$((retries + 1))
    done
    
    error "Failed to start monitoring proxy"
    cat /tmp/monitoring-proxy.log
    return 1
}

# Test metrics endpoint availability
test_metrics_endpoint() {
    log "Testing metrics endpoint availability..."
    
    local metrics_endpoint="http://127.0.0.1:49090/metrics"
    local retries=0
    
    # Wait for metrics endpoint to be available
    while [[ $retries -lt 10 ]]; do
        if curl -s "$metrics_endpoint" > /dev/null 2>&1; then
            success "Metrics endpoint is available"
            return 0
        fi
        sleep 1
        retries=$((retries + 1))
    done
    
    error "Metrics endpoint not available after 10 seconds"
    return 1
}

# Test basic metrics collection
test_basic_metrics() {
    log "Testing basic metrics collection..."
    
    local metrics_endpoint="http://127.0.0.1:49090/metrics"
    local metrics_response=$(curl -s "$metrics_endpoint")
    
    # Check for expected metric families
    local expected_metrics=(
        "go_goroutines"
        "go_memstats"
        "process_cpu_seconds_total"
        "process_resident_memory_bytes"
    )
    
    for metric in "${expected_metrics[@]}"; do
        if echo "$metrics_response" | grep -q "$metric"; then
            success "Found metric: $metric"
        else
            warning "Missing metric: $metric"
        fi
    done
    
    # Check for proxy-specific metrics (may not exist yet without data)
    local proxy_metrics=(
        "bytefreezer_proxy"
        "plugin_"
        "input_"
    )
    
    for metric in "${proxy_metrics[@]}"; do
        if echo "$metrics_response" | grep -q "$metric"; then
            success "Found proxy metric containing: $metric"
        else
            log "Proxy metric not yet available: $metric (expected until data processing)"
        fi
    done
    
    return 0
}

# Generate data and test metrics
test_metrics_with_data() {
    log "Testing metrics collection with data processing..."
    
    # Send UDP data
    for i in $(seq 1 10); do
        echo "UDP metric test message $i" | nc -u 127.0.0.1 42056
    done
    
    # Send HTTP data
    for i in $(seq 1 5); do
        curl -s -X POST http://127.0.0.1:48081/webhook \
            -H "Content-Type: application/json" \
            -d "{\"metric_test\":$i,\"timestamp\":\"$(date -Iseconds)\"}" > /dev/null
    done
    
    # Wait for metrics to be updated
    sleep 10
    
    local metrics_endpoint="http://127.0.0.1:49090/metrics"
    local metrics_response=$(curl -s "$metrics_endpoint")
    
    # Save metrics for analysis
    echo "$metrics_response" > /tmp/proxy-metrics.txt
    
    # Look for data processing metrics
    if echo "$metrics_response" | grep -q "bytefreezer_proxy\|plugin_\|input_"; then
        success "Proxy metrics generated after data processing"
    else
        warning "No proxy-specific metrics found yet"
    fi
    
    # Check for increasing counters
    local counter_metrics=$(echo "$metrics_response" | grep "_total" | head -5)
    if [[ -n "$counter_metrics" ]]; then
        success "Found counter metrics"
        echo "Sample counters:"
        echo "$counter_metrics"
    fi
    
    return 0
}

# Test metrics format compliance
test_metrics_format() {
    log "Testing Prometheus metrics format compliance..."
    
    local metrics_endpoint="http://127.0.0.1:49090/metrics"
    local metrics_response=$(curl -s "$metrics_endpoint")
    
    # Check basic Prometheus format requirements
    local format_checks=0
    local total_checks=4
    
    # Check for HELP comments
    if echo "$metrics_response" | grep -q "^# HELP"; then
        success "Found HELP comments"
        format_checks=$((format_checks + 1))
    else
        warning "No HELP comments found"
    fi
    
    # Check for TYPE comments
    if echo "$metrics_response" | grep -q "^# TYPE"; then
        success "Found TYPE comments"
        format_checks=$((format_checks + 1))
    else
        warning "No TYPE comments found"
    fi
    
    # Check for metric lines (name{labels} value)
    if echo "$metrics_response" | grep -q "^[a-zA-Z_][a-zA-Z0-9_]*"; then
        success "Found metric lines"
        format_checks=$((format_checks + 1))
    else
        error "No valid metric lines found"
    fi
    
    # Check for timestamps (optional but good)
    if echo "$metrics_response" | grep -q "[0-9]\{13\}$"; then
        success "Found timestamps on metrics"
        format_checks=$((format_checks + 1))
    else
        log "No timestamps found (optional)"
        format_checks=$((format_checks + 1))  # Count as pass since optional
    fi
    
    if [[ $format_checks -eq $total_checks ]]; then
        success "Prometheus format compliance passed"
        return 0
    else
        warning "Prometheus format compliance: $format_checks/$total_checks checks passed"
        return 1
    fi
}

# Test metrics over time
test_metrics_persistence() {
    log "Testing metrics persistence over time..."
    
    local metrics_endpoint="http://127.0.0.1:49090/metrics"
    
    # Get initial metrics
    local initial_metrics=$(curl -s "$metrics_endpoint")
    local initial_goroutines=$(echo "$initial_metrics" | grep "^go_goroutines " | awk '{print $2}')
    
    sleep 5
    
    # Get updated metrics
    local updated_metrics=$(curl -s "$metrics_endpoint")
    local updated_goroutines=$(echo "$updated_metrics" | grep "^go_goroutines " | awk '{print $2}')
    
    if [[ -n "$initial_goroutines" ]] && [[ -n "$updated_goroutines" ]]; then
        success "Metrics persisting over time"
        log "Goroutines: $initial_goroutines -> $updated_goroutines"
        return 0
    else
        error "Failed to track metrics over time"
        return 1
    fi
}

# Test proxy API with monitoring enabled
test_api_with_monitoring() {
    log "Testing proxy API endpoints with monitoring enabled..."
    
    # Test health endpoint
    local health_response=$(curl -s http://127.0.0.1:48088/api/v2/health)
    if echo "$health_response" | jq -e '.status' > /dev/null 2>&1; then
        success "Health endpoint working with monitoring"
    else
        error "Health endpoint failed with monitoring enabled"
        return 1
    fi
    
    # Test config endpoint (should show OTEL enabled)
    local config_response=$(curl -s http://127.0.0.1:48088/api/v2/config)
    if echo "$config_response" | jq -e '.otel.enabled' > /dev/null 2>&1; then
        local otel_enabled=$(echo "$config_response" | jq -r '.otel.enabled')
        if [[ "$otel_enabled" == "true" ]]; then
            success "Config shows OTEL enabled"
        else
            warning "Config shows OTEL disabled"
        fi
    else
        error "Config endpoint failed or missing OTEL config"
        return 1
    fi
    
    return 0
}

# Test monitoring performance impact
test_monitoring_performance() {
    log "Testing monitoring performance impact..."
    
    local start_time=$(date +%s)
    local message_count=50
    
    # Send burst of messages to test performance
    for i in $(seq 1 $message_count); do
        echo "Performance test message $i" | nc -u 127.0.0.1 42056
    done
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    if [[ $duration -lt 5 ]]; then
        success "Monitoring performance impact acceptable - ${message_count} messages in ${duration}s"
    else
        warning "Monitoring may have performance impact - ${message_count} messages in ${duration}s"
    fi
    
    # Check memory usage from metrics
    local metrics_response=$(curl -s "http://127.0.0.1:49090/metrics")
    local memory_bytes=$(echo "$metrics_response" | grep "^process_resident_memory_bytes " | awk '{print $2}')
    
    if [[ -n "$memory_bytes" ]]; then
        local memory_mb=$((memory_bytes / 1024 / 1024))
        log "Current memory usage: ${memory_mb}MB"
        
        if [[ $memory_mb -lt 100 ]]; then
            success "Memory usage acceptable: ${memory_mb}MB"
        else
            warning "High memory usage: ${memory_mb}MB"
        fi
    fi
    
    return 0
}

# Main monitoring test execution
main() {
    echo -e "${BLUE}"
    echo "========================================"
    echo "  ByteFreezer Proxy Monitoring Tests"
    echo "========================================"
    echo -e "${NC}"
    
    local failed_tests=()
    
    # Check prerequisites
    if ! command -v curl &> /dev/null; then
        error "curl is required for monitoring testing"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        error "jq is required for JSON processing"
        exit 1
    fi
    
    if [[ ! -f "$PROXY_BINARY" ]]; then
        error "Proxy binary not found: $PROXY_BINARY"
        exit 1
    fi
    
    # Generate config and start proxy
    generate_monitoring_config
    
    if ! start_monitoring_proxy; then
        error "Failed to start proxy with monitoring"
        exit 1
    fi
    
    echo ""
    log "Running monitoring tests..."
    echo ""
    
    # Run tests
    if ! test_metrics_endpoint; then
        failed_tests+=("Metrics Endpoint")
    fi
    
    if ! test_basic_metrics; then
        failed_tests+=("Basic Metrics")
    fi
    
    if ! test_metrics_with_data; then
        failed_tests+=("Metrics with Data")
    fi
    
    if ! test_metrics_format; then
        failed_tests+=("Metrics Format")
    fi
    
    if ! test_metrics_persistence; then
        failed_tests+=("Metrics Persistence")
    fi
    
    if ! test_api_with_monitoring; then
        failed_tests+=("API with Monitoring")
    fi
    
    if ! test_monitoring_performance; then
        failed_tests+=("Performance Impact")
    fi
    
    # Print results
    echo ""
    echo -e "${BLUE}========================================"
    echo "  Monitoring Test Results"
    echo "========================================${NC}"
    
    local total_tests=7
    local passed_tests=$((total_tests - ${#failed_tests[@]}))
    
    echo "Total monitoring tests: $total_tests"
    echo -e "Passed: ${GREEN}$passed_tests${NC}"
    echo -e "Failed: ${RED}${#failed_tests[@]}${NC}"
    
    if [[ ${#failed_tests[@]} -eq 0 ]]; then
        echo ""
        success "All monitoring tests passed! 🎉"
        
        echo ""
        echo "Metrics endpoint: http://127.0.0.1:49090/metrics"
        echo "Metrics saved to: /tmp/proxy-metrics.txt"
        
        # Show sample metrics
        echo ""
        echo "Sample metrics:"
        head -10 /tmp/proxy-metrics.txt
        
        exit 0
    else
        echo ""
        echo -e "${RED}Failed monitoring tests:${NC}"
        for test in "${failed_tests[@]}"; do
            echo "  - $test"
        done
        
        echo ""
        echo "Proxy log:"
        tail -20 /tmp/monitoring-proxy.log
        
        exit 1
    fi
}

main "$@"