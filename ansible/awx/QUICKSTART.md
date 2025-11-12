# AWX Quickstart for ByteFreezer Proxy

## Prerequisites

- AWX already configured with your inventory
- SSH access to tp1, tp4, tp5 configured in AWX
- Git repository synced to AWX

## Step 1: Add Group Variables in AWX

**Important**: The `group_vars/` directory in the repository is for CLI usage only.
For AWX, set variables directly in the AWX UI.

Navigate to: **AWX → Inventories → Your Inventory → Groups → [Group Name] → Variables**

### Complete Variable Reference

See **`AWX_GROUP_VARIABLES.yml`** in this directory for the complete YAML blocks to copy-paste.

That file contains:
- ✅ Full managed group variables
- ✅ Full on-prem group variables
- ✅ All settings with explanations
- ✅ Side-by-side comparison

### Quick Reference

**Managed Group** (tp1):
- Account: `ejq73vgnw26p`
- Receiver: `http://192.168.86.102:8081` (tp2)
- [See AWX_GROUP_VARIABLES.yml for full config]

**On-Prem Group** (tp4, tp5):
- Account: `s2jeovtia7zn`
- Receiver: `http://192.168.86.105:8081` (tp5)
- [See AWX_GROUP_VARIABLES.yml for full config]

---

### How Ansible Uses These Variables

The playbooks use Jinja2 templates that reference these variables:

```jinja2
# templates/config.yaml.j2
account_id: "{{ config.account_id }}"
bearer_token: "{{ config.bearer_token }}"
control_url: "{{ config.control_url }}"
receiver:
  base_url: "{{ config.receiver.base_url }}"
```

Ansible automatically:
1. Reads your AWX inventory
2. Sees which group the host belongs to (managed or onprem)
3. Loads the group variables from AWX
4. Renders templates with those variables
5. Deploys the configuration

## Step 2: Create Job Templates in AWX UI

### Template 1: Deploy to Managed (tp1)

**Name**: ByteFreezer Proxy - Deploy Managed (tp1)
**Job Type**: Run
**Inventory**: (Your existing inventory)
**Project**: ByteFreezer Proxy
**Playbook**: `ansible/playbooks/managed_install.yml`
**Credentials**: (Your SSH credential)
**Limit**: `tp1` or `managed` (your group name)
**Options**:
- ✅ Prompt on launch (Limit)
- ✅ Prompt on launch (Extra Variables)

**Uses**: Group variables from your "managed" group in AWX

---

### Template 2: Deploy to On-Prem (tp4)

**Name**: ByteFreezer Proxy - Deploy On-Prem (tp4)
**Job Type**: Run
**Inventory**: (Your existing inventory)
**Project**: ByteFreezer Proxy
**Playbook**: `ansible/playbooks/onprem_install.yml`
**Credentials**: (Your SSH credential)
**Limit**: `tp4` or `onprem` (your group name)
**Options**:
- ✅ Prompt on launch (Limit)
- ✅ Prompt on launch (Extra Variables)

**Uses**: Group variables from your "onprem" group in AWX

## Step 3: Run the Job

1. Go to **Templates** in AWX
2. Click 🚀 next to "ByteFreezer Proxy - Deploy On-Prem (tp4)"
3. Verify the limit is set to `tp4` or `onprem`
4. Launch

## Verify Deployment

```bash
# SSH to tp4 and check
ssh andrew@192.168.86.104

# Check service status
sudo systemctl status bytefreezer-proxy

# Check configuration
sudo cat /etc/bytefreezer-proxy/config.yaml | grep account_id

# Should show: s2jeovtia7zn (on-prem account)
```

## Next: Deploy Remaining Components

Use the same pattern for:
- **Receiver on tp5**: Copy proxy playbook structure
- **Piper on tp4**: Copy proxy playbook structure + add account_id filtering
- **Packer on tp5**: Copy proxy playbook structure

## Troubleshooting

### Wrong variables loaded
- Check which group the host belongs to in your AWX inventory
- Verify group variables are set correctly
- group_vars/onprem.yml should override group_vars/all.yml

### Job fails with permission denied
- Verify SSH credentials in AWX
- Check become/sudo is enabled
- Test manually: `ansible -i your_inventory tp4 -m ping`
