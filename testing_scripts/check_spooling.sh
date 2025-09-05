#!/bin/bash

# Script to check ByteFreezer Proxy spooling directory

SPOOLING_DIR="/tmp/bytefreezer-proxy"
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
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

error() {
    echo -e "${RED}❌ $1${NC}"
}

main() {
    log "📁 Checking ByteFreezer Proxy spooling directory: $SPOOLING_DIR"
    echo
    
    if [ ! -d "$SPOOLING_DIR" ]; then
        warning "Spooling directory does not exist"
        echo "This is normal if:"
        echo "  • The proxy hasn't been started yet"
        echo "  • No data has been spooled yet"
        echo "  • Spooling is disabled in config"
        exit 0
    fi
    
    # Count files
    ndjson_files=$(find "$SPOOLING_DIR" -name "*.ndjson" 2>/dev/null | wc -l)
    meta_files=$(find "$SPOOLING_DIR" -name "*.meta" 2>/dev/null | wc -l)
    total_size=$(find "$SPOOLING_DIR" -name "*.ndjson" -exec stat -c%s {} \; 2>/dev/null | awk '{sum+=$1} END {print sum+0}')
    
    # Display summary
    success "Directory exists: $SPOOLING_DIR"
    echo "📊 Summary:"
    echo "  • NDJSON files: $ndjson_files"
    echo "  • Metadata files: $meta_files"
    echo "  • Total data size: $total_size bytes ($(echo $total_size | numfmt --to=iec 2>/dev/null || echo $total_size))"
    echo
    
    if [ $ndjson_files -eq 0 ]; then
        success "No spooled files found - data is being forwarded successfully!"
        exit 0
    fi
    
    # Show detailed file info
    log "📄 Spooled files (most recent first):"
    find "$SPOOLING_DIR" -name "*.ndjson" -printf "%T@ %Tc %p %s\n" | sort -nr | head -10 | while read timestamp date time file size; do
        filename=$(basename "$file")
        size_human=$(echo $size | numfmt --to=iec 2>/dev/null || echo "${size}B")
        echo "  📄 $filename ($size_human) - $date $time"
    done
    echo
    
    # Show metadata for recent files
    log "📋 Metadata (most recent 3 files):"
    find "$SPOOLING_DIR" -name "*.meta" -printf "%T@ %p\n" | sort -nr | head -3 | while read timestamp file; do
        filename=$(basename "$file" .meta)
        echo "  📋 $filename:"
        if command -v jq &> /dev/null; then
            cat "$file" | jq -r '  "    Tenant: \(.tenant_id)"' 2>/dev/null
            cat "$file" | jq -r '  "    Dataset: \(.dataset_id)"' 2>/dev/null  
            cat "$file" | jq -r '  "    Created: \(.created_at)"' 2>/dev/null
            cat "$file" | jq -r '  "    Retries: \(.retry_count)"' 2>/dev/null
            cat "$file" | jq -r '  "    Reason: \(.failure_reason)"' 2>/dev/null
        else
            cat "$file" | sed 's/^/    /'
        fi
        echo
    done
    
    # Show sample data
    log "📝 Sample data from most recent file:"
    latest_file=$(find "$SPOOLING_DIR" -name "*.ndjson" -printf "%T@ %p\n" | sort -nr | head -1 | cut -d' ' -f2-)
    if [ -n "$latest_file" ] && [ -f "$latest_file" ]; then
        echo "  📄 $(basename "$latest_file"):"
        head -3 "$latest_file" | while read line; do
            if command -v jq &> /dev/null; then
                echo "$line" | jq -c '.' 2>/dev/null | cut -c1-100 | sed 's/^/    /'
            else
                echo "$line" | cut -c1-100 | sed 's/^/    /'
            fi
        done
        line_count=$(wc -l < "$latest_file")
        if [ $line_count -gt 3 ]; then
            echo "    ... ($line_count total lines)"
        fi
    fi
    echo
    
    warning "Files are being spooled, which means:"
    echo "  • The receiver might be unavailable"
    echo "  • Authentication might be failing"
    echo "  • Network connectivity issues"
    echo
    echo "💡 To monitor in real-time: watch -n2 '$0'"
    echo "💡 To test receiver: curl -f http://localhost:8080/health"
}

main "$@"