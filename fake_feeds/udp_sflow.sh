#!/bin/bash
# sFlow UDP Feed Generator for ByteFreezer Proxy Testing
# Simulates comprehensive sFlow packets with realistic network monitoring data

HOST="${1:-localhost}"
PORT="${2:-2056}"
COUNT="${3:-10}"
DELAY="${4:-1}"

echo "Sending $COUNT comprehensive sFlow packets to $HOST:$PORT (${DELAY}s interval)"
echo "Data hint: sflow (binary network monitoring data)"
echo ""

# Function to generate random IP addresses
generate_ip() {
    echo "$(($RANDOM % 256)).$(($RANDOM % 256)).$(($RANDOM % 256)).$(($RANDOM % 256))"
}

# Function to generate random MAC addresses
generate_mac() {
    printf "%02x:%02x:%02x:%02x:%02x:%02x" $((RANDOM%256)) $((RANDOM%256)) $((RANDOM%256)) $((RANDOM%256)) $((RANDOM%256)) $((RANDOM%256))
}

# Function to create extended sFlow packet with multiple flow samples
create_comprehensive_sflow_packet() {
    local seq_num=$1
    local timestamp=$2
    local packet_size=0

    # sFlow v5 Header (32 bytes base)
    HEADER="00000005000000010a000001000000$(printf "%08x" $seq_num)$(printf "%08x" $timestamp)00000003"

    # Flow Sample 1: HTTP Traffic
    SRC_IP=$(generate_ip)
    DST_IP=$(generate_ip)
    SRC_PORT=$((RANDOM % 65536))
    DST_PORT=80
    PROTOCOL=6  # TCP
    PACKET_COUNT=$((RANDOM % 1000 + 100))
    BYTE_COUNT=$((PACKET_COUNT * (RANDOM % 1400 + 60)))

    FLOW_SAMPLE1="0000000100000001$(printf "%08x" $PACKET_COUNT)$(printf "%08x" $BYTE_COUNT)0000$(printf "%04x" $SRC_PORT)0000$(printf "%04x" $DST_PORT)$(printf "%02x" $PROTOCOL)00"

    # Flow Sample 2: HTTPS Traffic
    SRC_IP2=$(generate_ip)
    DST_IP2=$(generate_ip)
    SRC_PORT2=$((RANDOM % 65536))
    DST_PORT2=443
    PACKET_COUNT2=$((RANDOM % 2000 + 200))
    BYTE_COUNT2=$((PACKET_COUNT2 * (RANDOM % 1400 + 60)))

    FLOW_SAMPLE2="0000000200000002$(printf "%08x" $PACKET_COUNT2)$(printf "%08x" $BYTE_COUNT2)0000$(printf "%04x" $SRC_PORT2)0000$(printf "%04x" $DST_PORT2)06000000"

    # Counter Sample: Interface Statistics
    IF_INDEX=1
    IF_TYPE=6  # Ethernet
    IF_SPEED=1000000000  # 1Gbps
    IF_DIRECTION=1
    IF_STATUS=3
    IN_OCTETS=$((RANDOM % 1000000000 + 1000000))
    IN_PACKETS=$((IN_OCTETS / 64))
    OUT_OCTETS=$((RANDOM % 1000000000 + 1000000))
    OUT_PACKETS=$((OUT_OCTETS / 64))

    COUNTER_SAMPLE="00000003$(printf "%08x" $IF_INDEX)$(printf "%08x" $IF_TYPE)$(printf "%08x" $IF_SPEED)$(printf "%08x" $IF_DIRECTION)$(printf "%08x" $IF_STATUS)$(printf "%08x" $IN_OCTETS)$(printf "%08x" $IN_PACKETS)$(printf "%08x" $OUT_OCTETS)$(printf "%08x" $OUT_PACKETS)"

    # Combine all samples
    echo "${HEADER}${FLOW_SAMPLE1}${FLOW_SAMPLE2}${COUNTER_SAMPLE}"
}

