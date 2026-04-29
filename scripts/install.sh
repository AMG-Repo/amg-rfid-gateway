#!/bin/bash
#
# AMG RFID Gateway Installation Script
# Installs the gateway binary, creates systemd service, and sets up directories
#
# Usage: sudo ./install.sh [--version VERSION] [--binary PATH]
#

set -euo pipefail

# Configuration
INSTALL_DIR="/opt/amg-rfid-gateway"
CONFIG_DIR="${INSTALL_DIR}"
DATA_DIR="${INSTALL_DIR}/data"
BACKUP_DIR="${INSTALL_DIR}/backup"
SERVICE_NAME="amg-rfid-gateway"
SERVICE_USER="gateway"
SERVICE_GROUP="gateway"
DEFAULT_VERSION="latest"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# Display usage
usage() {
    cat << EOF
Usage: sudo $(basename "$0") [OPTIONS]

Install AMG RFID Gateway on Raspberry Pi or ARM64 system.

OPTIONS:
    -v, --version VERSION    Specific version to install (default: latest)
    -b, --binary PATH        Local binary path (skip download)
    -h, --help              Show this help message

EXAMPLES:
    sudo ./install.sh                           # Install latest version
    sudo ./install.sh --version v1.2.0          # Install specific version
    sudo ./install.sh --binary ./gateway-arm64  # Install local binary

EOF
}

# Parse arguments
parse_args() {
    VERSION="${DEFAULT_VERSION}"
    LOCAL_BINARY=""
    
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -v|--version)
                VERSION="$2"
                shift 2
                ;;
            -b|--binary)
                LOCAL_BINARY="$2"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Check if running as root
check_root() {
    # NEGATIVE: Not running as root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

# Check system requirements
check_requirements() {
    log_info "Checking system requirements..."
    
    # NEGATIVE: systemd not available
    if ! command -v systemctl &> /dev/null; then
        log_error "systemctl not found. This script requires systemd."
        exit 1
    fi
    
    # NEGATIVE: architecture not supported
    local arch
    arch=$(uname -m)
    if [[ "$arch" != "aarch64" && "$arch" != "arm64" && "$arch" != "armv7l" ]]; then
        log_warn "Architecture '$arch' is not ARM64/ARM. Binary may not work."
    fi
    
    # HAPPY PATH: Requirements met
    log_info "System requirements satisfied"
}

# Create service user and group
create_user() {
    log_info "Creating service user..."
    
    # NEGATIVE: Group already exists (ok to continue)
    if ! getent group "$SERVICE_GROUP" > /dev/null 2>&1; then
        groupadd --system "$SERVICE_GROUP"
        log_info "Created group: $SERVICE_GROUP"
    else
        log_info "Group $SERVICE_GROUP already exists"
    fi
    
    # NEGATIVE: User already exists (ok to continue)
    if ! id -u "$SERVICE_USER" > /dev/null 2>&1; then
        useradd --system --gid "$SERVICE_GROUP" --home-dir "$INSTALL_DIR" \
                --shell /usr/sbin/nologin "$SERVICE_USER"
        log_info "Created user: $SERVICE_USER"
    else
        log_info "User $SERVICE_USER already exists"
    fi
}

# Create directory structure
create_directories() {
    log_info "Creating directory structure..."
    
    # Create directories with proper permissions
    mkdir -p "$INSTALL_DIR" "$DATA_DIR" "$BACKUP_DIR"
    
    # Set ownership
    chown -R "${SERVICE_USER}:${SERVICE_GROUP}" "$INSTALL_DIR"
    
    # Set permissions
    chmod 755 "$INSTALL_DIR"
    chmod 750 "$DATA_DIR" "$BACKUP_DIR"
    
    log_info "Directories created at $INSTALL_DIR"
}

# Download binary from GitHub releases
download_binary() {
    local version="$1"
    local output_path="$2"
    
    log_info "Downloading gateway binary (version: $version)..."
    
    # Determine architecture
    local arch_suffix
    local arch=$(uname -m)
    case "$arch" in
        aarch64|arm64)
            arch_suffix="arm64"
            ;;
        armv7l)
            arch_suffix="arm"
            ;;
        *)
            arch_suffix="amd64"
            ;;
    esac
    
    # Build download URL
    local download_url
    if [[ "$version" == "latest" ]]; then
        download_url="https://github.com/amg-rfid/amg-rfid-gateway/releases/latest/download/gateway-${arch_suffix}"
    else
        download_url="https://github.com/amg-rfid/amg-rfid-gateway/releases/download/${version}/gateway-${arch_suffix}"
    fi
    
    # Download with retry
    local max_retries=3
    local retry_count=0
    
    while [[ $retry_count -lt $max_retries ]]; do
        if curl -fsSL --retry 3 "$download_url" -o "$output_path"; then
            chmod +x "$output_path"
            log_info "Binary downloaded successfully"
            return 0
        fi
        
        retry_count=$((retry_count + 1))
        log_warn "Download failed, retrying ($retry_count/$max_retries)..."
        sleep 2
    done
    
    # NEGATIVE: All retries failed
    log_error "Failed to download binary after $max_retries attempts"
    return 1
}

