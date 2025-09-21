#!/bin/bash

# Test Script: ByteFreezer Proxy Trigger Reason Functionality
# This script demonstrates how to test and verify trigger reason tracking

set -e

echo "🔍 ByteFreezer Proxy - Trigger Reason Testing Script"
echo "=================================================="

# Configuration
SPOOL_DIR="/tmp/bytefreezer-proxy-test"
CONFIG_FILE="/tmp/test-config.yaml"

echo "📋 Setting up test environment..."

# Clean up any previous test data
rm -rf "$SPOOL_DIR" "$CONFIG_FILE"

# Create test configuration with trigger reason optimized settings
cat > "$CONFIG_FILE" << 'EOF'
app:
  name: bytefreezer-proxy
  version: 0.0.2

logging:
  level: info
  encoding: console

server:
  api_port: 8088

inputs:
  - type: "udp"
    name: "test-udp-listener"
    config:
      host: "127.0.0.1"
      port: 2056
      dataset_id: "test-data"
      protocol: "udp"
      read_buffer_size: 65536
      worker_count: 1

tenant_id: "test-tenant"
bearer_token: "test-token-123"

receiver:
  base_url: "http://localhost:9999/{tenantid}/{datasetid}"  # Intentionally unreachable for testing
  timeout_seconds: 5
  upload_worker_count: 2
  max_idle_conns: 10
  max_conns_per_host: 6

# Optimized for testing different trigger reasons
batching:
  enabled: true
  max_lines: 0          # Disabled - only size and timeout triggers
  max_bytes: 1024       # Small size for testing size_limit_reached
  timeout_seconds: 3    # Short timeout for testing timeout triggers
  compression_enabled: true
  compression_level: 6

spooling:
  enabled: true
  directory: "$SPOOL_DIR"
  max_size_bytes: 10485760
  retry_attempts: 2
  retry_interval_seconds: 5
  cleanup_interval_seconds: 30

housekeeping:
  enabled: true
  intervalseconds: 10

dev: true
EOF

echo "✅ Test configuration created at $CONFIG_FILE"

echo "📊 Expected Trigger Reasons for Testing:"
echo "  - timeout: Batches triggered after 3 seconds"
echo "  - size_limit_reached: Batches triggered after 1024 bytes"
echo "  - service_shutdown: Batches triggered during graceful shutdown"
echo "  - single_message: Individual messages (if batching disabled)"
echo "  - service_restart: Recovered files on startup"

echo ""
echo "🚀 Test Instructions:"
echo ""
echo "1. Start the proxy with test config:"
echo "   ./bytefreezer-proxy --config $CONFIG_FILE"
echo ""
echo "2. Send test data to trigger different scenarios:"
echo ""
echo "   # Test timeout trigger (wait 3+ seconds between sends)"
echo "   echo '{\"test\": \"timeout_trigger\", \"size\": 100}' | nc -u 127.0.0.1 2056"
echo "   sleep 4  # Wait for timeout"
echo ""
echo "   # Test size_limit_reached trigger (send >1024 bytes quickly)"
echo "   for i in {1..10}; do"
echo "     echo '{\"test\": \"size_trigger\", \"data\": \"'$(head -c 200 /dev/zero | tr '\0' 'x')'\"}'  | nc -u 127.0.0.1 2056"
echo "   done"
echo ""
echo "   # Test service_shutdown trigger"
echo "   pkill -TERM bytefreezer-proxy  # Graceful shutdown"
echo ""
echo "3. Examine trigger reasons in spool metadata:"
echo "   find $SPOOL_DIR -name '*.meta' -exec echo '=== {} ===' \\; -exec jq '.trigger_reason, .failure_reason' {} \\;"
echo ""
echo "4. Analyze trigger distribution:"
echo "   echo 'Trigger Reason Distribution:'"
echo "   find $SPOOL_DIR -name '*.meta' -exec jq -r '.trigger_reason' {} \\; | sort | uniq -c"
echo ""
echo "5. Check specific trigger types:"
echo "   echo 'Timeout triggers:'"
echo "   grep -l '\"trigger_reason\":\"timeout\"' $SPOOL_DIR/*/*/retry/*.meta || echo 'None found'"
echo "   echo 'Size limit triggers:'"
echo "   grep -l '\"trigger_reason\":\"size_limit_reached\"' $SPOOL_DIR/*/*/retry/*.meta || echo 'None found'"

echo ""
echo "📈 Monitoring Commands:"
echo ""
echo "# Watch spool directory for new files"
echo "watch -n 1 'find $SPOOL_DIR -name \"*.meta\" | wc -l'"
echo ""
echo "# Real-time trigger reason monitoring"
echo "watch -n 2 'find $SPOOL_DIR -name \"*.meta\" -exec jq -r \".trigger_reason\" {} \\; 2>/dev/null | sort | uniq -c'"
echo ""
echo "# View latest metadata file"
echo "find $SPOOL_DIR -name '*.meta' -printf '%T@ %p\\n' | sort -n | tail -1 | cut -d' ' -f2- | xargs jq ."

echo ""
echo "🔧 Configuration Tuning Based on Results:"
echo "- If mostly 'timeout' triggers: Consider increasing max_bytes or decreasing timeout_seconds"
echo "- If mostly 'size_limit_reached' triggers: Consider increasing max_bytes if appropriate"
echo "- If many 'service_restart' triggers: Investigate unclean shutdowns"
echo "- If unexpected patterns: Review data volume and timing"

echo ""
echo "✅ Test environment ready!"
echo "📁 Spool directory: $SPOOL_DIR"
echo "⚙️  Config file: $CONFIG_FILE"
echo ""
echo "Run: ./bytefreezer-proxy --config $CONFIG_FILE"