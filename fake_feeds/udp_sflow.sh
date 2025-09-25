#!/bin/bash
# sFlow UDP Feed Generator for ByteFreezer Proxy Testing
# Simulates sFlow packets with realistic network monitoring data

HOST="${1:-localhost}"
PORT="${2:-2056}"
COUNT="${3:-10}"
DELAY="${4:-1}"

echo "Sending $COUNT fake sFlow packets to $HOST:$PORT (${DELAY}s interval)"
echo "Data hint: sflow (binary network monitoring data)"
echo ""

for i in $(seq 1 $COUNT); do
    # Generate fake sFlow packet data (simulated binary sFlow v5 format)
    # sFlow header + flow samples + counter samples

    # Sample sFlow v5 packet structure (simplified for testing)
    # - sFlow version (4 bytes): 00000005 (version 5)
    # - Agent address type (4 bytes): 00000001 (IPv4)
    # - Agent address (4 bytes): C0A80101 (192.168.1.1)
    # - Sub-agent ID (4 bytes): 00000001
    # - Sequence number (4 bytes): incremental
    # - System uptime (4 bytes): current timestamp
    # - Number of samples (4 bytes): 00000001
    # - Sample data follows...

    TIMESTAMP=$(date +%s)
    SEQ_NUM=$(printf "%08x" $i)

    # Create binary sFlow packet (hex encoded for transmission)
    SFLOW_PACKET="00000005000000010a000001000000${SEQ_NUM}$(printf "%08x" $TIMESTAMP)000000010000000100000001000005dc00000000000000000000000a00000164000000010000001400000000000000000000000000000000"

    # Convert hex to binary and send via UDP
    echo -n "$SFLOW_PACKET" | xxd -r -p | nc -u -w1 $HOST $PORT

    # Also send a human-readable version for debugging
    echo "sFlow Sample $i: Agent=10.0.0.1, Seq=$i, Uptime=${TIMESTAMP}, Interfaces=1, Packets=100" | nc -u -w1 $HOST $PORT

    echo "[$i/$COUNT] Sent sFlow packet #$i (seq=$SEQ_NUM, timestamp=$TIMESTAMP)"

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending $COUNT sFlow packets"
echo "Configure your proxy with data_hint: \"sflow\" to process these packets"