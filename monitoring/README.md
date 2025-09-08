# ByteFreezer Proxy Monitoring Setup

This directory contains monitoring configurations for ByteFreezer Proxy, specifically for Prometheus metrics collection and monitoring in Kubernetes environments.

## Files Overview

### Prometheus Configuration Files

#### `prometheus/k3s-prometheus-values-packer.yaml`
- **Purpose**: Helm values file for deploying Prometheus stack in k3s cluster
- **Contains**: Complete Prometheus, Grafana, and AlertManager configuration
- **Use Case**: Production monitoring setup with persistent storage and alerts
- **Features**:
  - Prometheus server with 50GB persistent storage
  - Grafana with dashboards and data sources
  - AlertManager for notifications
  - Service monitors for ByteFreezer services

#### `prometheus/prometheus-external-config-packer.yaml`
- **Purpose**: External Prometheus configuration for scraping ByteFreezer metrics
- **Contains**: Prometheus scrape configs and rules
- **Use Case**: When using existing Prometheus instance to monitor ByteFreezer
- **Features**:
  - Custom scrape configurations
  - Recording and alerting rules
  - Service discovery configurations

#### `prometheus/config-prometheus-test.yaml`
- **Purpose**: Test configuration for ByteFreezer Proxy with Prometheus metrics enabled
- **Contains**: ByteFreezer Proxy config with metrics endpoint enabled
- **Use Case**: Development and testing environments
- **Features**:
  - Metrics endpoint on port 9099
  - Development-friendly settings
  - Simplified configuration

## Quick Setup

### For k3s with Prometheus Stack
```bash
# Deploy Prometheus stack with custom values
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -f monitoring/prometheus/k3s-prometheus-values-packer.yaml \
  -n monitoring --create-namespace
```

### For Existing Prometheus
```bash
# Apply the external configuration to your existing Prometheus
kubectl apply -f monitoring/prometheus/prometheus-external-config-packer.yaml
```

### For Testing
```bash
# Use the test configuration for development
cp monitoring/prometheus/config-prometheus-test.yaml config.yaml
./bytefreezer-proxy --config config.yaml
```

## Metrics Endpoints

ByteFreezer Proxy exposes metrics on:
- **Port**: 9099 (configurable)
- **Path**: `/metrics`
- **Format**: Prometheus format

## Grafana Dashboards

The Prometheus stack includes pre-configured Grafana dashboards for:
- Service health and performance metrics
- UDP listener statistics
- Data processing rates
- Error rates and alerts

## Alerts

Pre-configured alerts include:
- High error rates
- Service unavailability
- Memory/CPU usage thresholds
- Data processing delays