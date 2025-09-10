# ByteFreezer Proxy Test Scripts

This directory contains comprehensive test scripts for all protocols supported by ByteFreezer Proxy.

## Available Test Scripts

### Protocol-Specific Scripts

| Script | Protocol | Description |
|--------|----------|-------------|
| `test-syslog.py` | RFC3164/RFC5424 Syslog | Generates realistic syslog messages |
| `test-netflow-v5.py` | NetFlow v5 | Creates fixed-format NetFlow v5 packets |
| `test-netflow-v9.py` | NetFlow v9/IPFIX | Generates template-based flows |
| `test-sflow.py` | sFlow v5 | Creates flow and counter samples |

### Comprehensive Test Runner

| Script | Description |
|--------|-------------|
| `test-all-protocols.sh` | Runs all protocol tests with various scenarios |

## Quick Start

### Run All Tests
```bash
# Quick test (5 packets per protocol)
./test-all-protocols.sh --quick

# Normal test (20 packets per protocol)
./test-all-protocols.sh

# Stress test (sustained load)
./test-all-protocols.sh --stress --verbose
```

### Individual Protocol Tests
```bash
# Test syslog (RFC3164 and RFC5424)
python3 test-syslog.py --host localhost --port 2056 --count 10

# Test NetFlow v5
python3 test-netflow-v5.py --host localhost --port 2057 --count 10

# Test NetFlow v9
python3 test-netflow-v9.py --host localhost --port 2058 --protocol v9

# Test IPFIX
python3 test-netflow-v9.py --host localhost --port 2059 --protocol ipfix

# Test sFlow
python3 test-sflow.py --host localhost --port 2060 --type flow
```

## Configuration Requirements

### ByteFreezer Proxy Configuration

Ensure your `config.yaml` includes the test ports:

```yaml
udp:
  enabled: true
  host: "0.0.0.0"
  listeners:
    - port: 2056
      dataset_id: "syslog-test"
      protocol: "syslog"
    - port: 2057
      dataset_id: "netflow-v5-test"
      protocol: "netflow"
    - port: 2058
      dataset_id: "netflow-v9-test"
      protocol: "netflow"
    - port: 2059
      dataset_id: "ipfix-test"
      protocol: "netflow"
    - port: 2060
      dataset_id: "sflow-test"
      protocol: "sflow"
```

### Prerequisites

- **Python 3.6+** (standard library only)
- **ByteFreezer Proxy** running with configured ports
- **Network connectivity** to proxy host

## Test Script Features

### Syslog Test (`test-syslog.py`)
- **Message Types**: RFC3164, RFC5424, mixed
- **Realistic Content**: Web server, system, security, application logs
- **Stress Testing**: High-rate message generation
- **Validation**: Message format verification

```bash
# Examples
python3 test-syslog.py --type rfc3164 --count 50
python3 test-syslog.py --stress --stress-rate 200 --stress-duration 60
python3 test-syslog.py --validate
```

### NetFlow v5 Test (`test-netflow-v5.py`)
- **Realistic Flows**: HTTP, HTTPS, SSH, DNS, ICMP, etc.
- **Multiple Records**: Up to 8 flows per packet
- **Burst Mode**: Send multiple packets rapidly
- **Validation**: Packet structure verification

```bash
# Examples
python3 test-netflow-v5.py --count 20 --burst 3
python3 test-netflow-v5.py --validate --verbose
```

### NetFlow v9/IPFIX Test (`test-netflow-v9.py`)
- **Template Support**: Automatic template generation and caching
- **Protocol Selection**: NetFlow v9 or IPFIX
- **Template Frequency**: Configurable template refresh rate
- **Field Types**: 17 standard IPFIX field types

```bash
# Examples
python3 test-netflow-v9.py --protocol v9 --template-freq 5
python3 test-netflow-v9.py --protocol ipfix --count 30
```

### sFlow Test (`test-sflow.py`)
- **Sample Types**: Flow samples, counter samples, mixed
- **Flow Records**: Raw packet headers with Ethernet/IP/TCP parsing
- **Counter Records**: Interface statistics
- **Test Scenarios**: Comprehensive scenario runner

```bash
# Examples
python3 test-sflow.py --type flow --count 15
python3 test-sflow.py --scenarios
python3 test-sflow.py --agent-ip 192.168.1.254 --type mixed
```

## Advanced Usage

### Custom Host and Ports
```bash
# Test remote proxy
./test-all-protocols.sh --host 192.168.1.100 --syslog-port 514

# Custom port mapping
python3 test-netflow-v5.py --host 10.0.0.1 --port 9996
```

### Validation and Debugging
```bash
# Validate all packet structures
./test-all-protocols.sh --validate

# Verbose output for debugging
python3 test-sflow.py --verbose --count 5
```

### Stress Testing
```bash
# High-rate syslog messages
python3 test-syslog.py --stress --stress-rate 500 --stress-duration 120

# Comprehensive stress test
./test-all-protocols.sh --stress --verbose
```

## Output and Monitoring

### Test Output
- **Success Messages**: Green colored success indicators
- **Error Messages**: Red colored error indicators  
- **Progress Tracking**: Packet counts and rates
- **Validation Results**: Packet structure verification

### Monitoring Proxy
While tests are running, monitor the proxy:

```bash
# Check proxy health
curl http://localhost:8088/health

# Monitor proxy logs
tail -f /var/log/bytefreezer-proxy/proxy.log

# Check receiver endpoint
curl http://receiver-host:8080/health
```

### Expected Results
Successful tests should show:
- All packets sent without errors
- Proxy health endpoint responding
- Data appearing in ByteFreezer Receiver
- S3 storage with format-partitioned data

## Troubleshooting

### Common Issues

**Connection Refused**
```
Error: [Errno 111] Connection refused
```
- Ensure ByteFreezer Proxy is running
- Check port configuration matches test scripts
- Verify firewall settings

**Permission Denied** 
```
Error: [Errno 13] Permission denied
```
- Use ports > 1024 for non-root users
- Run with `sudo` if using privileged ports (< 1024)

**Python Not Found**
```
python3: command not found
```
- Install Python 3.6+ on your system
- Use `python` instead of `python3` if needed

### Debugging Steps

1. **Validate Packet Structure**:
   ```bash
   python3 test-netflow-v5.py --validate
   ```

2. **Test Connectivity**:
   ```bash
   nc -u localhost 2056
   ```

3. **Check Proxy Status**:
   ```bash
   curl http://localhost:8088/health
   ```

4. **Monitor Network Traffic**:
   ```bash
   sudo tcpdump -i lo udp port 2056
   ```

## Integration with CI/CD

### GitHub Actions Example
```yaml
- name: Test ByteFreezer Proxy
  run: |
    cd bytefreezer-proxy/scripts
    ./test-all-protocols.sh --quick --verbose
```

### Docker Testing
```bash
# Run proxy in container
docker run -d -p 2056-2065:2056-2065/udp bytefreezer-proxy

# Run tests against container
./test-all-protocols.sh --host localhost
```

## Contributing

When adding new test scripts:

1. **Follow naming convention**: `test-protocol.py`
2. **Include validation**: Add `--validate` option
3. **Support verbose mode**: Add `--verbose` option
4. **Add error handling**: Graceful failure handling
5. **Update documentation**: Add script to this README

## Support

For issues with test scripts:
- Check ByteFreezer Proxy logs
- Verify network connectivity  
- Validate proxy configuration
- Review test script output with `--verbose`