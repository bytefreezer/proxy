# ByteFreezer Proxy AWX Setup Guide

This guide provides step-by-step instructions for deploying ByteFreezer Proxy using AWX (Ansible AWX).

## Prerequisites

- AWX server running (version 21.0+)
- Admin access to AWX
- Target servers accessible from AWX
- SSH key pair for server access
- `awx-cli` installed (optional, for automated import)

## Quick Setup (5 Minutes)

### 1. Automated Import (Recommended)

```bash
# Install AWX CLI
pip install awxkit

# Run automated import
cd ansible/awx
python3 awx_import_script.py \
  --server https://your-awx-server \
  --username admin \
  --password your-password
```

### 2. Manual Setup

If you prefer manual setup or the automated import fails:

#### Step 1: Create Project
1. Go to **Resources → Projects**
2. Click **Add**
3. Configure:
   - **Name**: `ByteFreezer Proxy`
   - **SCM Type**: `Git`
   - **SCM URL**: `https://github.com/n0needt0/bytefreezer-proxy.git`
   - **SCM Branch**: `main`
   - **Playbook Directory**: `ansible`
   - **SCM Update Options**: Check all boxes

#### Step 2: Create Credential
1. Go to **Resources → Credentials**
2. Click **Add**
3. Configure:
   - **Name**: `ByteFreezer Proxy SSH`
   - **Credential Type**: `Machine`
   - **Username**: Your SSH username (e.g., `ubuntu`, `centos`)
   - **SSH Private Key**: Paste your private key
   - **Privilege Escalation Method**: `sudo`
   - **Privilege Escalation Username**: `root`

#### Step 3: Create Inventory
1. Go to **Resources → Inventories**
2. Click **Add → Add Inventory**
3. Configure:
   - **Name**: `ByteFreezer Proxy Servers`
   - **Description**: `Physical servers for ByteFreezer Proxy`

#### Step 4: Add Hosts to Inventory
1. Select your inventory
2. Go to **Hosts** tab
3. Click **Add**
4. For each server:
   - **Name**: `proxy-01` (or your naming convention)
   - **Variables**: Paste host-specific configuration (see examples below)

## Host Configuration Examples

### Production Server Example
```yaml
# Host: proxy-prod-01
ansible_host: 10.1.1.10
environment: production

# ByteFreezer Proxy configuration
bytefreezer_proxy_config:
  receiver:
    base_url: "http://receiver.prod.company.com:8080"
    tenant_id: "production-tenant"
    dataset_id: "prod-proxy-01-data"
  
  udp:
    port: 2056
    max_batch_lines: 100000
    max_batch_bytes: 268435456  # 256MB
    
  logging:
    level: "info"
    encoding: "json"
    
  otel:
    enabled: true
    endpoint: "otel-collector.prod.company.com:4317"
    service_name: "bytefreezer-proxy-prod"
```

### Staging Server Example
```yaml
# Host: proxy-staging-01
ansible_host: 192.168.1.100
environment: staging

bytefreezer_proxy_config:
  receiver:
    base_url: "http://receiver.staging.company.com:8080"
    tenant_id: "staging-tenant"
    dataset_id: "staging-proxy-01-data"
    
  udp:
    port: 2057  # Different port for staging
    max_batch_lines: 50000
    
  logging:
    level: "debug"
    encoding: "console"
```

## Job Templates

### Import Job Templates

Use the pre-configured job templates from the `awx/` directory:

1. **Installation/Deployment** (`bytefreezer_proxy_install.yml`)
2. **Configuration Update** (`bytefreezer_proxy_config_update.yml`)
3. **Service Management** (`bytefreezer_proxy_service_manage.yml`)
4. **Uninstallation** (`bytefreezer_proxy_uninstall.yml`)

### Manual Job Template Creation

#### 1. Installation/Deployment Job Template

1. Go to **Resources → Templates**
2. Click **Add → Add Job Template**
3. Configure:
   - **Name**: `ByteFreezer Proxy - Install`
   - **Job Type**: `Run`
   - **Inventory**: `ByteFreezer Proxy Servers`
   - **Project**: `ByteFreezer Proxy`
   - **Playbook**: `playbooks/deploy.yml`
   - **Credentials**: `ByteFreezer Proxy SSH`
   - **Options**: Enable survey, become, ask limit on launch
   - **Survey**: Enable and configure (see survey examples)

#### 2. Configuration Update Job Template

