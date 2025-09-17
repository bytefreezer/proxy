# ByteFreezer Proxy - Monitoring Setup

This document describes the comprehensive monitoring setup for ByteFreezer Proxy using OpenTelemetry with Prometheus integration.

## Overview

The ByteFreezer Proxy provides detailed metrics through OpenTelemetry with Prometheus integration. This setup includes:

- **Metrics Collection**: Prometheus scraping from HTTP endpoint (configurable port)
- **Plugin Metrics**: Per-plugin performance and health monitoring
- **System Metrics**: Go runtime and system resource monitoring
- **Custom Metrics**: Proxy-specific operational metrics
- **Health Monitoring**: API endpoints for service health checks

## Available Metrics

The proxy exposes the following key metrics:

### Core Proxy Metrics
- `bytefreezer_proxy_uptime_seconds` - Service uptime in seconds
- `bytefreezer_proxy_plugin_status` - Plugin health status (0=unhealthy, 1=healthy)
- `bytefreezer_proxy_batch_processing_duration_seconds` - Batch processing time histogram
- `bytefreezer_proxy_http_requests_total` - Total HTTP forwarding requests by status

### Plugin-Specific Metrics

#### UDP Plugin Metrics
- `bytefreezer_proxy_udp_bytes_received_total` - Total bytes received per port/dataset
- `bytefreezer_proxy_udp_packets_received_total` - Total packets received per port/dataset  
- `bytefreezer_proxy_udp_packets_dropped_total` - Total dropped packets per port/dataset
- `bytefreezer_proxy_udp_processing_duration_seconds` - UDP processing time histogram

#### HTTP Plugin Metrics
- `bytefreezer_proxy_http_webhook_requests_total` - Total webhook requests per endpoint/status
- `bytefreezer_proxy_http_webhook_bytes_total` - Total bytes received per endpoint
- `bytefreezer_proxy_http_auth_failures_total` - Authentication failures per endpoint
- `bytefreezer_proxy_http_payload_too_large_total` - Payload size violations per endpoint
- `bytefreezer_proxy_http_request_duration_seconds` - HTTP request processing time histogram

#### Kafka Plugin Metrics  
- `bytefreezer_proxy_kafka_messages_consumed_total` - Total messages consumed per topic
- `bytefreezer_proxy_kafka_bytes_consumed_total` - Total bytes consumed per topic
- `bytefreezer_proxy_kafka_consumer_lag` - Consumer lag per topic/partition
- `bytefreezer_proxy_kafka_connection_errors_total` - Connection errors per broker

#### NATS Plugin Metrics
- `bytefreezer_proxy_nats_messages_received_total` - Total messages received per subject
- `bytefreezer_proxy_nats_bytes_received_total` - Total bytes received per subject
- `bytefreezer_proxy_nats_connection_errors_total` - Connection errors per server
- `bytefreezer_proxy_nats_reconnects_total` - Reconnection attempts per server

### Forwarding & Spooling Metrics
- `bytefreezer_proxy_batches_forwarded_total` - Total batches forwarded by status
- `bytefreezer_proxy_batch_size_bytes` - Batch size distribution histogram
- `bytefreezer_proxy_batch_size_lines` - Batch line count distribution histogram
- `bytefreezer_proxy_spool_queue_size` - Files in spool queue
- `bytefreezer_proxy_spool_queue_bytes` - Bytes in spool queue
- `bytefreezer_proxy_dlq_files_total` - Files in dead letter queue

### System Metrics (Go Runtime)
- `go_goroutines` - Number of goroutines
- `go_memstats_alloc_bytes` - Allocated memory bytes
- `go_memstats_sys_bytes` - System memory bytes
- `process_cpu_seconds_total` - CPU usage
- `process_resident_memory_bytes` - Resident memory usage

## Configuration

### Enable Monitoring

Add OTEL configuration to your `config.yaml`:

