# ByteFreezer Proxy Prometheus Configuration Files

This directory contains Prometheus configuration files for monitoring ByteFreezer Proxy deployments in various environments.

## Files Overview

### `k3s-prometheus-values-proxy.yaml`
**Purpose**: Helm values file for deploying complete Prometheus monitoring stack in k3s
**Use Case**: New k3s clusters without existing monitoring
**Features**:
- Complete Prometheus + Grafana + AlertManager stack
- External host monitoring configuration
- Kubernetes service discovery
- Persistent storage configuration
- Production-ready settings

**Usage**:
```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -f k3s-prometheus-values-proxy.yaml -n monitoring --create-namespace
```

### `prometheus-external-config-proxy.yaml`
**Purpose**: External Prometheus configuration for existing Prometheus instances
**Use Case**: Adding ByteFreezer Proxy monitoring to existing Prometheus setup
**Features**:
- Scrape configuration for external ByteFreezer Proxy hosts
- Health endpoint monitoring
- Recording and alerting rules reference
- Static target configuration

**Usage**:
```bash
# Merge with existing prometheus.yml or apply as ConfigMap
kubectl create configmap prometheus-config \
  --from-file=prometheus.yml=prometheus-external-config-proxy.yaml -n monitoring
```

### `prometheus-proxy-targets-example.json`
**Purpose**: JSON target file example for dynamic service discovery
**Use Case**: Dynamic target configuration for file-based service discovery
**Features**:
- JSON format target definitions
- Label assignments
- Multiple environment support

**Usage**:
```bash
# Update targets and copy to Prometheus
cp prometheus-proxy-targets-example.json /etc/prometheus/targets/
```

## Configuration Customization

### External Host Monitoring

Update the target IPs in the configuration files:

```yaml
# In k3s-prometheus-values-proxy.yaml
- targets:
    - '192.168.1.100:9099'  # Replace with actual proxy host IP
    - '192.168.1.101:9099'  # Add more hosts as needed
```

### Port Configuration

ByteFreezer Proxy uses these ports by default:
- **9099**: Prometheus metrics endpoint
- **8088**: Health check endpoint

To change ports, update both the application configuration and these monitoring configs.

### Scrape Intervals

Default scrape interval is 15 seconds. Adjust based on your needs:

```yaml
scrape_configs:
  - job_name: 'bytefreezer-proxy'
    scrape_interval: 30s  # Increase for lower overhead
    scrape_timeout: 10s
```

## Environment-Specific Configurations

### Development Environment
- Longer scrape intervals (30s)
- Reduced retention (7 days)
- Minimal resource allocation

### Production Environment  
- Short scrape intervals (15s)
- Extended retention (30+ days)
- High availability configuration
- Persistent storage

### Testing Environment
- Debug-level metrics
- Temporary storage
- Simplified alerting

## Integration with Existing Monitoring

### If you have existing Prometheus:

1. **Merge configurations**: Add the scrape configs to your existing `prometheus.yml`
2. **Update targets**: Replace IP addresses with your actual ByteFreezer Proxy hosts
3. **Import dashboards**: Use the Grafana dashboard from `k8s/monitoring/`

### If you use different monitoring tools:

1. **DataDog**: Use the Prometheus integration to scrape `/metrics`
2. **New Relic**: Configure Prometheus remote write
3. **Splunk**: Use the Splunk Prometheus exporter
4. **Custom tools**: Any tool that supports Prometheus format can scrape the endpoints

## Security Considerations

### Network Access
Ensure your Prometheus instance can reach ByteFreezer Proxy metrics endpoints:

```bash
# Test connectivity
telnet <proxy-host> 9099
curl http://<proxy-host>:9099/metrics
```

### Authentication
For production environments, consider:
- TLS encryption for metrics endpoints
- Network policies to restrict access
- Authentication/authorization if required

