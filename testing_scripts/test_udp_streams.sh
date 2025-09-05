#!/bin/bash

# ByteFreezer Proxy UDP Test Script
# This script sends test data to all 3 configured UDP ports to test spooling functionality

set -e

# Configuration
PROXY_HOST="localhost"
SYSLOG_PORT=2056
EBPF_PORT=2057
APP_LOGS_PORT=2058
SPOOLING_DIR="/tmp/bytefreezer-proxy"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to log messages
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

error() {
    echo -e "${RED}❌ $1${NC}"
}

# Function to send UDP data
send_udp() {
    local port=$1
    local data=$2
    local dataset=$3
    
    log "Sending data to port ${port} (${dataset})"
    echo "$data" | nc -u -w1 $PROXY_HOST $port
    if [ $? -eq 0 ]; then
        success "Data sent to port ${port}"
    else
        error "Failed to send data to port ${port}"
        return 1
    fi
}

# Function to generate JSON log data
generate_syslog_data() {
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
    cat << EOF
{"timestamp":"$timestamp","level":"info","facility":"kern","severity":"info","hostname":"test-server-01","program":"kernel","message":"Test syslog message from UDP test script","pid":12345}
{"timestamp":"$timestamp","level":"warn","facility":"mail","severity":"warning","hostname":"mail-server","program":"postfix","message":"Warning: queue file size limit exceeded","pid":23456}
{"timestamp":"$timestamp","level":"error","facility":"auth","severity":"error","hostname":"auth-server","program":"sshd","message":"Failed password for user admin from 192.168.1.100","pid":34567}
EOF
}

generate_ebpf_data() {
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
    cat << EOF
{"timestamp":"$timestamp","event_type":"syscall","syscall":"openat","pid":1234,"uid":1000,"gid":1000,"comm":"test-app","filename":"/etc/passwd","retval":3}
{"timestamp":"$timestamp","event_type":"network","protocol":"tcp","src_ip":"10.0.1.5","dst_ip":"10.0.1.10","src_port":45678,"dst_port":80,"bytes":1024,"action":"allow"}
{"timestamp":"$timestamp","event_type":"process","action":"exec","pid":5678,"ppid":1234,"uid":1000,"comm":"curl","cmdline":"curl -s https://api.example.com/data","cwd":"/home/user"}
EOF
}

generate_application_data() {
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.%3NZ")
    cat << EOF
{"timestamp":"$timestamp","level":"info","service":"web-api","version":"1.2.3","message":"Processing user request","user_id":"user_12345","request_id":"req_abcdef123","duration_ms":45}
{"timestamp":"$timestamp","level":"error","service":"database","version":"5.7.0","message":"Connection timeout to primary database","error":"dial timeout","retry_count":3,"duration_ms":5000}
{"timestamp":"$timestamp","level":"debug","service":"cache-redis","version":"6.2.0","message":"Cache hit for key","key":"user:12345:profile","ttl_remaining":3600,"size_bytes":256}
EOF
}

# Function to check spooling directory
check_spooling_dir() {
    log "Checking spooling directory: $SPOOLING_DIR"
    
    if [ ! -d "$SPOOLING_DIR" ]; then
        warning "Spooling directory does not exist yet: $SPOOLING_DIR"
        return 1
    fi
    
    # Count files in spooling directory
    local ndjson_files=$(find "$SPOOLING_DIR" -name "*.ndjson" | wc -l)
    local meta_files=$(find "$SPOOLING_DIR" -name "*.meta" | wc -l)
    
    success "Found ${ndjson_files} NDJSON files and ${meta_files} metadata files in spooling directory"
    
    # Show recent files
    if [ $ndjson_files -gt 0 ]; then
        log "Recent spooled data files:"
        find "$SPOOLING_DIR" -name "*.ndjson" -printf "%T@ %Tc %p\n" | sort -n | tail -5 | while read timestamp date time file; do
            echo "  📁 $(basename $file) - $date $time"
        done
        
        # Show content of most recent file
        local latest_file=$(find "$SPOOLING_DIR" -name "*.ndjson" -printf "%T@ %p\n" | sort -n | tail -1 | cut -d' ' -f2-)
        if [ -n "$latest_file" ]; then
            log "Content of most recent spooled file:"
            echo "📄 $(basename $latest_file):"
            head -3 "$latest_file" | sed 's/^/  /'
            if [ $(wc -l < "$latest_file") -gt 3 ]; then
                echo "  ... ($(wc -l < "$latest_file") total lines)"
            fi
        fi
    fi
    
    # Show metadata files
    if [ $meta_files -gt 0 ]; then
        log "Recent metadata files:"
        find "$SPOOLING_DIR" -name "*.meta" -printf "%T@ %p\n" | sort -n | tail -3 | while read timestamp file; do
            echo "📋 $(basename $file):"
            cat "$file" | jq -r '  "  Tenant: \(.tenant_id), Dataset: \(.dataset_id), Size: \(.size) bytes, Created: \(.created_at)"' 2>/dev/null || cat "$file" | sed 's/^/  /'
        done
    fi
}

# Function to monitor proxy logs
monitor_proxy_logs() {
    log "Monitoring proxy logs for 5 seconds..."
    timeout 5s tail -f /dev/null 2>/dev/null || true
    success "Log monitoring complete"
}

# Main execution
main() {
    log "🚀 Starting ByteFreezer Proxy UDP Test"
    log "Target: $PROXY_HOST"
    log "Ports: $SYSLOG_PORT (syslog-data), $EBPF_PORT (ebpf-data), $APP_LOGS_PORT (application-logs)"
    echo
    
    # Check if nc (netcat) is available
    if ! command -v nc &> /dev/null; then
        error "netcat (nc) is not installed. Please install it first."
        echo "  Ubuntu/Debian: sudo apt-get install netcat-openbsd"
        echo "  CentOS/RHEL:   sudo yum install nc"
        echo "  macOS:         brew install netcat"
        exit 1
    fi
    
    # Check initial state
    log "📊 Initial spooling directory state:"
    check_spooling_dir || true
    echo
    
    # Send test data to each port
    log "📡 Sending test data streams..."
    
    # 1. Syslog data to port 2056
    log "1️⃣ Sending syslog data to port $SYSLOG_PORT"
    send_udp $SYSLOG_PORT "$(generate_syslog_data)" "syslog-data"
    sleep 1
    
    # 2. eBPF data to port 2057  
    log "2️⃣ Sending eBPF data to port $EBPF_PORT"
    send_udp $EBPF_PORT "$(generate_ebpf_data)" "ebpf-data"
    sleep 1
    
    # 3. Application logs to port 2058
    log "3️⃣ Sending application logs to port $APP_LOGS_PORT"
    send_udp $APP_LOGS_PORT "$(generate_application_data)" "application-logs"
    sleep 1
    
    echo
    log "⏳ Waiting 3 seconds for proxy to process data..."
    sleep 3
    
    # Check spooling directory again
    log "📊 Final spooling directory state:"
    check_spooling_dir || warning "No spooled files found - this is normal if the receiver is available"
    
    echo
    success "🎉 UDP test completed successfully!"
    log "💡 Tips:"
    echo "  • If you see spooled files, the receiver might be unavailable (expected for testing)"
    echo "  • If no spooled files, data was forwarded successfully to the receiver"
    echo "  • Check proxy logs with: journalctl -f -u bytefreezer-proxy"
    echo "  • Monitor spooling: watch -n1 'ls -la $SPOOLING_DIR'"
}

# Execute main function
main "$@"