# ByteFreezer Proxy Monitoring Setup Guide

This guide provides comprehensive instructions for setting up monitoring, metrics, and alerting for ByteFreezer Proxy deployments.

## Overview

ByteFreezer Proxy provides detailed metrics through Prometheus endpoints and integrates with standard Kubernetes monitoring stacks. All metrics are automatically tagged with tenant and dataset information for multi-tenant visibility.

## Monitoring Architecture

```
[ByteFreezer Proxy] → [Prometheus] → [Grafana]
                   → [AlertManager] → [Notifications]
```

## Quick Setup

### For k3s with Prometheus Stack

```bash
# 1. Deploy Prometheus stack
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -f prometheus-configs/k3s-prometheus-values-proxy.yaml \
  -n monitoring --create-namespace

# 2. Deploy ServiceMonitor for auto-discovery
kubectl apply -f k8s/monitoring/prometheus-servicemonitor.yaml

# 3. Import Grafana dashboard
kubectl apply -f k8s/monitoring/grafana-dashboard-configmap.yaml

# 4. Access Grafana (default: admin/prom-operator)
kubectl port-forward service/kube-prometheus-stack-grafana 3000:80 -n monitoring
```

### For Existing Prometheus

If you already have Prometheus running, add ByteFreezer Proxy scraping:

```bash
# Apply external configuration
kubectl apply -f prometheus-configs/prometheus-external-config-proxy.yaml

# Or manually add to your prometheus.yml
# See prometheus-configs/prometheus-external-config-proxy.yaml for configuration
```

## Metrics Endpoints

ByteFreezer Proxy exposes the following endpoints:

| Endpoint | Port | Description |
|----------|------|-------------|
| `/metrics` | 9099 | Prometheus metrics |
| `/health` | 8088 | Health check endpoint |

### Key Metrics

#### Application Metrics

| Metric Name | Type | Description |
|------------|------|-------------|
| `bytefreezer_proxy_udp_packets_total` | Counter | Total UDP packets received |
| `bytefreezer_proxy_udp_bytes_total` | Counter | Total UDP bytes received |
| `bytefreezer_proxy_forwarded_requests_total` | Counter | Requests forwarded to receiver |
| `bytefreezer_proxy_failed_requests_total` | Counter | Failed forwarding requests |
| `bytefreezer_proxy_spooled_files_total` | Gauge | Files currently spooled (active retries) |
| `bytefreezer_proxy_dlq_files_total` | Gauge | Files in Dead Letter Queue (failed permanently) |
| `bytefreezer_proxy_spool_queue_size` | Gauge | Current spool queue size by tenant and dataset |
| `bytefreezer_proxy_spool_queue_bytes` | Gauge | Current bytes in spool queue by tenant and dataset |
| `bytefreezer_proxy_active_listeners` | Gauge | Active UDP listeners |

#### System Metrics

| Metric Name | Type | Description |
|------------|------|-------------|
| `process_cpu_seconds_total` | Counter | CPU time consumed |
| `process_memory_bytes` | Gauge | Memory usage |
| `go_memstats_alloc_bytes` | Gauge | Go memory allocations |
| `go_goroutines` | Gauge | Number of goroutines |

## Configuration Files

### Prometheus Values for k3s

The `prometheus-configs/k3s-prometheus-values-proxy.yaml` file provides:

- **External Host Monitoring**: Scrape ByteFreezer Proxy running on external hosts
- **Kubernetes Service Discovery**: Auto-discover proxy services in k8s
- **Persistent Storage**: 20GB storage for metrics retention
- **AlertManager Integration**: Built-in alerting capabilities

Key configuration sections:

```yaml
# External hosts (update with your IPs)
- job_name: 'bytefreezer-proxy-external'
  static_configs:
    - targets:
        - '192.168.1.100:9099'  # Update with actual IPs
        - '192.168.1.101:9099'

# Kubernetes services
- job_name: 'bytefreezer-proxy-k8s'
  kubernetes_sd_configs:
    - role: service
  relabel_configs:
    - source_labels: [__meta_kubernetes_service_name]
      action: keep
      regex: bytefreezer-proxy
```

### Target Files

Use JSON target files for dynamic service discovery:

