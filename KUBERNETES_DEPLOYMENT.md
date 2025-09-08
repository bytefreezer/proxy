# ByteFreezer Proxy Kubernetes Deployment Guide

This guide provides comprehensive instructions for deploying ByteFreezer Proxy to Kubernetes clusters, specifically optimized for k3s environments.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick Start](#quick-start)
3. [Deployment Options](#deployment-options)
4. [Configuration](#configuration)
5. [Monitoring Setup](#monitoring-setup)
6. [Production Considerations](#production-considerations)
7. [Troubleshooting](#troubleshooting)

## Prerequisites

### System Requirements
- Kubernetes cluster (k3s, k8s, etc.) version 1.20+
- kubectl configured with cluster access
- Minimum 2GB RAM and 10GB storage per node
- Network access to required container registries

### Required Tools
```bash
# Install kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# Install kustomize (optional, for advanced deployments)
curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash
sudo mv kustomize /usr/local/bin/

# Verify cluster access
kubectl cluster-info
```

## Quick Start

### Option 1: Using Ansible (Recommended)

The fastest way to deploy to k3s:

```bash
# Clone repository
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy

# Deploy using Ansible automation
cd ansible/playbooks/kubernetes
ansible-playbook -i localhost, deploy.yml

# Verify deployment
kubectl get pods -n bytefreezer
```

### Option 2: Using Kustomize (Base Deployment)

```bash
# Deploy base configuration
kubectl apply -k k8s/base/

# Check deployment status
kubectl get all -n bytefreezer
```

### Option 3: Manual Deployment

```bash
# Apply manifests in order
kubectl apply -f k8s/base/namespace.yaml
kubectl apply -f k8s/base/configmap.yaml
kubectl apply -f k8s/base/persistent-volume.yaml
kubectl apply -f k8s/base/deployment.yaml
kubectl apply -f k8s/base/service.yaml
kubectl apply -f k8s/base/networkpolicy.yaml
```

## Deployment Options

### Development Environment

Use the development overlay for testing:

```bash
# Deploy development version
kubectl apply -k k8s/overlays/development/

# Features:
# - Single replica
# - Latest image tag
# - Reduced resource limits
# - Development namespace
```

### Production Environment

Use the production overlay for stable deployments:

```bash
# Deploy production version
kubectl apply -k k8s/overlays/production/

# Features:
# - Multiple replicas
# - Specific version tags
# - Higher resource limits
# - Production-ready configuration
```

## Configuration

### Basic Configuration

The default configuration is suitable for most deployments. Key settings:

```yaml
# In configmap.yaml
server:
  bind_addr: "0.0.0.0"
  health_port: 8088

listeners:
  - port: 2056
    dataset_id: "ebpf-data"
  - port: 2057  
    dataset_id: "syslog-data"
  - port: 2058
    dataset_id: "application-logs"

forwarding:
  endpoint: "http://bytefreezer-receiver.bytefreezer.svc.cluster.local:8080"
```

### Custom Configuration

To customize the configuration:

```bash
# Edit the ConfigMap
kubectl edit configmap bytefreezer-proxy-config -n bytefreezer

# Restart deployment to apply changes
kubectl rollout restart deployment/bytefreezer-proxy -n bytefreezer
```

### Environment-Specific Settings

For different environments, use kustomize patches:

```yaml
# k8s/overlays/production/config-patch.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: bytefreezer-proxy-config
data:
  config.yaml: |
    # Production-specific settings
    server:
      health_port: 8088
    otel:
      enabled: true
      metrics_port: 9099
    logging:
      level: "warn"
```

## Monitoring Setup

### Prometheus Integration

1. **Deploy Prometheus Stack (if not exists):**

```bash
# Add Prometheus Helm repository
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install with ByteFreezer configuration
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -f prometheus-configs/k3s-prometheus-values-proxy.yaml \
  -n monitoring --create-namespace
```

2. **Deploy ServiceMonitor:**

```bash
# Apply ServiceMonitor for automatic discovery
kubectl apply -f k8s/monitoring/prometheus-servicemonitor.yaml
```

3. **Import Grafana Dashboard:**

```bash
# Deploy dashboard ConfigMap
kubectl apply -f k8s/monitoring/grafana-dashboard-configmap.yaml
```

### Access Monitoring

```bash
# Port forward to Prometheus
kubectl port-forward service/kube-prometheus-stack-prometheus 9090:9090 -n monitoring

# Port forward to Grafana (admin/prom-operator)
kubectl port-forward service/kube-prometheus-stack-grafana 3000:80 -n monitoring

# View ByteFreezer metrics directly
kubectl port-forward service/bytefreezer-proxy 9099:9099 -n bytefreezer
curl http://localhost:9099/metrics
```

## Production Considerations

### Resource Management

**Recommended Resource Allocation:**

```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "250m" 
  limits:
    memory: "512Mi"
    cpu: "500m"
```

**For High Traffic Environments:**

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "500m"
  limits:
    memory: "1Gi"
    cpu: "1000m"
```

### High Availability

1. **Multiple Replicas:**

```bash
# Scale deployment
kubectl scale deployment bytefreezer-proxy --replicas=3 -n bytefreezer
```

2. **Pod Disruption Budget:**

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: bytefreezer-proxy-pdb
  namespace: bytefreezer
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: bytefreezer-proxy
```

3. **Anti-Affinity Rules:**

```yaml
# Add to deployment spec.template.spec
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/name
            operator: In
            values:
            - bytefreezer-proxy
        topologyKey: kubernetes.io/hostname
```

### Storage Considerations

**Persistent Volume Configuration:**

```yaml
# For production workloads
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi  # Increase based on spool requirements
  storageClassName: fast-ssd  # Use high-performance storage
```

### Network Configuration

**LoadBalancer Service (for k3s with MetalLB):**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: bytefreezer-proxy-lb
  namespace: bytefreezer
  annotations:
    metallb.universe.tf/loadBalancerIPs: "192.168.1.100"
spec:
  type: LoadBalancer
  ports:
  - port: 8088
    name: health
    protocol: TCP
  - port: 2056
    name: udp-2056
    protocol: UDP
  - port: 2057
    name: udp-2057
    protocol: UDP
  - port: 2058
    name: udp-2058
    protocol: UDP
  selector:
    app.kubernetes.io/name: bytefreezer-proxy
```

## Troubleshooting

### Common Issues

#### 1. Pods Stuck in Pending State

```bash
# Check node resources
kubectl describe nodes
kubectl top nodes

# Check PVC status
kubectl get pvc -n bytefreezer
kubectl describe pvc bytefreezer-proxy-spool-pvc -n bytefreezer

# Check for resource constraints
kubectl describe pod -l app.kubernetes.io/name=bytefreezer-proxy -n bytefreezer
```

#### 2. Service Not Accessible

```bash
# Verify service endpoints
kubectl get endpoints bytefreezer-proxy -n bytefreezer

# Check service configuration
kubectl describe service bytefreezer-proxy -n bytefreezer

# Test internal connectivity
kubectl run debug --image=busybox -it --rm --restart=Never -- /bin/sh
# Inside the pod:
nslookup bytefreezer-proxy.bytefreezer.svc.cluster.local
```

#### 3. High Memory/CPU Usage

```bash
# Monitor resource usage
kubectl top pods -n bytefreezer

# Check application logs
kubectl logs -f deployment/bytefreezer-proxy -n bytefreezer

# Scale down if necessary
kubectl scale deployment bytefreezer-proxy --replicas=1 -n bytefreezer
```

#### 4. UDP Traffic Not Reaching Pods

```bash
# Check network policies
kubectl get networkpolicy -n bytefreezer

# Verify service type and ports
kubectl get service bytefreezer-proxy -n bytefreezer -o yaml

# Test UDP connectivity (replace with actual LoadBalancer IP)
echo "test message" | nc -u <LOADBALANCER-IP> 2056
```

### Debug Commands

```bash
# Get detailed pod information
kubectl describe pod -l app.kubernetes.io/name=bytefreezer-proxy -n bytefreezer

# Check recent events
kubectl get events -n bytefreezer --sort-by='.lastTimestamp'

# Execute commands inside pod
kubectl exec -it deployment/bytefreezer-proxy -n bytefreezer -- /bin/sh

# View configuration
kubectl get configmap bytefreezer-proxy-config -n bytefreezer -o yaml

# Check logs with timestamps
kubectl logs -f deployment/bytefreezer-proxy -n bytefreezer --timestamps

# Monitor network traffic (if tcpdump available)
kubectl exec deployment/bytefreezer-proxy -n bytefreezer -- tcpdump -i any port 2056
```

### Performance Tuning

#### 1. Adjust Resource Limits

```bash
# Edit deployment resources
kubectl edit deployment bytefreezer-proxy -n bytefreezer

# Monitor after changes
kubectl top pods -n bytefreezer
```

#### 2. Tune Application Settings

Key configuration parameters for performance:

```yaml
# In ConfigMap
server:
  max_batch_size: 10000      # Increase for higher throughput
  batch_timeout: "5s"        # Decrease for lower latency

spooling:
  max_size_bytes: 2147483648  # 2GB for high-volume environments
  cleanup_interval_sec: 1800  # 30 minutes

logging:
  level: "warn"              # Reduce logging overhead
```

#### 3. Network Optimization

```yaml
# In deployment spec
spec:
  template:
    spec:
      dnsPolicy: ClusterFirstWithHostNet
      hostNetwork: false  # Keep false for security
      containers:
      - name: bytefreezer-proxy
        securityContext:
          capabilities:
            add: ["NET_BIND_SERVICE"]  # If binding to privileged ports
```

## Security Best Practices

1. **Use specific image tags, not `latest`**
2. **Implement resource quotas**
3. **Configure network policies**
4. **Use least-privilege security contexts**
5. **Regularly update images**
6. **Monitor for security vulnerabilities**

## Maintenance

### Updates

```bash
# Update to new version
kubectl set image deployment/bytefreezer-proxy \
  bytefreezer-proxy=ghcr.io/n0needt0/bytefreezer-proxy:v1.2.0 -n bytefreezer

# Monitor rollout
kubectl rollout status deployment/bytefreezer-proxy -n bytefreezer

# Rollback if needed
kubectl rollout undo deployment/bytefreezer-proxy -n bytefreezer
```

### Backup

```bash
# Backup configuration
kubectl get configmap bytefreezer-proxy-config -n bytefreezer -o yaml > backup-config.yaml

# Backup spooled data
kubectl exec deployment/bytefreezer-proxy -n bytefreezer -- \
  tar -czf - /var/spool/bytefreezer-proxy > spool-backup.tar.gz
```

## Support

For additional help:
- **Issues**: https://github.com/n0needt0/bytefreezer-proxy/issues  
- **Documentation**: Check README.md and configuration examples
- **Community**: Join discussions in GitHub Discussions
- **Enterprise Support**: Contact ByteFreezer team

---

**Next Steps:**
- Set up [monitoring and alerting](MONITORING_SETUP.md)
- Configure [production-ready storage](STORAGE_CONFIGURATION.md)
- Implement [backup and disaster recovery](BACKUP_GUIDE.md)