1. Same as above but:
   - **Name**: `ByteFreezer Proxy - Update Configuration`
   - **Playbook**: `playbooks/update-config.yml`

#### 3. Service Management Job Template

1. Same as above but:
   - **Name**: `ByteFreezer Proxy - Service Management`
   - **Playbook**: `playbooks/manage-service.yml`

#### 4. Uninstallation Job Template

1. Same as above but:
   - **Name**: `ByteFreezer Proxy - Uninstall`
   - **Playbook**: `playbooks/uninstall.yml`

## Survey Configurations

### Deployment Survey

Add this survey to the deployment job template:

| Question | Variable | Type | Default | Choices |
|----------|----------|------|---------|---------|
| Target Environment | `deployment_environment` | Multiple Choice | production | production, staging, development, all |
| Version to Deploy | `bytefreezer_proxy_version` | Text | latest | |
| Start Service After Deploy | `start_service_after_deploy` | Multiple Choice | true | true, false |
| Enable System Tuning | `enable_system_tuning` | Multiple Choice | true | true, false |
| Configure Firewall | `configure_firewall` | Multiple Choice | true | true, false |

### Configuration Update Survey

| Question | Variable | Type | Default | Required |
|----------|----------|------|---------|----------|
| Target Hosts | `target_limit` | Text | production | Yes |
| Receiver URL | `new_receiver_base_url` | Text | | No |
| Tenant ID | `new_tenant_id` | Text | | No |
| Dataset ID | `new_dataset_id` | Text | | No |
| Log Level | `new_log_level` | Multiple Choice | | debug, info, warn, error |
| Restart Service | `restart_after_update` | Multiple Choice | true | true, false |

### Service Management Survey

| Question | Variable | Type | Default | Choices |
|----------|----------|------|---------|---------|
| Target Hosts | `target_limit` | Text | production | |
| Service Action | `service_action` | Multiple Choice | status | start, stop, restart, reload, status, enable, disable |
| Show Logs | `show_logs` | Multiple Choice | true | true, false |
| Serial Execution | `serial_execution` | Multiple Choice | false | true, false |

## Usage Examples

### Install/Deploy to Production

1. Go to **Templates**
2. Click **🚀** next to `ByteFreezer Proxy - Install`
3. Fill survey:
   - Target Environment: `production`
   - Version: `latest` or `v1.2.3`
   - Start Service: `true`
4. Click **Launch**

### Update Configuration

1. Click **🚀** next to `ByteFreezer Proxy - Update Configuration`
2. Fill survey with new values
3. Launch job

### Manage Service

1. Click **🚀** next to `ByteFreezer Proxy - Service Management`
2. Select action (start, stop, restart, status)
3. Launch job

## Workflow Templates

### Creating a Full Deployment Workflow

1. Go to **Resources → Templates**
2. Click **Add → Add Workflow Template**
3. Configure:
   - **Name**: `ByteFreezer Proxy - Full Deployment`
   - **Description**: `Complete deployment with validation`
   - **Organization**: `Default`

4. **Workflow Visualizer**: 
   - Add job template nodes
   - Connect with success/failure paths
   - Configure convergence points

### Example Workflow Steps

```
Pre-deployment Checks
        ↓ (success)
Create Backup
        ↓ (success)
Main Deployment
        ↓ (success)         ↓ (failure)
Post-deployment Test → Rollback
        ↓ (success)         ↓
Success Notification   Failure Notification
```

## Environment-Specific Configurations

### Group Variables

Create group variables in AWX for different environments:

#### Production Group Variables
```yaml
# In AWX Inventory → Groups → production → Variables
bytefreezer_proxy_default_config:
  logging:
    level: "info"
    encoding: "json"
  otel:
    enabled: true
    endpoint: "otel-prod.company.com:4317"
  soc:
    enabled: true
    endpoint: "https://soc.company.com/alerts"
```

#### Staging Group Variables
```yaml
# In AWX Inventory → Groups → staging → Variables
bytefreezer_proxy_default_config:
  logging:
    level: "debug"
    encoding: "console"
  otel:
    enabled: true
    endpoint: "otel-staging.company.com:4317"
```

## Scheduling and Automation

### Schedule Regular Deployments

1. Go to job template
2. Click **Schedules** tab
3. Click **Add**
4. Configure:
   - **Name**: `Weekly Production Deployment`
   - **Frequency**: `Weekly`
   - **Days**: `Sunday`
   - **Time**: `02:00 UTC`

