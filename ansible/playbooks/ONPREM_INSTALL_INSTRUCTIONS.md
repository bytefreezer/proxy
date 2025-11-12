# ByteFreezer Proxy - On-Premises Installation Instructions

## On-Prem Account Credentials

**Account Created**: 2025-11-12
**Deployment Type**: `on_prem`

```
Account ID:  s2jeovtia7zn
Account Name: OnPrem Test Account
Email:       onprem@test.local
API Key:     onprem-92643688d4e31fbc4ca03c8cb87ef6c7
```

## Architecture

### Managed Deployment (Existing)
- **Proxy**: tp1 (192.168.86.101)
- **Receiver**: tp2 (192.168.86.102)
- **Piper**: tp1 (192.168.86.101)
- **Packer**: tp2 (192.168.86.102)
- **Control**: tp3 (192.168.86.103)

### On-Prem Deployment (New)
- **Proxy**: tp4 (192.168.86.104) ← Installing now
- **Receiver**: tp5 (192.168.86.105) ← Next
- **Piper**: tp4 (192.168.86.104) ← Next
- **Packer**: tp5 (192.168.86.105) ← Next
- **Control**: tp3 (192.168.86.103) ← Shared

## Installation Steps

### Step 1: Deploy Proxy to tp4

```bash
cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks

# Deploy using on-prem inventory and variables
ansible-playbook -i inventory/onprem.ini onprem_install.yml
```

**What this does**:
- Copies `bytefreezer-proxy` binary to `/usr/local/bin/`
- Creates config at `/etc/bytefreezer-proxy/config.yaml` with:
  - Account ID: `s2jeovtia7zn`
  - Receiver URL: `http://192.168.86.105:8081/{tenantid}/{datasetid}` (tp5)
  - Control URL: `http://192.168.86.103:8082` (tp3)
- Creates systemd service
- Starts and enables service

### Step 2: Verify Installation

```bash
# Check service status
ssh andrew@192.168.86.104 'sudo systemctl status bytefreezer-proxy'

# Check configuration
ssh andrew@192.168.86.104 'sudo cat /etc/bytefreezer-proxy/config.yaml | grep -A5 account_id'

# Check logs
ssh andrew@192.168.86.104 'sudo journalctl -u bytefreezer-proxy -f'

# Test health endpoint
curl http://192.168.86.104:8088/health
```

### Step 3: Verify Account Filtering

The on-prem proxy should only process its specific account:

```bash
# Check logs for account information
ssh andrew@192.168.86.104 'sudo journalctl -u bytefreezer-proxy | grep -i s2jeovtia7zn'

# Should see logs about processing account s2jeovtia7zn
# Should NOT see logs about other accounts
```

## Configuration Details

### On-Prem Config (tp4)
```yaml
account_id: "s2jeovtia7zn"
bearer_token: "onprem-92643688d4e31fbc4ca03c8cb87ef6c7"
control_url: "http://192.168.86.103:8082"
receiver:
  base_url: "http://192.168.86.105:8081/{tenantid}/{datasetid}"
```

### Managed Config (tp1)
```yaml
account_id: "ejq73vgnw26p"
bearer_token: "-6O9pIT7fVrIr715CIwIUgZuBkY7lB5sRYHx"
control_url: "http://192.168.86.103:8082"
receiver:
  base_url: "http://192.168.86.102:8081/{tenantid}/{datasetid}"
```

## Testing Data Flow

### Send test data to on-prem proxy

```bash
# Send UDP syslog to on-prem proxy on tp4
echo "<134>1 2025-11-12T00:00:00.000Z test-host test-app - - - test message" | \
  nc -u 192.168.86.104 5140
```

**Expected flow**:
1. Proxy on tp4 receives UDP data
2. Proxy batches and sends to receiver on tp5
3. Receiver stores to S3 (on-prem bucket)
4. Piper on tp4 processes from S3
5. Packer on tp5 compresses
6. Control on tp3 manages configuration (no data flows through)

## Next Steps

1. ✅ **Proxy on tp4** - Installing now
2. ⏳ **Receiver on tp5** - Install next
3. ⏳ **Piper on tp4** - Install after receiver
4. ⏳ **Packer on tp5** - Install after piper
5. ⏳ **Create tenant & dataset** in on-prem account
6. ⏳ **End-to-end test** with actual data

## Troubleshooting

### Proxy not starting
```bash
ssh andrew@192.168.86.104 'sudo journalctl -u bytefreezer-proxy -n 50'
```

### Wrong receiver URL
```bash
ssh andrew@192.168.86.104 'sudo grep receiver /etc/bytefreezer-proxy/config.yaml'
# Should show tp5 (.105), not tp2 (.102)
```