```bash
# Update prometheus-configs/prometheus-proxy-targets-example.json
[
  {
    "targets": [
      "192.168.1.100:9099",
      "192.168.1.101:9099"
    ],
    "labels": {
      "job": "bytefreezer-proxy",
      "environment": "production"
    }
  }
]
```

## Grafana Dashboards

### Pre-built Dashboard

The monitoring setup includes a pre-configured Grafana dashboard with:

- **Request Rate Graphs**: Visualize incoming UDP traffic
- **Error Rate Monitoring**: Track failed requests and errors  
- **Resource Usage**: Monitor CPU, memory, and network usage
- **Spool Statistics**: Track spooled files and disk usage

### Importing Custom Dashboards

1. **Access Grafana UI**:
   ```bash
   kubectl port-forward service/kube-prometheus-stack-grafana 3000:80 -n monitoring
   # Default credentials: admin / prom-operator
   ```

2. **Import Dashboard**:
   - Navigate to "+" → Import
   - Upload `k8s/monitoring/grafana-dashboard-configmap.yaml` content
   - Configure data sources and save

### Key Dashboard Panels

#### UDP Traffic Panel
```json
{
  "title": "UDP Packets per Second",
  "targets": [{
    "expr": "rate(bytefreezer_proxy_udp_packets_total[5m])",
    "legendFormat": "{{instance}} - {{port}}"
  }]
}
```

#### Error Rate Panel
```json
{
  "title": "Error Rate %",
  "targets": [{
    "expr": "rate(bytefreezer_proxy_failed_requests_total[5m]) / rate(bytefreezer_proxy_forwarded_requests_total[5m]) * 100",
    "legendFormat": "Error Rate %"
  }]
}
```

## Alerting Rules

### Basic Alert Rules

Create alerting rules in your Prometheus configuration:

```yaml
# bytefreezer_proxy_alerts.yml
groups:
- name: bytefreezer-proxy
  rules:
  
  # Service availability
  - alert: ByteFreezerProxyDown
    expr: up{job="bytefreezer-proxy"} == 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "ByteFreezer Proxy instance is down"
      description: "ByteFreezer Proxy instance {{ $labels.instance }} has been down for more than 1 minute"

  # High error rate
  - alert: ByteFreezerProxyHighErrorRate
    expr: rate(bytefreezer_proxy_failed_requests_total[5m]) / rate(bytefreezer_proxy_forwarded_requests_total[5m]) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High error rate in ByteFreezer Proxy"
      description: "Error rate is {{ $value | humanizePercentage }} for {{ $labels.instance }}"

  # High memory usage
  - alert: ByteFreezerProxyHighMemory
    expr: process_memory_bytes{job="bytefreezer-proxy"} > 500000000  # 500MB
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "ByteFreezer Proxy high memory usage"
      description: "Memory usage is {{ $value | humanizeBytes }} on {{ $labels.instance }}"

  # Spooling queue building up
  - alert: ByteFreezerProxySpoolBacklog
    expr: bytefreezer_proxy_spooled_files_total > 100
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "ByteFreezer Proxy spool backlog"
      description: "{{ $value }} files are spooled on {{ $labels.instance }}"

  # Dead Letter Queue accumulation (critical for data recovery)
  - alert: ByteFreezerProxyDLQAccumulation
    expr: increase(bytefreezer_proxy_dlq_files_total[1h]) > 0
    for: 0m
    labels:
      severity: critical
    annotations:
      summary: "ByteFreezer Proxy DLQ accumulating files"
      description: "{{ $value }} files moved to DLQ in last hour on {{ $labels.instance }}. Data recovery required!"

  # Large DLQ requiring immediate attention
  - alert: ByteFreezerProxyDLQLarge
    expr: bytefreezer_proxy_dlq_files_total > 50
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "ByteFreezer Proxy large DLQ"
      description: "{{ $value }} files in DLQ on {{ $labels.instance }}. Review and recover failed batches."
```

### AlertManager Configuration

Configure AlertManager to send notifications:

```yaml
# alertmanager.yml
global:
  smtp_smarthost: 'localhost:587'
  smtp_from: 'alertmanager@yourdomain.com'

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'web.hook'

receivers:
- name: 'web.hook'
  email_configs:
  - to: 'admin@yourdomain.com'
    subject: 'ByteFreezer Proxy Alert: {{ .GroupLabels.alertname }}'
    body: |
      {{ range .Alerts }}
      Alert: {{ .Annotations.summary }}
      Description: {{ .Annotations.description }}
      Instance: {{ .Labels.instance }}
      Severity: {{ .Labels.severity }}
      {{ end }}
```

