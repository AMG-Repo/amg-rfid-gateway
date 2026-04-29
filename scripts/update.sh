#!/bin/bash
#
# AMG RFID Gateway Update Script
# Checks for updates and performs safe update with backup/rollback
#
# Usage: sudo ./update.sh [--version VERSION] [--force]
#

set -euo pipefail

# Configuration
INSTALL_DIR="/opt/amg-rfid-gateway"
CONFIG_DIR="${INSTALL_DIR}"
DATA_DIR="${INSTALL_DIR}/data"
BACKUP_DIR="${INSTALL_DIR}/backup"
BINARY_PATH="${INSTALL_DIR}/gateway"
SERVICE_NAME="amg-rfid-gateway"
HEALTH_URL="http://localhost:8080/health"
HEALTH_TIMEOUT=30
MAX_BACKUPS=5

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_debug() { echo -e "${BLUE}[DEBUG]${NC} $*"; }

# Display usage
usage() {
    cat << EOF
Usage: sudo $(basename "$0") [OPTIONS]

Update AMG RFID Gateway to a new version with automatic backup and rollback.

OPTIONS:
    -v, --version VERSION    Specific version to install (default: check latest)
    -f, --force             Force update even if versions match
    -c, --check-only        Only check for updates, don't install
    -h, --help              Show this help message

EXAMPLES:
    sudo ./update.sh                    # Check and install latest
    sudo ./update.sh --version v1.2.0   # Install specific version
    sudo ./update.sh --check-only       # Only check for updates

EOF
}

# Parse arguments
parse_args() {
    TARGET_VERSION=""
    FORCE_UPDATE=false
    CHECK_ONLY=false
    
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -v|--version)
                TARGET_VERSION="$2"
                shift 2
                ;;
            -f|--force)
                FORCE_UPDATE=true
                shift
                ;;
            -c|--check-only)
                CHECK_ONLY=true
                shift
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

# Check if gateway is installed
check_installed() {
    # NEGATIVE: Binary not found
    if [[ ! -f "$BINARY_PATH" ]]; then
        log_error "Gateway not found at $BINARY_PATH"
        log_info "Run install.sh first: sudo ./install.sh"
        exit 1
    fi
    
    # NEGATIVE: Config not found
    if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
        log_error "Configuration not found at ${CONFIG_DIR}/config.yaml"
        exit 1
    fi
}

# Get current installed version
get_current_version() {
    # NEGATIVE: Cannot get version
    if ! "$BINARY_PATH" --version 2>/dev/null; then
        # Try alternative method
        if [[ -f "${INSTALL_DIR}/.version" ]]; then
            cat "${INSTALL_DIR}/.version"
        else
            echo "unknown"
        fi
    fi
}

# Get latest version from GitHub
get_latest_version() {
    local api_url="https://api.github.com/repos/amg-rfid/amg-rfid-gateway/releases/latest"
    
    # NEGATIVE: Cannot reach GitHub
    if ! curl -fsSL "$api_url" 2>/dev/null | grep -oP '"tag_name":\s*"\K[^"]+'; then
        log_error "Cannot fetch latest version from GitHub"
        return 1
    fi
}

# Compare versions (returns 0 if v1 >= v2)
version_ge() {
    printf '%s\n%s\n' "$1" "$2" | sort -V -C
}

# Check for updates
check_update() {
    log_info "Checking for updates..."
    
    local current_version
    current_version=$(get_current_version)
    log_info "Current version: $current_version"
    
    local latest_version
    if [[ -n "$TARGET_VERSION" ]]; then
        latest_version="$TARGET_VERSION"
    else
        latest_version=$(get_latest_version) || return 1
    fi
    log_info "Latest version: $latest_version"
    
    # NEGATIVE: Already up to date
    if [[ "$current_version" == "$latest_version" ]] && [[ "$FORCE_UPDATE" == false ]]; then
        log_info "Already up to date (use --force to reinstall)"
        return 2
    fi
    
    # NEGATIVE: Current version is newer
    if version_ge "$current_version" "$latest_version" && [[ "$FORCE_UPDATE" == false ]]; then
        log_info "Current version ($current_version) is newer than target ($latest_version)"
        log_info "Use --force to downgrade"
        return 2
    fi
    
    # HAPPY PATH: Update available
    log_info "Update available: $current_version -> $latest_version"
    return 0
}

# Download binary
download_binary() {
    local version="$1"
    local output_path="$2"
    
    log_info "Downloading version $version..."
    
    # Determine architecture
    local arch_suffix
    local arch
    arch=$(uname -m)
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
    
    # Download
    log_debug "Downloading from: $download_url"
    if ! curl -fsSL --retry 3 "$download_url" -o "$output_path"; then
        log_error "Failed to download binary"
        return 1
    fi
    
    chmod +x "$output_path"
    log_info "Download complete"
}