```yaml
otel:
  enabled: true                    # Enable OpenTelemetry metrics
  service_name: "bytefreezer-proxy"
  prometheus_mode: true            # Enable Prometheus format
  metrics_port: 9090               # Metrics endpoint port
  metrics_host: "0.0.0.0"          # Bind address (0.0.0.0 for all interfaces)
  scrapeIntervalseconds: 15        # Metrics collection interval
```

### Production Configuration

```yaml
otel:
  enabled: true
  service_name: "bytefreezer-proxy-prod"
  prometheus_mode: true
  metrics_port: 9090
  metrics_host: "127.0.0.1"        # Restrict to localhost in production
  scrapeIntervalseconds: 30        # Less frequent collection for performance
```

### Development Configuration

```yaml
otel:
  enabled: true
  service_name: "bytefreezer-proxy-dev"
  prometheus_mode: true
  metrics_port: 9090
  metrics_host: "0.0.0.0"
  scrapeIntervalseconds: 5         # Frequent collection for debugging
```

## Deployment Options

### 1. Standalone Deployment

For production deployments, the proxy exposes metrics on the configured port that can be scraped by existing Prometheus instances.

#### Prometheus Configuration

Add this job to your existing `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'bytefreezer-proxy'
    static_configs:
      - targets: ['proxy-host:9090']
    metrics_path: /metrics
    scrape_interval: 30s
    scrape_timeout: 10s
    honor_labels: true
```

#### Multi-Instance Monitoring

For multiple proxy instances:

```yaml
scrape_configs:
  - job_name: 'bytefreezer-proxy'
    static_configs:
      - targets: 
          - 'proxy-1:9090'
          - 'proxy-2:9090'
          - 'proxy-3:9090'
    metrics_path: /metrics
    scrape_interval: 30s
    relabel_configs:
      - source_labels: [__address__]
        target_label: instance
      - source_labels: [__address__]
        regex: '([^:]+):.+'
        target_label: hostname
        replacement: '${1}'
```

### 2. Docker Deployment

#### Docker Compose with Monitoring

```yaml
version: '3.8'
services:
  bytefreezer-proxy:
    image: ghcr.io/n0needt0/bytefreezer-proxy:latest
    ports:
      - "8088:8088"     # API
      - "9090:9090"     # Metrics
      - "2056:2056/udp" # UDP
      - "8081:8081"     # HTTP
    volumes:
      - ./config.yaml:/config.yaml
    environment:
      - OTEL_ENABLED=true
      - OTEL_METRICS_PORT=9090
      
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/etc/prometheus/console_libraries'
      - '--web.console.templates=/etc/prometheus/consoles'
      
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin123
    volumes:
      - grafana-storage:/var/lib/grafana
      
volumes:
  grafana-storage:
```

### 3. Kubernetes Deployment

#### ServiceMonitor for Prometheus Operator

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: bytefreezer-proxy
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app: bytefreezer-proxy
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

#### PodMonitor for Pod-level Discovery

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: bytefreezer-proxy
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app: bytefreezer-proxy
  podMetricsEndpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

#### Service Definition

```yaml
apiVersion: v1
kind: Service
metadata:
  name: bytefreezer-proxy
  labels:
    app: bytefreezer-proxy
spec:
  ports:
    - name: api
      port: 8088
      targetPort: 8088
    - name: metrics
      port: 9090
      targetPort: 9090
    - name: udp
      port: 2056
      targetPort: 2056
      protocol: UDP
    - name: http
      port: 8081
      targetPort: 8081
  selector:
    app: bytefreezer-proxy
```

### 4. Ansible Deployment

The Ansible playbooks support metrics configuration:

```bash
# Deploy with monitoring enabled
ansible-playbook -i inventory.yml install.yml \
  -e otel_enabled=true \
  -e otel_metrics_port=9090 \
  -e otel_prometheus_mode=true
```

Ansible variables for monitoring:

