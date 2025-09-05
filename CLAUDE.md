# Claude Code Session Notes

## Pre-commit Checklist 
**RUN THESE EVERY TIME BEFORE COMPLETING TASKS:**

```bash
# 1. Format Go code
go fmt ./...

# 2. Check formatting 
gofmt -s -l .

# 3. Run Go vet
go vet ./...

# 4. Build to verify no compile errors
go build .

# 5. Test Ansible syntax if playbooks were modified
ansible-playbook --syntax-check playbooks/*.yml
```

## Project Structure
- **Go application**: ByteFreezer Proxy (UDP data collection & forwarding)
- **Ansible automation**: Production deployment via playbooks
- **Templates**: Centralized in `ansible/templates/`
- **Variables**: Single source in `ansible/group_vars/all.yml`

## Recent Fixes
- ✅ Fixed spooling file extensions (.ndjson vs .ndjson.gz for compressed)
- ✅ Added file-based logging via systemd StandardOutput/StandardError  
- ✅ Refactored Ansible to use proper templates instead of inline content
- ✅ Consolidated duplicate variables files
- ✅ Fixed Go formatting issues

## Commands to Remember
- **View logs**: `journalctl -u bytefreezer-proxy -f` or check `/var/log/bytefreezer-proxy/`
- **Service status**: `systemctl status bytefreezer-proxy`
- **Deploy**: `ansible-playbook -i inventory.yml playbooks/install.yml`