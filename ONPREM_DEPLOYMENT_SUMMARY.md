# ByteFreezer On-Premises Deployment Summary

## ✅ What's Been Completed

### 1. Database Schema
- Added `deployment_type` field to `control_accounts` table
- Migration `009_add_account_deployment_type.sql` applied
- Supports: `managed`, `on_prem`, `air_gapped`

### 2. On-Prem Account Created
```
Account ID:  s2jeovtia7zn
Name:        OnPrem Test Account
Email:       onprem@test.local
Type:        on_prem
API Key:     onprem-92643688d4e31fbc4ca03c8cb87ef6c7
Created:     2025-11-12
```

### 3. Control API Updated
- CreateAccount accepts `deployment_type` parameter
- UpdateAccount allows changing deployment type
- Validation ensures valid types only

### 4. Piper Discovery Filtering
- Managed pipers: Only process `deployment_type='managed'` accounts
- On-prem pipers: Only process configured `account_id`
- Configuration option: `control_service.account_id`

### 5. Ansible Playbooks & AWX Templates
- Inventory files for managed and on-prem
- Group variables with correct credentials
- AWX job templates ready to import
- Deployment guide with instructions

## 🎯 Ready to Deploy: Proxy on tp4

### Quick Start

```bash
cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks

# Deploy proxy to tp4 (on-prem)
ansible-playbook -i inventory/onprem.ini onprem_install.yml

# Verify installation
ssh andrew@192.168.86.104 'sudo systemctl status bytefreezer-proxy'
curl http://192.168.86.104:8088/health
```

## 📋 Architecture

### Managed Environment (tp1, tp2, tp3)
```
┌─────────────────────────────────────────────────────────┐
│  Managed Account: ejq73vgnw26p                          │
│  Deployment Type: managed                               │
└─────────────────────────────────────────────────────────┘

  Proxy (tp1:101) → Receiver (tp2:102) → S3 (managed)
                                    ↓
                    Control (tp3:103) ← Piper (tp1:101)
                         ↓                   ↓
                    Packer (tp2:102) ← S3 (managed)
```

### On-Prem Environment (tp4, tp5 + shared tp3)
```
┌─────────────────────────────────────────────────────────┐
│  On-Prem Account: s2jeovtia7zn                          │
│  Deployment Type: on_prem                               │
└─────────────────────────────────────────────────────────┘

  Proxy (tp4:104) → Receiver (tp5:105) → S3 (on-prem)
                                    ↓
                Control (tp3:103) ← Piper (tp4:104)
                  SHARED              ↓
                                Packer (tp5:105) ← S3 (on-prem)
```

## 🔐 Credentials Reference

### Managed Account
```yaml
Account ID:  ejq73vgnw26p
API Key:     -6O9pIT7fVrIr715CIwIUgZuBkY7lB5sRYHx
Control URL: http://192.168.86.103:8082
Receiver URL: http://192.168.86.102:8081
Hosts:       tp1 (.101), tp2 (.102), tp3 (.103)
```

### On-Prem Account
```yaml
Account ID:  s2jeovtia7zn
API Key:     onprem-92643688d4e31fbc4ca03c8cb87ef6c7
Control URL: http://192.168.86.103:8082  # Shared
Receiver URL: http://192.168.86.105:8081  # On-prem tp5
Hosts:       tp4 (.104), tp5 (.105), tp3 (.103 shared)
```

## 📝 Next Steps

### 1. Deploy Proxy to tp4 ← YOU ARE HERE

```bash
cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks
ansible-playbook -i inventory/onprem.ini onprem_install.yml
```

### 2. Create On-Prem Receiver Playbooks (Next)

Copy the same structure to bytefreezer-receiver:
- `inventory/onprem.ini` → tp5 (.105)
- `group_vars/onprem.yml` → S3 endpoints, account config
- Deploy receiver to tp5

### 3. Create On-Prem Piper Playbooks

Copy structure to bytefreezer-piper:
- `inventory/onprem.ini` → tp4 (.104)
- `group_vars/onprem.yml` → Add `account_id: s2jeovtia7zn`
- This configures piper to ONLY process on-prem account
- Deploy piper to tp4

### 4. Create On-Prem Packer Playbooks

Copy structure to bytefreezer-packer:
- `inventory/onprem.ini` → tp5 (.105)
- `group_vars/onprem.yml` → S3 endpoints, account config
- Deploy packer to tp5

