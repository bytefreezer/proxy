# ByteFreezer Proxy Deployment Guide

## Deployment Types

### 1. Managed Deployment (tp1, tp2, tp3)
Default setup where ByteFreezer manages all infrastructure.

**Architecture**:
- Proxy: tp1 (.101)
- Receiver: tp2 (.102)
- Control: tp3 (.103)

**Deploy**:
```bash
cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks

# Deploy to managed infrastructure
ansible-playbook -i inventory/managed.ini onprem_install.yml
```

### 2. On-Premises Deployment (tp4, tp5)
Customer-hosted setup with shared control plane.

**Architecture**:
- Proxy: tp4 (.104) - on-prem
- Receiver: tp5 (.105) - on-prem
- Piper: tp4 (.104) - on-prem
- Packer: tp5 (.105) - on-prem
- Control: tp3 (.103) - **shared**

## Setup On-Prem Account (First Time)

### Step 1: Create On-Prem Account in Control

```bash
curl -X POST http://192.168.86.103:8082/api/v1/accounts \
  -H "Authorization: Bearer bytefreezer-service-api-key-8f4a2d1b-3c5e-4f6a-9b8c-7d2e1f3a4b5c" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "OnPrem Test Account",
    "email": "onprem@test.local",
    "deployment_type": "on_prem"
  }'
```

**Response**:
```json
{
  "account": {
    "id": "abc123xyz456",
    "name": "OnPrem Test Account",
    "email": "onprem@test.local",
    "deployment_type": "on_prem",
    ...
  },
  "api_key": "your-onprem-api-key-here"
}
```

### Step 2: Update On-Prem Variables

Edit `group_vars/onprem.yml`:

```yaml
config:
  account_id: "abc123xyz456"  # From API response
  bearer_token: "your-onprem-api-key-here"  # From API response
  control_url: "http://192.168.86.103:8082"
  receiver:
    base_url: "http://192.168.86.105:8081/{tenantid}/{datasetid}"
```

### Step 3: Deploy to On-Prem

```bash
cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks

# Deploy to on-prem infrastructure
ansible-playbook -i inventory/onprem.ini onprem_install.yml
```

## Variables Structure

```
playbooks/
├── group_vars/
│   ├── all.yml       # Common variables (defaults)
│   ├── managed.yml   # Managed-specific (tp1, tp2, tp3)
│   └── onprem.yml    # On-prem-specific (tp4, tp5)
└── inventory/
    ├── managed.ini   # Managed hosts
    └── onprem.ini    # On-prem hosts
```

**Variable Precedence**: `onprem.yml` > `managed.yml` > `all.yml`

## Verify Installation

### Managed
```bash
ssh andrew@192.168.86.101 'systemctl status bytefreezer-proxy'
curl http://192.168.86.101:8088/health
```

### On-Prem
```bash
ssh andrew@192.168.86.104 'systemctl status bytefreezer-proxy'
curl http://192.168.86.104:8088/health
```

## Configuration Differences

| Setting | Managed | On-Prem |
|---------|---------|---------|
| Proxy Host | tp1 (.101) | tp4 (.104) |
| Receiver URL | tp2:8081 | tp5:8081 |
| Control URL | tp3:8082 | tp3:8082 (shared) |
| Account ID | ejq73vgnw26p | abc123xyz456 |
| Deployment Type | managed | on_prem |

## Troubleshooting

### Check proxy is connecting to correct receiver
```bash
# Managed
ssh andrew@192.168.86.101 'grep receiver /etc/bytefreezer-proxy/config.yaml'

# On-prem
ssh andrew@192.168.86.104 'grep receiver /etc/bytefreezer-proxy/config.yaml'
```

### Check account filtering
```bash
# Managed proxy should only process managed accounts
ssh andrew@192.168.86.101 'journalctl -u bytefreezer-proxy | grep -i account'

# On-prem proxy should only process its specific account
ssh andrew@192.168.86.104 'journalctl -u bytefreezer-proxy | grep -i account'
```