```yaml
# inventory.yml or group_vars/all.yml
otel_enabled: true
otel_service_name: "bytefreezer-proxy-{{ inventory_hostname }}"
otel_metrics_port: 9090
otel_metrics_host: "0.0.0.0"
otel_scrape_interval: 30
```

## Testing Monitoring

### Enable and Test Monitoring

1. **Enable monitoring in config:**
   ```yaml
   otel:
     enabled: true
     metrics_port: 9090
   ```

2. **Start the proxy:**
   ```bash
   ./bytefreezer-proxy --config config.yaml
   ```

3. **Test metrics endpoint:**
   ```bash
   curl http://localhost:9090/metrics
   ```

4. **Run monitoring integration test:**
   ```bash
   cd integration-tests
   ./test-with-monitoring.sh
   ```

### Verify Metrics Collection

#### Check Metrics Endpoint
```bash
# Test metrics availability
curl -s http://localhost:9090/metrics | head -20

# Check for proxy-specific metrics
curl -s http://localhost:9090/metrics | grep bytefreezer_proxy

# Check for plugin metrics
curl -s http://localhost:9090/metrics | grep -E "(udp|http|kafka|nats)_"
```

#### Generate Test Data

```bash
# Generate UDP data for metrics
for i in {1..100}; do
  echo "Test message $i" | nc -u localhost 2056
done

# Generate HTTP data for metrics  
for i in {1..50}; do
  curl -X POST http://localhost:8081/webhook \
    -H "Content-Type: application/json" \
    -d "{\"test\":$i,\"timestamp\":\"$(date -Iseconds)\"}"
done

# Check updated metrics
curl -s http://localhost:9090/metrics | grep _total
```

#### Verify Metric Labels

```bash
# Check metric labels for plugin identification
curl -s http://localhost:9090/metrics | grep 'plugin=' | head -5

# Check tenant/dataset labels
curl -s http://localhost:9090/metrics | grep 'tenant_id=' | head -5

# Check status labels
curl -s http://localhost:9090/metrics | grep 'status=' | head -5
```

## Grafana Dashboards

### Import Proxy Dashboard

Create a Grafana dashboard with the following panels:

#### 1. Overview Panel
```json
{
  "title": "ByteFreezer Proxy Overview",
  "type": "stat",
  "targets": [
    {
      "expr": "bytefreezer_proxy_uptime_seconds",
      "legendFormat": "Uptime (seconds)"
    }
  ]
}
```

#### 2. Plugin Status Panel
```json
{
  "title": "Plugin Health Status",
  "type": "stat",
  "targets": [
    {
      "expr": "bytefreezer_proxy_plugin_status",
      "legendFormat": "{{plugin}} - {{dataset_id}}"
    }
  ]
}
```

#### 3. Throughput Panel
```json
{
  "title": "Data Throughput",
  "type": "graph",
  "targets": [
    {
      "expr": "rate(bytefreezer_proxy_udp_bytes_received_total[5m])",
      "legendFormat": "UDP {{port}} - {{dataset_id}}"
    },
    {
      "expr": "rate(bytefreezer_proxy_http_webhook_bytes_total[5m])",
      "legendFormat": "HTTP {{port}} - {{dataset_id}}"
    }
  ]
}
```

#### 4. Error Rate Panel
```json
{
  "title": "Error Rates",
  "type": "graph",
  "targets": [
    {
      "expr": "rate(bytefreezer_proxy_udp_packets_dropped_total[5m])",
      "legendFormat": "UDP Drops {{port}}"
    },
    {
      "expr": "rate(bytefreezer_proxy_http_auth_failures_total[5m])",
      "legendFormat": "HTTP Auth Failures {{port}}"
    }
  ]
}
```

#### 5. Performance Panel
```json
{
  "title": "Processing Performance",
  "type": "graph",
  "targets": [
    {
      "expr": "histogram_quantile(0.95, rate(bytefreezer_proxy_batch_processing_duration_seconds_bucket[5m]))",
      "legendFormat": "95th percentile batch processing"
    },
    {
      "expr": "histogram_quantile(0.50, rate(bytefreezer_proxy_http_request_duration_seconds_bucket[5m]))",
      "legendFormat": "50th percentile HTTP processing"
    }
  ]
}
```

