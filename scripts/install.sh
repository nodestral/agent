#!/bin/bash
# Nodestral Agent Install Script
# Usage: curl -sSL https://nodestral.web.id/install.sh | sh -s -- <INSTALL_TOKEN>
#
# Flags:
#   --system      Install system-wide with dedicated user (requires sudo)
#   --no-service  Skip systemd service setup, just install binary
#
# Get your install token from: https://nodestral.web.id/dashboard

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[-]${NC} $1" >&2; exit 1; }
step()  { echo -e "${CYAN}[→]${NC} $1"; }

# Defaults
INSTALL_TOKEN=""
SYSTEM_MODE=false
SETUP_SERVICE=true
API_URL="https://api.nodestral.web.id"

# Parse args
for arg in "$@"; do
  case "$arg" in
    --system)     SYSTEM_MODE=true ;;
    --no-service) SETUP_SERVICE=false ;;
    *)
      if [ -z "$INSTALL_TOKEN" ]; then
        INSTALL_TOKEN="$arg"
      fi
      ;;
  esac
done

echo ""
echo -e "${CYAN}  ╔═══════════════════════════╗${NC}"
echo -e "${CYAN}  ║   Nodestral Agent Setup   ║${NC}"
echo -e "${CYAN}  ╚═══════════════════════════╝${NC}"
echo ""

if [ -z "$INSTALL_TOKEN" ]; then
  warn "No install token provided."
  echo "  Usage: curl -sSL https://nodestral.web.id/install.sh | sh -s -- YOUR_TOKEN"
  echo ""
  echo "  Options:"
  echo "    --system       System-wide install with dedicated user (needs sudo)"
  echo "    --no-service   Install only, no systemd service"
  echo ""
  echo "  Get your token: https://nodestral.web.id/dashboard"
  exit 1
fi

# ── Detect OS & Arch ──────────────────────────────────────────────────────────

OS="$(uname -s)"
case "$OS" in
  Linux*)     OS="linux" ;;
  Darwin*)    OS="darwin" ;;
  *)          error "Unsupported OS: $OS. Only Linux and macOS supported." ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)             error "Unsupported architecture: $ARCH" ;;
esac

BINARY="nodestral-agent-${OS}_${ARCH}"

# ── Determine install paths ───────────────────────────────────────────────────

if [ "$SYSTEM_MODE" = true ]; then
  # System-wide install — requires sudo/root
  if [ "$(id -u)" -ne 0 ]; then
    if ! command -v sudo >/dev/null 2>&1; then
      error "System install requires root or sudo."
    fi
    SUDO="sudo"
  else
    SUDO=""
  fi
  INSTALL_DIR="/usr/local/bin"
  CONFIG_DIR="/etc/nodestral"
  SERVICE_MODE="system"
  info "Mode: system-wide install (requires sudo)"
else
  # User-space install (default) — no root needed
  SUDO=""
  INSTALL_DIR="${HOME}/.local/bin"
  CONFIG_DIR="${HOME}/.config/nodestral"
  SERVICE_MODE="user"
  info "Mode: user-space install (no root needed)"
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"

info "Detected: ${OS}/${ARCH} | Install: ${INSTALL_DIR}"

# ── Check dependencies ────────────────────────────────────────────────────────

command -v curl >/dev/null 2>&1 || error "curl is required. Install it first."

# ── Check for existing installation ───────────────────────────────────────────

if [ -f "${INSTALL_DIR}/nodestral-agent" ]; then
  warn "Existing installation found at ${INSTALL_DIR}/nodestral-agent — updating."
fi

# ── Download binary ───────────────────────────────────────────────────────────

URL1="https://nx.nodestral.web.id/bin/${BINARY}"
URL2="https://github.com/nodestral/agent/releases/latest/download/${BINARY}"

DOWNLOADED=false
TMPFILE="$(mktemp)"

for url in "$URL1" "$URL2"; do
  step "Downloading from ${url}..."
  if curl -fsSL --connect-timeout 5 --max-time 120 "${url}" -o "${TMPFILE}"; then
    DOWNLOADED=true
    info "Downloaded successfully"
    break
  fi
  warn "Mirror unavailable, trying next..."
done

if [ "$DOWNLOADED" = false ]; then
  error "Failed to download from all mirrors. Check your internet connection."
fi

# ── Install binary ────────────────────────────────────────────────────────────

step "Installing binary..."
$SUDO mv "${TMPFILE}" "${INSTALL_DIR}/nodestral-agent"
$SUDO chmod +x "${INSTALL_DIR}/nodestral-agent"
info "Binary installed to ${INSTALL_DIR}/nodestral-agent"

