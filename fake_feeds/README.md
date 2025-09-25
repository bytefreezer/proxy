# Fake Feed Scripts for ByteFreezer Proxy Testing

This directory contains test scripts that generate fake data feeds for testing different data format hints with the ByteFreezer proxy.

## Available Scripts

### UDP Protocol Scripts

| Script | Data Hint | Description |
|--------|-----------|-------------|
| `udp_sflow.sh` | `sflow` | Generates fake sFlow v5 packets (binary network monitoring data) |
| `udp_netflow.sh` | `netflow` | Generates fake NetFlow v5 packets (binary flow data) |
| `udp_syslog.sh` | `syslog` | Generates RFC3164/RFC5424 syslog messages |
| `udp_ndjson.sh` | `ndjson` | Generates structured application logs in NDJSON format |
| `udp_raw.sh` | `raw` | Generates unstructured/legacy log formats |

### HTTP Protocol Scripts

| Script | Data Hint | Description |
|--------|-----------|-------------|
| `http_ndjson.sh` | `ndjson` | Sends NDJSON data via HTTP webhook |

## Usage

### Basic Usage
```bash
# Send 10 sFlow packets to localhost:2056
./fake_feeds/udp_sflow.sh

# Send 20 syslog messages to remote host
./fake_feeds/udp_syslog.sh remote-host.com 514 20 1

# Send HTTP NDJSON events
./fake_feeds/http_ndjson.sh localhost 8081 /webhook 5 2
```

### Parameters

All scripts accept the following parameters:

**UDP Scripts:**
```bash
script.sh [HOST] [PORT] [COUNT] [DELAY_SECONDS]
```

**HTTP Scripts:**
```bash
script.sh [HOST] [PORT] [ENDPOINT] [COUNT] [DELAY_SECONDS]
```

### Default Values

| Parameter | UDP Default | HTTP Default |
|-----------|-------------|--------------|
| HOST | `localhost` | `localhost` |
| PORT | `2056` | `8081` |
| ENDPOINT | N/A | `/webhook` |
| COUNT | `10` | `5` |
| DELAY | `1-2s` | `2s` |

## Data Format Examples

### sFlow (Binary)
```bash
# Generates binary sFlow v5 packets with:
# - Agent address, sequence numbers
# - Flow samples and counter samples
# - Both binary and text representations
./fake_feeds/udp_sflow.sh localhost 2056 5 2
```

### NetFlow (Binary)
```bash
# Generates NetFlow v5 packets with:
# - Flow records (src/dst IP, ports, protocols)
# - Packet/byte counters
# - Realistic network flow data
./fake_feeds/udp_netflow.sh localhost 2056 10 1
```

### Syslog (Structured Text)
```bash
# Generates mixed RFC3164/RFC5424 syslog:
# - Different facilities and severities
# - Multiple hostname/process combinations
# - Realistic system log messages
./fake_feeds/udp_syslog.sh localhost 514 15 1
```

### NDJSON (Structured)
```bash
# Generates application event logs:
# - User actions, API calls, errors
# - Business metrics, order events
# - Structured JSON with consistent schema
./fake_feeds/udp_ndjson.sh localhost 2056 20 1
```

### Raw (Unstructured)
```bash
# Generates legacy system logs:
# - Multiple proprietary formats
# - Key=value, delimited, custom formats
# - Simulates real-world legacy systems
./fake_feeds/udp_raw.sh localhost 2056 10 2
```

## Configuration Examples

### Proxy Configuration

Configure your ByteFreezer proxy to handle different data hints:

```yaml
inputs:
  # sFlow monitoring
  - type: "udp"
    name: "sflow-collector"
    config:
      host: "0.0.0.0"
      port: 6343
      dataset_id: "network-sflow"
      data_hint: "sflow"

  # NetFlow monitoring
  - type: "udp"
    name: "netflow-collector"
    config:
      host: "0.0.0.0"
      port: 2055
      dataset_id: "network-flows"
      data_hint: "netflow"

  # Syslog collection
  - type: "udp"
    name: "syslog-collector"
    config:
      host: "0.0.0.0"
      port: 514
      dataset_id: "system-logs"
      data_hint: "syslog"

  # Application logs
  - type: "udp"
    name: "app-logs"
    config:
      host: "0.0.0.0"
      port: 2056
      dataset_id: "application-events"
      data_hint: "ndjson"

  # Legacy systems
  - type: "udp"
    name: "legacy-logs"
    config:
      host: "0.0.0.0"
      port: 5140
      dataset_id: "legacy-data"
      data_hint: "raw"

  # HTTP webhooks
  - type: "http"
    name: "webhook-endpoint"
    config:
      host: "0.0.0.0"
      port: 8081
      path: "/webhook"
      dataset_id: "webhook-events"
      data_hint: "ndjson"
```

## Testing Workflow

1. **Start ByteFreezer Proxy**:
   ```bash
   ./bytefreezer-proxy --config config.yaml
   ```

2. **Run Feed Scripts**:
   ```bash
   # Test different data formats
   ./fake_feeds/udp_sflow.sh localhost 6343 5 2
   ./fake_feeds/udp_netflow.sh localhost 2055 10 1
   ./fake_feeds/udp_syslog.sh localhost 514 15 1
   ./fake_feeds/udp_ndjson.sh localhost 2056 20 1
   ./fake_feeds/http_ndjson.sh localhost 8081 /webhook 5 2
   ```

3. **Monitor Proxy Logs**:
   - Check data ingestion and processing
   - Verify data hints are applied correctly
   - Monitor upload success to receiver

4. **Verify Downstream Processing**:
   - Check ByteFreezer receiver logs
   - Verify data format recognition
   - Confirm Parquet conversion (for structured formats)

## Script Features

- **Realistic Data**: All scripts generate realistic test data
- **Multiple Formats**: Each script creates varied message formats
- **Progress Indication**: Scripts show progress and completion status
- **Error Handling**: Basic error detection for HTTP scripts
- **Configurable**: All parameters can be customized
- **Self-Documenting**: Scripts output their configuration and status

## Dependencies

- `bash` (all scripts)
- `nc` (netcat) for UDP transmission
- `curl` for HTTP transmission
- `xxd` for binary data (sFlow/NetFlow)
- `bc` for calculations (optional)
- `jq` for JSON formatting (optional, graceful fallback)
- `uuidgen` for generating unique IDs