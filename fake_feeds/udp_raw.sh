#!/bin/bash
# Raw UDP Feed Generator for ByteFreezer Proxy Testing
# Simulates raw/unstructured log data from legacy systems

HOST="${1:-localhost}"
PORT="${2:-2056}"
COUNT="${3:-10}"
DELAY="${4:-1}"

echo "Sending $COUNT fake raw messages to $HOST:$PORT (${DELAY}s interval)"
echo "Data hint: raw (unstructured/legacy format)"
echo ""

# Sample raw log formats from different systems
RAW_FORMATS=(
    # Legacy application logs
    "APP: %timestamp% [%level%] Processing request from %ip% - %message%"
    # Custom delimiter format
    "%timestamp%|%hostname%|%process%|%level%|%message%"
    # Simple key=value format
    "time=%timestamp% host=%hostname% level=%level% msg='%message%'"
    # Mixed format with custom fields
    "%timestamp% %hostname% %process%[%pid%]: STATUS=%status% DURATION=%duration%ms MSG=%message%"
    # Proprietary system format
    ">>%level%<< %timestamp% @%hostname% #%process# :%message%:"
)

HOSTNAMES=("server01" "app-prod-02" "db-primary" "cache-node-1" "worker-03")
PROCESSES=("myapp" "legacy-sys" "batch-job" "monitor" "backup")
LEVELS=("INFO" "WARN" "ERROR" "DEBUG" "TRACE")
MESSAGES=(
    "Request processed successfully"
    "Cache hit for key session_12345"
    "Database connection pool exhausted"
    "Background task completed"
    "Memory allocation failed"
    "File upload received"
    "User session expired"
    "Configuration updated"
    "Network timeout occurred"
    "Backup checkpoint created"
)
STATUSES=("OK" "FAIL" "TIMEOUT" "RETRY" "PENDING")

for i in $(seq 1 $COUNT); do
    FORMAT_TEMPLATE=${RAW_FORMATS[$((i % ${#RAW_FORMATS[@]}))]}
    HOSTNAME=${HOSTNAMES[$((i % ${#HOSTNAMES[@]}))]}
    PROCESS=${PROCESSES[$((i % ${#PROCESSES[@]}))]}
    LEVEL=${LEVELS[$((i % ${#LEVELS[@]}))]}
    MESSAGE=${MESSAGES[$((i % ${#MESSAGES[@]}))]}
    STATUS=${STATUSES[$((i % ${#STATUSES[@]}))]}

    PID=$((1000 + RANDOM % 9000))
    DURATION=$((10 + RANDOM % 500))
    IP="192.168.1.$((10 + i % 100))"
    TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S.%3N')

    # Replace placeholders in the template
    RAW_MSG="$FORMAT_TEMPLATE"
    RAW_MSG="${RAW_MSG//%timestamp%/$TIMESTAMP}"
    RAW_MSG="${RAW_MSG//%hostname%/$HOSTNAME}"
    RAW_MSG="${RAW_MSG//%process%/$PROCESS}"
    RAW_MSG="${RAW_MSG//%level%/$LEVEL}"
    RAW_MSG="${RAW_MSG//%message%/$MESSAGE}"
    RAW_MSG="${RAW_MSG//%pid%/$PID}"
    RAW_MSG="${RAW_MSG//%status%/$STATUS}"
    RAW_MSG="${RAW_MSG//%duration%/$DURATION}"
    RAW_MSG="${RAW_MSG//%ip%/$IP}"

    echo "$RAW_MSG" | nc -u -w1 $HOST $PORT

    echo "[$i/$COUNT] Sent raw message: $LEVEL from $HOSTNAME ($PROCESS)"

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending $COUNT raw messages"
echo "Configure your proxy with data_hint: \"raw\" to process these messages"