### Account filtering not working
```bash
# Check which account the proxy is configured for
ssh andrew@192.168.86.104 'sudo grep account_id /etc/bytefreezer-proxy/config.yaml'
# Should show: s2jeovtia7zn
```

### Verify deployment type in Control
```bash
curl http://192.168.86.103:8082/api/v1/accounts/s2jeovtia7zn \
  -H "Authorization: Bearer bytefreezer-service-api-key-8f4a2d1b-3c5e-4f6a-9b8c-7d2e1f3a4b5c" | jq '.deployment_type'
# Should return: "on_prem"
```


**************
  1. On-Prem Account Created
  Account ID:  s2jeovtia7zn
  API Key:     onprem-92643688d4e31fbc4ca03c8cb87ef6c7
  Type:        on_prem

  2. Proxy Configuration
  - ✅ Inventory files: inventory/managed.ini and inventory/onprem.ini
  - ✅ Group variables: group_vars/managed.yml and group_vars/onprem.yml
  - ✅ Credentials configured in group_vars

  3. AWX Templates
  - ✅ Job templates defined in ansible/awx/job_templates.yml
  - ✅ AWX Quickstart guide for your existing inventory
  - ✅ Works with your existing AWX inventory control

  🚀 Deploy Proxy to tp4 Now

  Option A: Command Line

  cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks
  ansible-playbook -i inventory/onprem.ini onprem_install.yml

  Option B: AWX (Your Preferred Method)

  1. Add group variables in your AWX inventory for the on-prem group:
  bytefreezer_proxy_account_id: "s2jeovtia7zn"
  bytefreezer_proxy_bearer_token: "onprem-92643688d4e31fbc4ca03c8cb87ef6c7"
  bytefreezer_control_url: "http://192.168.86.103:8082"
  bytefreezer_receiver_url: "http://192.168.86.101:8080"
  2. Create job template in AWX:
    - Playbook: ansible/playbooks/onprem_install.yml
    - Limit: tp4 or your on-prem group name
    - See: ansible/awx/QUICKSTART.md for details
  3. Run the job from AWX

  📁 Files Created

  bytefreezer-proxy/ansible/
  ├── playbooks/
  │   ├── inventory/
  │   │   ├── managed.ini  → tp1
  │   │   └── onprem.ini   → tp4
  │   ├── group_vars/
  │   │   ├── all.yml      → Defaults
  │   │   ├── managed.yml  → Managed credentials
  │   │   └── onprem.yml   → On-prem credentials (s2jeovtia7zn)
  │   ├── DEPLOYMENT_GUIDE.md
  │   └── ONPREM_INSTALL_INSTRUCTIONS.md
  ├── awx/
  │   ├── job_templates.yml  → AWX job definitions
  │   ├── QUICKSTART.md      → AWX setup for YOUR inventory
  │   └── README.md          → Full AWX guide
  └── ONPREM_DEPLOYMENT_SUMMARY.md  → Complete overview

  Ready to deploy when you are!

> these instructions are in a file? 

● Yes! All instructions are saved in files. Here's where to find them:

● Bash(find /home/andrew/workspace/bytefreezer/bytefreezer-proxy -name "*.md" -type f | grep -v node_modules | sort)
  ⎿  /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/awx/QUICKSTART.md            
     /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/awx/README.md
     /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks/DEPLOYMENT_GUIDE.md
     … +7 lines (ctrl+o to expand)

● Yes! Here are all the instruction files I created:

  📄 Main Documentation

  1. Complete Overview
  /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ONPREM_DEPLOYMENT_SUMMARY.md
  - ✅ Account credentials
  - ✅ Architecture diagrams
  - ✅ Deployment commands
  - ✅ Next steps

  2. Step-by-Step Install Guide
  /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks/ONPREM_INSTALL_INSTRUCTIONS.md
  - ✅ Account details with credentials
  - ✅ Deployment commands
  - ✅ Verification steps
  - ✅ Testing commands

  3. General Deployment Guide
  /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks/DEPLOYMENT_GUIDE.md
  - ✅ Both managed and on-prem setup
  - ✅ Troubleshooting

  🔧 AWX-Specific Documentation

  4. AWX Quickstart (For YOUR inventory)
  /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/awx/QUICKSTART.md
  - ✅ Variables to add to your AWX inventory
  - ✅ Job template creation
  - ✅ Quick deployment steps

  5. Full AWX Guide
  /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/awx/README.md
  - ✅ Complete AWX setup
  - ✅ API usage
  - ✅ Advanced configuration

  🚀 Quick Command to Deploy

  # View the main summary
  cat /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ONPREM_DEPLOYMENT_SUMMARY.md

  # Deploy proxy to tp4
  cd /home/andrew/workspace/bytefreezer/bytefreezer-proxy/ansible/playbooks
  ansible-playbook -i inventory/onprem.ini onprem_install.yml

  All credentials and instructions are saved in these files!