# Install binary
install_binary() {
    local binary_path="$1"
    local install_path="${INSTALL_DIR}/gateway"
    
    log_info "Installing binary..."
    
    # NEGATIVE: Binary doesn't exist
    if [[ ! -f "$binary_path" ]]; then
        log_error "Binary not found: $binary_path"
        exit 1
    fi
    
    # NEGATIVE: Binary is not executable
    if [[ ! -x "$binary_path" ]]; then
        chmod +x "$binary_path"
    fi
    
    # Backup existing binary if present
    if [[ -f "$install_path" ]]; then
        local backup_name="gateway-backup-$(date +%Y%m%d-%H%M%S)"
        cp "$install_path" "${BACKUP_DIR}/${backup_name}"
        log_info "Existing binary backed up to ${BACKUP_DIR}/${backup_name}"
    fi
    
    # Copy new binary
    cp "$binary_path" "$install_path"
    chown "${SERVICE_USER}:${SERVICE_GROUP}" "$install_path"
    chmod 755 "$install_path"
    
    log_info "Binary installed to $install_path"
}

# Create default configuration
create_config() {
    local config_path="${CONFIG_DIR}/config.yaml"
    
    # NEGATIVE: Config already exists (preserve user config)
    if [[ -f "$config_path" ]]; then
        log_info "Configuration file already exists, preserving existing config"
        return 0
    fi
    
    log_info "Creating default configuration..."
    
    # Generate gateway ID from hostname or random
    local gateway_id
    gateway_id=$(hostname 2>/dev/null || echo "gateway-$(tr -dc 'a-z0-9' < /dev/urandom | head -c 8)")
    
    cat > "$config_path" << EOF
# AMG RFID Gateway Configuration
# Generated on $(date)

gateway_id: "${gateway_id}"
company_id: "your-company-id"
cloud_url: "wss://api.your-domain.com/ws"
jwt_secret: ""

# Sync configuration
sync_interval: 30s
batch_size: 100
max_retries: 5

# Health check endpoint
health_port: 8080

# Data storage path
data_path: "${DATA_DIR}"

# Antenna configurations
antennas:
  - id: "ANT-001"
    ip: "192.168.1.100"
    port: 49153
    enabled: false
  
  - id: "ANT-002"
    ip: "192.168.1.101"
    port: 49153
    enabled: false

# Logging
log_level: "info"
EOF

    chown "${SERVICE_USER}:${SERVICE_GROUP}" "$config_path"
    chmod 640 "$config_path"
    
    log_info "Configuration created at $config_path"
    log_warn "IMPORTANT: Edit $config_path with your actual settings before starting the service"
}

# Install systemd service
install_systemd_service() {
    log_info "Installing systemd service..."
    
    local service_path="/etc/systemd/system/${SERVICE_NAME}.service"
    
    # Check if service file already exists
    if [[ -f "$service_path" ]]; then
        log_info "Service file already exists, updating..."
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    fi
    
    # Copy service file
    cp "${INSTALL_DIR}/scripts/systemd/gateway.service" "$service_path"
    
    # Reload systemd
    systemctl daemon-reload
    
    # Enable service (start on boot)
    systemctl enable "$SERVICE_NAME"
    
    log_info "Systemd service installed and enabled"
}

