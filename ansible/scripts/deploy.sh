#!/bin/bash
# ByteFreezer Proxy Deployment Script
# Usage: ./deploy.sh [environment] [additional_args...]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ANSIBLE_DIR="$(dirname "$SCRIPT_DIR")"
PROJECT_ROOT="$(dirname "$ANSIBLE_DIR")"

# Default values
ENVIRONMENT="${1:-production}"
INVENTORY_FILE="$ANSIBLE_DIR/inventories/hosts.yml"
PLAYBOOK="$ANSIBLE_DIR/playbooks/deploy.yml"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to show usage
show_usage() {
    cat << EOF
ByteFreezer Proxy Deployment Script

Usage: $0 [environment] [ansible-playbook-options...]

Arguments:
  environment    Target environment (default: production)
                 This should match your inventory group name

Examples:
  $0                                    # Deploy to production environment
  $0 staging                           # Deploy to staging environment  
  $0 production --check                # Dry run deployment
  $0 production --limit proxy-01       # Deploy to specific host
  $0 production --tags config          # Only update configuration
  $0 production -e bytefreezer_proxy_version=v1.2.3  # Deploy specific version

Available playbooks:
  deploy.yml        - Full deployment (default)
  update-config.yml - Update configuration only
  manage-service.yml - Manage service (start/stop/restart/status)
  uninstall.yml     - Uninstall ByteFreezer Proxy

Environment Setup:
  1. Edit $ANSIBLE_DIR/inventories/hosts.yml
  2. Configure group_vars/host_vars as needed
  3. Ensure SSH access to target hosts
  4. Run deployment

EOF
}

# Check for help flags
if [[ "$1" == "-h" || "$1" == "--help" ]]; then
    show_usage
    exit 0
fi

# Validation
if [[ ! -f "$INVENTORY_FILE" ]]; then
    print_error "Inventory file not found: $INVENTORY_FILE"
    print_warning "Please create the inventory file first:"
    print_warning "  cp $ANSIBLE_DIR/inventories/hosts.yml.example $INVENTORY_FILE"
    print_warning "  # Edit the file with your server details"
    exit 1
fi

if [[ ! -f "$PLAYBOOK" ]]; then
    print_error "Playbook not found: $PLAYBOOK"
    exit 1
fi

# Check if ansible-playbook is available
if ! command -v ansible-playbook &> /dev/null; then
    print_error "ansible-playbook command not found. Please install Ansible:"
    print_warning "  # Ubuntu/Debian:"
    print_warning "  sudo apt update && sudo apt install ansible"
    print_warning "  # RHEL/CentOS:"
    print_warning "  sudo dnf install ansible"
    print_warning "  # macOS:"
    print_warning "  brew install ansible"
    exit 1
fi

# Shift to get additional arguments
shift

print_status "Starting ByteFreezer Proxy deployment"
print_status "Environment: $ENVIRONMENT"
print_status "Inventory: $INVENTORY_FILE"
print_status "Playbook: $PLAYBOOK"

# Change to ansible directory for relative paths
cd "$ANSIBLE_DIR"

# Build ansible-playbook command
ANSIBLE_CMD=(
    ansible-playbook
    "$PLAYBOOK"
    -i "$INVENTORY_FILE"
    --limit "${ENVIRONMENT}"
)

# Add any additional arguments passed to the script
if [[ $# -gt 0 ]]; then
    ANSIBLE_CMD+=("$@")
    print_status "Additional arguments: $*"
fi

print_status "Executing: ${ANSIBLE_CMD[*]}"
echo

# Execute the playbook
if "${ANSIBLE_CMD[@]}"; then
    print_success "ByteFreezer Proxy deployment completed successfully!"
    echo
    print_status "Next steps:"
    echo "  1. Check service status: ansible-playbook $ANSIBLE_DIR/playbooks/manage-service.yml -i $INVENTORY_FILE --limit $ENVIRONMENT -e action=status"
    echo "  2. View logs: ssh <host> 'journalctl -u bytefreezer-proxy -f'"
    echo "  3. Test health endpoint: curl http://<host>:8088/health"
else
    print_error "Deployment failed! Check the output above for details."
    exit 1
fi