## Advanced Monitoring

### Custom Metrics

ByteFreezer Proxy supports custom metrics through configuration:

```yaml
# config.yaml
otel:
  enabled: true
  prometheus_mode: true
  metrics_port: 9099
  metrics_host: "0.0.0.0"
  custom_metrics:
    - name: "custom_processing_time"
      help: "Custom processing time metric"
      type: "histogram"
```

### Log Aggregation

Integrate with log aggregation systems:

```yaml
# fluent-bit configuration
[INPUT]
    Name              tail
    Path              /var/log/bytefreezer-proxy/*.log
    Tag               bytefreezer.proxy
    Parser            json

[OUTPUT]
    Name              es
    Match             bytefreezer.*
    Host              elasticsearch.monitoring.svc.cluster.local
    Port              9200
    Index             bytefreezer-logs
```

### Distributed Tracing

Enable distributed tracing with OpenTelemetry:

```yaml
# config.yaml
otel:
  enabled: true
  endpoint: "jaeger-collector.monitoring.svc.cluster.local:4317"
  service_name: "bytefreezer-proxy"
  trace_sampling_rate: 0.1
```

## Monitoring in Different Environments

### Development Environment

```yaml
# Reduced monitoring overhead
otel:
  enabled: true
  scrapeIntervalseconds: 30
  prometheus_mode: true
  
logging:
  level: "debug"
  
resources:
  limits:
    memory: "256Mi"
    cpu: "250m"
```

### Production Environment

```yaml
# Full monitoring enabled
otel:
  enabled: true
  scrapeIntervalseconds: 15
  prometheus_mode: true
  metrics_port: 9099
  metrics_host: "0.0.0.0"
  
logging:
  level: "info"
  format: "json"
  
resources:
  limits:
    memory: "1Gi"
    cpu: "500m"
```

## Troubleshooting Monitoring

### Common Issues

#### 1. Metrics Not Appearing

```bash
# Check if metrics endpoint is accessible
kubectl port-forward service/bytefreezer-proxy 9099:9099 -n bytefreezer
curl http://localhost:9099/metrics

# Verify ServiceMonitor
kubectl get servicemonitor bytefreezer-proxy-metrics -n bytefreezer -o yaml

# Check Prometheus targets
kubectl port-forward service/kube-prometheus-stack-prometheus 9090:9090 -n monitoring
# Visit http://localhost:9090/targets
```

#### 2. Grafana Dashboard Not Loading

```bash
# Check if dashboard ConfigMap is applied
kubectl get configmap bytefreezer-proxy-dashboard -n monitoring

# Verify Grafana can access Prometheus
kubectl logs -f deployment/kube-prometheus-stack-grafana -n monitoring
```

#### 3. Alerts Not Firing

```bash
# Check AlertManager status
kubectl port-forward service/kube-prometheus-stack-alertmanager 9093:9093 -n monitoring

# Verify alert rules are loaded
kubectl get prometheusrule -n monitoring

# Check rule syntax
kubectl describe prometheusrule -n monitoring
```

### Debug Commands

```bash
# Check all monitoring components
kubectl get all -n monitoring

# View Prometheus configuration
kubectl get secret kube-prometheus-stack-prometheus -n monitoring -o yaml

# Check ServiceMonitor discovery
kubectl get servicemonitor -A

# View metrics directly from pod
kubectl exec deployment/bytefreezer-proxy -n bytefreezer -- curl localhost:9099/metrics
```

## Performance Considerations

### Metrics Collection Overhead

- **Scrape Interval**: 15s is optimal for most use cases
- **Metric Cardinality**: Monitor the number of unique metric series
- **Resource Usage**: Metrics collection uses ~50MB RAM + 10m CPU

### Storage Requirements

- **Retention**: Default 15 days, adjust based on needs
- **Storage**: ~1GB per million samples, plan accordingly
- **Compression**: Enable compression for long-term storage

### Optimization Tips