### Dashboard Variables

Add dashboard variables for filtering:

```json
{
  "templating": {
    "list": [
      {
        "name": "instance",
        "type": "query",
        "query": "label_values(bytefreezer_proxy_uptime_seconds, instance)"
      },
      {
        "name": "plugin",
        "type": "query", 
        "query": "label_values(bytefreezer_proxy_plugin_status, plugin)"
      },
      {
        "name": "tenant_id",
        "type": "query",
        "query": "label_values(bytefreezer_proxy_udp_bytes_received_total, tenant_id)"
      }
    ]
  }
}
```

## Alert Rules

### Prometheus Alert Rules

Create alert rules in `bytefreezer_proxy_rules.yml`:

```yaml
groups:
  - name: bytefreezer-proxy-alerts
    rules:
      # Service availability alerts
      - alert: ByteFreezerProxyDown
        expr: up{job="bytefreezer-proxy"} == 0
        for: 1m
        labels:
          severity: critical
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy instance is down"
          description: "ByteFreezer Proxy instance {{ $labels.instance }} has been down for more than 1 minute."

      # Plugin health alerts
      - alert: ByteFreezerProxyPluginUnhealthy
        expr: bytefreezer_proxy_plugin_status == 0
        for: 2m
        labels:
          severity: warning
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy plugin unhealthy"
          description: "Plugin {{ $labels.plugin }} on instance {{ $labels.instance }} has been unhealthy for more than 2 minutes."

      # High error rate alerts
      - alert: ByteFreezerProxyHighUDPDropRate
        expr: rate(bytefreezer_proxy_udp_packets_dropped_total[5m]) > 100
        for: 2m
        labels:
          severity: warning
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy high UDP drop rate"
          description: "Instance {{ $labels.instance }} port {{ $labels.port }} is dropping UDP packets at rate of {{ $value }} packets/sec."

      - alert: ByteFreezerProxyHighHTTPErrorRate
        expr: rate(bytefreezer_proxy_http_webhook_requests_total{status!="success"}[5m]) > 10
        for: 2m
        labels:
          severity: critical
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy high HTTP error rate"
          description: "Instance {{ $labels.instance }} has high HTTP error rate: {{ $value }} errors/sec."

      # Performance alerts
      - alert: ByteFreezerProxySlowProcessing
        expr: histogram_quantile(0.95, rate(bytefreezer_proxy_batch_processing_duration_seconds_bucket[5m])) > 30
        for: 5m
        labels:
          severity: warning
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy slow batch processing"
          description: "Instance {{ $labels.instance }} 95th percentile processing time is {{ $value }}s."

      # Spooling alerts
      - alert: ByteFreezerProxyHighSpoolQueueSize
        expr: bytefreezer_proxy_spool_queue_size > 1000
        for: 5m
        labels:
          severity: warning
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy high spool queue size"
          description: "Instance {{ $labels.instance }} has {{ $value }} files in spool queue."

      - alert: ByteFreezerProxyDLQFilesAccumulating
        expr: bytefreezer_proxy_dlq_files_total > 10
        for: 2m
        labels:
          severity: critical
          service: bytefreezer-proxy
        annotations:
          summary: "ByteFreezer Proxy DLQ files accumulating"
          description: "Instance {{ $labels.instance }} has {{ $value }} files in dead letter queue."

  - name: bytefreezer-proxy-performance
    rules:
      # Recording rules for dashboards
      - record: bytefreezer_proxy:udp_throughput_bytes_per_second
        expr: rate(bytefreezer_proxy_udp_bytes_received_total[1m])

      - record: bytefreezer_proxy:http_throughput_bytes_per_second
        expr: rate(bytefreezer_proxy_http_webhook_bytes_total[1m])

      - record: bytefreezer_proxy:batch_success_rate
        expr: |
          rate(bytefreezer_proxy_batches_forwarded_total{status="success"}[5m]) /
          rate(bytefreezer_proxy_batches_forwarded_total[5m])

      - record: bytefreezer_proxy:avg_batch_size_bytes
        expr: rate(bytefreezer_proxy_batch_size_bytes_sum[5m]) / rate(bytefreezer_proxy_batch_size_bytes_count[5m])

      - record: bytefreezer_proxy:avg_batch_size_lines
        expr: rate(bytefreezer_proxy_batch_size_lines_sum[5m]) / rate(bytefreezer_proxy_batch_size_lines_count[5m])
```

