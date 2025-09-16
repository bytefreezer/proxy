# Testing Guide

## Quick Test - Plugin System

```bash
# Start service with plugin configuration
./bytefreezer-proxy --config config.yaml

# Test UDP plugin
echo '{"test": "message"}' | nc -u localhost 2055

# Check health
curl http://localhost:8080/health
```

## Plugin Testing

### Test Plugin Health
```bash
# Check all plugins
curl http://localhost:8080/api/plugins/health

# Get plugin metrics
curl http://localhost:8080/metrics | grep plugin
```

### Test Different Input Plugins
```bash
# If Kafka plugin is configured
kafka-console-producer --broker-list localhost:9092 --topic test-topic

# If NATS plugin is configured
nats pub test.subject "test message"
```

## Manual Testing

### Send Data to Plugin Inputs
```bash
# UDP plugin (default port 2055)
echo '{"level": "info", "message": "test log"}' | nc -u localhost 2055

# Test plugin health
curl http://localhost:8080/api/plugins/health

# View plugin metrics
curl http://localhost:8080/metrics
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
docker run -p 8080:8080 -p 2055:2055/udp ghcr.io/n0needt0/bytefreezer-proxy:latest

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