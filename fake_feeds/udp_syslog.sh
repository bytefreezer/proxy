#!/bin/bash
# Syslog UDP Feed Generator for ByteFreezer Proxy Testing
# Simulates RFC3164 and RFC5424 syslog messages

HOST="${1:-localhost}"
PORT="${2:-2056}"
COUNT="${3:-10}"
DELAY="${4:-1}"

echo "Sending $COUNT fake syslog messages to $HOST:$PORT (${DELAY}s interval)"
echo "Data hint: syslog (structured system logs)"
echo ""

# Sample log messages
MESSAGES=(
    "User authentication successful"
    "Connection established from client"
    "Database query completed"
    "Cache miss for key user_session"
    "Background job processing started"
    "SSL certificate validation passed"
    "Memory usage threshold exceeded"
    "Network interface eth0 up"
    "Backup process initiated"
    "Configuration file reloaded"
)

FACILITIES=("auth" "daemon" "kernel" "mail" "user" "local0" "local1")
SEVERITIES=("info" "warn" "error" "debug" "notice")
HOSTS=("web01" "db01" "cache01" "lb01" "app01")
PROCESSES=("nginx" "postgres" "redis" "haproxy" "myapp")

for i in $(seq 1 $COUNT); do
    FACILITY=${FACILITIES[$((i % ${#FACILITIES[@]}))]}
    SEVERITY=${SEVERITIES[$((i % ${#SEVERITIES[@]}))]}
    HOSTNAME=${HOSTS[$((i % ${#HOSTS[@]}))]}
    PROCESS=${PROCESSES[$((i % ${#PROCESSES[@]}))]}
    MESSAGE=${MESSAGES[$((i % ${#MESSAGES[@]}))]}
    PID=$((1000 + RANDOM % 9000))

    # Calculate priority (facility * 8 + severity)
    case $FACILITY in
        "auth") FAC_NUM=32 ;;
        "daemon") FAC_NUM=24 ;;
        "kernel") FAC_NUM=0 ;;
        "mail") FAC_NUM=16 ;;
        "user") FAC_NUM=8 ;;
        "local0") FAC_NUM=128 ;;
        *) FAC_NUM=136 ;;
    esac

    case $SEVERITY in
        "debug") SEV_NUM=7 ;;
        "info") SEV_NUM=6 ;;
        "notice") SEV_NUM=5 ;;
        "warn") SEV_NUM=4 ;;
        "error") SEV_NUM=3 ;;
        *) SEV_NUM=6 ;;
    esac

    PRIORITY=$((FAC_NUM + SEV_NUM))
    TIMESTAMP=$(date '+%b %d %H:%M:%S')

    # RFC3164 format
    if [ $((i % 2)) -eq 0 ]; then
        SYSLOG_MSG="<${PRIORITY}>${TIMESTAMP} ${HOSTNAME} ${PROCESS}[${PID}]: ${MESSAGE}"
        FORMAT="RFC3164"
    else
        # RFC5424 format
        SYSLOG_TIMESTAMP=$(date --rfc-3339=seconds | sed 's/ /T/')
        SYSLOG_MSG="<${PRIORITY}>1 ${SYSLOG_TIMESTAMP} ${HOSTNAME} ${PROCESS} ${PID} - - ${MESSAGE}"
        FORMAT="RFC5424"
    fi

    echo "$SYSLOG_MSG" | nc -u -w1 $HOST $PORT

    echo "[$i/$COUNT] Sent $FORMAT syslog: <$PRIORITY> $HOSTNAME $PROCESS[$PID]: $MESSAGE"

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending $COUNT syslog messages"
echo "Configure your proxy with data_hint: \"syslog\" to process these messages"