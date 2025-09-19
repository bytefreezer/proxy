#!/bin/bash

echo "🧪 Testing URL Connectivity"
echo "============================"

# Check if proxy is running
if ! pgrep -x "bytefreezer-proxy" > /dev/null; then
    echo "Starting bytefreezer-proxy..."
    ./bytefreezer-proxy &
    PROXY_PID=$!
    sleep 3
    echo "Proxy started with PID: $PROXY_PID"
else
    echo "Proxy already running"
    PROXY_PID=""
fi

echo ""
echo "🔍 Testing Proxy → Receiver Connectivity"

# Test the connectivity API
echo ""
echo "📡 Running automatic connectivity test..."
RESPONSE=$(curl -s -X POST http://localhost:8088/api/v2/test/connectivity -H "Content-Type: application/json" -d '{}')

echo "Response:"
echo "$RESPONSE" | jq '.'

# Extract status information
SUCCESS_COUNT=$(echo "$RESPONSE" | jq -r '.summary.success_count // 0')
TOTAL_TESTS=$(echo "$RESPONSE" | jq -r '.summary.total_tests // 0')

echo ""
if [ "$SUCCESS_COUNT" -eq "$TOTAL_TESTS" ] && [ "$TOTAL_TESTS" -gt 0 ]; then
    echo "✅ ALL CONNECTIVITY TESTS PASSED ($SUCCESS_COUNT/$TOTAL_TESTS)"
    echo "   Proxy → Receiver URL format is working correctly!"
else
    echo "❌ Some connectivity tests failed ($SUCCESS_COUNT/$TOTAL_TESTS)"
    echo "   Check receiver service status or configuration"
fi

# Show the actual URLs being tested
echo ""
echo "📋 URLs being tested:"
echo "$RESPONSE" | jq -r '.results[]? | "   \(.plugin_name): \(.receiver_url)"'

# Cleanup
if [ -n "$PROXY_PID" ]; then
    echo ""
    echo "Stopping proxy..."
    kill $PROXY_PID 2>/dev/null
    wait $PROXY_PID 2>/dev/null
fi

echo ""
echo "🏁 Test complete!"