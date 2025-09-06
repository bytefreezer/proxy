# AWX Deployment Guide

Quick setup guide for deploying ByteFreezer Proxy using AWX.

## Setup Steps

### 1. Create Project
```yaml
Name: ByteFreezer Proxy
SCM Type: Git
SCM URL: https://github.com/n0needt0/bytefreezer-proxy.git
SCM Subdirectory: ansible/playbooks
Branch: main
```

### 2. Create Credential
```yaml
Name: ByteFreezer SSH
Type: Machine
Username: your-user
SSH Private Key: [paste key]
Privilege Escalation: sudo
```

### 3. Create Inventory
```yaml
Name: ByteFreezer Servers
Hosts:
  - proxy-01.example.com
  - proxy-02.example.com
```

### 4. Create Job Templates

**Install Service:**
```yaml
Name: ByteFreezer Proxy - Install
Playbook: install.yml
Inventory: ByteFreezer Servers
Credential: ByteFreezer SSH
Options: ☑ Become Privilege Escalation
```

**Remove Service:**
```yaml
Name: ByteFreezer Proxy - Remove  
Playbook: remove.yml
Inventory: ByteFreezer Servers
Credential: ByteFreezer SSH
Options: ☑ Become Privilege Escalation
```

## Configuration

Variables are in `group_vars/all.yml`. Override in AWX job templates as needed:

```yaml
# Example override variables
bytefreezer_proxy_version: "v1.2.3"
tenant_id: "customer-prod"
bearer_token: "prod-token-123"
```

## Usage

1. Run "ByteFreezer Proxy - Install" to deploy service
2. Run "ByteFreezer Proxy - Remove" to uninstall service

Service will be available on:
- API: `http://server:8088/health`
- UDP: ports 2056, 2057, 2058