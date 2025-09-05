# ByteFreezer Proxy Testing Guide

This document explains how to test the ByteFreezer Proxy UDP functionality and spooling behavior.

## Test Scripts

### 1. `test_udp_streams.sh` - Single Test Run

Sends test data to all three configured UDP ports and checks the spooling directory.

```bash
./testing_scripts/test_udp_streams.sh
```

**What it does:**
- Sends JSON data to ports 2056 (syslog-data), 2057 (ebpf-data), and 2058 (application-logs)
- Waits for processing and checks the spooling directory
- Shows detailed information about spooled files and metadata

### 2. `test_continuous.sh` - Continuous Testing

Sends data continuously to test batching behavior and load handling.

```bash
./testing_scripts/test_continuous.sh
```

**Configuration (edit the script to modify):**
- `INTERVAL=5` - seconds between sends
- `COUNT=10` - number of iterations
- Automatically rotates through different data patterns

### 3. `check_spooling.sh` - Monitor Spooling Directory

Checks and displays the current state of the spooling directory.

```bash
./testing_scripts/check_spooling.sh

# For real-time monitoring:
watch -n2 ./testing_scripts/check_spooling.sh
```

## Test Scenarios

### Scenario 1: Test Spooling (Receiver Unavailable)

1. **Ensure receiver is NOT running** (or set invalid URL in config)
2. **Start the proxy:**
   ```bash
   ./bytefreezer-proxy --config config.yaml
   ```
3. **Send test data:**
   ```bash
   ./testing_scripts/test_udp_streams.sh
   ```
4. **Check spooling directory:**
   ```bash
   ./testing_scripts/check_spooling.sh
   ```

**Expected Result:** Files should appear in `/tmp/bytefreezer-proxy/`

### Scenario 2: Test Successful Forwarding

1. **Start bytefreezer-receiver** (ensure it's running on localhost:8080)
2. **Start the proxy:**
   ```bash
   ./bytefreezer-proxy --config config.yaml
   ```
3. **Send test data:**
   ```bash
   ./testing_scripts/test_udp_streams.sh
   ```
4. **Check results:**
   ```bash
   ./testing_scripts/check_spooling.sh  # Should show no spooled files
   ```

**Expected Result:** No files in spooling directory (data forwarded successfully)

### Scenario 3: Test Batching and Load

1. **Start the proxy**
2. **Run continuous test:**
   ```bash
   ./testing_scripts/test_continuous.sh
   ```
3. **Monitor in real-time:**
   ```bash
   # In another terminal:
   watch -n2 ./testing_scripts/check_spooling.sh
   ```

## Port Configuration

The test scripts target these ports based on your `config.yaml`:

| Port | Dataset ID | Description |
|------|------------|-------------|
| 2056 | syslog-data | System logs in syslog format |
| 2057 | ebpf-data | eBPF/kernel events |
| 2058 | application-logs | Application-specific logs |

## Understanding the Output

### Spooled Files Structure

```
/tmp/bytefreezer-proxy/
├── 1641234567890_customer-1_syslog-data.ndjson    # Data file
├── 1641234567890_customer-1_syslog-data.meta      # Metadata file
└── ...
```

### Metadata Content

```json
{
  "id": "1641234567890_customer-1_syslog-data",
  "tenant_id": "customer-1", 
  "dataset_id": "syslog-data",
  "filename": "1641234567890_customer-1_syslog-data.ndjson",
  "size": 1024,
  "created_at": "2025-01-03T15:30:45Z",
  "last_retry": "2025-01-03T15:31:45Z",
  "retry_count": 1,
  "failure_reason": "HTTP request failed: dial tcp: connection refused"
}
```

## Troubleshooting

### No Data in Spooling Directory

This is **normal** when:
- Receiver is available and working correctly
- Data is being forwarded successfully
- No errors occurred during forwarding

### Data Appears in Spooling Directory

This indicates:
- Receiver is unavailable (connection refused)
- Authentication failure (invalid bearer token)
- Network connectivity issues
- Receiver returned HTTP error status

### Common Issues

1. **"nc: command not found"**
   ```bash
   # Ubuntu/Debian:
   sudo apt-get install netcat-openbsd
   
   # CentOS/RHEL:
   sudo yum install nc
   
   # macOS:
   brew install netcat
   ```

2. **Permission denied on spooling directory**
   ```bash
   sudo mkdir -p /tmp/bytefreezer-proxy
   sudo chown $USER:$USER /tmp/bytefreezer-proxy
   ```

3. **Proxy not receiving data**
   - Check if proxy is running: `curl http://localhost:8088/health`
   - Verify UDP ports are open: `netstat -ulnp | grep -E '205[6-8]'`
   - Check proxy logs for errors

## Advanced Testing

### Custom Data Testing

Send your own JSON data:

```bash
echo '{"custom":"data","timestamp":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}' | nc -u localhost 2056
```

### Load Testing

```bash
# Send 100 messages quickly
for i in {1..100}; do
  echo '{"test_id":'$i',"timestamp":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}' | nc -u localhost 2056
done
```

### Monitoring Proxy Logs

```bash
# If running as systemd service:
journalctl -f -u bytefreezer-proxy

# If running directly:
./bytefreezer-proxy --config config.yaml
```