# ByteFreezer Proxy Ansible Deployment

This directory contains Ansible playbooks and configuration for automated deployment of ByteFreezer Proxy to physical hardware.

## Features

- ✅ **Automated Binary Download**: Fetches latest releases from GitHub
- ✅ **Multi-Architecture Support**: AMD64, ARM64 automatic detection
- ✅ **Configuration Management**: Template-based config with variable overrides
- ✅ **System Integration**: systemd service, firewall, system tuning
- ✅ **Security Hardening**: Non-root user, restricted permissions
- ✅ **Backup & Recovery**: Automatic backups before updates
- ✅ **Service Management**: Start/stop/restart operations
- ✅ **Health Monitoring**: Service validation and health checks

## Quick Start

1. **Setup Inventory**:
   ```bash
   # Copy and edit the inventory file
   cp inventories/hosts.yml.example inventories/hosts.yml
   # Edit with your server details
   ```

2. **Deploy**:
   ```bash
   # Deploy to all hosts in 'bytefreezer_proxy' group
   ./scripts/deploy.sh
   
   # Deploy to specific environment
   ./scripts/deploy.sh production
   
   # Dry run (check mode)
   ./scripts/deploy.sh production --check
   ```

3. **Verify**:
   ```bash
   # Check service status
   ansible-playbook playbooks/manage-service.yml -i inventories/hosts.yml -e action=status
   
   # Test health endpoint
   curl http://YOUR_SERVER:8088/health
   ```

## Directory Structure

```
ansible/
├── ansible.cfg                 # Ansible configuration
├── inventories/
│   └── hosts.yml              # Server inventory and variables
├── group_vars/
│   └── bytefreezer_proxy.yml  # Group-level variables
├── host_vars/                 # Host-specific variables (optional)
├── templates/
│   ├── config.yaml.j2         # Configuration template
│   └── bytefreezer-proxy.service.j2  # systemd service template
├── playbooks/
│   ├── deploy.yml             # Main deployment playbook
│   ├── update-config.yml      # Configuration update
│   ├── manage-service.yml     # Service management
│   └── uninstall.yml          # Uninstallation
├── scripts/
│   └── deploy.sh              # Deployment wrapper script
└── README.md                  # This file
```

## Configuration Management

### Inventory Configuration

Edit `inventories/hosts.yml` to define your servers and their specific configurations:

```yaml
all:
  children:
    bytefreezer_proxy:
      hosts:
        proxy-01:
          ansible_host: 192.168.1.10
          bytefreezer_proxy_config:
            receiver:
              base_url: "http://receiver.example.com:8080"
              tenant_id: "customer-1"
              dataset_id: "proxy-01-data"
            udp:
              port: 2056
              max_batch_lines: 50000
```

### Configuration Hierarchy

Configuration is merged from multiple sources (highest priority first):

1. **Host Variables** (`host_vars/hostname.yml`)
2. **Inventory Host Variables** (in `hosts.yml`)
3. **Group Variables** (`group_vars/bytefreezer_proxy.yml`)
4. **Default Values** (defined in playbooks)

### Common Configuration Options

```yaml
# In inventories/hosts.yml or group_vars/bytefreezer_proxy.yml
bytefreezer_proxy_config:
  # Application settings
  app:
    name: "bytefreezer-proxy"
    version: "0.0.1"
  
  # Logging
  logging:
    level: "info"  # debug, info, warn, error
    encoding: "console"
  
  # API server
  server:
    api_port: 8088
  
  # UDP listener
  udp:
    enabled: true
    host: "0.0.0.0"
    port: 2056
    max_batch_lines: 100000
    max_batch_bytes: 268435456  # 256MB
    batch_timeout_seconds: 30
    enable_compression: true
    compression_level: 6
  
  # Receiver connection
  receiver:
    base_url: "http://localhost:8080"
    tenant_id: "default-tenant"
    dataset_id: "default-dataset"
    timeout_seconds: 30
    retry_count: 3
  
  # Optional: OpenTelemetry
  otel:
    enabled: false
    endpoint: "localhost:4317"
    service_name: "bytefreezer-proxy"
  
  # Optional: SOC alerting
  soc:
    enabled: false
    endpoint: ""
```

## Playbooks

### 1. Main Deployment (`deploy.yml`)

Performs complete installation and configuration:

```bash
# Full deployment
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml

# Deploy specific version
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml -e bytefreezer_proxy_version=v1.2.3

# Deploy to specific host
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml --limit proxy-01
```

### 2. Configuration Update (`update-config.yml`)

Updates configuration without reinstalling:

```bash
# Update configuration on all hosts
ansible-playbook playbooks/update-config.yml -i inventories/hosts.yml

# Update specific host
ansible-playbook playbooks/update-config.yml -i inventories/hosts.yml --limit proxy-01
```

### 3. Service Management (`manage-service.yml`)

Start, stop, restart, or check service status:

```bash
# Check status
ansible-playbook playbooks/manage-service.yml -i inventories/hosts.yml -e action=status

# Restart service
ansible-playbook playbooks/manage-service.yml -i inventories/hosts.yml -e action=restart

# Stop service
ansible-playbook playbooks/manage-service.yml -i inventories/hosts.yml -e action=stop

# Available actions: start, stop, restart, reload, status, enable, disable
```

