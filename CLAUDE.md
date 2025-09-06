# Claude Code Session Notes

## Always Test Before Completing Tasks

```bash
# 1. Format Go code
go fmt ./...

# 2. Run Go vet
go vet ./...

# 3. Build to verify compilation
go build .

# 4. Test Ansible syntax (if modified)
cd ansible/playbooks && ansible-playbook --syntax-check *.yml
```

## Current Project Status

### Structure
- **Go service**: UDP proxy with batching and compression
- **Ansible deployment**: `install.yml` and `remove.yml` playbooks
- **Templates**: `templates/` directory with 3 template files
- **Variables**: Single source in `group_vars/all.yml`

### Recent Work
- Simplified Ansible playbooks (removed docker variants)
- Moved templates to separate directory with proper references
- Removed duplicate variables and become directives
- Fixed permissions using playbook-level `become: yes`

### Key Commands
- **Deploy service**: `ansible-playbook install.yml`
- **Remove service**: `ansible-playbook remove.yml`
- **Check health**: `curl http://server:8088/health`
- **View logs**: `/var/log/bytefreezer-proxy/*.log`