#!/bin/bash
# Real sFlow Test Script for ByteFreezer Proxy
# Uses the Python sFlow generator to send properly formatted sFlow v5 packets

HOST="${1:-localhost}"
PORT="${2:-2066}"
RATE="${3:-10}"
DURATION="${4:-5}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Testing sFlow plugin with real sFlow v5 packets"
echo "Target: $HOST:$PORT"
echo "Rate: $RATE packets/second"
echo "Duration: $DURATION seconds"
echo ""

# Check if Python 3 is available
if ! command -v python3 &> /dev/null; then
    echo "Error: python3 is required but not installed"
    exit 1
fi

# Run the Python sFlow generator
python3 "$SCRIPT_DIR/sflow_generator.py" \
    --host "$HOST" \
    --port "$PORT" \
    --rate "$RATE" \
    --duration "$DURATION"