### 5. Create Test Tenant & Dataset

```bash
# Create tenant in on-prem account
curl -X POST http://192.168.86.103:8082/api/v1/accounts/s2jeovtia7zn/tenants \
  -H "Authorization: Bearer bytefreezer-service-api-key-8f4a2d1b-3c5e-4f6a-9b8c-7d2e1f3a4b5c" \
  -H "Content-Type: application/json" \
  -d '{"name":"onprem-tenant-1","display_name":"On-Prem Test Tenant"}'

# Create dataset
curl -X POST http://192.168.86.103:8082/api/v1/tenants/TENANT_ID/datasets \
  -H "Authorization: Bearer bytefreezer-service-api-key-8f4a2d1b-3c5e-4f6a-9b8c-7d2e1f3a4b5c" \
  -H "Content-Type: application/json" \
  -d '{"name":"test-dataset","display_name":"Test Dataset"}'
```

### 6. End-to-End Test

```bash
# Send test data to on-prem proxy
echo "<134>1 2025-11-12T00:00:00.000Z test-host test-app - - - test message" | \
  nc -u 192.168.86.104 5140

# Watch the flow
ssh andrew@192.168.86.104 'sudo journalctl -u bytefreezer-proxy -f'
ssh andrew@192.168.86.105 'sudo journalctl -u bytefreezer-receiver -f'
ssh andrew@192.168.86.104 'sudo journalctl -u bytefreezer-piper -f'
```

## 🔍 Verification Commands

### Check Account Details
```bash
curl http://192.168.86.103:8082/api/v1/accounts/s2jeovtia7zn \
  -H "Authorization: Bearer bytefreezer-service-api-key-8f4a2d1b-3c5e-4f6a-9b8c-7d2e1f3a4b5c" \
  | jq '{id, name, deployment_type, active}'
```

### Check Proxy Configuration
```bash
ssh andrew@192.168.86.104 'sudo cat /etc/bytefreezer-proxy/config.yaml | grep -A5 account_id'
```

### Check Service Health
```bash
curl http://192.168.86.104:8088/health
```

## 📁 Files Created

```
bytefreezer-proxy/
├── ansible/
│   ├── playbooks/
│   │   ├── inventory/
│   │   │   ├── managed.ini       # tp1 (.101)
│   │   │   └── onprem.ini        # tp4 (.104)
│   │   ├── group_vars/
│   │   │   ├── all.yml           # Defaults
│   │   │   ├── managed.yml       # Managed config
│   │   │   └── onprem.yml        # On-prem config with credentials
│   │   ├── DEPLOYMENT_GUIDE.md
│   │   └── ONPREM_INSTALL_INSTRUCTIONS.md
│   └── awx/
│       ├── job_templates.yml     # AWX job templates
│       ├── inventories.yml       # AWX inventories
│       └── README.md             # AWX setup guide
└── ONPREM_DEPLOYMENT_SUMMARY.md  # This file

bytefreezer-control/
├── migrations/
│   └── 009_add_account_deployment_type.sql  # Applied ✅
└── storage/
    └── interface.go              # Updated with DeploymentType

bytefreezer-piper/
├── config/
│   └── config.go                 # Added account_id config
└── services/
    └── simple_discovery_manager.go  # Deployment filtering
```

## ⚠️ Important Notes

1. **Control plane is shared** between managed and on-prem
2. **Data never flows** through control - only configuration
3. **Piper filtering** ensures account isolation
4. **S3 buckets** should be separate for on-prem
5. **Credentials** are different per deployment type

## 🐛 Troubleshooting

### Proxy connects to wrong receiver
```bash
ssh andrew@192.168.86.104 'sudo grep receiver /etc/bytefreezer-proxy/config.yaml'
# Should show tp5 (.105), not tp2 (.102)
```

### Piper processes wrong accounts
```bash
# On-prem piper should only log s2jeovtia7zn
ssh andrew@192.168.86.104 'sudo journalctl -u bytefreezer-piper | grep -i account'
```

### Check deployment type
```bash
PGPASSWORD="bytefreezer123" psql -h 192.168.86.137 -p 5432 -U bytefreezer -d bytefreezer \
  -c "SELECT id, name, deployment_type FROM control_accounts;"
```
