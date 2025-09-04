# AWX Deployment Guide - ByteFreezer Proxy

This guide walks you through setting up ByteFreezer Proxy in AWX (Ansible Tower) for automated deployment and management, including scenarios with multiple projects.

## Table of Contents

- [Prerequisites](#prerequisites)
- [AWX Project Setup](#awx-project-setup)
- [Multi-Project Environment Setup](#multi-project-environment-setup)
- [Importing Configuration](#importing-configuration)
- [Setting Up Credentials](#setting-up-credentials)
- [Creating Inventories](#creating-inventories)
- [Job Templates Configuration](#job-templates-configuration)
- [Workflow Templates](#workflow-templates)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Prerequisites

### AWX Environment
- AWX/Ansible Tower installed and configured
- Admin access to AWX web interface
- Git repository access (GitHub/GitLab/etc.)
- Target servers accessible from AWX

### Repository Requirements
- ByteFreezer Proxy repository with Ansible playbooks
- SSH access keys for target servers
- Vault passwords (if using encrypted variables)

## AWX Project Setup

### 1. Single Project Setup

#### Create Project in AWX

1. **Navigate to Projects**:
   - Go to `Resources > Projects`
   - Click `Add` button

2. **Configure Project**:
   ```yaml
   Name: ByteFreezer Proxy
   Description: ByteFreezer Proxy deployment and management
   Organization: Default (or your organization)
   
   SCM Type: Git
   SCM URL: https://github.com/n0needt0/bytefreezer-proxy.git
   SCM Branch/Tag/Commit: main
   SCM Credential: <select your Git credential>
   
   Options:
   ☑ Clean
   ☑ Delete on Update  
   ☑ Update Revision on Launch
   
   Cache Timeout: 0
   ```

3. **Sync Project**:
   - Click `Save`
   - Click the sync button to pull the repository
   - Verify playbooks appear in the project

### 2. Multi-Project Environment Setup

When managing multiple ByteFreezer projects (proxy, receiver, packer), you have several architectural options:

#### Option A: Separate Projects (Recommended)

Create individual projects for each component:

```yaml
# Project 1: ByteFreezer Proxy
Name: ByteFreezer-Proxy
SCM URL: https://github.com/n0needt0/bytefreezer-proxy.git
Playbook Path: ansible/

# Project 2: ByteFreezer Receiver  
Name: ByteFreezer-Receiver
SCM URL: https://github.com/n0needt0/bytefreezer-receiver.git
Playbook Path: ansible/

# Project 3: ByteFreezer Packer
Name: ByteFreezer-Packer
SCM URL: https://github.com/n0needt0/bytefreezer-packer.git
Playbook Path: ansible/
```

**Benefits**:
- Independent version control
- Separate permissions per component
- Isolated deployment pipelines
- Component-specific inventory management

#### Option B: Monorepo Project

Single project with all components:

```yaml
Name: ByteFreezer-Suite
SCM URL: https://github.com/your-org/bytefreezer-monorepo.git
Structure:
├── bytefreezer-proxy/ansible/
├── bytefreezer-receiver/ansible/  
├── bytefreezer-packer/ansible/
└── shared/ansible/
```

**Benefits**:
- Centralized configuration
- Shared variables and templates
- Coordinated deployments
- Single source of truth

#### Option C: Hybrid Approach

Core infrastructure project + component projects:

```yaml
# Core Infrastructure
Name: ByteFreezer-Infrastructure
SCM URL: https://github.com/your-org/bytefreezer-infra.git
Contents: Common roles, group_vars, inventory templates

# Individual Components (as in Option A)
```

## Importing Configuration

### Using the Import Script

The project includes an automated import script:

```bash
# From your local machine
cd bytefreezer-proxy/ansible
python3 awx_import_script.py --awx-url https://your-awx.example.com --username admin
```

### Manual Import Process

#### 1. Import Inventory

```bash
# Upload inventory file
awx-cli receive --organization "Default" \
    --inventory "ByteFreezer Proxy Servers" \
    --overwrite_vars \
    < inventories/hosts.yml
```

#### 2. Import Job Templates

Navigate to `Templates > Job Templates` and create each template:

**ByteFreezer Proxy - Deploy**:
```yaml
Name: ByteFreezer Proxy - Deploy
Job Type: Run
Inventory: ByteFreezer Proxy Servers
Project: ByteFreezer Proxy
Playbook: playbooks/deploy.yml
Credential: ByteFreezer SSH Key

Variables:
  bytefreezer_proxy_version: "latest"
  bytefreezer_proxy_systemd_started: true

Options:
☑ Become Privilege Escalation
☑ Enable Survey
☑ Prompt on launch (Variables)
☑ Prompt on launch (Limit)
```

**ByteFreezer Proxy - Update Configuration**:
```yaml
Name: ByteFreezer Proxy - Update Configuration  
Job Type: Run
Inventory: ByteFreezer Proxy Servers
Project: ByteFreezer Proxy
Playbook: playbooks/update-config.yml
Credential: ByteFreezer SSH Key

Options:
☑ Become Privilege Escalation
☑ Enable Survey
☑ Show Changes (Diff Mode)
```

## Setting Up Credentials

### 1. SSH Credentials

Create SSH key credential for server access:

```yaml
Name: ByteFreezer SSH Key
Organization: Default
Credential Type: Machine

Details:
  Username: ansible
  SSH Private Key: [paste your private key]
  Privilege Escalation Method: sudo
  Privilege Escalation Username: root
```

### 2. Vault Credentials (if using encrypted vars)

```yaml
Name: ByteFreezer Vault Password
Organization: Default  
Credential Type: Vault

Details:
  Vault Password: [your vault password]
```

### 3. Multi-Project Credential Management

For multiple projects, organize credentials by:

#### Environment-Based
```yaml
ByteFreezer-SSH-Production
ByteFreezer-SSH-Staging
ByteFreezer-SSH-Development
```

#### Component-Based
```yaml
ByteFreezer-Proxy-SSH
ByteFreezer-Receiver-SSH
ByteFreezer-Packer-SSH
```

#### Hybrid Organization
```yaml
# Environment + Component
ByteFreezer-Proxy-Production-SSH
ByteFreezer-Receiver-Production-SSH
ByteFreezer-Proxy-Staging-SSH
```

## Creating Inventories

### 1. Environment-Separated Inventories

#### Production Inventory
```yaml
Name: ByteFreezer-Production
Description: Production environment servers
Organization: Default

Groups:
  bytefreezer_proxy_production:
    hosts:
      - proxy-prod-01.example.com
      - proxy-prod-02.example.com
  
  bytefreezer_receiver_production:
    hosts:
      - receiver-prod-01.example.com
      - receiver-prod-02.example.com
```

#### Staging Inventory  
```yaml
Name: ByteFreezer-Staging
Description: Staging environment servers

Groups:
  bytefreezer_proxy_staging:
    hosts:
      - proxy-stage-01.example.com
  
  bytefreezer_receiver_staging:
    hosts:
      - receiver-stage-01.example.com
```

### 2. Component-Separated Inventories

```yaml
# Separate inventory per component
ByteFreezer-Proxy-Servers
ByteFreezer-Receiver-Servers  
ByteFreezer-Packer-Servers
```

### 3. Host Variables Configuration

Configure host-specific variables:

```yaml
# Host: proxy-prod-01.example.com
bytefreezer_proxy_config:
  receiver:
    base_url: "http://receiver-prod-01.example.com:8080"
    tenant_id: "customer-alpha"
  udp:
    listeners:
      - port: 2056
        dataset_id: "syslog-prod"
      - port: 2057  
        dataset_id: "metrics-prod"
```

## Job Templates Configuration

### Survey Forms for Multi-Project Environments

#### Environment Selection Survey
```yaml
Survey Questions:
  - Question: "Target Environment"
    Variable: deployment_environment
    Type: multiple_choice
    Choices: [production, staging, development, all]
    
  - Question: "Component Selection"  
    Variable: target_component
    Type: multiple_choice
    Choices: [proxy, receiver, packer, all]
    
  - Question: "Deployment Strategy"
    Variable: deployment_strategy  
    Type: multiple_choice
    Choices: [rolling, blue-green, immediate]
```

#### Configuration Management Survey
```yaml
Survey Questions:
  - Question: "Configuration Scope"
    Variable: config_scope
    Type: multiple_choice  
    Choices: [global, environment, host-specific]
    
  - Question: "Service Restart Required"
    Variable: restart_services
    Type: multiple_choice
    Choices: [yes, no, rolling-restart]
```

### Job Template Organization

#### Naming Convention for Multiple Projects
```yaml
# Pattern: [Component] - [Action] - [Environment]
ByteFreezer-Proxy - Deploy - Production
ByteFreezer-Proxy - Deploy - Staging  
ByteFreezer-Receiver - Deploy - Production
ByteFreezer-Packer - Deploy - All-Environments

# Or: [Environment] - [Component] - [Action]  
Production - ByteFreezer-Proxy - Deploy
Staging - ByteFreezer-Receiver - Update-Config
```

## Workflow Templates

### 1. Full Stack Deployment Workflow

Create a workflow template for complete ByteFreezer deployment:

```yaml
Name: ByteFreezer - Full Stack Deployment
Description: Deploy entire ByteFreezer suite in order

Workflow Steps:
  1. Deploy ByteFreezer Receiver (wait for completion)
  2. Deploy ByteFreezer Proxy (parallel deployment) 
  3. Deploy ByteFreezer Packer (wait for proxy)
  4. Run Health Checks (parallel on all components)
  5. Send Notification (on success/failure)
```

#### Workflow Node Configuration

**Node 1: Deploy Receiver**
```yaml
Job Template: ByteFreezer Receiver - Deploy
Credential: ByteFreezer-SSH-Production
Limit: receiver_production
Success Node: Deploy Proxy
Failure Node: Notification - Failed
```

**Node 2: Deploy Proxy**  
```yaml
Job Template: ByteFreezer Proxy - Deploy
Credential: ByteFreezer-SSH-Production
Limit: proxy_production
Success Node: Deploy Packer
Failure Node: Notification - Failed
```

### 2. Rolling Update Workflow

```yaml
Name: ByteFreezer - Rolling Update
Description: Update all components with zero downtime

Steps:
  1. Pre-flight Checks
  2. Update Receivers (one at a time)
  3. Update Proxies (one at a time) 
  4. Update Packers (parallel)
  5. Post-deployment Verification
```

### 3. Multi-Environment Deployment

```yaml
Name: ByteFreezer - Multi-Environment Deploy
Description: Deploy to staging, test, then production

Steps:
  1. Deploy to Staging
  2. Run Integration Tests
  3. Manual Approval Gate
  4. Deploy to Production
  5. Monitor and Alert
```

## Best Practices

### 1. Project Organization

```yaml
# Recommended folder structure in AWX
Projects/
├── ByteFreezer-Infrastructure/     # Shared components
├── ByteFreezer-Proxy/             # Proxy-specific
├── ByteFreezer-Receiver/          # Receiver-specific
└── ByteFreezer-Packer/            # Packer-specific

Inventories/
├── Production/
├── Staging/  
├── Development/
└── Testing/

Credentials/
├── SSH-Keys/
├── Service-Accounts/
└── API-Tokens/
```

### 2. Variable Management

#### Group Variables Hierarchy
```yaml
# group_vars/all.yml - Global defaults
bytefreezer_common:
  log_level: info
  backup_enabled: true

# group_vars/production.yml - Environment-specific  
bytefreezer_common:
  log_level: warn
  backup_retention_days: 30

# host_vars/proxy-01.yml - Host-specific
bytefreezer_proxy_config:
  server:
    api_port: 8088
```

### 3. Security Considerations

#### Credential Isolation
```yaml
# Production credentials
Production-SSH: Only accessible to Production team
Production-Vault: Only accessible to Security team

# Development credentials  
Development-SSH: Accessible to Development team
```

#### Survey Validation
```yaml
Survey Questions:
  - Question: "Target Hosts"
    Variable: target_hosts
    Type: text
    Required: true
    Min: 1
    Max: 100
    Default: "staging"
    # Validation prevents accidental production deployments
```

### 4. Monitoring and Notifications

#### Notification Templates
```yaml
# Success Notification
Name: ByteFreezer Deployment Success
Type: Email/Slack
Message: |
  ✅ ByteFreezer {{ awx_job_template_name }} completed successfully
  
  Environment: {{ deployment_environment }}
  Components: {{ target_component }}
  Duration: {{ awx_job_elapsed }}
  
  View details: {{ awx_job_url }}

# Failure Notification  
Name: ByteFreezer Deployment Failed
Type: Email/Slack/PagerDuty
Message: |
  ❌ ByteFreezer {{ awx_job_template_name }} failed
  
  Error: {{ awx_job_stdout_lines[-10:] }}
  View logs: {{ awx_job_url }}
```

### 5. Multi-Project Coordination

#### Dependency Management
```yaml
# Use workflow templates to manage dependencies
Workflow: ByteFreezer Full Deployment
  Node 1: Check Prerequisites
    - Verify network connectivity
    - Check system resources
    - Validate configurations
  
  Node 2: Deploy Backend (Receiver + Packer)
    - Deploy in parallel where safe
    - Wait for health checks
  
  Node 3: Deploy Frontend (Proxy)
    - Deploy after backend is ready
    - Update load balancers
  
  Node 4: Final Validation
    - End-to-end testing
    - Performance validation
```

## Troubleshooting

### Common Issues

#### 1. Project Sync Failures
```bash
# Check SCM credentials
# Verify repository URL and branch
# Check network connectivity from AWX to Git server

# Debug commands:
git ls-remote https://github.com/n0needt0/bytefreezer-proxy.git
```

#### 2. Inventory Import Issues
```yaml
# Verify inventory format:
all:
  children:
    bytefreezer_proxy:
      hosts:
        proxy-01.example.com:
          ansible_host: 10.0.1.10
```

#### 3. Job Template Failures
```bash
# Check logs in AWX job output
# Verify credentials have proper permissions
# Test SSH connectivity manually:
ssh -i /path/to/key user@target-host

# Test privilege escalation:
sudo -l
```

#### 4. Multi-Project Conflicts
```yaml
# Use unique naming:
# Bad:  proxy_config
# Good: bytefreezer_proxy_config

# Separate variable namespaces:
bytefreezer_proxy:
  api_port: 8088
bytefreezer_receiver:  
  api_port: 8080
```

### Debugging Multi-Project Deployments

#### 1. Component Health Checks
```bash
# Create health check job templates
curl -f http://proxy-01:8088/health
curl -f http://receiver-01:8080/health  
curl -f http://packer-01:8090/health
```

#### 2. Dependency Verification
```bash
# Test connectivity between components
nc -zv receiver-01.example.com 8080
dig receiver-01.example.com
```

#### 3. Configuration Validation
```bash
# Validate configurations before deployment
bytefreezer-proxy --validate-config
bytefreezer-receiver --validate-config
```

### Performance Optimization

#### 1. Parallel Execution
```yaml
# Use forks setting for parallel deployment
Job Template Settings:
  Forks: 10  # Deploy to up to 10 hosts simultaneously
  
# Use async for long-running tasks
- name: Deploy large component
  async: 3600
  poll: 60
```

#### 2. Smart Inventory Management
```yaml
# Use dynamic inventories for large environments
# Cache inventory data
# Use host patterns for targeted deployments
Limit: "proxy_*:!proxy_maintenance"
```

This guide provides comprehensive instructions for deploying ByteFreezer Proxy in AWX, whether as a single project or as part of a larger multi-project ByteFreezer suite. The modular approach allows you to adapt the configuration to your specific organizational needs and deployment patterns.