1. **Reduce Scrape Frequency** for development environments
2. **Use Recording Rules** for complex queries
3. **Implement Metric Filtering** to reduce cardinality
4. **Monitor Prometheus Performance** itself

## Security Considerations

1. **Network Policies**: Restrict access to metrics endpoints
2. **Authentication**: Enable authentication for Grafana access
3. **TLS**: Use TLS for metric scraping in production
4. **RBAC**: Limit Prometheus service account permissions
5. **Multi-Tenant Token Security**: 
   - Store bearer tokens securely (use Kubernetes secrets, Vault, etc.)
   - Rotate bearer tokens regularly for each tenant
   - Monitor for authentication failures by tenant
   - Ensure tenant isolation in alerting and dashboard access

## Maintenance

### Regular Tasks

```bash
# Update monitoring stack
helm upgrade kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -f prometheus-configs/k3s-prometheus-values-proxy.yaml -n monitoring

# Backup Grafana dashboards
kubectl get configmap -n monitoring -o yaml > grafana-dashboards-backup.yaml

# Check metric storage usage
kubectl exec prometheus-kube-prometheus-stack-prometheus-0 -n monitoring -- \
  du -sh /prometheus/
```

### Scaling Monitoring

For large deployments:

1. **Federation**: Set up Prometheus federation
2. **Remote Storage**: Use remote storage backends
3. **Horizontal Scaling**: Deploy multiple Prometheus instances
4. **Metric Sampling**: Implement sampling for high-cardinality metrics

## DLQ Operations and Monitoring

### DLQ Metrics Collection

Since DLQ files are stored on the filesystem, use node exporter metrics for monitoring:

```yaml
# Add to Prometheus scrape configs
- job_name: 'node-exporter-dlq'
  static_configs:
    - targets: ['bytefreezer-proxy-host:9100']
  metric_relabel_configs:
    # Only collect filesystem metrics for spool directory
    - source_labels: [__name__, mountpoint]
      regex: 'node_filesystem_.+;/var/spool/bytefreezer-proxy'
      target_label: __tmp_dlq_metric
      replacement: 'true'
    - source_labels: [__tmp_dlq_metric]
      regex: 'true'
      target_label: service
      replacement: 'bytefreezer-proxy-dlq'
```

### DLQ Dashboard Panels

**Grafana DLQ Panels:**

1. **DLQ File Count:**
```prometheus
# Query
node_filesystem_files_free{service="bytefreezer-proxy-dlq"} - node_filesystem_files{service="bytefreezer-proxy-dlq"}

# Panel Settings
- Title: "DLQ Files Count"
- Type: Stat/Single Value
- Unit: "Files"
- Alert: > 10 files (warning), > 50 files (critical)
```

2. **DLQ Directory Size:**
```prometheus  
# Query
(node_filesystem_size_bytes{service="bytefreezer-proxy-dlq"} - node_filesystem_avail_bytes{service="bytefreezer-proxy-dlq"}) / 1024 / 1024

# Panel Settings  
- Title: "DLQ Directory Size"
- Type: Graph/Time Series
- Unit: "MB"
- Alert: > 500MB (warning), > 2GB (critical)
```

3. **DLQ Growth Rate:**
```prometheus
# Query
increase(node_filesystem_files{service="bytefreezer-proxy-dlq"}[1h])

# Panel Settings
- Title: "DLQ Files Added (Hourly)"  
- Type: Bar Graph
- Unit: "Files/hour"
- Alert: > 0 files/hour (immediate attention)
```

### DLQ Alerting Rules

```yaml
# dlq_alerts.yml
groups:
- name: bytefreezer_dlq_alerts
  rules:
  - alert: DLQFilesDetected
    expr: (node_filesystem_size_bytes{service="bytefreezer-proxy-dlq"} - node_filesystem_avail_bytes{service="bytefreezer-proxy-dlq"}) > 0
    for: 0m
    labels:
      severity: critical
      component: dlq
    annotations:
      summary: "DLQ files detected - data recovery required"
      description: "Dead Letter Queue contains failed batches on {{ $labels.instance }}. Immediate data recovery action required."
      runbook_url: "https://docs.bytefreezer.com/proxy/dlq-recovery"

  - alert: DLQDirectoryFull
    expr: node_filesystem_avail_bytes{service="bytefreezer-proxy-dlq"} / node_filesystem_size_bytes{service="bytefreezer-proxy-dlq"} < 0.1
    for: 5m
    labels:
      severity: warning
      component: dlq
    annotations:
      summary: "DLQ directory filling up"
      description: "DLQ directory is {{ $value | humanizePercentage }} full on {{ $labels.instance }}"
```