# Verify installation
verify_installation() {
    log_info "Verifying installation..."
    
    local errors=0
    
    # Check binary exists and is executable
    if [[ ! -x "${INSTALL_DIR}/gateway" ]]; then
        log_error "Binary not found or not executable"
        errors=$((errors + 1))
    fi
    
    # Check config exists
    if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
        log_error "Configuration file not found"
        errors=$((errors + 1))
    fi
    
    # Check directories
    if [[ ! -d "$DATA_DIR" ]]; then
        log_error "Data directory not found"
        errors=$((errors + 1))
    fi
    
    # Check service file
    if [[ ! -f "/etc/systemd/system/${SERVICE_NAME}.service" ]]; then
        log_error "Systemd service file not found"
        errors=$((errors + 1))
    fi
    
    # NEGATIVE: Verification failed
    if [[ $errors -gt 0 ]]; then
        log_error "Installation verification failed with $errors errors"
        exit 1
    fi
    
    # HAPPY PATH: All checks passed
    log_info "Installation verified successfully"
}

# Print post-installation instructions
print_instructions() {
    cat << EOF

========================================
  AMG RFID Gateway Installation Complete
========================================

Installation directory: ${INSTALL_DIR}
Configuration file:     ${CONFIG_DIR}/config.yaml
Data directory:         ${DATA_DIR}
Service name:           ${SERVICE_NAME}

NEXT STEPS:
-----------
1. Edit the configuration file:
   sudo nano ${CONFIG_DIR}/config.yaml

2. Set your company_id, cloud_url, and antenna configurations

3. Start the service:
   sudo systemctl start ${SERVICE_NAME}

4. Check service status:
   sudo systemctl status ${SERVICE_NAME}

5. View logs:
   sudo journalctl -u ${SERVICE_NAME} -f

6. Health check:
   curl http://localhost:8080/health

MANAGEMENT COMMANDS:
--------------------
- Start:   sudo systemctl start ${SERVICE_NAME}
- Stop:    sudo systemctl stop ${SERVICE_NAME}
- Restart: sudo systemctl restart ${SERVICE_NAME}
- Status:  sudo systemctl status ${SERVICE_NAME}
- Logs:    sudo journalctl -u ${SERVICE_NAME} -f

========================================

EOF
}

# Cleanup on error
cleanup() {
    local exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        log_error "Installation failed with exit code $exit_code"
        log_info "You may need to manually clean up: ${INSTALL_DIR}"
    fi
}

trap cleanup EXIT

# Main installation flow
main() {
    log_info "Starting AMG RFID Gateway installation..."
    
    # Parse command line arguments
    parse_args "$@"
    
    # NEGATIVE: Not root
    check_root
    
    # NEGATIVE: Missing requirements
    check_requirements
    
    # Setup
    create_user
    create_directories
    
    # Install binary
    if [[ -n "$LOCAL_BINARY" ]]; then
        # Use local binary
        install_binary "$LOCAL_BINARY"
    else
        # Download from GitHub
        local temp_binary="/tmp/gateway-download-$$"
        download_binary "$VERSION" "$temp_binary"
        install_binary "$temp_binary"
        rm -f "$temp_binary"
    fi
    
    # Copy scripts directory if installing from local
    if [[ -n "$LOCAL_BINARY" ]]; then
        local script_source
        script_source=$(dirname "$LOCAL_BINARY")/scripts
        if [[ -d "$script_source" ]]; then
            cp -r "$script_source"/* "${INSTALL_DIR}/scripts/" 2>/dev/null || true
        fi
    fi
    
    # Setup configuration
    create_config
    
    # Install systemd service
    install_systemd_service
    
    # Verify
    verify_installation
    
    # Success
    log_info "Installation completed successfully!"
    print_instructions
}

# Run main
main "$@"
