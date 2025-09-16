# ByteFreezer Proxy k3s Installation Guide

This guide provides step-by-step instructions for installing ByteFreezer Proxy into a k3s Kubernetes cluster.

## Prerequisites

### System Requirements
- k3s cluster running (single node or multi-node)
- kubectl configured to access your k3s cluster
- Helm 3.x installed
- At least 4GB RAM and 20GB storage available

### Required Tools
```bash
# Install kubectl (if not already installed)
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# Install Helm
curl https://get.helm.sh/helm-v3.12.0-linux-amd64.tar.gz | tar xz
sudo mv linux-amd64/helm /usr/local/bin/

# Verify k3s is running
kubectl get nodes
```

## Installation Options

### Option 1: Quick Install (Recommended)

Use the Ansible automation for zero-touch deployment:

```bash
# Clone the repository
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy

# Install to k3s using Ansible
cd ansible/playbooks/kubernetes
ansible-playbook -i localhost, deploy.yml
```

### Option 2: Manual kubectl Install

Deploy using raw Kubernetes manifests:

```bash
# Apply all manifests
kubectl apply -k kubernetes/

# Or apply individually for more control
kubectl apply -f kubernetes/namespace.yaml
kubectl apply -f kubernetes/configmap.yaml
kubectl apply -f kubernetes/persistent-volume.yaml
kubectl apply -f kubernetes/deployment.yaml
kubectl apply -f kubernetes/service.yaml
kubectl apply -f kubernetes/networkpolicy.yaml
```

### Option 3: Helm Chart Install (Future)

```bash
# Add ByteFreezer Helm repository (when available)
helm repo add bytefreezer https://n0needt0.github.io/helm-charts
helm repo update

# Install ByteFreezer Proxy
helm install bytefreezer-proxy bytefreezer/bytefreezer-proxy \
  -n bytefreezer --create-namespace
```

## Configuration

### Basic Configuration

Edit the ConfigMap before deployment:

```bash
# Edit the configuration
kubectl edit configmap bytefreezer-proxy-config -n bytefreezer
```

Key configuration sections:
- **UDP Listeners**: Configure ports 2056-2065 for different datasets
- **Forwarding**: Set the receiver endpoint URL
- **Spooling**: Configure local storage for failed uploads
- **Logging**: Set log levels and formats

### Network Configuration

For k3s with MetalLB (recommended for production):

```bash
# Install MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.7/config/manifests/metallb-native.yaml

# Configure IP address pool
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default-pool
  namespace: metallb-system
spec:
  addresses:
  - 192.168.1.100-192.168.1.110  # Adjust to your network
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: default-l2-adv
  namespace: metallb-system
spec:
  ipAddressPools:
  - default-pool
EOF
```

### Storage Configuration

For persistent storage (production environments):

```bash
# Create a storage class (example for local-path)
cat <<EOF | kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: bytefreezer-storage
provisioner: rancher.io/local-path
parameters:
  nodePath: /opt/local-path-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
EOF
```

## Monitoring Setup (Optional)

Monitoring can be set up using your preferred monitoring solution. The ByteFreezer Proxy exposes metrics when OpenTelemetry is enabled in the configuration.

## Verification

### Check Deployment Status

```bash
# Check all resources
kubectl get all -n bytefreezer

# Check pod logs
kubectl logs -f deployment/bytefreezer-proxy -n bytefreezer

# Check service endpoints
kubectl get endpoints -n bytefreezer

# Verify health endpoint
kubectl port-forward service/bytefreezer-proxy 8088:8088 -n bytefreezer
curl http://localhost:8088/health
```

### Test UDP Listeners

```bash
# Get LoadBalancer IP (if using MetalLB)
kubectl get service bytefreezer-proxy -n bytefreezer

# Test UDP listener (replace IP with your LoadBalancer IP)
echo "test message" | nc -u <LOADBALANCER-IP> 2056
```

### Monitor Metrics

```bash
# Port forward to metrics endpoint
kubectl port-forward service/bytefreezer-proxy 9099:9099 -n bytefreezer

# View metrics
curl http://localhost:9099/metrics
```

## Troubleshooting

### Common Issues

#### Pods stuck in Pending
```bash
# Check node resources
kubectl describe nodes
kubectl top nodes

# Check PVC status
kubectl get pvc -n bytefreezer
kubectl describe pvc -n bytefreezer
```

#### Service not accessible
```bash
# Check service configuration
kubectl describe service bytefreezer-proxy -n bytefreezer

# Check network policies
kubectl get networkpolicy -n bytefreezer

# Verify endpoints
kubectl get endpoints bytefreezer-proxy -n bytefreezer
```

#### High resource usage
```bash
# Check resource usage
kubectl top pods -n bytefreezer

# Scale down if needed
kubectl scale deployment bytefreezer-proxy --replicas=1 -n bytefreezer
```

### Debug Commands

```bash
# Get detailed pod information
kubectl describe pod -l app.kubernetes.io/name=bytefreezer-proxy -n bytefreezer

# Check events
kubectl get events -n bytefreezer --sort-by='.lastTimestamp'

# Execute commands in pod
kubectl exec -it deployment/bytefreezer-proxy -n bytefreezer -- /bin/sh

# Check configuration
kubectl exec deployment/bytefreezer-proxy -n bytefreezer -- cat /etc/bytefreezer-proxy/config.yaml
```

## Scaling and Performance

### Horizontal Scaling

```bash
# Scale to multiple replicas
kubectl scale deployment bytefreezer-proxy --replicas=3 -n bytefreezer

# Use HPA for auto-scaling
kubectl autoscale deployment bytefreezer-proxy --cpu-percent=70 --min=1 --max=5 -n bytefreezer
```

### Resource Tuning

Edit deployment resources:
```bash
kubectl edit deployment bytefreezer-proxy -n bytefreezer
```

Recommended resource limits:
- **Memory**: 512Mi - 2Gi depending on load
- **CPU**: 250m - 1000m depending on traffic
- **Storage**: 2Gi minimum for spooling

## Security Considerations

1. **Network Policies**: Restrict pod-to-pod communication
2. **RBAC**: Use service accounts with minimal permissions
3. **Secrets Management**: Store sensitive data in Kubernetes secrets
4. **Image Security**: Use specific image tags, not `latest`
5. **Resource Limits**: Prevent resource exhaustion

## Maintenance

### Updates

```bash
# Update to new version
kubectl set image deployment/bytefreezer-proxy bytefreezer-proxy=ghcr.io/n0needt0/bytefreezer-proxy:v1.2.0 -n bytefreezer

# Check rollout status
kubectl rollout status deployment/bytefreezer-proxy -n bytefreezer

# Rollback if needed
kubectl rollout undo deployment/bytefreezer-proxy -n bytefreezer
```

### Backup

```bash
# Backup configuration
kubectl get configmap bytefreezer-proxy-config -n bytefreezer -o yaml > backup-config.yaml

# Backup persistent data
kubectl exec deployment/bytefreezer-proxy -n bytefreezer -- tar -czf - /var/spool/bytefreezer-proxy > spool-backup.tar.gz
```

## Support

For issues and questions:
- GitHub Issues: https://github.com/n0needt0/bytefreezer-proxy/issues
- Documentation: Check the docs/README.md and configuration examples
- Logs: Always include pod logs when reporting issues