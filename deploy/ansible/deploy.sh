#!/bin/bash
# Ansible Deployment Wrapper for Go Email Service
# Wraps all ansible-playbook operations with a simple CLI

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INVENTORY="${SCRIPT_DIR}/inventories/production.ini"
ANSIBLE_CONFIG="${SCRIPT_DIR}/ansible.cfg"

# Export Ansible config location
export ANSIBLE_CONFIG

# Helper functions
print_header() {
    echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║   $1${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}→ $1${NC}"
}

# Check prerequisites
check_prerequisites() {
    local missing=0

    # Check Ansible
    if ! command -v ansible &> /dev/null; then
        print_error "Ansible is not installed"
        echo "Install: brew install ansible (macOS) or sudo apt install ansible (Ubuntu)"
        missing=1
    fi

    # Check ansible-playbook
    if ! command -v ansible-playbook &> /dev/null; then
        print_error "ansible-playbook is not installed"
        missing=1
    fi

    # Check collections
    if ! ansible-galaxy collection list 2>/dev/null | grep -q "community.docker"; then
        print_warning "community.docker collection not installed"
        echo "Install: ansible-galaxy collection install community.docker"
        missing=1
    fi

    if ! ansible-galaxy collection list 2>/dev/null | grep -q "community.crypto"; then
        print_warning "community.crypto collection not installed"
        echo "Install: ansible-galaxy collection install community.crypto"
        missing=1
    fi

    if [ $missing -eq 1 ]; then
        return 1
    fi

    return 0
}

# Test connection to server
test_connection() {
    print_info "Testing connection to server..."
    if ansible -i "$INVENTORY" emailservers -m ping &> /dev/null; then
        print_success "Connection successful"
        return 0
    else
        print_error "Cannot connect to server"
        echo "Make sure you have SSH access: ssh root@apps.afterdarksys.com"
        return 1
    fi
}

# Install Ansible collections
install_collections() {
    print_header "Installing Ansible Collections"

    print_info "Installing community.docker..."
    ansible-galaxy collection install community.docker

    print_info "Installing community.crypto..."
    ansible-galaxy collection install community.crypto

    print_success "Collections installed"
}

# Run Ansible playbook with common options
run_playbook() {
    local playbook=$1
    shift
    local extra_args=("$@")

    local cmd=(ansible-playbook -i "$INVENTORY")

    # Add vault password file if exists
    if [ -f "${SCRIPT_DIR}/.vault_pass" ]; then
        cmd+=(--vault-password-file="${SCRIPT_DIR}/.vault_pass")
    elif [ "$USE_VAULT" = "yes" ]; then
        cmd+=(--ask-vault-pass)
    fi

    # Add verbosity
    if [ "$VERBOSE" = "yes" ]; then
        cmd+=(-vvv)
    elif [ "$VERBOSE" = "v" ]; then
        cmd+=(-v)
    fi

    # Add check mode
    if [ "$CHECK_MODE" = "yes" ]; then
        cmd+=(--check)
    fi

    # Add diff mode
    if [ "$DIFF_MODE" = "yes" ]; then
        cmd+=(--diff)
    fi

    # Add tags
    if [ -n "$TAGS" ]; then
        cmd+=(--tags "$TAGS")
    fi

    # Add skip tags
    if [ -n "$SKIP_TAGS" ]; then
        cmd+=(--skip-tags "$SKIP_TAGS")
    fi

    # Add extra vars
    if [ -n "$EXTRA_VARS" ]; then
        cmd+=(--extra-vars "$EXTRA_VARS")
    fi

    # Add playbook
    cmd+=("${SCRIPT_DIR}/${playbook}")

    # Add any additional arguments
    cmd+=("${extra_args[@]}")

    # Print command if verbose
    if [ "$VERBOSE" = "yes" ] || [ "$VERBOSE" = "v" ]; then
        echo -e "${CYAN}Running: ${cmd[*]}${NC}"
        echo ""
    fi

    # Run command
    "${cmd[@]}"
}