# Download checksum file
download_checksum() {
    local version="$1"
    local output_path="$2"
    
    # Determine architecture
    local arch_suffix
    local arch
    arch=$(uname -m)
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
    
    # Build checksum URL
    local checksum_url
    if [[ "$version" == "latest" ]]; then
        checksum_url="https://github.com/amg-rfid/amg-rfid-gateway/releases/latest/download/checksums.txt"
    else
        checksum_url="https://github.com/amg-rfid/amg-rfid-gateway/releases/download/${version}/checksums.txt"
    fi
    
    # Download checksum
    if ! curl -fsSL --retry 3 "$checksum_url" -o "$output_path" 2>/dev/null; then
        log_warn "Could not download checksum file, skipping verification"
        return 1
    fi
    
    return 0
}

# Verify checksum
verify_checksum() {
    local binary_path="$1"
    local checksum_file="$2"
    
    # NEGATIVE: No checksum file
    if [[ ! -f "$checksum_file" ]]; then
        log_warn "Skipping checksum verification (no checksum file)"
        return 0
    fi
    
    log_info "Verifying checksum..."
    
    # Get expected checksum
    local binary_name
    binary_name=$(basename "$binary_path")
    local expected_checksum
    expected_checksum=$(grep "$binary_name" "$checksum_file" | awk '{print $1}')
    
    # NEGATIVE: Checksum not found in file
    if [[ -z "$expected_checksum" ]]; then
        log_warn "Checksum not found for $binary_name, skipping verification"
        return 0
    fi
    
    # Calculate actual checksum
    local actual_checksum
    if command -v sha256sum &> /dev/null; then
        actual_checksum=$(sha256sum "$binary_path" | awk '{print $1}')
    elif command -v shasum &> /dev/null; then
        actual_checksum=$(shasum -a 256 "$binary_path" | awk '{print $1}')
    else
        log_warn "No checksum tool available, skipping verification"
        return 0
    fi
    
    # NEGATIVE: Checksum mismatch
    if [[ "$expected_checksum" != "$actual_checksum" ]]; then
        log_error "Checksum verification failed!"
        log_error "Expected: $expected_checksum"
        log_error "Actual:   $actual_checksum"
        return 1
    fi
    
    # HAPPY PATH: Checksum verified
    log_info "Checksum verified"
    return 0
}

# Create backup
create_backup() {
    local version="$1"
    local backup_name="gateway-${version}-$(date +%Y%m%d-%H%M%S)"
    local backup_path="${BACKUP_DIR}/${backup_name}"
    
    log_info "Creating backup: $backup_name"
    
    # Copy binary
    cp "$BINARY_PATH" "$backup_path"
    
    # Store version info
    echo "$version" > "${backup_path}.version"
    
    # Cleanup old backups
    cleanup_old_backups
    
    # Return backup path
    echo "$backup_path"
}

# Cleanup old backups (keep only MAX_BACKUPS)
cleanup_old_backups() {
    local backup_count
    backup_count=$(find "$BACKUP_DIR" -name 'gateway-*' -type f | wc -l)
    
    # NEGATIVE: Too many backups
    if [[ $backup_count -gt $MAX_BACKUPS ]]; then
        log_info "Cleaning up old backups (keeping $MAX_BACKUPS)..."
        find "$BACKUP_DIR" -name 'gateway-*' -type f -printf '%T@ %p\n' | \
            sort -n | head -n -$MAX_BACKUPS | cut -d' ' -f2- | xargs rm -f
    fi
}

# Stop service
stop_service() {
    log_info "Stopping service..."
    
    # NEGATIVE: Service not running
    if ! systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        log_info "Service is not running"
        return 0
    fi
    
    if ! systemctl stop "$SERVICE_NAME"; then
        log_error "Failed to stop service"
        return 1
    fi
    
    log_info "Service stopped"
}

# Start service
start_service() {
    log_info "Starting service..."
    
    if ! systemctl start "$SERVICE_NAME"; then
        log_error "Failed to start service"
        return 1
    fi
    
    log_info "Service started"
}

# Wait for service to be healthy
wait_for_health() {
    log_info "Waiting for service health check (max ${HEALTH_TIMEOUT}s)..."
    
    local elapsed=0
    while [[ $elapsed -lt $HEALTH_TIMEOUT ]]; do
        # Check health endpoint
        if curl -fsSL "$HEALTH_URL" &>/dev/null; then
            log_info "Service is healthy"
            return 0
        fi
        
        sleep 1
        elapsed=$((elapsed + 1))
        
        # Show progress every 5 seconds
        if [[ $((elapsed % 5)) -eq 0 ]]; then
            log_debug "Waiting... (${elapsed}s)"
        fi
    done
    
    # NEGATIVE: Health check timeout
    log_error "Health check timed out after ${HEALTH_TIMEOUT}s"
    return 1
}

