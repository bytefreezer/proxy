#!/bin/bash

# Test script to verify DLQ functionality
echo "Testing DLQ functionality..."

# Start the proxy in the background
echo "Starting bytefreezer-proxy..."
./bytefreezer-proxy &
PROXY_PID=$!

# Give it time to start
sleep 3

echo ""
echo "=== Initial DLQ Stats ==="
curl -s http://localhost:8088/api/v2/dlq/stats | jq '.'

echo ""
echo "=== Files currently in DLQ ==="
curl -s http://localhost:8088/api/v2/dlq/files | jq '.'

echo ""
echo "=== Retrying DLQ files for customer-1/web-data ==="
curl -s -X POST http://localhost:8088/api/v2/dlq/retry \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "customer-1", "dataset_id": "web-data"}' | jq '.'

echo ""
echo "=== DLQ Stats after retry ==="
curl -s http://localhost:8088/api/v2/dlq/stats | jq '.'

echo ""
echo "=== Files in DLQ after retry ==="
curl -s http://localhost:8088/api/v2/dlq/files | jq '.'

# Stop the proxy
echo ""
echo "Stopping bytefreezer-proxy..."
kill $PROXY_PID
wait $PROXY_PID 2>/dev/null

echo "Test complete!"