# Deploy command
cmd_deploy() {
    print_header "Deploying Email Service to apps.afterdarksys.com"

    # Check prerequisites
    if ! check_prerequisites; then
        print_error "Prerequisites check failed"
        exit 1
    fi

    # Test connection
    if ! test_connection; then
        exit 1
    fi

    # Show configuration
    print_info "Current Configuration:"
    grep -E "^email_domain:|^admin_password:|^letsencrypt_enabled:" "${SCRIPT_DIR}/group_vars/emailservers.yml" | sed 's/^/  /'
    echo ""

    # Confirm
    if [ "$FORCE" != "yes" ]; then
        read -p "Deploy to production? (yes/no): " confirm
        if [ "$confirm" != "yes" ]; then
            print_warning "Deployment cancelled"
            exit 0
        fi
    fi

    # Run deployment
    run_playbook "deploy.yml" "$@"

    print_success "Deployment complete!"
    echo ""
    print_info "Next steps:"
    echo "  1. Configure DNS records (see README.md)"
    echo "  2. Access Grafana: http://apps.afterdarksys.com:3000"
    echo "  3. Check health: curl http://apps.afterdarksys.com:8080/health"
}

# Update command
cmd_update() {
    print_header "Updating Email Service"

    if [ "$FORCE" != "yes" ]; then
        read -p "Update production server? (yes/no): " confirm
        if [ "$confirm" != "yes" ]; then
            print_warning "Update cancelled"
            exit 0
        fi
    fi

    run_playbook "update.yml" "$@"

    print_success "Update complete!"
}

# Backup command
cmd_backup() {
    print_header "Creating Backup"

    local download=""
    if [ "$DOWNLOAD_BACKUP" = "yes" ]; then
        download="-e download_backup=true"
    fi

    run_playbook "backup.yml" $download "$@"

    print_success "Backup complete!"

    if [ "$DOWNLOAD_BACKUP" = "yes" ]; then
        echo ""
        print_info "Backup downloaded to: ${SCRIPT_DIR}/backups/"
        ls -lh "${SCRIPT_DIR}/backups/" 2>/dev/null || true
    fi
}

# Restore command
cmd_restore() {
    print_header "Restoring from Backup"

    if [ -z "$BACKUP_FILE" ]; then
        print_error "No backup file specified"
        echo "Usage: $0 restore --backup-file /path/to/backup.tar.gz"
        exit 1
    fi

    print_warning "This will overwrite current data!"
    if [ "$FORCE" != "yes" ]; then
        read -p "Continue with restore? (yes/no): " confirm
        if [ "$confirm" != "yes" ]; then
            print_warning "Restore cancelled"
            exit 0
        fi
    fi

    run_playbook "restore.yml" -e "backup_file=$BACKUP_FILE" -e "confirm_restore=yes" "$@"

    print_success "Restore complete!"
}

# Ping command
cmd_ping() {
    print_header "Testing Connection"
    ansible -i "$INVENTORY" emailservers -m ping
}

# Status command
cmd_status() {
    print_header "Service Status"

    print_info "Checking service status..."
    ansible -i "$INVENTORY" emailservers -m shell -a "systemctl status emailservice --no-pager" 2>/dev/null || true

    echo ""
    print_info "Checking health endpoint..."
    ansible -i "$INVENTORY" emailservers -m uri -a "url=http://localhost:8080/health" 2>/dev/null || true

    echo ""
    print_info "Docker containers..."
    ansible -i "$INVENTORY" emailservers -m shell -a "docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'" 2>/dev/null || true
}

# Logs command
cmd_logs() {
    print_header "Viewing Logs"

    local service="mail-primary"
    local lines="50"

    if [ -n "$SERVICE" ]; then
        service="$SERVICE"
    fi

    if [ -n "$TAIL_LINES" ]; then
        lines="$TAIL_LINES"
    fi

    if [ "$FOLLOW" = "yes" ]; then
        print_info "Following logs for $service (Ctrl+C to stop)..."
        ssh root@apps.afterdarksys.com "docker logs -f $service"
    else
        print_info "Last $lines lines from $service..."
        ansible -i "$INVENTORY" emailservers -m shell -a "docker logs --tail $lines $service"
    fi
}

# Shell command
cmd_shell() {
    print_info "Opening SSH connection to apps.afterdarksys.com..."
    ssh root@apps.afterdarksys.com
}