# Ensure INSTALL_DIR is in PATH
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
  warn "${INSTALL_DIR} is not in your PATH."
  SHELL_RC="${HOME}/.bashrc"
  [ -f "${HOME}/.zshrc" ] && [ -n "$ZSH_VERSION" ] && SHELL_RC="${HOME}/.zshrc"
  echo "" >> "$SHELL_RC"
  echo "# Added by Nodestral Agent installer" >> "$SHELL_RC"
  echo "export PATH=\"${INSTALL_DIR}:\$PATH\"" >> "$SHELL_RC"
  info "Added ${INSTALL_DIR} to PATH in ${SHELL_RC}"
  info "Run: source ${SHELL_RC}  (or open a new terminal)"
  export PATH="${INSTALL_DIR}:${PATH}"
  ;;
esac

# ── Register agent ────────────────────────────────────────────────────────────

step "Registering agent with Nodestral..."

HOSTNAME_VAL="$(hostname 2>/dev/null || echo 'unknown')"
KERNEL_VAL="$(uname -r)"

if [ -f /etc/os-release ]; then
  OS_NAME="$(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"' || uname -s)"
else
  OS_NAME="$(uname -s)"
fi

CPU_CORES="$(nproc 2>/dev/null || echo 1)"

RAM_MB="0"
if [ -f /proc/meminfo ]; then
  RAM_MB="$(awk '/MemTotal/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)"
fi

DISK_GB="0"
DISK_GB="$(df -h / 2>/dev/null | awk 'NR==2 {print $2}' | sed 's/G//' 2>/dev/null || echo 0)"

# ── Detect cloud provider ─────────────────────────────────────────────────────

step "Detecting cloud provider..."

PROVIDER_NAME=""
PROVIDER_REGION=""
PROVIDER_INSTANCE_TYPE=""

# Tencent Cloud
if curl -sf --connect-timeout 2 --max-time 3 \
  -H "Metadata-Flavor: TencentCloud" \
  "http://metadata.tencentyun.com/latest/meta-data/instance-id" >/dev/null 2>&1; then
  PROVIDER_NAME="tencent"
  PROVIDER_REGION="$(curl -sf --connect-timeout 2 --max-time 3 \
    -H "Metadata-Flavor: TencentCloud" \
    "http://metadata.tencentyun.com/latest/meta-data/placement/zone" 2>/dev/null || echo '')"
  PROVIDER_INSTANCE_TYPE="$(curl -sf --connect-timeout 2 --max-time 3 \
    -H "Metadata-Flavor: TencentCloud" \
    "http://metadata.tencentyun.com/latest/meta-data/instance/instance-type" 2>/dev/null || echo '')"
  info "Detected: Tencent Cloud (${PROVIDER_REGION})"
# AWS
elif curl -sf --connect-timeout 2 --max-time 3 \
  "http://169.254.169.254/latest/meta-data/instance-id" >/dev/null 2>&1; then
  PROVIDER_NAME="aws"
  PROVIDER_REGION="$(curl -sf --connect-timeout 2 --max-time 3 \
    "http://169.254.169.254/latest/meta-data/placement/availability-zone" 2>/dev/null || echo '')"
  PROVIDER_INSTANCE_TYPE="$(curl -sf --connect-timeout 2 --max-time 3 \
    "http://169.254.169.254/latest/meta-data/instance-type" 2>/dev/null || echo '')"
  info "Detected: AWS (${PROVIDER_REGION})"
# GCP
elif curl -sf --connect-timeout 2 --max-time 3 \
  -H "Metadata-Flavor: Google" \
  "http://metadata.google.internal/computeMetadata/v1/instance/zone" >/dev/null 2>&1; then
  PROVIDER_NAME="gcp"
  ZONE_RAW="$(curl -sf --connect-timeout 2 --max-time 3 \
    -H "Metadata-Flavor: Google" \
    "http://metadata.google.internal/computeMetadata/v1/instance/zone" 2>/dev/null || echo '')"
  PROVIDER_REGION="$(echo "$ZONE_RAW" | rev | cut -d/ -f1 | rev)"
  info "Detected: GCP (${PROVIDER_REGION})"
# Azure
elif curl -sf --connect-timeout 2 --max-time 3 \
  -H "Metadata: true" \
  "http://169.254.169.254/metadata/instance/compute/location?api-version=2021-02-01" >/dev/null 2>&1; then
  PROVIDER_NAME="azure"
  PROVIDER_REGION="$(curl -sf --connect-timeout 2 --max-time 3 \
    -H "Metadata: true" \
    "http://169.254.169.254/metadata/instance/compute/location?api-version=2021-02-01" 2>/dev/null || echo '')"
  info "Detected: Azure (${PROVIDER_REGION})"
