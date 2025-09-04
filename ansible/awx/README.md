# ByteFreezer Proxy AWX Configuration

This directory contains AWX-specific configurations for deploying and managing ByteFreezer Proxy through the AWX web interface.

## Overview

AWX provides a web-based UI for managing Ansible playbooks with features like:
- Job scheduling and automation
- Role-based access control (RBAC)
- Survey forms for dynamic variables
- Workflow templates for complex deployments
- Real-time job monitoring and logs

## AWX Setup Requirements

### Prerequisites
- AWX server running (version 21.0+)
- Git repository access from AWX
- Target hosts accessible from AWX
- Proper credentials configured in AWX

### Required AWX Objects

1. **Project**: Points to this Git repository
2. **Inventory**: Defines target hosts and groups
3. **Credentials**: SSH keys and vault passwords
4. **Job Templates**: Playbook execution templates
5. **Survey Specs**: Dynamic forms for variables
6. **Workflow Templates**: Multi-step deployment processes

## Quick Start

### 1. Create Project in AWX

1. Go to **Resources → Projects**
2. Click **Add**
3. Configure:
   - **Name**: `ByteFreezer Proxy`
   - **SCM Type**: `Git`
   - **SCM URL**: `https://github.com/n0needt0/bytefreezer-proxy.git`
   - **SCM Branch**: `main`
   - **SCM Update Options**: 
     - ☑️ Clean
     - ☑️ Delete on Update
     - ☑️ Update Revision on Launch
   - **Playbook Directory**: `ansible`

### 2. Create Machine Credential

1. Go to **Resources → Credentials**
2. Click **Add**
3. Configure:
   - **Name**: `ByteFreezer Proxy SSH`
   - **Credential Type**: `Machine`
   - **Username**: Your SSH username
   - **SSH Private Key**: Paste your private key
   - **Privilege Escalation Method**: `sudo`
   - **Privilege Escalation Username**: `root`

### 3. Create Inventory

1. Go to **Resources → Inventories**
2. Click **Add → Add Inventory**
3. Configure:
   - **Name**: `ByteFreezer Proxy Servers`
   - **Description**: `Physical servers for ByteFreezer Proxy deployment`

### 4. Import Inventory from Repository

Use the `inventory_import.yml` file in this directory to import your inventory structure.

## Job Templates

Import all job templates using the configuration files in this directory:

- `bytefreezer_proxy_install.yml` - Main installation/deployment
- `bytefreezer_proxy_config_update.yml` - Configuration updates  
- `bytefreezer_proxy_service_manage.yml` - Service management
- `bytefreezer_proxy_uninstall.yml` - Safe uninstallation

## Workflow Templates

For complex deployments, use workflow templates:

- `workflow_full_deployment.yml` - Complete deployment process
- `workflow_rolling_update.yml` - Zero-downtime updates

## Survey Specifications

Interactive forms for common operations:

- `survey_deployment.yml` - Deployment parameters
- `survey_config_update.yml` - Configuration changes
- `survey_service_management.yml` - Service operations