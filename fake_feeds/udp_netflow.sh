#!/bin/bash
# NetFlow UDP Feed Generator for ByteFreezer Proxy Testing
# Simulates NetFlow v5/v9 packets with realistic flow data

HOST="${1:-localhost}"
PORT="${2:-2056}"
COUNT="${3:-10}"
DELAY="${4:-2}"

echo "Sending $COUNT fake NetFlow packets to $HOST:$PORT (${DELAY}s interval)"
echo "Data hint: netflow (binary flow monitoring data)"
echo ""

for i in $(seq 1 $COUNT); do
    # Generate fake NetFlow v5 packet data
    # NetFlow v5 header + flow records

    TIMESTAMP=$(date +%s)
    SRC_IP="192.168.1.$((10 + i))"
    DST_IP="10.0.0.$((20 + i))"
    SRC_PORT=$((8000 + i))
    DST_PORT=$((i % 2 == 0 ? 80 : 443))
    PROTOCOL=$((i % 2 == 0 ? 6 : 17))  # TCP or UDP
    PACKETS=$((100 + i * 10))
    BYTES=$((PACKETS * 1500))

    # NetFlow v5 header (24 bytes) + flow record (48 bytes)
    # Version=5, Count=1, SysUptime, UnixSecs, UnixNSecs, FlowSequence, EngineType, EngineID, SamplingInterval
    NETFLOW_HEADER="0005000100000001$(printf "%08x" $TIMESTAMP)00000000$(printf "%08x" $i)000000000000"

    # Flow record: SrcAddr, DstAddr, NextHop, Input, Output, dPkts, dOctets, First, Last, SrcPort, DstPort, etc.
    FLOW_RECORD="$(printf "%08x" $(echo $SRC_IP | awk -F. '{print ($1*256^3)+($2*256^2)+($3*256)+$4}'))$(printf "%08x" $(echo $DST_IP | awk -F. '{print ($1*256^3)+($2*256^2)+($3*256)+$4}'))00000000000000010000000$(printf "%08x" $PACKETS)$(printf "%08x" $BYTES)$(printf "%08x" $TIMESTAMP)$(printf "%08x" $TIMESTAMP)$(printf "%04x" $SRC_PORT)$(printf "%04x" $DST_PORT)00$(printf "%02x" $PROTOCOL)0000000000000000000000000000000000000000"

    # Send binary NetFlow packet
    echo -n "${NETFLOW_HEADER}${FLOW_RECORD}" | xxd -r -p | nc -u -w1 $HOST $PORT

    # Also send human-readable version for debugging
    echo "NetFlow v5: $SRC_IP:$SRC_PORT -> $DST_IP:$DST_PORT, Proto=$PROTOCOL, Pkts=$PACKETS, Bytes=$BYTES" | nc -u -w1 $HOST $PORT

    echo "[$i/$COUNT] Sent NetFlow packet: $SRC_IP:$SRC_PORT -> $DST_IP:$DST_PORT ($PACKETS pkts, $BYTES bytes)"

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending $COUNT NetFlow packets"
echo "Configure your proxy with data_hint: \"netflow\" to process these packets"