# Hetzner
elif curl -sf --connect-timeout 2 --max-time 3 \
  "http://169.254.169.254/hetzner/v1/metadata" >/dev/null 2>&1; then
  PROVIDER_NAME="hetzner"
  HZ="$(curl -sf --connect-timeout 2 --max-time 3 \
    "http://169.254.169.254/hetzner/v1/metadata" 2>/dev/null | grep -o '"'"'"availability-zone"'"'"":"'"'"'"[^"'"'"']*'"'"'"'"'"'"' | cut -d'"'"'"' -f4 || echo '')"
  PROVIDER_REGION="$(echo "$HZ" | sed 's/-dc[0-9]*$//')"
  info "Detected: Hetzner (${PROVIDER_REGION})"
# DigitalOcean
elif curl -sf --connect-timeout 2 --max-time 3 \
  "http://169.254.169.254/metadata/v1.json" >/dev/null 2>&1; then
  PROVIDER_NAME="digitalocean"
  PROVIDER_REGION="$(curl -sf --connect-timeout 2 --max-time 3 \
    "http://169.254.169.254/metadata/v1.json" 2>/dev/null | grep -o '"'"'"region"'"'"":"'"'"'"[^"'"'"']*'"'"'"'"'"'"' | cut -d'"'"'"' -f4 || echo '')"
  info "Detected: DigitalOcean (${PROVIDER_REGION})"
else
  info "No cloud provider detected (bare metal or unknown VPS)"
fi

PUBLIC_IP="$(curl -sf --connect-timeout 3 ifconfig.me 2>/dev/null || echo '')"

REGISTER_RESPONSE=$(curl -sf --connect-timeout 10 --max-time 30 \
  -X POST "${API_URL}/agent/register/token" \
  -H "Content-Type: application/json" \
  -H "X-Install-Token: ${INSTALL_TOKEN}" \
  -d "{
  \"system\": {
    \"hostname\": \"${HOSTNAME_VAL}\",
    \"os\": \"${OS_NAME}\",
    \"kernel\": \"${KERNEL_VAL}\",
    \"arch\": \"$(uname -m)\",
    \"cpu_cores\": ${CPU_CORES},
    \"ram_mb\": ${RAM_MB},
    \"disk_gb\": ${DISK_GB},
    \"public_ip\": \"${PUBLIC_IP}\"
  },
  \"provider\": { \"name\": \"${PROVIDER_NAME}\", \"region\": \"${PROVIDER_REGION}\", \"instance_type\": \"${PROVIDER_INSTANCE_TYPE}\" }
}" 2>/dev/null) || error "Registration failed. Check your install token and network."

# Parse response
NODE_ID=""
AUTH_TOKEN=""

if command -v python3 >/dev/null 2>&1; then
  NODE_ID="$(echo "$REGISTER_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('node_id',''))" 2>/dev/null || true)"
  AUTH_TOKEN="$(echo "$REGISTER_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('auth_token',''))" 2>/dev/null || true)"
fi

if [ -z "$NODE_ID" ]; then
  NODE_ID="$(echo "$REGISTER_RESPONSE" | grep -o '"node_id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)"
