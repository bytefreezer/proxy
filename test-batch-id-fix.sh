#!/bin/bash

echo "🧪 Testing Batch ID Consistency Fix"
echo "=================================="

# Start proxy in background
echo "Starting proxy..."
./bytefreezer-proxy > /tmp/proxy.log 2>&1 &
PROXY_PID=$!
sleep 3

echo "Proxy started with PID: $PROXY_PID"

# Send some UDP data to trigger batch processing
echo "Sending test UDP data..."
echo '{"test": "data", "timestamp": "'$(date -Iseconds)'"}' | nc -u localhost 2056
sleep 2

# Check logs for batch ID consistency
echo ""
echo "📋 Checking batch ID consistency in logs..."
STORED_BATCH=$(grep "Stored failed batch" /tmp/proxy.log | tail -1 | grep -o "batch_[0-9]*" || echo "")
UPLOAD_BATCH=$(grep "Immediate upload successful" /tmp/proxy.log | tail -1 | grep -o "batch_[0-9]*" || echo "")
WARNING_BATCH=$(grep "No data file found for batch" /tmp/proxy.log | tail -1 | grep -o "batch_[0-9]*" || echo "")

echo "Stored batch ID:    $STORED_BATCH"
echo "Upload batch ID:    $UPLOAD_BATCH"
echo "Warning batch ID:   $WARNING_BATCH"

# Check for the warning message
if grep -q "No data file found for batch" /tmp/proxy.log; then
    echo ""
    echo "❌ ISSUE STILL EXISTS: Warning message found"
    echo "The batch ID mismatch issue is still present"
else
    echo ""
    echo "✅ ISSUE FIXED: No batch ID mismatch warnings"
    echo "Batch IDs are now consistent"
fi

# Show recent relevant log lines
echo ""
echo "📄 Recent relevant log entries:"
grep -E "(Stored failed batch|Immediate upload|No data file found)" /tmp/proxy.log | tail -5

# Cleanup
echo ""
echo "Stopping proxy..."
kill $PROXY_PID 2>/dev/null
wait $PROXY_PID 2>/dev/null

echo ""
echo "🏁 Test complete!"