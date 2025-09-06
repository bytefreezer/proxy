# Testing Guide

## Quick Test

```bash
# Start service
./bytefreezer-proxy --config config.yaml

# Send test data
echo '{"test": "message"}' | nc -u localhost 2056

# Check health
curl http://localhost:8088/health
```

## Test Scripts

### UDP Streams Test
```bash
./testing_scripts/test_udp_streams.sh
```
Tests all UDP ports (2056, 2057, 2058) and verifies spooling.

### Continuous Load Test
```bash
./testing_scripts/test_continuous.sh
```
Sends continuous data to test batching and performance.

### Check Spooling
```bash
./testing_scripts/check_spooling.sh
```
Shows spooled files and directory status.

## Manual Testing

### Send Data to Different Ports
```bash
# Syslog data (port 2056)
echo '{"level": "info", "message": "test log"}' | nc -u localhost 2056

# eBPF data (port 2057)  
echo '{"event": "process_exec", "pid": 1234}' | nc -u localhost 2057

# Application logs (port 2058)
echo '{"app": "web", "status": "started"}' | nc -u localhost 2058
```

### Check Results
```bash
# View spooled files
ls -la /var/spool/bytefreezer-proxy/

# Check service metrics
curl http://localhost:8088/metrics

# View logs
tail -f /var/log/bytefreezer-proxy/bytefreezer-proxy.log
```

## Docker Testing

```bash
# Run with Docker
docker run -p 8088:8088 -p 2056-2058:2056-2058/udp ghcr.io/n0needt0/bytefreezer-proxy:latest

# Test from host
echo '{"test": "docker"}' | nc -u localhost 2056
```

## Ansible Testing

```bash
cd ansible/playbooks

# Deploy to test server
ansible-playbook -i inventory install.yml --limit test-server

# Remove after testing
ansible-playbook -i inventory remove.yml --limit test-server
```