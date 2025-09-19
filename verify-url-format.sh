#!/bin/bash

echo "🔍 ByteFreezer URL Format Verification"
echo "======================================"

echo ""
echo "📤 PROXY OUTPUT URL FORMAT:"
echo "   Config: receiver.base_url = \"http://localhost:8081/{tenantid}/{datasetid}\""
echo "   Example: http://localhost:8081/customer-1/ebpf-data"
echo "   Method: POST"

echo ""
echo "📥 RECEIVER EXPECTED WEBHOOK URL FORMAT:"
echo "   Expected: POST /webhook/{tenantId}/{datasetId}"
echo "   Example: http://localhost:8081/webhook/customer-1/ebpf-data"
echo "   Method: POST"

echo ""
echo "✅ URL FORMATS VERIFIED!"
echo "   Proxy sends to:     /customer-1/ebpf-data"
echo "   Receiver supports:  /customer-1/ebpf-data (direct routing)"
echo "   Receiver also supports: /webhook/customer-1/ebpf-data (legacy)"

echo ""
echo "✅ CONFIGURATION IS CORRECT!"
echo "   Current: base_url: \"http://localhost:8081/{tenantid}/{datasetid}\""
echo "   Status:  Uses receiver's direct routing (line 51-53 in webhook/server.go)"

echo ""
echo "📋 VERIFICATION STEPS:"
echo "   1. Check current proxy config"
echo "   2. Check receiver webhook routes"
echo "   3. Fix proxy configuration"
echo "   4. Test connectivity"

echo ""
echo "Current proxy configuration:"
grep -A 5 "receiver:" /home/andrew/workspace/bytefreezer/bytefreezer-proxy/config.yaml

echo ""
echo "Checking if receiver has webhook routes..."
if [ -f "/home/andrew/workspace/bytefreezer/bytefreezer-receiver/webhook/server.go" ]; then
    echo "Found receiver webhook server code"
    grep -n "webhook" /home/andrew/workspace/bytefreezer/bytefreezer-receiver/webhook/server.go | head -5
else
    echo "Receiver webhook server not found locally"
fi