### Load Alert Rules

```bash
# Add to prometheus.yml
rule_files:
  - "bytefreezer_proxy_rules.yml"

# Restart Prometheus to load rules
systemctl restart prometheus

# Verify rules loaded
curl http://localhost:9090/api/v1/rules
```

## Troubleshooting

### Metrics Not Appearing

1. **Check proxy configuration:**
   ```bash
   curl http://localhost:8088/api/v2/config | jq '.otel'
   ```

2. **Verify metrics endpoint:**
   ```bash
   curl http://localhost:9090/metrics
   ```

3. **Check proxy logs:**
   ```bash
   tail -f /var/log/bytefreezer-proxy/proxy.log | grep -i otel
   ```

4. **Test with data generation:**
   ```bash
   cd integration-tests
   ./test-with-monitoring.sh
   ```

### High Resource Usage

Monitor these metrics for resource consumption:

```bash
# Check memory usage
curl -s http://localhost:9090/metrics | grep process_resident_memory_bytes

# Check CPU usage
curl -s http://localhost:9090/metrics | grep process_cpu_seconds_total

# Check goroutine count
curl -s http://localhost:9090/metrics | grep go_goroutines
```

### Prometheus Scraping Issues

1. **Check Prometheus targets:**
   ```bash
   curl http://prometheus:9090/api/v1/targets
   ```

2. **Verify network connectivity:**
   ```bash
   curl -v http://proxy-host:9090/metrics
   ```

3. **Check Prometheus logs:**
   ```bash
   tail -f /var/log/prometheus/prometheus.log | grep bytefreezer-proxy
   ```

### Plugin Metrics Missing

1. **Ensure plugins are active:**
   ```bash
   curl http://localhost:8088/api/v2/health
   ```

2. **Generate plugin activity:**
   ```bash
   # UDP plugin
   echo "test" | nc -u localhost 2056
   
   # HTTP plugin
   curl -X POST http://localhost:8081/webhook -d "test"
   ```

3. **Check for plugin-specific metrics:**
   ```bash
   curl -s http://localhost:9090/metrics | grep -E "(udp|http|kafka|nats)_"
   ```

## Performance Impact

The monitoring setup has minimal performance impact:

- **Metrics collection**: <1% CPU overhead
- **Memory usage**: ~5-10MB additional for metrics storage
- **Network**: Metrics scraping adds <1KB/s traffic per scrape
- **Disk**: Prometheus TSDB storage requirements vary by retention period

### Optimization Recommendations

1. **Adjust scrape intervals** based on monitoring needs:
   - Production: 30-60 seconds
   - Development: 5-15 seconds
   - High-volume: 60-120 seconds

2. **Use recording rules** for complex queries to reduce dashboard load

3. **Configure appropriate retention** in Prometheus:
   ```yaml
   # prometheus.yml
   global:
     scrape_interval: 30s
   
   # Command line
   --storage.tsdb.retention.time=30d
   --storage.tsdb.retention.size=10GB
   ```

4. **Monitor the monitoring** - track Prometheus resource usage

## Support

For monitoring-related issues:
1. Check the proxy logs for OTEL initialization errors
2. Verify network connectivity to metrics endpoint
3. Review Prometheus configuration and target health
4. Use the monitoring integration test for validation
5. Consult the main README.md for general proxy issues