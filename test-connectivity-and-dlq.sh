#!/bin/bash

# Comprehensive test script for proxy features
echo "Testing ByteFreezer Proxy - Connectivity & DLQ Features"
echo "======================================================="

# Start the proxy in the background
echo "Starting bytefreezer-proxy..."
./bytefreezer-proxy &
PROXY_PID=$!

# Give it time to start
sleep 3

echo ""
echo "=== Health Check ==="
curl -s http://localhost:8088/api/v2/health | jq '.'

echo ""
echo "=== Configuration ==="
curl -s http://localhost:8088/api/v2/config | jq '.'

echo ""
echo "=== Testing Connectivity to Receiver ==="
echo "Testing all configured plugins/tenants/tokens..."
curl -s -X POST http://localhost:8088/api/v2/test/connectivity \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.'

echo ""
echo "=== Testing Specific Connectivity ==="
echo "Testing specific tenant/dataset: customer-1/ebpf-data"
curl -s -X POST http://localhost:8088/api/v2/test/connectivity \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "customer-1", "dataset_id": "ebpf-data"}' | jq '.'

echo ""
echo "=== DLQ Stats ==="
curl -s http://localhost:8088/api/v2/dlq/stats | jq '.'

echo ""
echo "=== Files in DLQ ==="
curl -s http://localhost:8088/api/v2/dlq/files | jq '.'

if [ $(curl -s http://localhost:8088/api/v2/dlq/stats | jq '.total_files_in_dlq') -gt 0 ]; then
    echo ""
    echo "=== Retrying DLQ files ==="
    curl -s -X POST http://localhost:8088/api/v2/dlq/retry \
      -H "Content-Type: application/json" \
      -d '{}' | jq '.'

    echo ""
    echo "=== DLQ Stats after retry ==="
    curl -s http://localhost:8088/api/v2/dlq/stats | jq '.'
fi

echo ""
echo "=== API Documentation ==="
echo "API documentation available at: http://localhost:8088/v2/docs"

# Stop the proxy
echo ""
echo "Stopping bytefreezer-proxy..."
kill $PROXY_PID
wait $PROXY_PID 2>/dev/null

echo ""
echo "Test complete!"
echo ""
echo "Summary of new features:"
echo "  ✓ Enhanced error logging from receiver uploads"
echo "  ✓ Connectivity testing API for all plugins/tenants/tokens"
echo "  ✓ Fixed DLQ metadata cleanup bug"
echo "  ✓ DLQ retry functionality with counter reset"
echo ""
echo "Available API endpoints:"
echo "  POST /api/v2/test/connectivity    - Test receiver connectivity"
echo "  GET  /api/v2/dlq/stats           - View DLQ statistics"
echo "  GET  /api/v2/dlq/files           - List DLQ files"
echo "  POST /api/v2/dlq/retry           - Retry DLQ files"
echo "  GET  /api/v2/health              - Health check"
echo "  GET  /api/v2/config              - View configuration"