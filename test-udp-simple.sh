#!/bin/bash
# Simple UDP Port Test Script

HOST="${1:-localhost}"
PORT="${2:-2060}"
COUNT="${3:-5}"

echo "Testing UDP port $HOST:$PORT with simple messages"
echo "================================================="

for i in $(seq 1 $COUNT); do
    MESSAGE="Simple UDP test message $i - timestamp: $(date +%Y-%m-%d_%H:%M:%S) - from $(hostname)"
    echo "Sending: $MESSAGE"
    echo "$MESSAGE" | nc -u -w1 $HOST $PORT
    echo "[$i/$COUNT] Message sent"
    sleep 1
done

echo ""
echo "✓ Sent $COUNT simple UDP messages to $HOST:$PORT"
echo "If proxy is running, check logs for these messages"