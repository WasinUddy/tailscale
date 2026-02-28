#!/bin/bash
#
# Tailscale Custom Build Installer
# 
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/YOUR_USERNAME/tailscale/main/scripts/install-custom.sh | sudo bash
#
# Or with options:
#   curl -fsSL https://raw.githubusercontent.com/YOUR_USERNAME/tailscale/main/scripts/install-custom.sh | sudo bash -s -- --version v1.2.3 --authkey tskey-auth-xxx
#
# Environment variables:
#   TAILSCALE_AUTHKEY - Tailscale auth key for automatic login
#   GITHUB_REPO - GitHub repository (default: auto-detect or prompt)
#   VERSION - Specific version to install (default: latest)
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
GITHUB_REPO="${GITHUB_REPO:-}"
VERSION="${VERSION:-latest}"
TAILSCALE_AUTHKEY="${TAILSCALE_AUTHKEY:-}"
INSTALL_DIR=""
BINARY_ARCHIVE=""
CLEANUP_ON_ERROR=1

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --authkey)
      TAILSCALE_AUTHKEY="$2"
      shift 2
      ;;
    --repo)
      GITHUB_REPO="$2"
      shift 2
      ;;
    --help)
      cat << EOF
Tailscale Custom Build Installer

Usage:
  $0 [options]

Options:
  --version VERSION     Install specific version (default: latest)
  --authkey KEY        Tailscale auth key for automatic login
  --repo OWNER/REPO    GitHub repository (e.g., username/tailscale)
  --help               Show this help message

Environment Variables:
  TAILSCALE_AUTHKEY    Tailscale auth key
  GITHUB_REPO          GitHub repository
  VERSION              Version to install

Examples:
  # Install latest version
  sudo $0

  # Install with auth key
  sudo $0 --authkey tskey-auth-xxx

  # Install specific version
  sudo $0 --version custom-build-1.58.2-123
EOF
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      exit 1
      ;;
  esac
done

# Utility functions
log_info() {
  echo -e "${BLUE}==>${NC} $1"
}

log_success() {
  echo -e "${GREEN}✓${NC} $1"
}

log_error() {
  echo -e "${RED}✗${NC} $1"
}

log_warn() {
  echo -e "${YELLOW}!${NC} $1"
}

cleanup() {
  if [ -n "$BINARY_ARCHIVE" ] && [ -f "$BINARY_ARCHIVE" ]; then
    rm -f "$BINARY_ARCHIVE"
  fi
  if [ -d "/tmp/tailscale-custom-install" ]; then
    rm -rf /tmp/tailscale-custom-install
  fi
}

error_exit() {
  log_error "$1"
  if [ "$CLEANUP_ON_ERROR" = "1" ]; then
    cleanup
  fi
  exit 1
}

# Trap errors
trap 'error_exit "Installation failed"' ERR

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  error_exit "Please run as root or with sudo"
fi

# Detect OS and architecture
detect_platform() {
  local os=""
  local arch=""
  
  case "$(uname -s)" in
    Linux*)
      os="linux"
      ;;
    Darwin*)
      os="darwin"
      ;;
    MINGW*|MSYS*|CYGWIN*)
      os="windows"
      ;;
    *)
      error_exit "Unsupported OS: $(uname -s)"
      ;;
  esac
  
  case "$(uname -m)" in
    x86_64|amd64)
      arch="amd64"
      ;;
    aarch64|arm64)
      arch="arm64"
      ;;
    armv7l|armv6l)
      arch="arm"
      ;;
    *)
      error_exit "Unsupported architecture: $(uname -m)"
      ;;
  esac
  
  echo "${os}-${arch}"
}

# Set install directory based on OS
set_install_dir() {
  case "$(uname -s)" in
    Darwin*)
      INSTALL_DIR="/usr/local/bin"
      ;;
    *)
      INSTALL_DIR="/usr/bin"
      ;;
  esac
}