for i in $(seq 1 $COUNT); do
    TIMESTAMP=$(date +%s)

    # Generate comprehensive sFlow packet
    SFLOW_PACKET=$(create_comprehensive_sflow_packet $i $TIMESTAMP)

    # Send binary sFlow packet
    echo -n "$SFLOW_PACKET" | xxd -r -p | nc -u -w1 $HOST $PORT 2>/dev/null

    # Generate additional flow records (multiple per packet)
    for j in $(seq 1 5); do
        SRC_IP=$(generate_ip)
        DST_IP=$(generate_ip)
        MAC_SRC=$(generate_mac)
        MAC_DST=$(generate_mac)
        BYTES=$((RANDOM % 10000 + 1000))
        PACKETS=$((RANDOM % 100 + 10))
        PROTOCOL=$((RANDOM % 255 + 1))
        VLAN=$((RANDOM % 4000 + 1))

        # Create detailed flow record with multiple samples
        FLOW_RECORD="sFlow-v5|Agent:10.0.0.1|Seq:$i-$j|Time:$TIMESTAMP|SrcIP:$SRC_IP|DstIP:$DST_IP|SrcMAC:$MAC_SRC|DstMAC:$MAC_DST|Protocol:$PROTOCOL|SrcPort:$((RANDOM%65536))|DstPort:$((RANDOM%65536))|Bytes:$BYTES|Packets:$PACKETS|VLAN:$VLAN|TOS:$((RANDOM%8))|Interface:eth$((RANDOM%8))|Direction:$((RANDOM%2))|Sampling:1:1000"

        echo "$FLOW_RECORD" | nc -u -w1 $HOST $PORT 2>/dev/null
    done

    # Send interface counter records
    for k in $(seq 0 3); do
        IN_OCTETS=$((RANDOM % 1000000000 + 1000000))
        OUT_OCTETS=$((RANDOM % 1000000000 + 1000000))
        IN_PACKETS=$((IN_OCTETS / 64))
        OUT_PACKETS=$((OUT_OCTETS / 64))
        ERRORS=$((RANDOM % 100))
        DROPS=$((RANDOM % 50))

        COUNTER_RECORD="sFlow-Counter|Agent:10.0.0.1|Seq:$i|Interface:$k|Type:ethernet|Speed:1000000000|Direction:fullduplex|Status:up|InOctets:$IN_OCTETS|InPackets:$IN_PACKETS|InErrors:$ERRORS|InDrops:$DROPS|OutOctets:$OUT_OCTETS|OutPackets:$OUT_PACKETS|OutErrors:$((ERRORS/2))|OutDrops:$((DROPS/2))|Collisions:0"

        echo "$COUNTER_RECORD" | nc -u -w1 $HOST $PORT 2>/dev/null
    done

    # Send host performance counters
    CPU_UTIL=$((RANDOM % 100))
    MEM_UTIL=$((RANDOM % 100))
    DISK_UTIL=$((RANDOM % 100))
    LOAD_AVG=$(echo "scale=2; $RANDOM/32767*10" | bc 2>/dev/null || echo "1.5")

    HOST_COUNTER="sFlow-Host|Agent:10.0.0.1|Seq:$i|Hostname:server-$((i%10+1))|OS:Linux|CPUs:$((RANDOM%32+1))|CPU_Speed:2400000000|Memory:$((RANDOM%64+8))GB|CPU_Util:$CPU_UTIL%|Mem_Util:$MEM_UTIL%|Disk_Util:$DISK_UTIL%|Load_Avg:$LOAD_AVG|Uptime:$((RANDOM%1000000+86400))"

    echo "$HOST_COUNTER" | nc -u -w1 $HOST $PORT 2>/dev/null

    # Application performance data
    APP_RESPONSE=$((RANDOM % 5000 + 10))
    APP_THROUGHPUT=$((RANDOM % 10000 + 100))
    APP_ERRORS=$((RANDOM % 100))

    APP_COUNTER="sFlow-App|Agent:10.0.0.1|Seq:$i|App:nginx|PID:$((RANDOM%30000+1000))|Requests:$APP_THROUGHPUT|Responses:$((APP_THROUGHPUT-APP_ERRORS))|Errors:$APP_ERRORS|Response_Time:${APP_RESPONSE}ms|Memory:$((RANDOM%1000+100))MB|CPU:$((RANDOM%100))%|Connections:$((RANDOM%1000+50))|Workers:$((RANDOM%20+4))"

    echo "$APP_COUNTER" | nc -u -w1 $HOST $PORT 2>/dev/null

    TOTAL_RECORDS=12  # 1 binary + 5 flows + 4 interfaces + 1 host + 1 app
    TOTAL_BYTES=$((${#SFLOW_PACKET}/2 + 500 * 10))  # Approximate

    echo "[$i/$COUNT] Sent comprehensive sFlow packet #$i:"
    echo "  - 1 binary sFlow v5 packet ($(printf "%d" $((${#SFLOW_PACKET}/2))) bytes)"
    echo "  - 5 detailed flow records"
    echo "  - 4 interface counter records"
    echo "  - 1 host performance record"
    echo "  - 1 application performance record"
    echo "  Total: $TOTAL_RECORDS records, ~$TOTAL_BYTES bytes"

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending comprehensive sFlow data:"
echo "  - Total packets: $COUNT"
echo "  - Total records: $((COUNT * 12))"
echo "  - Estimated data: ~$((COUNT * 6000)) bytes"
echo ""
echo "Data includes:"
echo "  • Binary sFlow v5 packets with flow/counter samples"
echo "  • Detailed network flow records (TCP/UDP/ICMP)"
echo "  • Interface statistics and performance counters"
echo "  • Host system performance metrics"
echo "  • Application performance data"
echo ""
echo "Configure your proxy with data_hint: \"sflow\" to process these packets"