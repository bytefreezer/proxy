#!/bin/bash
# NDJSON HTTP Feed Generator for ByteFreezer Proxy Testing
# Sends NDJSON data via HTTP webhook endpoint

HOST="${1:-localhost}"
PORT="${2:-8081}"
ENDPOINT="${3:-/webhook}"
COUNT="${4:-5}"
DELAY="${5:-2}"

echo "Sending $COUNT NDJSON HTTP requests to http://$HOST:$PORT$ENDPOINT (${DELAY}s interval)"
echo "Data hint: ndjson (HTTP webhook)"
echo ""

# Sample application event data
EVENTS=("user_registered" "order_placed" "payment_processed" "item_shipped" "review_submitted")
USERS=("user123" "user456" "user789" "user101" "user202")
PRODUCTS=("laptop" "phone" "headphones" "keyboard" "mouse")
COUNTRIES=("US" "CA" "UK" "DE" "AU")

for i in $(seq 1 $COUNT); do
    EVENT=${EVENTS[$((i % ${#EVENTS[@]}))]}
    USER=${USERS[$((i % ${#USERS[@]}))]}
    PRODUCT=${PRODUCTS[$((i % ${#PRODUCTS[@]}))]}
    COUNTRY=${COUNTRIES[$((i % ${#COUNTRIES[@]}))]}

    TIMESTAMP=$(date --rfc-3339=seconds)
    SESSION_ID="sess-$(uuidgen | cut -c1-12)"
    ORDER_ID="ord-$(printf "%06d" $((1000 + i)))"
    AMOUNT=$(echo "scale=2; (50 + $i * 25.99) + ($RANDOM % 100)" | bc)

    # Create multi-line NDJSON payload
    NDJSON_PAYLOAD=""

    # Add 3-5 related events per request
    for j in $(seq 1 $((3 + i % 3))); do
        case $((j % 4)) in
            0)
                LINE="{\"timestamp\":\"$TIMESTAMP\",\"event\":\"$EVENT\",\"user_id\":\"$USER\",\"session_id\":\"$SESSION_ID\",\"properties\":{\"product\":\"$PRODUCT\",\"amount\":$AMOUNT,\"currency\":\"USD\",\"country\":\"$COUNTRY\"}}"
                ;;
            1)
                LINE="{\"timestamp\":\"$TIMESTAMP\",\"event\":\"page_view\",\"user_id\":\"$USER\",\"session_id\":\"$SESSION_ID\",\"properties\":{\"page\":\"/product/$PRODUCT\",\"referrer\":\"google.com\",\"device\":\"desktop\"}}"
                ;;
            2)
                LINE="{\"timestamp\":\"$TIMESTAMP\",\"event\":\"button_click\",\"user_id\":\"$USER\",\"session_id\":\"$SESSION_ID\",\"properties\":{\"button_id\":\"add-to-cart\",\"product\":\"$PRODUCT\",\"category\":\"electronics\"}}"
                ;;
            3)
                LINE="{\"timestamp\":\"$TIMESTAMP\",\"event\":\"api_call\",\"user_id\":\"$USER\",\"session_id\":\"$SESSION_ID\",\"properties\":{\"endpoint\":\"/api/v1/orders\",\"method\":\"POST\",\"status_code\":200,\"response_time_ms\":$((50 + RANDOM % 200))}}"
                ;;
        esac

        if [ -n "$NDJSON_PAYLOAD" ]; then
            NDJSON_PAYLOAD="$NDJSON_PAYLOAD\n$LINE"
        else
            NDJSON_PAYLOAD="$LINE"
        fi
    done

    # Send HTTP POST request
    RESPONSE=$(curl -s -w "%{http_code}" -X POST \
        -H "Content-Type: application/x-ndjson" \
        -H "User-Agent: fake-feed/1.0" \
        --data-raw "$NDJSON_PAYLOAD" \
        "http://$HOST:$PORT$ENDPOINT")

    HTTP_CODE="${RESPONSE: -3}"

    if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "202" ]; then
        echo "[$i/$COUNT] ✓ HTTP $HTTP_CODE: Sent $(echo -e "$NDJSON_PAYLOAD" | wc -l) NDJSON events for user $USER"
    else
        echo "[$i/$COUNT] ✗ HTTP $HTTP_CODE: Failed to send NDJSON events"
    fi

    if [ $i -lt $COUNT ]; then
        sleep $DELAY
    fi
done

echo ""
echo "✓ Completed sending $COUNT HTTP NDJSON requests"
echo "Configure your HTTP plugin with data_hint: \"ndjson\" to process these requests"