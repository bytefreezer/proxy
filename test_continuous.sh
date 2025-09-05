#!/bin/bash

# Continuous UDP Test Script for ByteFreezer Proxy
# Sends data continuously to test batching and spooling behavior

set -e

# Configuration
PROXY_HOST="localhost"
PORTS=(2056 2057 2058)
DATASETS=("syslog-data" "ebpf-data" "application-logs")
INTERVAL=5  # seconds between sends
COUNT=10    # number of iterations

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $1"
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# Generate different types of log data
generate_log_data() {
    local port=$1
    local iteration=$2
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
    
    case $port in
        2056) # syslog-data
            echo "{\"timestamp\":\"$timestamp\",\"level\":\"info\",\"host\":\"server-$(($iteration % 3 + 1))\",\"service\":\"syslogd\",\"message\":\"System event $iteration\",\"iteration\":$iteration}"
            ;;
        2057) # ebpf-data  
            echo "{\"timestamp\":\"$timestamp\",\"event\":\"syscall\",\"pid\":$((1000 + $iteration)),\"syscall\":\"read\",\"fd\":3,\"bytes\":$((100 + ($iteration * 10) % 500)),\"iteration\":$iteration}"
            ;;
        2058) # application-logs
            echo "{\"timestamp\":\"$timestamp\",\"level\":\"info\",\"app\":\"web-api\",\"request_id\":\"req-$iteration\",\"user\":\"user-$(($iteration % 10))\",\"response_time\":$((50 + $iteration % 200)),\"iteration\":$iteration}"
            ;;
    esac
}

# Main loop
main() {
    log "🔄 Starting continuous UDP test"
    log "Sending $COUNT iterations every $INTERVAL seconds to ports: ${PORTS[*]}"
    log "Press Ctrl+C to stop"
    echo
    
    for i in $(seq 1 $COUNT); do
        log "📡 Iteration $i/$COUNT"
        
        # Send to all three ports
        for idx in ${!PORTS[@]}; do
            port=${PORTS[$idx]}
            dataset=${DATASETS[$idx]}
            data=$(generate_log_data $port $i)
            
            echo "$data" | nc -u -w1 $PROXY_HOST $port
            if [ $? -eq 0 ]; then
                echo "  ✓ Sent to $port ($dataset)"
            else
                echo "  ✗ Failed to send to $port ($dataset)"
            fi
        done
        
        # Show spooling directory status every 3 iterations
        if [ $((i % 3)) -eq 0 ]; then
            spooled_files=$(find /tmp/bytefreezer-proxy -name "*.ndjson" 2>/dev/null | wc -l || echo 0)
            echo "  📁 Spooled files: $spooled_files"
        fi
        
        if [ $i -lt $COUNT ]; then
            echo "  ⏳ Waiting ${INTERVAL}s..."
            sleep $INTERVAL
        fi
        echo
    done
    
    success "🎉 Continuous test completed"
    
    # Final status
    log "📊 Final spooling directory status:"
    if [ -d "/tmp/bytefreezer-proxy" ]; then
        find /tmp/bytefreezer-proxy -name "*.ndjson" -printf "%T@ %Tc %p\n" | sort -n | tail -5 | while read timestamp date time file; do
            echo "  📄 $(basename $file) - $date $time ($(stat -c%s "$file") bytes)"
        done
    else
        echo "  No spooling directory found"
    fi
}

# Handle Ctrl+C gracefully
trap 'echo -e "\n${YELLOW}⚠️  Interrupted by user${NC}"; exit 0' INT

main "$@"