# Perform rollback
rollback() {
    local backup_path="$1"
    
    log_warn "Performing rollback..."
    
    # Stop service
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    
    # Restore from backup
    if [[ -f "$backup_path" ]]; then
        cp "$backup_path" "$BINARY_PATH"
        log_info "Restored binary from backup"
    fi
    
    # Start service
    if start_service; then
        log_info "Rollback successful, service restarted"
    else
        log_error "Rollback failed, service could not be started"
        exit 1
    fi
}

# Perform update
perform_update() {
    local version="$1"
    local temp_dir="/tmp/gateway-update-$$"
    local temp_binary="${temp_dir}/gateway"
    local temp_checksum="${temp_dir}/checksums.txt"
    
    # Create temp directory
    mkdir -p "$temp_dir"
    trap "rm -rf '$temp_dir'" EXIT
    
    # Download new binary
    download_binary "$version" "$temp_binary" || return 1
    
    # Download and verify checksum
    if download_checksum "$version" "$temp_checksum"; then
        if ! verify_checksum "$temp_binary" "$temp_checksum"; then
            log_error "Binary verification failed, aborting update"
            return 1
        fi
    fi
    
    # Get current version for backup
    local current_version
    current_version=$(get_current_version)
    
    # Create backup
    local backup_path
    backup_path=$(create_backup "$current_version")
    
    # Stop service
    if ! stop_service; then
        return 1
    fi
    
    # Install new binary
    log_info "Installing new binary..."
    cp "$temp_binary" "$BINARY_PATH"
    chown gateway:gateway "$BINARY_PATH"
    chmod 755 "$BINARY_PATH"
    
    # Save new version info
    echo "$version" > "${INSTALL_DIR}/.version"
    
    # Start service
    if ! start_service; then
        log_error "Failed to start service with new binary"
        rollback "$backup_path"
        return 1
    fi
    
    # Health check
    if ! wait_for_health; then
        log_error "Health check failed after update"
        rollback "$backup_path"
        return 1
    fi
    
    # HAPPY PATH: Update successful
    log_info "Update completed successfully!"
    log_info "Backup saved: $backup_path"
    
    # Cleanup temp directory
    rm -rf "$temp_dir"
    
    return 0
}

# List available backups
list_backups() {
    log_info "Available backups:"
    
    # NEGATIVE: No backups
    if [[ ! -d "$BACKUP_DIR" ]] || [[ -z "$(ls -A "$BACKUP_DIR" 2>/dev/null)" ]]; then
        log_info "No backups found"
        return 0
    fi
    
    # List backups with details
    find "$BACKUP_DIR" -name 'gateway-*' -type f | while read -r backup; do
        local name
        name=$(basename "$backup")
        local size
        size=$(du -h "$backup" | cut -f1)
        local date
        date=$(stat -c %y "$backup" 2>/dev/null || stat -f %Sm "$backup" 2>/dev/null)
        echo "  $name ($size) - $date"
    done
}

# Cleanup function for main
cleanup_main() {
    local exit_code=$?
    if [[ $exit_code -ne 0 ]] && [[ $exit_code -ne 2 ]]; then
        log_error "Update failed with exit code $exit_code"
    fi
}

trap cleanup_main EXIT

# Main update flow
main() {
    log_info "AMG RFID Gateway Update Script"
    log_info "================================"
    
    # Parse arguments
    parse_args "$@"
    
    # NEGATIVE: Not root
    check_root
    
    # NEGATIVE: Not installed
    check_installed
    
    # Check for updates
    if ! check_update; then
        local exit_code=$?
        if [[ $exit_code -eq 2 ]]; then
            # Already up to date
            exit 0
        fi
        exit 1
    fi
    
    # NEGATIVE: Check-only mode
    if [[ "$CHECK_ONLY" == true ]]; then
        log_info "Check complete (not installing)"
        exit 0
    fi
    
    # Confirm update
    local target_version_display
    if [[ -n "$TARGET_VERSION" ]]; then
        target_version_display="$TARGET_VERSION"
    else
        target_version_display=$(get_latest_version)
    fi
    
    echo ""
    read -r -p "Proceed with update to $target_version_display? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        log_info "Update cancelled"
        exit 0
    fi
    
    # Perform update
    if perform_update "$target_version_display"; then
        echo ""
        log_info "Gateway updated successfully!"
        log_info "Current version: $(get_current_version)"
        echo ""
        log_info "Backups available:"
        list_backups
    else
        log_error "Update failed"
        exit 1
    fi
}

# Run main
main "$@"
