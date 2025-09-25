#!/bin/bash
# Master Test Script - Run All Fake Feed Tests
# Tests all data format hints supported by ByteFreezer Proxy

HOST="${1:-localhost}"
UDP_PORT="${2:-2056}"
HTTP_PORT="${3:-8081}"
COUNT="${4:-5}"

echo "ByteFreezer Proxy - Comprehensive Data Format Testing"
echo "======================================================"
echo "Target: UDP $HOST:$UDP_PORT, HTTP $HOST:$HTTP_PORT"
echo "Count: $COUNT messages per format"
echo ""

# Check if proxy is responding
echo "🔍 Checking if ByteFreezer Proxy is running..."
if ! nc -z $HOST $UDP_PORT 2>/dev/null; then
    echo "⚠️  Warning: UDP port $UDP_PORT appears to be closed"
fi

if ! curl -s -f "http://$HOST:8088/health" > /dev/null 2>&1; then
    echo "⚠️  Warning: Proxy API (port 8088) not responding"
fi

echo ""

# Run all UDP tests
echo "🚀 Starting UDP Protocol Tests..."
echo "================================="

echo ""
echo "📊 Testing sFlow (Binary Network Monitoring)..."
./fake_feeds/udp_sflow.sh $HOST $UDP_PORT $COUNT 1

echo ""
echo "🌊 Testing NetFlow (Binary Flow Data)..."
./fake_feeds/udp_netflow.sh $HOST $UDP_PORT $COUNT 1

echo ""
echo "📋 Testing Syslog (Structured System Logs)..."
./fake_feeds/udp_syslog.sh $HOST $UDP_PORT $COUNT 1

echo ""
echo "📝 Testing NDJSON (Structured Application Logs)..."
./fake_feeds/udp_ndjson.sh $HOST $UDP_PORT $COUNT 1

echo ""
echo "📄 Testing Raw (Legacy/Unstructured Data)..."
./fake_feeds/udp_raw.sh $HOST $UDP_PORT $COUNT 1

echo ""
echo "🌐 Starting HTTP Protocol Tests..."
echo "=================================="

echo ""
echo "📡 Testing HTTP NDJSON (Webhook Events)..."
./fake_feeds/http_ndjson.sh $HOST $HTTP_PORT /webhook $COUNT 1

echo ""
echo "✅ Comprehensive Testing Complete!"
echo "=================================="
echo "Summary:"
echo "  • sFlow packets: $COUNT (binary network monitoring)"
echo "  • NetFlow packets: $COUNT (binary flow data)"
echo "  • Syslog messages: $COUNT (structured system logs)"
echo "  • NDJSON messages: $COUNT (structured application logs)"
echo "  • Raw messages: $COUNT (legacy/unstructured data)"
echo "  • HTTP NDJSON requests: $COUNT (webhook events)"
echo ""
echo "Total messages sent: $((COUNT * 6))"
echo ""
echo "Next Steps:"
echo "1. Check proxy logs for ingestion confirmation"
echo "2. Monitor /api/v2/stats endpoint for processing metrics"
echo "3. Verify data reaches ByteFreezer receiver"
echo "4. Confirm data_hint processing in downstream components"

# Show some useful monitoring commands
echo ""
echo "Monitoring Commands:"
echo "  Proxy stats: curl http://$HOST:8088/api/v2/stats"
echo "  Proxy health: curl http://$HOST:8088/health"
echo "  DLQ status: curl http://$HOST:8088/api/v2/dlq/stats"
echo "  Plugin health: curl http://$HOST:8088/api/v2/plugins/health"