### DLQ Recovery Automation

**Automated DLQ Check Script:**

```bash
#!/bin/bash
# dlq_check.sh - Add to cron for regular DLQ monitoring

DLQ_DIR="/var/spool/bytefreezer-proxy/DLQ"
ALERT_WEBHOOK="https://your-webhook-url"

# Count DLQ files
DLQ_COUNT=$(find "$DLQ_DIR" -name "*.ndjson" 2>/dev/null | wc -l)

if [ "$DLQ_COUNT" -gt 0 ]; then
    # Send alert
    curl -X POST "$ALERT_WEBHOOK" -H "Content-Type: application/json" -d "{
        \"text\": \"🚨 DLQ Alert: $DLQ_COUNT failed batches detected in ByteFreezer Proxy\",
        \"severity\": \"high\",
        \"component\": \"dlq\",
        \"action_required\": \"Data recovery needed\",
        \"dlq_files\": $DLQ_COUNT
    }"
    
    # Log recent DLQ files for analysis
    echo "Recent DLQ files:" | logger -t dlq_check
    find "$DLQ_DIR" -name "*.meta" -mtime -1 -exec basename {} .meta \; | logger -t dlq_check
fi
```

**Cron Configuration:**
```bash
# Check DLQ every 15 minutes
*/15 * * * * /opt/scripts/dlq_check.sh

# Daily DLQ summary report  
0 8 * * * /opt/scripts/dlq_daily_report.sh
```

### DLQ Recovery Procedures

**Standard Operating Procedure for DLQ Files:**

1. **Immediate Response (< 1 hour):**
   ```bash
   # Assess DLQ situation
   cd /var/spool/bytefreezer-proxy/DLQ
   echo "DLQ Files: $(ls *.ndjson | wc -l)"
   echo "Total Size: $(du -sh .)"
   
   # Check failure reasons
   jq -r '.failure_reason' *.meta | sort | uniq -c
   ```

2. **Analysis Phase (< 4 hours):**
   ```bash
   # Group by failure type and tenant
   for meta in *.meta; do
     echo "=== $meta ==="
     jq '{tenant: .tenant_id, dataset: .dataset_id, reason: .failure_reason, retry_count: .retry_count}' "$meta"
   done | tee dlq_analysis.log
   ```

3. **Recovery Phase (< 24 hours):**
   ```bash
   # Attempt reprocessing (adjust URL as needed)
   for file in *.ndjson; do
     meta="${file%.ndjson}.meta"
     tenant=$(jq -r '.tenant_id' "$meta")
     dataset=$(jq -r '.dataset_id' "$meta")
     
     echo "Reprocessing $file..."
     curl -f -X POST \
       -H "Content-Type: application/x-ndjson" \
       --data-binary "@$file" \
       "http://bytefreezer-receiver:8080/data/$tenant/$dataset" && \
       rm "$file" "$meta" && \
       echo "✅ Successfully recovered $file"
   done
   ```

### DLQ Maintenance Tasks

**Weekly DLQ Maintenance:**
- Review DLQ trends and patterns
- Archive old DLQ files (>30 days) 
- Update recovery procedures based on patterns
- Test DLQ monitoring alerts

**Monthly DLQ Analysis:**
- Analyze root cause patterns
- Optimize retry configurations
- Review DLQ storage requirements
- Update operational runbooks

## Support and Resources

- **Documentation**: [Prometheus Docs](https://prometheus.io/docs/)
- **Community**: [Grafana Community](https://community.grafana.com/)
- **Issues**: Report monitoring issues to ByteFreezer repository
- **Examples**: See `prometheus-configs/` for configuration examples

## Next Steps

- Configure [alerting notifications](ALERTING_GUIDE.md)
- Set up [log aggregation](LOGGING_GUIDE.md)
- Implement [distributed tracing](TRACING_GUIDE.md)
- Plan [capacity and scaling](SCALING_GUIDE.md)