### Webhook Triggers

1. In job template settings
2. Enable **Enable Webhook**
3. Use webhook URL for CI/CD integration:
   ```bash
   curl -X POST \
     -H "Content-Type: application/json" \
     -d '{"extra_vars": {"bytefreezer_proxy_version": "v1.2.3"}}' \
     https://your-awx-server/api/v2/job_templates/ID/github/
   ```

## Monitoring and Troubleshooting

### Job Monitoring

1. **Dashboard**: Real-time job status overview
2. **Jobs**: Detailed job history and logs
3. **Activity Stream**: Audit trail of all actions

### Common Issues

#### SSH Connection Failed
- Check credential configuration
- Verify SSH key permissions
- Test connectivity: `ssh -i key.pem user@host`

#### Playbook Execution Failed
- Review job logs in AWX
- Check inventory host variables
- Verify project sync status

#### Survey Variables Not Working
- Check variable names match playbook expectations
- Verify survey is enabled on job template
- Review extra variables in job details

### Log Analysis

Access detailed logs:
1. Go to **Jobs** → Select job
2. View **Details** tab for execution log
3. Check **Facts** tab for gathered information
4. Review **Inventory** tab for host details

## Best Practices

### Security
- Use separate credentials for different environments
- Limit job template access with RBAC
- Regular credential rotation
- Enable audit logging

### Organization
- Use consistent naming conventions
- Tag job templates by environment/function  
- Document changes in template descriptions
- Use workflow templates for complex processes

### Monitoring
- Set up notification templates for job failures
- Monitor job execution times
- Review failed jobs regularly
- Set up dashboards for deployment metrics

## Integration Examples

### CI/CD Pipeline Integration

```yaml
# .github/workflows/deploy-awx.yml
name: Deploy via AWX

on:
  push:
    tags: ['v*']

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger AWX Deployment
        run: |
          curl -X POST \
            -H "Authorization: Bearer ${{ secrets.AWX_TOKEN }}" \
            -H "Content-Type: application/json" \
            -d '{
              "extra_vars": {
                "bytefreezer_proxy_version": "${{ github.ref_name }}",
                "deployment_environment": "production"
              }
            }' \
            ${{ secrets.AWX_URL }}/api/v2/job_templates/${{ secrets.AWX_JOB_TEMPLATE_ID }}/launch/
```

### Slack Notifications

Set up notification templates in AWX to send deployment status to Slack channels.

## Support and Troubleshooting

### AWX API Access

Access AWX API for automation:
```bash
# Get job template info
curl -H "Authorization: Bearer TOKEN" \
  https://awx-server/api/v2/job_templates/

# Launch job with variables
curl -X POST \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"extra_vars": {"version": "v1.0.0"}}' \
  https://awx-server/api/v2/job_templates/ID/launch/
```

### Getting Help

- AWX Documentation: https://docs.ansible.com/ansible-tower/
- ByteFreezer Proxy Issues: https://github.com/n0needt0/bytefreezer-proxy/issues
- Ansible Community: https://docs.ansible.com/ansible/latest/community/

## File Structure Summary

After setup, your AWX directory contains these key files:

```
ansible/awx/
├── AWX_SETUP_GUIDE.md                  # This comprehensive setup guide
├── README.md                           # AWX overview and quick start
├── awx_import_script.py               # Automated import tool
├── inventory_import.yml               # Inventory structure template
├── bytefreezer_proxy_install.yml      # Installation/deployment job template
├── bytefreezer_proxy_config_update.yml # Configuration update template  
├── bytefreezer_proxy_service_manage.yml # Service management template
├── bytefreezer_proxy_uninstall.yml    # Safe uninstall template
└── workflow_template_full_deployment.yml # Complete deployment workflow
```

## AWX Template Names

Once imported, you'll see these templates in AWX:

- **ByteFreezer Proxy - Install** (from `bytefreezer_proxy_install.yml`)
- **ByteFreezer Proxy - Update Configuration** (from `bytefreezer_proxy_config_update.yml`)
- **ByteFreezer Proxy - Service Management** (from `bytefreezer_proxy_service_manage.yml`)
- **ByteFreezer Proxy - Uninstall** (from `bytefreezer_proxy_uninstall.yml`)

This guide provides a complete setup for managing ByteFreezer Proxy deployments through AWX with web-based management, scheduling, RBAC, and workflow automation.