fi
if [ -z "$AUTH_TOKEN" ]; then
  AUTH_TOKEN="$(echo "$REGISTER_RESPONSE" | grep -o '"auth_token":"[^"]*"' | head -1 | cut -d'"' -f4 || true)"
fi

if [ -z "$NODE_ID" ] || [ -z "$AUTH_TOKEN" ]; then
  error "Registration failed: unexpected response. ${REGISTER_RESPONSE}"
fi

NODE_ID_SHORT="$(echo "$NODE_ID" | cut -c1-8)"
info "Registered: node ${NODE_ID_SHORT}..."

# ── Write config ──────────────────────────────────────────────────────────────

step "Writing config..."
$SUDO tee "${CONFIG_DIR}/agent.yaml" > /dev/null << EOF
api_url: ${API_URL}
node_id: ${NODE_ID}
auth_token: ${AUTH_TOKEN}
heartbeat_interval: 30s
discovery_interval: 5m
EOF
$SUDO chmod 600 "${CONFIG_DIR}/agent.yaml"
info "Config: ${CONFIG_DIR}/agent.yaml"

# ── Systemd service ───────────────────────────────────────────────────────────

if [ "$SETUP_SERVICE" = false ]; then
  # User chose --no-service, skip service setup
  SERVICE_INSTALLED=false
else
  SERVICE_INSTALLED=false

  if [ "$SERVICE_MODE" = "system" ]; then
    # ── System-wide service (needs sudo) ──
    if command -v systemctl >/dev/null 2>&1; then
      step "Setting up system service (requires sudo)..."

      # Create dedicated least-privilege user
      if ! id -u nodestral &>/dev/null; then
        $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin nodestral
        info "Created nodestral system user"
      fi
      $SUDO chown -R nodestral:nodestral "$CONFIG_DIR"
      $SUDO chmod 700 "$CONFIG_DIR"

      $SUDO tee /etc/systemd/system/nodestral-agent.service > /dev/null << EOF
[Unit]
Description=Nodestral Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=nodestral
Group=nodestral
ExecStart=${INSTALL_DIR}/nodestral-agent
Restart=always
RestartSec=5
LimitNOFILE=65536
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=${CONFIG_DIR}

[Install]
WantedBy=multi-user.target
EOF

      $SUDO systemctl daemon-reload
      $SUDO systemctl enable nodestral-agent
      $SUDO systemctl restart nodestral-agent

      sleep 2
      if $SUDO systemctl is-active --quiet nodestral-agent; then
        info "System service running as nodestral user ✅"
        SERVICE_INSTALLED=true
      else
        warn "Service may still be starting. Check: sudo journalctl -u nodestral-agent -n 20"
        SERVICE_INSTALLED=true
      fi
    fi

  else
    # ── User-level service (no root) ──
    USER_SYSTEMD_AVAILABLE=false
    if [ "$OS" = "linux" ] && command -v systemctl >/dev/null 2>&1; then
      # Try to set up user-level systemd
      systemctl --user daemon-reload 2>/dev/null && USER_SYSTEMD_AVAILABLE=true
    fi

    if [ "$USER_SYSTEMD_AVAILABLE" = true ]; then
      step "Setting up user-level systemd service..."
      SERVICE_DIR="${HOME}/.config/systemd/user"
      mkdir -p "$SERVICE_DIR"

      cat > "${SERVICE_DIR}/nodestral-agent.service" << EOF
[Unit]
Description=Nodestral Agent
After=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/nodestral-agent
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=default.target
EOF

      systemctl --user daemon-reload
      systemctl --user enable nodestral-agent
      systemctl --user restart nodestral-agent

      # Enable linger so service survives logout
      if command -v loginctl >/dev/null 2>&1; then
        loginctl enable-linger "$(whoami)" 2>/dev/null || warn "Could not enable linger. Service may stop when you log out."
      fi

      sleep 2
      if systemctl --user is-active --quiet nodestral-agent; then
        info "User service running ✅"
        SERVICE_INSTALLED=true
      else
        warn "Service may still be starting. Check: systemctl --user status nodestral-agent"
        SERVICE_INSTALLED=true
      fi
    fi

    # ── No systemd available — offer guidance ──
    if [ "$SERVICE_INSTALLED" = false ]; then
      echo ""
      echo -e "${YELLOW}  ⚠ systemd user service is not available on this system.${NC}"
      echo ""
      echo "  The agent binary is installed. Choose how to run it:"
      echo ""
      echo -e "  ${CYAN}Option 1: System service (requires sudo)${NC}"
      echo "    Re-run with: curl -sSL https://nodestral.web.id/install.sh | sudo sh -s -- ${INSTALL_TOKEN} --system"
      echo ""
      echo -e "  ${CYAN}Option 2: Run manually in background${NC}"
      echo "    nohup ${INSTALL_DIR}/nodestral-agent > /dev/null 2>&1 &"
      echo ""
      echo -e "  ${CYAN}Option 3: Add to your shell profile (runs on login)${NC}"
      echo "    echo '${INSTALL_DIR}/nodestral-agent &' >> ~/.bashrc"
      echo ""
      step "Starting agent in background for now..."
      nohup "${INSTALL_DIR}/nodestral-agent" > /dev/null 2>&1 &
      info "Agent started (PID $!). It will NOT survive a reboot."
      echo ""
      echo -e "  ${CYAN}Tip:${NC} For auto-start on boot, re-run with --system (needs sudo)"
    fi
  fi
fi

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo -e "${GREEN}  ✅ Nodestral agent installed successfully!${NC}"
echo ""
echo "  Node ID:    ${NODE_ID}"
echo "  Binary:     ${INSTALL_DIR}/nodestral-agent"
echo "  Config:     ${CONFIG_DIR}/agent.yaml"
echo "  Dashboard:  https://nodestral.web.id/dashboard"

if [ "$SERVICE_INSTALLED" = true ]; then
  if [ "$SERVICE_MODE" = "user" ]; then
    echo "  Service:    user-level systemd (auto-start on login)"
    echo "  Logs:       systemctl --user journalctl -u nodestral-agent -f"
    echo ""
    echo -e "  ${CYAN}Tip:${NC} For system-wide service (auto-start on boot), re-run with --system"
  else
    echo "  Service:    system-level systemd (auto-start on boot)"
    echo "  User:       nodestral (least-privilege)"
    echo "  Logs:       sudo journalctl -u nodestral-agent -f"
  fi
fi
echo ""