# Prompt for GitHub repo if not set
get_github_repo() {
  if [ -z "$GITHUB_REPO" ]; then
    log_warn "GitHub repository not set"
    echo -n "Enter GitHub repository (e.g., username/tailscale): "
    read GITHUB_REPO
    
    if [ -z "$GITHUB_REPO" ]; then
      error_exit "GitHub repository is required"
    fi
  fi
  
  # Validate format
  if [[ ! "$GITHUB_REPO" =~ ^[a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+$ ]]; then
    error_exit "Invalid repository format. Use: owner/repo"
  fi
}

# Get the latest release tag
get_latest_release() {
  local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases"
  
  log_info "Fetching latest release..."
  
  # Get the latest custom build release
  local latest_tag=$(curl -fsSL "$api_url" | grep -o '"tag_name": "custom-build-[^"]*"' | head -1 | cut -d'"' -f4)
  
  if [ -z "$latest_tag" ]; then
    error_exit "No custom build releases found in ${GITHUB_REPO}"
  fi
  
  echo "$latest_tag"
}

# Download and verify binary
download_binary() {
  local platform=$1
  local version=$2
  local extension=""
  
  if [[ "$platform" == "windows-"* ]]; then
    extension="zip"
  else
    extension="tar.gz"
  fi
  
  local filename="tailscale-custom-${platform}.${extension}"
  local download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${filename}"
  local checksum_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${filename}.sha256"
  
  log_info "Downloading ${filename}..."
  
  BINARY_ARCHIVE="/tmp/${filename}"
  
  if ! curl -fsSL -o "$BINARY_ARCHIVE" "$download_url"; then
    error_exit "Failed to download ${filename} from ${download_url}"
  fi
  
  log_success "Downloaded ${filename}"
  
  # Download and verify checksum
  log_info "Verifying checksum..."
  
  local checksum_file="/tmp/${filename}.sha256"
  if curl -fsSL -o "$checksum_file" "$checksum_url"; then
    cd /tmp
    if shasum -a 256 -c "$checksum_file" 2>/dev/null; then
      log_success "Checksum verified"
    else
      error_exit "Checksum verification failed"
    fi
    rm -f "$checksum_file"
  else
    log_warn "Checksum file not found, skipping verification"
  fi
}

# Extract and install binaries
install_binaries() {
  local platform=$1
  
  log_info "Extracting binaries..."
  
  local extract_dir="/tmp/tailscale-custom-install"
  mkdir -p "$extract_dir"
  
  if [[ "$platform" == "windows-"* ]]; then
    unzip -q -o "$BINARY_ARCHIVE" -d "$extract_dir"
  else
    tar -xzf "$BINARY_ARCHIVE" -C "$extract_dir"
  fi
  
  log_info "Installing to ${INSTALL_DIR}..."
  
  # Copy binaries
  if [ -f "$extract_dir/tailscaled" ]; then
    cp "$extract_dir/tailscaled" "$INSTALL_DIR/tailscaled"
    chmod +x "$INSTALL_DIR/tailscaled"
    log_success "Installed tailscaled"
  fi
  
  if [ -f "$extract_dir/tailscale" ]; then
    cp "$extract_dir/tailscale" "$INSTALL_DIR/tailscale"
    chmod +x "$INSTALL_DIR/tailscale"
    log_success "Installed tailscale"
  fi
  
  # Cleanup
  rm -rf "$extract_dir"
  rm -f "$BINARY_ARCHIVE"
}

# Setup systemd service (Linux only)
setup_systemd_service() {
  if [ "$(uname -s)" != "Linux" ]; then
    return
  fi
  
  if ! command -v systemctl &> /dev/null; then
    log_warn "systemd not found, skipping service setup"
    return
  fi
  
  log_info "Setting up systemd service..."
  
  cat > /etc/systemd/system/tailscaled.service << 'EOF'
[Unit]
Description=Tailscale Custom Build
Documentation=https://tailscale.com/kb/
After=network-pre.target
Wants=network-pre.target

[Service]
Type=notify
ExecStart=/usr/bin/tailscaled --state=/var/lib/tailscale/tailscaled.state
Restart=on-failure
RuntimeDirectory=tailscale
RuntimeDirectoryMode=0755
StateDirectory=tailscale
StateDirectoryMode=0750
CacheDirectory=tailscale
CacheDirectoryMode=0750

[Install]
WantedBy=multi-user.target
EOF
  
  systemctl daemon-reload
  systemctl enable tailscaled.service
  log_success "Systemd service created and enabled"
}

# Start tailscaled
start_tailscaled() {
  log_info "Starting tailscaled..."
  
  if [ "$(uname -s)" = "Linux" ] && command -v systemctl &> /dev/null; then
    systemctl start tailscaled.service
    log_success "Started tailscaled service"
  else
    log_warn "Please start tailscaled manually:"
    echo "  sudo tailscaled &"
  fi
}

# Login to Tailscale
login_tailscale() {
  if [ -z "$TAILSCALE_AUTHKEY" ]; then
    log_info "To login to Tailscale, run:"
    echo "  tailscale up"
    return
  fi
  
  log_info "Logging in to Tailscale with auth key..."
  
  # Wait a moment for tailscaled to start
  sleep 2
  
  if tailscale up --authkey="$TAILSCALE_AUTHKEY"; then
    log_success "Successfully logged in to Tailscale"
    
    # Get Tailscale IP
    sleep 2
    local ts_ip=$(tailscale ip -4 2>/dev/null || echo "")
    if [ -n "$ts_ip" ]; then
      echo ""
      log_success "Tailscale IP: ${ts_ip}"
      echo ""
      echo -e "${GREEN}Web server available at:${NC}"
      echo "  http://${ts_ip}:6942/"
      echo "  http://${ts_ip}:6942/metrics"
    fi
  else
    log_error "Failed to login with auth key"
    log_info "Try manually: tailscale up --authkey=YOUR_KEY"
  fi
}

# Main installation flow
main() {
  echo ""
  echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║  Tailscale Custom Build Installer     ║${NC}"
  echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
  echo ""
  
  # Detect platform
  local platform=$(detect_platform)
  log_info "Detected platform: ${platform}"
  
  # Set install directory
  set_install_dir
  log_info "Install directory: ${INSTALL_DIR}"
  
  # Get GitHub repository
  get_github_repo
  log_info "Repository: ${GITHUB_REPO}"
  
  # Get version
  if [ "$VERSION" = "latest" ]; then
    VERSION=$(get_latest_release)
    log_info "Latest version: ${VERSION}"
  else
    log_info "Installing version: ${VERSION}"
  fi
  
  # Download binary
  download_binary "$platform" "$VERSION"
  
  # Install binaries
  install_binaries "$platform"
  
  # Setup service (Linux only)
  setup_systemd_service
  
  # Start tailscaled
  start_tailscaled
  
  # Login if auth key provided
  login_tailscale
  
  echo ""
  echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║  Installation Complete!                ║${NC}"
  echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
  echo ""
  
  if [ -z "$TAILSCALE_AUTHKEY" ]; then
    echo "Next steps:"
    echo "  1. Login: tailscale up"
    echo "  2. Access web server: http://[tailscale-ip]:6942/"
  fi
  
  echo ""
}

# Run main
main