### 4. Uninstallation (`uninstall.yml`)

Removes ByteFreezer Proxy (requires confirmation):

```bash
# Uninstall (keeping configuration and logs)
ansible-playbook playbooks/uninstall.yml -i inventories/hosts.yml -e force_uninstall=true

# Complete removal (including data)
ansible-playbook playbooks/uninstall.yml -i inventories/hosts.yml -e force_uninstall=true -e purge_data=true
```

## CI/CD Integration

### GitHub Actions Workflow

The repository includes a complete CI/CD pipeline (`.github/workflows/build-and-release.yml`) that:

1. **Tests** code on pull requests
2. **Builds** binaries for multiple architectures
3. **Creates** GitHub releases with assets
4. **Builds** and publishes Docker images

### Automated Deployment

You can integrate with your CI/CD by triggering Ansible deployments:

```bash
# In your CI/CD pipeline
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy/ansible

# Configure inventory and variables
# Run deployment
./scripts/deploy.sh production
```

## System Requirements

### Ansible Control Host

- Ansible 2.9+ (tested with 2.15+)
- Python 3.6+
- SSH access to target hosts

Installation:
```bash
# Ubuntu/Debian
sudo apt update && sudo apt install ansible

# RHEL/CentOS
sudo dnf install ansible

# macOS
brew install ansible

# Python pip
pip install ansible
```

### Target Hosts

- **OS**: Ubuntu 18.04+, CentOS 7+, RHEL 8+
- **Architecture**: AMD64 or ARM64
- **Memory**: 512MB+ RAM
- **Disk**: 100MB+ free space
- **Network**: Internet access for binary downloads
- **Privileges**: sudo/root access

### Firewall Ports

- **8088/tcp**: API endpoint (health, config, docs)
- **2056/udp**: UDP listener (configurable)

## Security Considerations

- Service runs as non-root user (`bytefreezer`)
- systemd hardening (NoNewPrivileges, ProtectSystem, etc.)
- Configuration files have restricted permissions (640)
- Network restrictions in systemd service
- Firewall rules automatically configured

## Troubleshooting

### Common Issues

1. **SSH Connection Failed**:
   ```bash
   # Test SSH connectivity
   ansible all -i inventories/hosts.yml -m ping
   ```

2. **Binary Download Failed**:
   ```bash
   # Check GitHub API access
   curl -s https://api.github.com/repos/n0needt0/bytefreezer-proxy/releases/latest
   ```

3. **Service Won't Start**:
   ```bash
   # Check service logs
   ssh your-host 'journalctl -u bytefreezer-proxy -f'
   
   # Validate configuration
   ssh your-host '/opt/bytefreezer-proxy/bytefreezer-proxy --validate-config'
   ```

4. **UDP Buffer Issues**:
   ```bash
   # Check system limits
   ssh your-host 'sysctl net.core.rmem_max'
   
   # The playbook automatically configures these, but you can verify
   ```

### Debug Mode

Run playbooks with verbose output:

```bash
# Verbose output
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml -v

# Extra verbose (shows task details)
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml -vv

# Debug level (shows everything)
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml -vvv
```

### Manual Service Management

```bash
# SSH to target host
ssh your-host

# Check service status
sudo systemctl status bytefreezer-proxy

# View logs
sudo journalctl -u bytefreezer-proxy -f

# Restart service
sudo systemctl restart bytefreezer-proxy

# Check configuration
sudo -u bytefreezer /opt/bytefreezer-proxy/bytefreezer-proxy --validate-config
```

## Advanced Usage

### Multi-Environment Setup

Create separate inventory files for different environments:

```bash
inventories/
├── production.yml      # Production servers
├── staging.yml         # Staging servers
└── development.yml     # Development servers
```

Deploy to specific environment:
```bash
./scripts/deploy.sh production
./scripts/deploy.sh staging
```

### Custom Configuration Templates

1. Copy the template: `cp templates/config.yaml.j2 templates/config-custom.yaml.j2`
2. Modify the template as needed
3. Override template in variables:
   ```yaml
   bytefreezer_proxy_config_template: "config-custom.yaml.j2"
   ```

### Rolling Updates

Deploy to hosts one at a time:
```bash
ansible-playbook playbooks/deploy.yml -i inventories/hosts.yml --forks 1 --serial 1
```

### Health Checks Integration

The deployment includes health check endpoints that can be integrated with monitoring systems:

```bash
# Health check endpoint
curl http://your-host:8088/health

# Configuration endpoint (sensitive values masked)
curl http://your-host:8088/config

# Metrics endpoint (if OTEL enabled)
curl http://your-host:8088/metrics
```

## Support

- **Documentation**: See main [README.md](../README.md)
- **Issues**: [GitHub Issues](https://github.com/n0needt0/bytefreezer-proxy/issues)
- **CI/CD**: [GitHub Actions](https://github.com/n0needt0/bytefreezer-proxy/actions)