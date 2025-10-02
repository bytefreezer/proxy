# sFlow Testing Tools

## Overview
Tools for testing the sFlow plugin with real sFlow v5 packets.

## Tools

### sflow_generator.py (Recommended)
Python script that generates **real sFlow v5 binary packets** conforming to the sFlow specification.

**Usage:**
```bash
python3 fake_feeds/sflow_generator.py --host localhost --port 2066 --rate 10 --duration 5
```

**Options:**
- `--host`, `-H`: Collector host (default: localhost)
- `--port`, `-p`: Collector port (default: 2066)
- `--rate`, `-r`: Packets per second (default: 10)
- `--duration`, `-d`: Duration in seconds (default: unlimited)

**Features:**
- Generates standards-compliant sFlow v5 packets
- Uses realistic IP ranges from different geographic regions
- Includes multiple flow samples per datagram
- Works with goflow2 and other sFlow parsers

### test_sflow.sh (Wrapper)
Shell wrapper for the Python generator - easier to use from command line.

**Usage:**
```bash
./fake_feeds/test_sflow.sh [host] [port] [rate] [duration]

# Examples:
./fake_feeds/test_sflow.sh                      # Default: localhost:2066, 10pps, 5 seconds
./fake_feeds/test_sflow.sh localhost 2066 50 10 # Custom: 50 packets/sec for 10 seconds
```

### udp_sflow.sh (Deprecated)
**⚠️ DO NOT USE** - This script generates malformed data that does not conform to sFlow v5 specification. It will fail to decode with standards-compliant parsers like goflow2.

The script is kept for reference only and will exit with a warning if run.

## Testing Workflow

1. **Start the proxy** with sFlow plugin enabled:
   ```bash
   ./bytefreezer-proxy --config config-test-flows.yaml
   ```

2. **Generate test traffic** using the Python generator:
   ```bash
   python3 fake_feeds/sflow_generator.py --host localhost --port 2066 --rate 10 --duration 10
   ```

3. **Verify decoding** in proxy logs:
   ```bash
   tail -f /tmp/proxy.log | grep "Stored sFlow message"
   ```

4. **Check output files**:
   ```bash
   # Raw NDJSON files (before batching)
   ls -lh /tmp/bytefreezer-proxy-spool/test-tenant/sflow-data/raw/

   # Batched files (ready for upload)
   ls -lh /tmp/bytefreezer-proxy-spool/test-tenant/sflow-data/queue/
   ```

5. **Inspect the data**:
   ```bash
   # View raw NDJSON (pretty-print)
   cat /tmp/bytefreezer-proxy-spool/test-tenant/sflow-data/raw/*.ndjson | jq .

   # View batched data (decompress first)
   zcat /tmp/bytefreezer-proxy-spool/test-tenant/sflow-data/queue/*.gz | jq .
   ```

## Expected Output

### Successful Decoding
```json
{
  "version": 5,
  "ip-version": 1,
  "agent-ip": "52.20.45.123",
  "sub-agent-id": 0,
  "sequence-number": 1,
  "uptime": 1759434303544,
  "samples-count": 2,
  "samples": [
    {
      "header": {
        "format": 1,
        "length": 112,
        "sample-sequence-number": 1,
        "source-id-type": 0,
        "source-id-value": 23
      },
      "sampling-rate": 1024,
      "sample-pool": 45678,
      "drops": 0,
      "input": 1,
      "output": 2,
      "flow-records-count": 1,
      "records": [...]
    }
  ]
}
```

### Log Output
```
{"level":"DEBUG","msg":"Stored sFlow message from 127.0.0.1:59678 as NDJSON directly to filesystem (978 bytes)"}
```

## Troubleshooting

### Error: "unknown IP version"
This means the data is not valid sFlow v5 format. Make sure you're using:
- `sflow_generator.py` (Python) - ✅ CORRECT
- NOT `udp_sflow.sh` (shell) - ❌ MALFORMED DATA

### No packets received
1. Check proxy is listening on correct port: `netstat -uln | grep 2066`
2. Check firewall: `sudo ufw status`
3. Verify config: `./bytefreezer-proxy --validate-config --config config-test-flows.yaml`

### Packets received but not decoded
1. Check logs for decode errors: `grep "Failed to decode" /tmp/proxy.log`
2. Verify you're using real sFlow v5 data (use Python generator)
3. Check goflow2 library is installed: `go list -m github.com/netsampler/goflow2/v2`

## Real-World Testing

To test with real network devices:

1. **Configure your network device** to export sFlow to the proxy:
   - Target IP: Your proxy server IP
   - Target Port: 2066 (or your configured port)
   - Sampling Rate: 1:1000 (recommended)

2. **Configure the proxy** with production settings:
   ```yaml
   - type: "sflow"
     name: "sflow-collector"
     config:
       host: "0.0.0.0"
       port: 6343                    # Standard sFlow port
       dataset_id: "network-flows"
       protocol: "sflow"
       data_hint: "ndjson"
       read_buffer_size: 65536
       worker_count: 4
   ```

3. **Monitor incoming traffic**:
   ```bash
   # Watch packet count
   watch -n 1 'grep "Stored sFlow message" /tmp/proxy.log | wc -l'

   # Monitor for errors
   tail -f /tmp/proxy.log | grep -E "ERROR|WARN"
   ```

## References

- [sFlow Specification](https://sflow.org/sflow_version_5.txt)
- [goflow2 Library](https://github.com/netsampler/goflow2)
- [ByteFreezer Flow Plugins Documentation](../FLOW_PLUGINS_UPDATE.md)
