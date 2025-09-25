#!/bin/bash
# NDJSON UDP Feed Generator for ByteFreezer Proxy Testing
# Simulates structured application logs in NDJSON format

HOST="${1:-localhost}"
PORT="${2:-2056}"
COUNT="${3:-10}"
DELAY="${4:-1}"

echo "Sending $COUNT fake NDJSON messages to $HOST:$PORT (${DELAY}s interval)"
echo "Data hint: ndjson (newline-delimited JSON)"
echo ""

# Sample data for realistic logs
USERS=("alice" "bob" "charlie" "diana" "eve" "frank" "grace" "henry")
ACTIONS=("login" "logout" "view_profile" "update_settings" "upload_file" "delete_item" "search" "checkout")
SERVICES=("api-gateway" "user-service" "order-service" "payment-service" "notification-service")
LEVELS=("info" "warn" "error" "debug")
STATUSES=("success" "failure" "timeout" "retry")

for i in $(seq 1 $COUNT); do
    USER=${USERS[$((i % ${#USERS[@]}))]}
    ACTION=${ACTIONS[$((i % ${#ACTIONS[@]}))]}
    SERVICE=${SERVICES[$((i % ${#SERVICES[@]}))]}
    LEVEL=${LEVELS[$((i % ${#LEVELS[@]}))]}
    STATUS=${STATUSES[$((i % ${#STATUSES[@]}))]}

    TIMESTAMP=$(date --rfc-3339=seconds)
    REQUEST_ID="req-$(uuidgen | cut -c1-8)"
    USER_ID=$((1000 + i))
    DURATION=$((50 + RANDOM % 500))

    # Generate different types of NDJSON messages
    case $((i % 4)) in
        0)
            # User action log
            NDJSON_MSG="{\"timestamp\":\"$TIMESTAMP\",\"level\":\"$LEVEL\",\"service\":\"$SERVICE\",\"request_id\":\"$REQUEST_ID\",\"user_id\":$USER_ID,\"username\":\"$USER\",\"action\":\"$ACTION\",\"status\":\"$STATUS\",\"duration_ms\":$DURATION,\"ip\":\"192.168.1.$((10 + i % 100))\",\"user_agent\":\"Mozilla/5.0 (compatible)\"}"
            ;;
        1)
            # API metrics log
            RESPONSE_SIZE=$((1000 + RANDOM % 5000))
            HTTP_CODE=$((i % 2 == 0 ? 200 : 404))
            NDJSON_MSG="{\"timestamp\":\"$TIMESTAMP\",\"level\":\"info\",\"service\":\"$SERVICE\",\"request_id\":\"$REQUEST_ID\",\"method\":\"GET\",\"path\":\"/api/v1/$ACTION\",\"http_status\":$HTTP_CODE,\"response_size_bytes\":$RESPONSE_SIZE,\"duration_ms\":$DURATION,\"remote_ip\":\"10.0.0.$((i % 255))\"}"
            ;;
        2)
            # Error log with stack trace
            ERROR_MSGS=("Connection timeout" "Database unavailable" "Invalid input format" "Authentication failed" "Rate limit exceeded")
            ERROR_MSG=${ERROR_MSGS[$((i % ${#ERROR_MSGS[@]}))]}
            NDJSON_MSG="{\"timestamp\":\"$TIMESTAMP\",\"level\":\"error\",\"service\":\"$SERVICE\",\"request_id\":\"$REQUEST_ID\",\"error\":\"$ERROR_MSG\",\"error_code\":\"ERR_$((1000 + i))\",\"stack_trace\":\"at handler.process() line $((i % 100 + 1))\",\"context\":{\"user_id\":$USER_ID,\"action\":\"$ACTION\"}}"
            ;;
        3)
            # Business metrics log
            REVENUE=$(echo "scale=2; ($i * 29.99) + ($RANDOM % 100)" | bc)
            ORDER_ID="order-$(printf "%06d" $i)"
            NDJSON_MSG="{\"timestamp\":\"$TIMESTAMP\",\"level\":\"info\",\"service\":\"order-service\",\"event\":\"order_completed\",\"order_id\":\"$ORDER_ID\",\"user_id\":$USER_ID,\"amount\":$REVENUE,\"currency\":\"USD\",\"items_count\":$((1 + i % 5)),\"payment_method\":\"credit_card\",\"shipping_address\":{\"country\":\"US\",\"state\":\"CA\"}}"
            ;;
    esac

    echo "$NDJSON_MSG" | nc -u -w1 $HOST $PORT

    echo "[$i/$COUNT] Sent NDJSON: $(echo $NDJSON_MSG | jq -r '.service + ": " + .action // .event // .error // "metric"' 2>/dev/null || echo "$SERVICE: $ACTION")"

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending $COUNT NDJSON messages"
echo "Configure your proxy with data_hint: \"ndjson\" to process these messages"