# Setup command (install collections)
cmd_setup() {
    print_header "Setting Up Ansible Environment"

    if ! command -v ansible &> /dev/null; then
        print_error "Ansible is not installed"
        echo ""
        echo "Install Ansible:"
        echo "  macOS:   brew install ansible"
        echo "  Ubuntu:  sudo apt install ansible"
        echo "  pip:     pip3 install ansible"
        exit 1
    fi

    print_success "Ansible is installed: $(ansible --version | head -n1)"

    install_collections

    print_success "Setup complete!"
}

# Vault command
cmd_vault() {
    local vault_file="${SCRIPT_DIR}/group_vars/emailservers_vault.yml"
    local action="$1"

    case "$action" in
        create)
            print_header "Creating Vault File"
            if [ -f "$vault_file" ]; then
                print_error "Vault file already exists: $vault_file"
                exit 1
            fi
            ansible-vault create "$vault_file"
            print_success "Vault file created"
            ;;
        edit)
            print_header "Editing Vault File"
            if [ ! -f "$vault_file" ]; then
                print_error "Vault file does not exist: $vault_file"
                print_info "Create it first: $0 vault create"
                exit 1
            fi
            ansible-vault edit "$vault_file"
            ;;
        view)
            print_header "Viewing Vault File"
            ansible-vault view "$vault_file"
            ;;
        encrypt)
            print_header "Encrypting Vault File"
            ansible-vault encrypt "$vault_file"
            print_success "Vault file encrypted"
            ;;
        decrypt)
            print_header "Decrypting Vault File"
            ansible-vault decrypt "$vault_file"
            print_success "Vault file decrypted"
            ;;
        *)
            echo "Usage: $0 vault {create|edit|view|encrypt|decrypt}"
            echo ""
            echo "Vault commands:"
            echo "  create   - Create new encrypted vault file"
            echo "  edit     - Edit encrypted vault file"
            echo "  view     - View encrypted vault file"
            echo "  encrypt  - Encrypt existing vault file"
            echo "  decrypt  - Decrypt vault file"
            exit 1
            ;;
    esac
}

# Config command
cmd_config() {
    print_header "Configuration"

    echo -e "${CYAN}Inventory:${NC}"
    cat "$INVENTORY"

    echo ""
    echo -e "${CYAN}Variables (group_vars/emailservers.yml):${NC}"
    cat "${SCRIPT_DIR}/group_vars/emailservers.yml"

    if [ -f "${SCRIPT_DIR}/group_vars/emailservers_vault.yml" ]; then
        echo ""
        echo -e "${CYAN}Vault file exists:${NC} group_vars/emailservers_vault.yml"
        echo "(Use '$0 vault view' to see contents)"
    fi
}