### Firewall Rules
Open necessary ports:
```bash
# Allow Prometheus to scrape metrics
sudo ufw allow from <prometheus-ip> to any port 9099
sudo ufw allow from <prometheus-ip> to any port 8088
```

## Troubleshooting

### Metrics Not Available

1. **Check ByteFreezer Proxy configuration**:
   ```yaml
   otel:
     enabled: true
     prometheus_mode: true
     metrics_port: 9099
     metrics_host: "0.0.0.0"
   ```

2. **Verify metrics endpoint**:
   ```bash
   curl http://localhost:9099/metrics
   ```

3. **Check network connectivity**:
   ```bash
   nc -zv <proxy-host> 9099
   ```

### Prometheus Not Scraping

1. **Verify Prometheus configuration**:
   ```bash
   # Check Prometheus targets page
   http://<prometheus-host>:9090/targets
   ```

2. **Check service discovery**:
   ```bash
   # For Kubernetes deployments
   kubectl get servicemonitor -n bytefreezer
   kubectl get endpoints bytefreezer-proxy -n bytefreezer
   ```

3. **Review Prometheus logs**:
   ```bash
   kubectl logs -f prometheus-kube-prometheus-stack-prometheus-0 -n monitoring
   ```

### High Cardinality Issues

If you see high memory usage in Prometheus:

1. **Check metric cardinality**:
   ```bash
   # Query Prometheus for series count
   prometheus_tsdb_symbol_table_size_bytes
   ```

2. **Reduce label cardinality**:
   ```yaml
   # Use recording rules for high-cardinality metrics
   rules:
   - record: bytefreezer:proxy_requests_rate
     expr: rate(bytefreezer_proxy_requests_total[5m])
   ```

3. **Implement metric filtering**:
   ```yaml
   metric_relabel_configs:
   - source_labels: [__name__]
     regex: 'unwanted_metric_.*'
     action: drop
   ```

## Performance Optimization

### Scrape Configuration
```yaml
scrape_configs:
- job_name: 'bytefreezer-proxy'
  # Optimize for your environment
  scrape_interval: 15s      # Balance between freshness and overhead
  scrape_timeout: 10s       # Prevent hanging scrapes
  metrics_path: '/metrics'  # Default path
  
  # Reduce metric cardinality
  metric_relabel_configs:
  - source_labels: [__name__]
    regex: 'go_.*|process_.*'  # Drop Go runtime metrics if not needed
    action: drop
```

### Resource Planning
- **Prometheus storage**: ~1-2 bytes per sample
- **Scrape frequency**: 15s = 240 samples/hour per metric
- **Retention**: Plan storage accordingly

## Example Deployments

### Single Node k3s
```bash
# Minimal monitoring setup
helm install prometheus prometheus-community/kube-prometheus-stack \
  --set prometheus.prometheusSpec.retention="7d" \
  --set prometheus.prometheusSpec.resources.requests.memory="1Gi" \
  -f k3s-prometheus-values-proxy.yaml -n monitoring --create-namespace
```

### Multi-Node Production
```bash
# High availability setup
helm install prometheus prometheus-community/kube-prometheus-stack \
  --set prometheus.prometheusSpec.retention="30d" \
  --set prometheus.prometheusSpec.replicas=2 \
  --set prometheus.prometheusSpec.resources.requests.memory="4Gi" \
  --set prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage="100Gi" \
  -f k3s-prometheus-values-proxy.yaml -n monitoring --create-namespace
```

## Support

For configuration questions or issues:
1. Check the main [MONITORING_SETUP.md](../MONITORING_SETUP.md) guide
2. Review Prometheus documentation
3. Open an issue in the ByteFreezer Proxy repository

## Next Steps

After setting up monitoring:
1. Configure [alerting rules](../ALERTING_GUIDE.md)
2. Import [Grafana dashboards](../k8s/monitoring/)
3. Set up [log aggregation](../LOGGING_GUIDE.md)
4. Implement [capacity planning](../SCALING_GUIDE.md)