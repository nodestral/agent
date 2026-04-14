#!/bin/bash
set -euo pipefail

# Nodestral Agent Install Script
# Usage: curl -sSL https://nodestral.io/install | sh

NODESTAL_VERSION="${NODESTAL_VERSION:-latest}"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/nodestral"
SERVICE_NAME="nodestral-agent"
BINARY_NAME="nodestral-agent"
DOWNLOAD_URL="${NODESTAL_DOWNLOAD_URL:-https://nodestral.io/releases}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    error "This script must be run as root. Try: sudo curl -sSL https://nodestral.io/install | sh"
fi

# Detect architecture
detect_arch() {
    local arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac
}

# Detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        echo "${ID}"
    elif command -v lsb_release &>/dev/null; then
        lsb_release -is | tr '[:upper:]' '[:lower:]'
    else
        echo "linux"
    fi
}

ARCH=$(detect_arch)
OS=$(detect_os)
BINARY_URL="${DOWNLOAD_URL}/${NODESTAL_VERSION}/nodestral-agent-${OS}-${ARCH}"

info "Installing Nodestral Agent ${NODESTAL_VERSION} for ${OS}/${ARCH}..."

# Download binary
info "Downloading from ${BINARY_URL}..."
if ! curl -fsSL -o "${INSTALL_DIR}/${BINARY_NAME}" "${BINARY_URL}"; then
    error "Failed to download agent binary"
fi

# Make executable
chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
info "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"

# Create config directory
mkdir -p "${CONFIG_DIR}"

# Create config if not exists
if [ ! -f "${CONFIG_DIR}/agent.yaml" ]; then
    cat > "${CONFIG_DIR}/agent.yaml" <<EOF
# Nodestral Agent Configuration
api_url: https://api.nodestral.io
node_id: ""
auth_token: ""
heartbeat_interval: 30s
discovery_interval: 300s
EOF
    info "Config created at ${CONFIG_DIR}/agent.yaml"
fi

# Create systemd service
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=Nodestral Agent - VPS Fleet Management
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
Restart=always
RestartSec=5
LimitNOFILE=65536
Environment=NODESTAL_CONFIG=${CONFIG_DIR}/agent.yaml

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and start service
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl start "${SERVICE_NAME}"

info "Nodestral Agent is now running!"
info ""
info "Next steps:"
info "  1. View status:  systemctl status ${SERVICE_NAME}"
info "  2. View logs:    journalctl -u ${SERVICE_NAME} -f"
info "  3. Edit config:  nano ${CONFIG_DIR}/agent.yaml"
info "  4. Open dashboard: https://nodestral.io/dashboard"