# Help command
cmd_help() {
    cat <<EOF
${GREEN}Go Email Service - Ansible Deployment Wrapper${NC}

${CYAN}USAGE:${NC}
    $0 <command> [options]

${CYAN}COMMANDS:${NC}
    ${GREEN}deploy${NC}        Deploy email service to production
    ${GREEN}update${NC}        Update application without full redeploy
    ${GREEN}backup${NC}        Create backup of email service
    ${GREEN}restore${NC}       Restore from backup
    ${GREEN}status${NC}        Show service status
    ${GREEN}logs${NC}          View service logs
    ${GREEN}ping${NC}          Test connection to server
    ${GREEN}shell${NC}         Open SSH shell to server
    ${GREEN}setup${NC}         Install Ansible collections
    ${GREEN}vault${NC}         Manage Ansible vault
    ${GREEN}config${NC}        Show current configuration
    ${GREEN}help${NC}          Show this help message

${CYAN}OPTIONS:${NC}
    ${YELLOW}--force${NC}               Skip confirmation prompts
    ${YELLOW}--vault${NC}               Use Ansible vault (prompt for password)
    ${YELLOW}--verbose, -v${NC}         Verbose output
    ${YELLOW}--vvv${NC}                 Very verbose output
    ${YELLOW}--check${NC}               Dry-run mode (don't make changes)
    ${YELLOW}--diff${NC}                Show differences
    ${YELLOW}--tags <tags>${NC}         Run specific tags
    ${YELLOW}--skip-tags <tags>${NC}    Skip specific tags
    ${YELLOW}--extra-vars <vars>${NC}   Extra variables (key=value)

${CYAN}BACKUP/RESTORE OPTIONS:${NC}
    ${YELLOW}--download${NC}            Download backup to local machine
    ${YELLOW}--backup-file <path>${NC}  Backup file to restore from

${CYAN}LOG OPTIONS:${NC}
    ${YELLOW}--service <name>${NC}      Service to show logs from (default: mail-primary)
    ${YELLOW}--lines <n>${NC}           Number of lines to show (default: 50)
    ${YELLOW}--follow, -f${NC}          Follow logs in real-time

${CYAN}EXAMPLES:${NC}
    # Deploy to production
    $0 deploy

    # Deploy with vault
    $0 deploy --vault

    # Deploy without confirmation
    $0 deploy --force

    # Dry-run deployment
    $0 deploy --check --diff

    # Update application only
    $0 update --tags emailservice

    # Create and download backup
    $0 backup --download

    # Restore from backup
    $0 restore --backup-file /opt/go-emailservice-ads/backup/emailservice_backup_20260309_120000.tar.gz

    # View logs
    $0 logs
    $0 logs --service mail-primary --lines 100
    $0 logs --follow

    # Check status
    $0 status

    # Test connection
    $0 ping

    # Setup Ansible collections
    $0 setup

    # Manage vault
    $0 vault create
    $0 vault edit
    $0 vault view

${CYAN}FILES:${NC}
    Inventory:     inventories/production.ini
    Variables:     group_vars/emailservers.yml
    Vault:         group_vars/emailservers_vault.yml
    Playbooks:     deploy.yml, update.yml, backup.yml, restore.yml

${CYAN}DOCUMENTATION:${NC}
    Full guide:    README.md
    Quick start:   DEPLOYMENT_GUIDE.md

EOF
}

# Main function
main() {
    # Parse global options
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --force)
                FORCE="yes"
                shift
                ;;
            --vault)
                USE_VAULT="yes"
                shift
                ;;
            --verbose|-v)
                VERBOSE="v"
                shift
                ;;
            --vvv)
                VERBOSE="yes"
                shift
                ;;
            --check)
                CHECK_MODE="yes"
                shift
                ;;
            --diff)
                DIFF_MODE="yes"
                shift
                ;;
            --tags)
                TAGS="$2"
                shift 2
                ;;
            --skip-tags)
                SKIP_TAGS="$2"
                shift 2
                ;;
            --extra-vars)
                EXTRA_VARS="$2"
                shift 2
                ;;
            --download)
                DOWNLOAD_BACKUP="yes"
                shift
                ;;
            --backup-file)
                BACKUP_FILE="$2"
                shift 2
                ;;
            --service)
                SERVICE="$2"
                shift 2
                ;;
            --lines)
                TAIL_LINES="$2"
                shift 2
                ;;
            --follow|-f)
                FOLLOW="yes"
                shift
                ;;
            deploy|update|backup|restore|status|logs|ping|shell|setup|vault|config|help)
                COMMAND="$1"
                shift
                break
                ;;
            *)
                print_error "Unknown option: $1"
                echo "Run '$0 help' for usage"
                exit 1
                ;;
        esac
    done

    # Default command
    if [ -z "$COMMAND" ]; then
        COMMAND="help"
    fi

    # Run command
    case "$COMMAND" in
        deploy)
            cmd_deploy "$@"
            ;;
        update)
            cmd_update "$@"
            ;;
        backup)
            cmd_backup "$@"
            ;;
        restore)
            cmd_restore "$@"
            ;;
        status)
            cmd_status "$@"
            ;;
        logs)
            cmd_logs "$@"
            ;;
        ping)
            cmd_ping "$@"
            ;;
        shell)
            cmd_shell "$@"
            ;;
        setup)
            cmd_setup "$@"
            ;;
        vault)
            cmd_vault "$@"
            ;;
        config)
            cmd_config "$@"
            ;;
        help)
            cmd_help
            ;;
        *)
            print_error "Unknown command: $COMMAND"
            echo "Run '$0 help' for usage"
            exit 1
            ;;
    esac
}

# Run main
main "$@"
